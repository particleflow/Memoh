import { describe, expect, it, vi } from 'vitest'
import { SpeechReplayCache } from './speech-replay-cache'
import { ChatReadAloud, LiveSpeechReplies, speechText } from './chat-read-aloud'
import { normalizeSpeechText } from './speech-rendered-text'
import type { ChatAssistantTurn } from '@/store/chat/types'

const tick = async () => { for (let i = 0; i < 30; i++) await Promise.resolve() }
const reply = (content: string, streaming = true): ChatAssistantTurn => ({ id: 'bubble', turnId: 'turn', runtimeRunId: 'run', role: 'assistant', timestamp: '', streaming, messages: [{ id: 0, type: 'text', content }] })
function bytes(text: string, type = 'audio/wav', identity = 'voice/model/format-one') {
  return { identity, contentType: type, stream: new ReadableStream<Uint8Array>({ start(c) { c.enqueue(new TextEncoder().encode(text)); c.close() } }) }
}
async function consume(stream: ReadableStream<Uint8Array>) { const reader = stream.getReader(); for (;;) { if ((await reader.read()).done) break }; reader.releaseLock() }

describe('uninterrupted text coverage before synthesis', () => {
  it('does not let a settled sibling row consume a distinct live assistant row in the same turn/run', () => {
    const live = new LiveSpeechReplies()
    const old = { ...reply('Old response.', false), id: 'old-row' }
    const fresh = { ...reply('Fresh response. Next'), id: 'fresh-row' }
    expect(live.update([old, fresh], true, false).map(part => part.text)).toEqual(['Fresh response. '])
  })
  it.each([
    '🙂 开头。\n\n请看[文档](https://hidden.test/x)，价格 US$3–6/月；3.14 和版本 1.2.3。\n\n最后一句。',
    '1. 第一项 3.14。\n2. 第二项🙂。\n\n结尾完整。',
    '**重要。**后续也必须朗读。',
    'Dr. Smith has 3.14 dollars. Next sentence is complete.',
  ])('preserves all visible words once in order through per-code-point deltas and final flush: %s', source => {
    const live = new LiveSpeechReplies(), output: string[] = []; let current = ''
    for (const point of Array.from(source)) { current += point; output.push(...live.update([reply(current)], true, false).map(part => part.text)) }
    output.push(...live.update([reply(source, false)], true, false).map(part => part.text))
    expect(normalizeSpeechText(output.join(''))).toBe(normalizeSpeechText(speechText(reply(source, false))))
  })
})

it('keeps full EOF-completed bytes after stop and replays them without a synthesis request', async () => {
  const player = Object.assign(new EventTarget(), { src: '', play: vi.fn(async () => {}), pause: vi.fn(), load: vi.fn() })
  const synthesize = vi.fn(async () => bytes('all generated audio'))
  const sources: Blob[] = [], states: string[] = []
  const speech = new ChatReadAloud({ synthesize, createPlayer: () => player, createURL: source => { sources.push(source as Blob); return 'blob:cached' }, revokeURL: vi.fn(), onState: value => states.push(value.status) })
  const entry = { key: 'turn', cacheKey: 'account/bot/session/run', text: 'Complete response.' }
  speech.enqueue(entry); await tick(); speech.stop()
  expect(speech.replayAvailability(entry)).toBe('ready')
  expect(speech.replay(entry, 'voice/model/format-one')).toBe(true); await tick()
  expect(synthesize).toHaveBeenCalledOnce()
  expect(await sources.at(-1)!.text()).toBe('all generated audio')
  player.dispatchEvent(new Event('playing')); expect(states.at(-1)).toBe('playing'); speech.stop()
  expect(speech.replay({ ...entry, cacheKey: 'other-account/bot/session/run' }, 'voice/model/format-one')).toBe(false)
  expect(speech.replay({ ...entry, text: 'Changed text.' }, 'voice/model/format-one')).toBe(false)
  expect(speech.replay(entry, 'different-voice')).toBe(false)
  expect(synthesize).toHaveBeenCalledOnce()
})

it('retains exact source spans and response order when EOF completion arrives out of order', async () => {
  const cache = new SpeechReplayCache(), abort = new AbortController()
  const first = cache.capture('owner/bot/run', 'First. ', bytes('first audio'), abort.signal)
  const second = cache.capture('owner/bot/run', 'Second.', bytes('second audio'), abort.signal)
  await consume(second.stream)
  expect(cache.status('owner/bot/run', 'First. Second.')).toBe('incomplete')
  await consume(first.stream)
  const parts = cache.read('owner/bot/run', 'First. Second.', 'voice/model/format-one')!
  expect(await Promise.all(parts.map(part => part.text()))).toEqual(['first audio', 'second audio'])
  expect(cache.read('owner/bot/run', 'First. Second.', 'changed-format')).toBeUndefined()
  cache.clear(); expect(cache.status('owner/bot/run', 'First. Second.')).toBe('missing')
})

it('never labels a canceled partial body, missing identity or incomplete message as replayable', async () => {
  const cache = new SpeechReplayCache(), abort = new AbortController()
  const generated = cache.capture('owner/bot/run', 'Generated. ', bytes('complete audio'), abort.signal)
  await consume(generated.stream)
  let feed!: ReadableStreamDefaultController<Uint8Array>
  const unfinished = cache.capture('owner/bot/run', 'Pending.', { identity: 'voice/model/format-one', contentType: 'audio/wav', stream: new ReadableStream({ start(c) { feed = c } }) }, abort.signal)
  const reader = unfinished.stream.getReader(); feed.enqueue(new Uint8Array([1, 2])); await reader.read()
  abort.abort(); await reader.cancel(); reader.releaseLock()
  expect(cache.status('owner/bot/run', 'Generated. Pending.')).toBe('incomplete')
  expect(cache.read('owner/bot/run', 'Generated. Pending.', 'voice/model/format-one')).toBeUndefined()
  const noIdentity = cache.capture('missing', 'Text.', bytes('audio', 'audio/wav', ''), new AbortController().signal)
  await consume(noIdentity.stream); expect(cache.status('missing', 'Text.')).toBe('incomplete')
})
it('includes a final rendered tail arriving after an earlier complete cached prefix', async () => {
  const cache = new SpeechReplayCache(), signal = new AbortController().signal
  await consume(cache.capture('owner/bot/run', 'First.', bytes('first'), signal).stream)
  expect(cache.status('owner/bot/run', 'First.')).toBe('ready')
  await consume(cache.capture('owner/bot/run', ' Final tail.', bytes('tail'), signal).stream)
  expect(cache.status('owner/bot/run', 'First. Final tail.')).toBe('ready')
  expect((await Promise.all(cache.read('owner/bot/run', 'First. Final tail.', 'voice/model/format-one')!.map(part => part.text())))).toEqual(['first', 'tail'])
})
