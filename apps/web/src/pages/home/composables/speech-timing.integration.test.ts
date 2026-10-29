// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest'
import { ChatReadAloud } from './chat-read-aloud'
import { beginSpeechTiming } from './speech-timing'

interface TraceRow { events: { event: string }[] }
const traces = () => (window as unknown as { __memohSpeechTiming: { snapshot(): TraceRow[], clear(): void } }).__memohSpeechTiming
const settle = async () => { for (let i = 0; i < 20; i++) await Promise.resolve() }
afterEach(() => {
  sessionStorage.removeItem('memoh.speechTiming')
  traces()?.clear()
})

it('timing opt-in preserves cancellation of a late response without creating or replaying audio', async () => {
  sessionStorage.setItem('memoh.speechTiming', '1')
  const timing = beginSpeechTiming('manual')!
  let resolve!: (value: { stream: ReadableStream<Uint8Array>, contentType: string }) => void
  let signal: AbortSignal | undefined
  const synthesize = vi.fn((_text: string, requestSignal: AbortSignal) => {
    signal = requestSignal
    return new Promise<{ stream: ReadableStream<Uint8Array>, contentType: string }>(done => { resolve = done })
  })
  const createPlayer = vi.fn(() => { throw new Error('late response must not create a player') })
  const createURL = vi.fn(() => { throw new Error('late response must not create a URL') })
  const cancel = vi.fn()
  const controller = new ChatReadAloud({ synthesize, createPlayer, createURL, revokeURL: vi.fn(), onState: vi.fn() })
  controller.replay({ key: 'private-turn-id', text: 'private spoken text', timing })
  await settle()
  controller.stop()
  expect(signal?.aborted).toBe(true)
  resolve({ stream: new ReadableStream<Uint8Array>({ cancel }), contentType: 'audio/mpeg' })
  await settle()
  expect(cancel).toHaveBeenCalledOnce()
  expect(synthesize).toHaveBeenCalledOnce()
  expect(createPlayer).not.toHaveBeenCalled()
  expect(createURL).not.toHaveBeenCalled()
  const snapshot = traces().snapshot()
  expect(snapshot.some(row => row.events.some(event => event.event === 'interrupted'))).toBe(true)
  expect(JSON.stringify(snapshot)).not.toMatch(/private-turn-id|private spoken text/u)
})

it('timing opt-in keeps blocked replay on the same loaded source and never restarts a stopped queue', async () => {
  sessionStorage.setItem('memoh.speechTiming', '1')
  const target = new EventTarget()
  const player = Object.assign(target, {
    src: '', pause: vi.fn(), load: vi.fn(),
    play: vi.fn<() => Promise<void>>()
      .mockRejectedValueOnce(Object.assign(new Error('private autoplay error'), { name: 'NotAllowedError' }))
      .mockResolvedValue(undefined),
  })
  const synthesize = vi.fn(async () => new Blob(['controlled local bytes']))
  const changed = vi.fn()
  const controller = new ChatReadAloud({ synthesize, createPlayer: () => player, createURL: () => 'blob:private-fixture', revokeURL: vi.fn(), onState: changed })
  controller.replay({ key: 'private-turn-id', text: 'private spoken text', timing: beginSpeechTiming('manual') })
  await settle()
  expect(changed).toHaveBeenLastCalledWith(expect.objectContaining({ status: 'blocked' }))
  const sameSource = player.src
  controller.resume()
  expect(player.play).toHaveBeenCalledTimes(2)
  expect(synthesize).toHaveBeenCalledOnce()
  expect(player.src).toBe(sameSource)
  player.dispatchEvent(new Event('playing'))
  expect(changed).toHaveBeenLastCalledWith(expect.objectContaining({ status: 'playing' }))
  controller.stop()
  player.dispatchEvent(new Event('playing'))
  player.dispatchEvent(new Event('ended'))
  await settle()
  expect(changed).toHaveBeenLastCalledWith(expect.objectContaining({ status: 'idle' }))
  expect(synthesize).toHaveBeenCalledOnce()
  expect(JSON.stringify(traces().snapshot())).not.toMatch(/private-turn-id|private spoken text|private autoplay error|blob:private-fixture/u)
})
