import { expect, it } from 'vitest'
import type { ChatAssistantTurn } from '@/store/chat/types'
import { LiveSpeechReplies } from './chat-read-aloud'
import { SpeechReplayCache } from './speech-replay-cache'

const message = (text: string, streaming: boolean, run?: string): ChatAssistantTurn => ({ id: 'same-render-row', turnId: 'original-turn', ...(run ? { runtimeRunId: run } : {}), role: 'assistant', streaming, timestamp: '', messages: [{ id: 0, type: 'text', content: text }] })
const visible = (m: ChatAssistantTurn) => m.messages.flatMap(block => block.type === 'text' ? [block.content] : []).join('')
const collect = (live: LiveSpeechReplies, m: ChatAssistantTurn) => live.update([m], true, false, false, item => visible(item as ChatAssistantTurn)).map(part => part.text)
function started() {
  const live = new LiveSpeechReplies()
  expect(collect(live, message('Original sentence. Waiting', true, 'original-run'))).toEqual(['Original sentence. '])
  return live
}
it.each([['retained', 'original-run'], ['omitted', undefined]])('commits a delayed final tail exactly once when runtimeRunId is %s', (_name, run) => {
  const live = started(), final = message('Original sentence. Delayed tail.', false, run)
  expect(collect(live, final)).toEqual(['Delayed tail.'])
  expect(collect(live, final)).toEqual([])
  expect(collect(live, final)).toEqual([])
})
it('keeps actual EOF-completed cache coverage ready after repeated final DOM collection without a runtime run id', async () => {
  const live = new LiveSpeechReplies(), cache = new SpeechReplayCache(), signal = new AbortController().signal
  const rows = [message('Original sentence. Waiting', true, 'original-run'), message('Original sentence. Delayed tail.', false), message('Original sentence. Delayed tail.', false)]
  let count = 0
  for (const row of rows) for (const text of collect(live, row)) {
    count++
    const audio = cache.capture('account/bot/scope/original-run/same-render-row', text, { identity: 'same-effective-voice-model-format', contentType: 'audio/wav', stream: new ReadableStream({ start(c) { c.enqueue(new Uint8Array([count])); c.close() } }) }, signal)
    const reader = audio.stream.getReader(); while (!(await reader.read()).done) {}; reader.releaseLock()
  }
  expect(cache.status('account/bot/scope/original-run/same-render-row', 'Original sentence. Delayed tail.')).toBe('ready')
  expect(count).toBe(2)
})
it('keeps an explicitly identified new run on its own cursor after an omitted-run final tail', () => {
  const live = started()
  expect(collect(live, message('Original sentence. Delayed tail.', false))).toEqual(['Delayed tail.'])
  expect(collect(live, message('New sentence. Waiting', true, 'new-run'))).toEqual(['New sentence. '])
  expect(collect(live, message('New sentence. New tail.', false))).toEqual(['New tail.'])
  expect(collect(live, message('New sentence. New tail.', false))).toEqual([])
})
it('retains interruption fences and establishes a silent history baseline after scope reset', () => {
  const live = started()
  live.interrupt([message('Original sentence. Waiting', true, 'original-run')])
  expect(collect(live, message('Original sentence. Delayed tail.', false))).toEqual([])
  expect(collect(live, message('Original sentence. Late frame.', true, 'original-run'))).toEqual([])
  expect(collect(live, message('New sentence. Waiting', true, 'new-run'))).toEqual(['New sentence. '])
  live.reset()
  expect(collect(live, message('History in next scope.', false))).toEqual([])
  expect(collect(live, message('Fresh scope sentence. Waiting', true, 'scope-run'))).toEqual(['Fresh scope sentence. '])
})
