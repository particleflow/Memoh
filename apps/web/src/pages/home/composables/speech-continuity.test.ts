// @vitest-environment jsdom
import { createApp, h, nextTick, ref } from 'vue'
import { afterEach, expect, it, vi } from 'vitest'
import type { ChatMessage } from '@/store/chat/types'
import { useChatReadAloud } from './useChatReadAloud'
import { ChatReadAloud, LiveSpeechReplies, type SpeechState } from './chat-read-aloud'

const api = vi.hoisted(() => ({ playback: vi.fn() }))
vi.mock('@memohai/sdk', () => ({ postBotsByBotIdTtsPlayback: api.playback }))
afterEach(() => { vi.unstubAllGlobals(); api.playback.mockReset() })
const reply = (text: string, streaming = true): ChatMessage => ({ id: 'bubble', turnId: 'turn', runtimeRunId: 'run', role: 'assistant', timestamp: '', streaming, messages: [{ id: 0, type: 'text', content: text }] })
const settle = async () => { await nextTick(); for (let i = 0; i < 20; i++) await Promise.resolve(); await nextTick() }

it('automatically reads a live reply which settles before the post-render watcher runs, without reading baseline history', async () => {
  api.playback.mockImplementation(async () => ({ data: new ReadableStream({ start(c) { c.enqueue(new Uint8Array([1])); c.close() } }), response: new Response(null, { headers: { 'Content-Type': 'audio/wav' } }) }))
  vi.stubGlobal('Audio', class extends EventTarget { src = ''; play = vi.fn(async () => {}); pause() {}; load() {} })
  vi.stubGlobal('URL', { createObjectURL: () => 'blob:fixture', revokeObjectURL() {} })
  const messages = ref<ChatMessage[]>([{ ...reply('History', false), id: 'old', turnId: 'old' }]), draft = ref('')
  const root = document.createElement('div'); document.body.append(root)
  const app = createApp({ setup() {
    useChatReadAloud({ botId: ref('bot'), scope: ref('bot:session'), enabled: ref(true), loading: ref(false), draft, messages,
      bodyRoot: () => root, messageElement: id => root.querySelector(`[data-message-id="${id}"]`) })
    return () => h('div', messages.value.map(m => h('div', { 'data-message-id': m.id }, [h('div', { 'data-speech-body': '', 'data-speech-block-id': '0' }, m.role === 'assistant' && m.messages[0]?.type === 'text' ? m.messages[0].content : '')])))
  } }); app.mount(root)
  try {
    await settle(); expect(api.playback).not.toHaveBeenCalled()
    messages.value.push(reply(''))
    messages.value[messages.value.length - 1] = reply('Fresh complete reply.', false)
    await settle()
    expect(api.playback).toHaveBeenCalledOnce()
    expect(api.playback.mock.calls[0]![0].body.text).toBe('Fresh complete reply.')
    draft.value = 'interrupt'; await settle()
    messages.value[messages.value.length - 1] = reply('Fresh complete reply. Late tail.', false)
    draft.value = ''; await settle()
    expect(api.playback).toHaveBeenCalledOnce()
  } finally { app.unmount(); root.remove() }
})

it('does not split an unfinished sentence at commas, character counts or elapsed flush intervals', () => {
  const live = new LiveSpeechReplies()
  const text = 'We keep this sentence together, including a longer clause with more than thirty two characters'
  expect(live.update([reply(text)], true, false)).toEqual([])
  expect(live.update([reply(text + '.')], true, false)).toEqual([{ key: 'turn', text: text + '.' }])
  expect(live.update([reply(text + '. Next sentence is unfinished')], true, false)).toEqual([])
  expect(live.update([reply(text + '. Next sentence is unfinished', false)], true, false)).toEqual([{ key: 'turn', text: ' Next sentence is unfinished' }])
})

