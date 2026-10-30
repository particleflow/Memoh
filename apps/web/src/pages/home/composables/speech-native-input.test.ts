import { afterEach, expect, it, vi } from 'vitest'
import { ChatReadAloud, type SpeechInputFrame, type SpeechState } from './chat-read-aloud'
class BufferFixture extends EventTarget { mode = 'segments'; appendBuffer() { queueMicrotask(() => this.dispatchEvent(new Event('updateend'))) } }
class MediaFixture extends EventTarget {
  static current: MediaFixture
  static isTypeSupported() { return true }
  readyState = 'open'; buffer = new BufferFixture(); endOfStream = vi.fn()
  constructor() { super(); MediaFixture.current = this }
  addSourceBuffer() { return this.buffer }
}
const settle = async () => { for (let i = 0; i < 70; i++) await Promise.resolve() }
afterEach(() => vi.unstubAllGlobals())
function fixture() {
  vi.stubGlobal('MediaSource', MediaFixture)
  let output!: ReadableStreamDefaultController<Uint8Array>, signal!: AbortSignal
  const frames: SpeechInputFrame[] = [], states: SpeechState[] = []
  const player = Object.assign(new EventTarget(), { src: '', play: vi.fn(async () => {}), pause: vi.fn(), load: vi.fn() })
  const synthesize = vi.fn(), openInput = vi.fn(async (_entry, current: AbortSignal) => {
    signal = current
    const stream = new ReadableStream<Uint8Array>({ start(c) { output = c; c.enqueue(new Uint8Array([1])); current.addEventListener('abort', () => { try { c.error(current.reason) } catch {} }, { once: true }) } })
    return { audio: { stream, contentType: 'audio/mpeg', identity: 'configured' }, append: vi.fn(async (frame: SpeechInputFrame) => { frames.push(frame); if (frame.final) output.close(); else output.enqueue(new Uint8Array([frame.sequence + 1])) }) }
  })
  const createURL = vi.fn(() => { queueMicrotask(() => MediaFixture.current.dispatchEvent(new Event('sourceopen'))); return 'blob:native' })
  const speech = new ChatReadAloud({ openInput, synthesize, createPlayer: () => player, createURL, revokeURL: vi.fn(), onState: s => states.push(s) })
  const entry = { key: 'turn', messageId: 'row', cacheKey: 'account-bot-session-run-row', text: '甲😀' }
  return { speech, entry, frames, states, synthesize, openInput, player, createURL, get signal() { return signal } }
}
it('appends exact Unicode offsets to one task, flushes the final tail once, and caches the whole message for zero-generation replay', async () => {
  const f = fixture(); f.speech.setLive(['row']); f.speech.enqueue(f.entry); await settle()
  expect(f.frames).toEqual([])
  f.speech.enqueue({ ...f.entry, text: '乙。' }); await settle()
  expect(f.frames).toEqual([{ sequence: 1, offset: 2, text: '乙。', final: false }])
  f.speech.enqueue({ ...f.entry, text: '尾巴' }); f.speech.setLive([]); await settle()
  expect(f.frames).toEqual([{ sequence: 1, offset: 2, text: '乙。', final: false }, { sequence: 2, offset: 4, text: '尾巴', final: false }, { sequence: 3, offset: 6, text: '', final: true }])
  expect(f.openInput).toHaveBeenCalledOnce(); expect(f.synthesize).not.toHaveBeenCalled()
  const full = { ...f.entry, text: '甲😀乙。尾巴' }
  expect(f.speech.replayAvailability(full)).toBe('ready')
  f.speech.stop(); expect(f.speech.replay(full, 'configured')).toBe(true); await settle()
  expect(f.openInput).toHaveBeenCalledOnce(); expect(f.synthesize).not.toHaveBeenCalled()
  f.speech.stop()
})
it('input cancellation aborts the task and consumes pending text without converting it into another synthesis', async () => {
  const f = fixture(); f.speech.setLive(['row']); f.speech.enqueue(f.entry); await settle()
  f.player.dispatchEvent(new Event('playing')); expect(f.states.at(-1)?.status).toBe('playing')
  f.speech.stop(); expect(f.signal.aborted).toBe(true); expect(f.states.at(-1)?.status).toBe('idle')
  f.speech.setLive([]); await settle(); f.player.dispatchEvent(new Event('playing'))
  expect(f.frames).toEqual([]); expect(f.openInput).toHaveBeenCalledOnce(); expect(f.synthesize).not.toHaveBeenCalled(); expect(f.states.at(-1)?.status).toBe('idle')
  expect(f.speech.replayAvailability(f.entry)).toBe('incomplete')
})

it('does not finish or restart after a late acknowledgement of an already canceled input append', async () => {
  const f = fixture(); f.speech.setLive(['row']); f.speech.enqueue(f.entry); await settle()
  const session = await f.openInput.mock.results[0]!.value
  let acknowledge!: () => void
  session.append.mockImplementationOnce((frame: SpeechInputFrame) => { f.frames.push(frame); return new Promise<void>(done => { acknowledge = done }) })
  f.speech.enqueue({ ...f.entry, text: 'pending tail' }); await settle()
  expect(f.frames).toHaveLength(1)
  f.speech.stop(); acknowledge(); await settle()
  expect(f.frames).toHaveLength(1); expect(f.frames[0]!.final).toBe(false)
  expect(f.openInput).toHaveBeenCalledOnce(); expect(f.synthesize).not.toHaveBeenCalled()
  expect(f.states.at(-1)?.status).toBe('idle')
})
