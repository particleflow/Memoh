// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest'
import { ChatReadAloud, type SpeechState } from './chat-read-aloud'
import { prepareSpeechSource } from './speech-audio-stream'

const tick = async () => { for (let i = 0; i < 30; i++) await Promise.resolve() }
afterEach(() => vi.unstubAllGlobals())
function fixture() {
  vi.stubGlobal('MediaSource', undefined)
  let feed!: ReadableStreamDefaultController<Uint8Array>
  const cancel = vi.fn()
  const stream = new ReadableStream<Uint8Array>({ start(value) { feed = value }, cancel })
  const player = Object.assign(new EventTarget(), { src: '', play: vi.fn(async () => {}), pause: vi.fn(), load: vi.fn() })
  const states: SpeechState[] = []
  const synthesize = vi.fn(async (_text: string, _signal: AbortSignal) => ({ stream, contentType: 'audio/wav' }))
  const createURL = vi.fn((_source: Blob | MediaSource) => 'blob:wav')
  const revokeURL = vi.fn()
  const speech = new ChatReadAloud({ synthesize, createPlayer: () => player, createURL, revokeURL, onState: state => states.push(state) })
  return { feed, cancel, player, states, synthesize, createURL, revokeURL, speech, stream }
}

it('preserves WAV type and bytes through one response; creates no Blob URL before EOF', async () => {
  const f = fixture()
  f.speech.enqueue({ key: 'wav', text: 'visible' })
  f.feed.enqueue(new Uint8Array([1, 2])); await tick()
  expect(f.createURL).not.toHaveBeenCalled(); expect(f.player.play).not.toHaveBeenCalled()
  f.feed.enqueue(new Uint8Array([3, 4])); f.feed.close(); await tick()
  expect(f.synthesize).toHaveBeenCalledOnce(); expect(f.createURL).toHaveBeenCalledOnce(); expect(f.player.play).toHaveBeenCalledOnce()
  const blob = f.createURL.mock.calls[0]![0] as Blob
  expect(blob.type).toBe('audio/wav')
  const reader = new FileReader(), read = new Promise<ArrayBuffer>(resolve => { reader.onload = () => resolve(reader.result as ArrayBuffer) })
  reader.readAsArrayBuffer(blob)
  expect([...new Uint8Array(await read)]).toEqual([1, 2, 3, 4])
  f.speech.stop()
})

it('typing/stop cancels a pending WAV read and discards the old queue without creating an audio source', async () => {
  const f = fixture()
  f.speech.enqueue({ key: 'wav', text: 'visible' }); f.speech.enqueue({ key: 'later', text: 'old queue' })
  f.feed.enqueue(new Uint8Array([1])); await tick(); f.speech.stop(); await tick()
  expect(f.synthesize.mock.calls[0]![1].aborted).toBe(true); expect(f.cancel).toHaveBeenCalledOnce()
  expect(f.stream.locked).toBe(false); expect(f.createURL).not.toHaveBeenCalled(); expect(f.player.play).not.toHaveBeenCalled()
  expect(f.states.at(-1)?.status).toBe('idle'); expect(f.synthesize).toHaveBeenCalledOnce()
})

it('an already-aborted fallback releases the reader and preserves the abort reason', async () => {
  const f = fixture(), abort = new AbortController(), reason = new Error('first abort')
  abort.abort(reason)
  await expect(prepareSpeechSource({ stream: f.stream, contentType: 'audio/wav' }, abort.signal, f.createURL)).rejects.toBe(reason)
  expect(f.cancel).toHaveBeenCalledOnce(); expect(f.stream.locked).toBe(false); expect(f.createURL).not.toHaveBeenCalled()
})

it('abort settles with its original reason even when upstream cancellation never settles', async () => {
  const f = fixture(), abort = new AbortController(), reason = new Error('first abort while cancel stalls')
  f.cancel.mockImplementation(() => new Promise<void>(() => {}))
  const pending = prepareSpeechSource({ stream: f.stream, contentType: 'audio/wav' }, abort.signal, f.createURL)
  await tick(); abort.abort(reason)
  await expect(pending).rejects.toBe(reason)
  expect(f.cancel).toHaveBeenCalledOnce(); expect(f.stream.locked).toBe(false); expect(f.createURL).not.toHaveBeenCalled()
})

