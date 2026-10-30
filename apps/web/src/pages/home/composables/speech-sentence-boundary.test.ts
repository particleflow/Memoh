import { describe, expect, it } from 'vitest'
import { LiveSpeechReplies } from './chat-read-aloud'
import type { ChatAssistantTurn } from '@/store/chat/types'

const message = (content: string, streaming = true): ChatAssistantTurn => ({ id: 'bubble', turnId: 'turn', runtimeRunId: 'run', role: 'assistant', timestamp: '', streaming, messages: [{ id: 0, type: 'text', content }] })
describe('prosody boundaries across incremental tokens', () => {
  it.each(['Dr.', 'Mr.', 'Prof.', 'U.S.', 'Ph.D.', '3.', '1.', '第3.'])('does not commit an ambiguous partial prefix %s as its own synthesis request', prefix => {
    const live = new LiveSpeechReplies()
    expect(live.update([message(prefix)], true, false)).toEqual([])
  })
  it('keeps a title and following name together instead of resetting context after Dr.', () => {
    const live = new LiveSpeechReplies()
    expect(live.update([message('Dr.')], true, false)).toEqual([])
    expect(live.update([message('Dr. Smith has 3.14 dollars. Next')], true, false)).toEqual([{ key: 'turn', messageId: 'bubble', text: 'Dr. Smith has 3.14 dollars. ' }])
  })
  it('keeps decimal/version digits and ordered-list labels with their content', () => {
    for (const text of ['The value is 3.14 and version 1.2.3 is ready', '1. First item', '2. Second item', 'U.S. policy and Ph.D. students']) {
      expect(new LiveSpeechReplies().update([message(text)], true, false)).toEqual([])
    }
  })
  it('does not break mixed-language clauses at an abbreviation or comma, but can commit a clear CJK sentence', () => {
    const live = new LiveSpeechReplies()
    expect(live.update([message('请看第3.14项，Dr. Smith 说明')], true, false)).toEqual([])
    expect(live.update([message('请看第3.14项，Dr. Smith 说明。下一句')], true, false)).toEqual([{ key: 'turn', messageId: 'bubble', text: '请看第3.14项，Dr. Smith 说明。' }])
  })
  it('retains a trailing English period for textual lookahead; finalization flushes a genuine single sentence', () => {
    const live = new LiveSpeechReplies()
    expect(live.update([message('A complete sentence.')], true, false)).toEqual([])
    expect(live.update([message('A complete sentence.', false)], true, false)).toEqual([{ key: 'turn', messageId: 'bubble', text: 'A complete sentence.' }])
  })
})