class BufferFixture extends EventTarget {
  mode = 'segments'
  end = 0
  appendBuffer() { this.end++; queueMicrotask(() => this.dispatchEvent(new Event('updateend'))) }
}
class MediaFixture extends EventTarget {
  static current: MediaFixture
  static isTypeSupported(mime: string) { return mime === 'audio/mpeg' }
  readyState = 'open'
  buffer = new BufferFixture()
  endOfStream = vi.fn()
  constructor() { super(); MediaFixture.current = this }
  addSourceBuffer() { return this.buffer }
}
function continuousFixture() {
  vi.stubGlobal('MediaSource', MediaFixture)
  const player = Object.assign(new EventTarget(), { src: '', currentTime: 0, play: vi.fn(async () => {}), pause: vi.fn(), load: vi.fn() })
  Object.defineProperty(player, 'buffered', { get: () => ({ length: 1, start: () => 0, end: () => MediaFixture.current.buffer.end }) })
  const cancel = vi.fn(), states: SpeechState[] = []
  const synthesize = vi.fn(async (_text: string, _signal: AbortSignal) => ({ contentType: 'audio/mpeg', stream: new ReadableStream<Uint8Array>({ start(c) { c.enqueue(new Uint8Array([1])); c.close() }, cancel }) }))
  const createURL = vi.fn(() => { queueMicrotask(() => MediaFixture.current.dispatchEvent(new Event('sourceopen'))); return 'blob:sequence' })
  const speech = new ChatReadAloud({ synthesize, createPlayer: () => player, createURL, revokeURL: vi.fn(), onState: state => states.push(state) })
  return { speech, player, synthesize, createURL, states }
}
it('keeps one decoder and player, prefetches one response ahead and waits on the native clock before fetching further', async () => {
  const f = continuousFixture(); f.speech.setLive(['turn'])
  for (const text of ['First.', 'Second.', 'Third.']) f.speech.enqueue({ key: 'turn', text })
  await settle()
  expect(f.synthesize.mock.calls.map(call => call[0])).toEqual(['First.', 'Second.'])
  expect(f.player.play).toHaveBeenCalledOnce(); expect(f.createURL).toHaveBeenCalledOnce()
  expect(MediaFixture.current.buffer.mode).toBe('sequence'); expect(MediaFixture.current.endOfStream).not.toHaveBeenCalled()
  f.player.dispatchEvent(new Event('playing'))
  f.player.currentTime = 1; f.player.dispatchEvent(new Event('timeupdate')); await settle()
  expect(f.synthesize.mock.calls.map(call => call[0])).toEqual(['First.', 'Second.', 'Third.'])
  expect(f.player.play).toHaveBeenCalledOnce(); expect(f.states.at(-1)?.status).toBe('playing')
  expect(MediaFixture.current.endOfStream).not.toHaveBeenCalled()
  f.speech.setLive([]); await settle(); expect(MediaFixture.current.endOfStream).toHaveBeenCalledOnce()
  f.player.dispatchEvent(new Event('ended')); expect(f.states.at(-1)?.status).toBe('idle')
})
it('stop cancels a clock-blocked queue and stale events cannot restart it; a new reply can still play', async () => {
  const f = continuousFixture(); f.speech.setLive(['turn'])
  for (const text of ['First.', 'Second.', 'Never request.']) f.speech.enqueue({ key: 'turn', text })
  await settle(); expect(f.synthesize).toHaveBeenCalledTimes(2)
  f.speech.stop(); f.player.currentTime = 10
  for (const event of ['timeupdate', 'playing', 'ended']) f.player.dispatchEvent(new Event(event))
  await settle()
  expect(f.states.at(-1)?.status).toBe('idle'); expect(f.synthesize).toHaveBeenCalledTimes(2)
  expect(f.synthesize.mock.calls.every(call => call[1].aborted)).toBe(true)
  f.speech.enqueue({ key: 'fresh', text: 'Fresh.' }); await settle()
  f.player.dispatchEvent(new Event('playing'))
  expect(f.states.at(-1)).toMatchObject({ key: 'fresh', status: 'playing' }); f.speech.stop()
})
it('a failed prefetch stops the active decoder and clears later text without retrying', async () => {
  const f = continuousFixture(), fail = new Error('controlled prefetch failure')
  const first = f.synthesize.getMockImplementation()!
  f.synthesize.mockImplementationOnce(first).mockRejectedValueOnce(fail)
  for (const text of ['First.', 'Failure.', 'Never request.']) f.speech.enqueue({ key: 'turn', text })
  await settle()
  expect(f.synthesize).toHaveBeenCalledTimes(2)
  expect(f.states.at(-1)).toMatchObject({ key: 'turn', status: 'error', error: fail })
  expect(f.player.src).toBe('')
  for (const event of ['playing', 'ended', 'timeupdate']) f.player.dispatchEvent(new Event(event))
  await settle(); expect(f.synthesize).toHaveBeenCalledTimes(2)
  expect(f.states.at(-1)?.status).toBe('error')
})