it('the first response read error clears the queue without playing partial WAV or retrying synthesis', async () => {
  const f = fixture(), reason = new Error('first body failure')
  f.speech.enqueue({ key: 'wav', text: 'visible' }); f.speech.enqueue({ key: 'later', text: 'old queue' })
  f.feed.enqueue(new Uint8Array([1])); await tick(); f.feed.error(reason); await tick()
  expect(f.states.at(-1)).toMatchObject({ key: 'wav', status: 'error', error: reason })
  expect(f.stream.locked).toBe(false); expect(f.createURL).not.toHaveBeenCalled(); expect(f.player.play).not.toHaveBeenCalled()
  expect(f.synthesize).toHaveBeenCalledOnce(); expect(f.synthesize.mock.calls[0]![1].aborted).toBe(true)
})

it('a Blob URL allocation error stays the first error and does not retry or start queued audio', async () => {
  const f = fixture(), reason = new Error('first URL failure')
  f.createURL.mockImplementation(() => { throw reason })
  f.speech.enqueue({ key: 'wav', text: 'visible' }); f.speech.enqueue({ key: 'later', text: 'old queue' })
  f.feed.close(); await tick()
  expect(f.states.at(-1)?.error).toBe(reason); expect(f.states.at(-1)?.status).toBe('error')
  expect(f.player.play).not.toHaveBeenCalled(); expect(f.synthesize).toHaveBeenCalledOnce(); expect(f.stream.locked).toBe(false)
})

it('native decode failure after Blob preparation clears the URL and old queue without resynthesis', async () => {
  const f = fixture()
  f.speech.enqueue({ key: 'wav', text: 'visible' }); f.speech.enqueue({ key: 'later', text: 'old queue' })
  f.feed.close(); await tick(); f.player.dispatchEvent(new Event('error')); await tick()
  expect(f.states.at(-1)?.status).toBe('error'); expect(f.player.src).toBe(''); expect(f.revokeURL).toHaveBeenCalledExactlyOnceWith('blob:wav')
  expect(f.synthesize).toHaveBeenCalledOnce(); expect(f.player.play).toHaveBeenCalledOnce()
})

it('stop between fallback completion and controller continuation revokes the late URL without playback', async () => {
  const f = fixture()
  f.speech.enqueue({ key: 'wav', text: 'visible' }); await tick()
  // Stop at the actual allocation/continuation seam, independent of stream-tap
  // microtask depth. This still requires creation and revocation of the URL.
  f.createURL.mockImplementationOnce(() => { queueMicrotask(() => f.speech.stop()); return 'blob:wav' })
  f.feed.close(); await tick()
  expect(f.createURL).toHaveBeenCalledOnce(); expect(f.revokeURL).toHaveBeenCalledExactlyOnceWith('blob:wav')
  expect(f.player.play).not.toHaveBeenCalled(); expect(f.states.at(-1)?.status).toBe('idle')
})

it('the new selector still forwards a supported MSE response before EOF without a Blob fallback', async () => {
  class Buffer extends EventTarget { chunks: number[][] = []; appendBuffer(value: ArrayBuffer) { this.chunks.push([...new Uint8Array(value)]); queueMicrotask(() => this.dispatchEvent(new Event('updateend'))) } }
  class Media extends EventTarget {
    static isTypeSupported(type: string) { return type === 'audio/mpeg' }
    static instance: Media
    readyState = 'open'; buffer = new Buffer()
    constructor() { super(); Media.instance = this }
    addSourceBuffer() { return this.buffer }
    endOfStream() {}
  }
  const f = fixture(); vi.stubGlobal('MediaSource', Media)
  const abort = new AbortController(), source = await prepareSpeechSource({ stream: f.stream, contentType: 'audio/mp3' }, abort.signal, f.createURL)
  const settled = source.done.catch(error => error)
  Media.instance.dispatchEvent(new Event('sourceopen')); f.feed.enqueue(new Uint8Array([1, 2])); await tick()
  expect(f.createURL.mock.calls[0]![0]).toBeInstanceOf(Media); expect(Media.instance.buffer.chunks).toEqual([[1, 2]])
  abort.abort(); expect(await settled).toBeDefined(); expect(f.cancel).toHaveBeenCalledOnce()
})
