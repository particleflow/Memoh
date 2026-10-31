// @vitest-environment jsdom
import { createApp, defineComponent, h, nextTick, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ChatMessage } from '@/store/chat/types'
import { useChatReadAloud } from './useChatReadAloud'
const api = vi.hoisted(() => ({ playback: vi.fn() }))
vi.mock('@memohai/sdk', () => ({ postBotsByBotIdTtsPlayback: api.playback }))
class Player extends EventTarget { src = ''; play = vi.fn(async () => {}); pause = vi.fn(); load = vi.fn() }
class MediaFixture extends EventTarget {
  static isTypeSupported() { return true }
  readyState = 'open'
  addSourceBuffer() { const buffer = new EventTarget(); return Object.assign(buffer, { appendBuffer() { queueMicrotask(() => buffer.dispatchEvent(new Event('updateend'))) } }) }
  endOfStream() {}
}
const settle = async () => { await nextTick(); for (let i = 0; i < 20; i++) await Promise.resolve(); await nextTick() }
let app: ReturnType<typeof createApp> | undefined
beforeEach(() => {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  api.playback.mockReset()
  vi.stubGlobal('Audio', Player)
  vi.stubGlobal('MediaSource', MediaFixture)
  vi.stubGlobal('URL', { createObjectURL: (media: MediaFixture) => { queueMicrotask(() => media.dispatchEvent(new Event('sourceopen'))); return 'blob:stream' }, revokeObjectURL: vi.fn() })
})
afterEach(() => { app?.unmount(); app = undefined; vi.unstubAllGlobals(); vi.useRealTimers() })
function mount() {
  const messages = ref<ChatMessage[]>([]), draft = ref(''), enabled = ref(true)
  let speech!: ReturnType<typeof useChatReadAloud>
  app = createApp(defineComponent({ setup() {
    speech = useChatReadAloud({ botId: ref('configured-bot'), scope: ref('scope'), enabled, loading: ref(false), draft, messages })
    return () => h('div')
  } }))
  app.mount(document.createElement('div'))
  return { messages, draft, enabled, get speech() { return speech } }
}
function message(text: string): ChatMessage { return { id: 'm', turnId: 't', runtimeRunId: 'r', role: 'assistant', timestamp: '', streaming: true, messages: [{ type: 'text', id: 0, content: text }] } }
function response() {
  const cancel = vi.fn()
  let feed!: ReadableStreamDefaultController<Uint8Array>
  const stream = new ReadableStream<Uint8Array>({ start(controller) { feed = controller }, cancel })
  api.playback.mockResolvedValue({ data: stream, response: { headers: new Headers({ 'Content-Type': 'audio/mpeg' }) } })
  return { cancel, feed }
}
describe('actual read-aloud composable streaming wiring', () => {
  it('requests streaming audio after 80ms of a short live opening while reply and audio remain incomplete', async () => {
    const f = mount(), audio = response()
    f.messages.value.push(message('自动短句')); await settle()
    expect(api.playback).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(80); await settle()
    expect(api.playback).toHaveBeenCalledOnce()
    expect(api.playback.mock.calls[0]![0]).toMatchObject({ path: { bot_id: 'configured-bot' }, body: { text: '自动短句' }, parseAs: 'stream' })
    expect(f.messages.value[0]!.streaming).toBe(true)
    audio.feed.enqueue(new Uint8Array([1, 2])); await settle()
    f.speech.stop(); await settle(); expect(audio.cancel).toHaveBeenCalledOnce()
  })
  it('typing cancels the delayed flush and consumes the old live reply', async () => {
    const f = mount(); response()
    f.messages.value.push(message('旧短句')); await settle()
    f.draft.value = '新问题'; await settle()
    await vi.advanceTimersByTimeAsync(100); await settle()
    f.draft.value = ''; f.messages.value[0] = { ...message('旧短句追加。'), streaming: false }; await settle()
    expect(api.playback).not.toHaveBeenCalled(); expect(f.speech.state.value.status).toBe('idle')
  })
  it('manual replay uses the same streaming transport and aborts immediately on hiding', async () => {
    const f = mount(), audio = response()
    const manual = { ...message('[文档](https://hidden.test) 中的可见正文'), streaming: false }
    f.messages.value.push(manual); await settle()
    f.speech.replay(manual); await settle()
    expect(api.playback.mock.calls[0]![0]).toMatchObject({ body: { text: '文档 中的可见正文' }, parseAs: 'stream' })
    const signal = api.playback.mock.calls[0]![0].signal as AbortSignal
    f.enabled.value = false
    expect(signal.aborted).toBe(true)
    await settle(); expect(audio.cancel).toHaveBeenCalledOnce()
    expect(f.speech.state.value.status).toBe('idle')
  })
  it('a failed streaming request consumes subsequent deltas instead of automatically retrying the utterance', async () => {
    const f = mount()
    api.playback.mockRejectedValue(new Error('synthetic provider failure'))
    f.messages.value.push(message('第一句。')); await settle()
    expect(f.speech.state.value.status).toBe('error')
    f.messages.value[0] = message('第一句。后续正文。'); await settle()
    await vi.advanceTimersByTimeAsync(100); await settle()
    expect(api.playback).toHaveBeenCalledOnce()
    expect(f.speech.state.value.status).toBe('error')
  })
})
