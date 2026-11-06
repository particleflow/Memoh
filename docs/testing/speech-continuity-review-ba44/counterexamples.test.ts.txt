import { describe, expect, it } from 'vitest'
import type { ChatAssistantTurn } from '@/store/chat/types'
import { LiveSpeechReplies } from './chat-read-aloud'

const row = (run: string | undefined, streaming: boolean): ChatAssistantTurn => ({
  id: 'same-render-row', turnId: 'original-turn', runtimeRunId: run,
  role: 'assistant', timestamp: '', streaming,
  messages: [{ id: 0, type: 'text', content: 'Original sentence. Delayed tail.' }],
})

describe('independent final DOM coverage after history reconciliation', () => {
  for (const keepRun of [true, false]) {
    it(`emits a late tail once with runtimeRunId ${keepRun ? 'retained' : 'omitted'}`, () => {
      const live = new LiveSpeechReplies()
      const streaming = row('original-run', true)
      live.observe([streaming], true, false)
      let visible = 'Original sentence.'
      const rendered = () => visible
      expect(live.update([streaming], true, false, true, rendered).map(part => part.text)).toEqual(['Original sentence.'])
      const final = row(keepRun ? 'original-run' : undefined, false)
      visible = 'Original sentence. Delayed tail.'
      expect(live.update([final], true, false, false, rendered).map(part => part.text)).toEqual([' Delayed tail.'])
      // MutationObserver, post-render collection and final snapshot can all
      // observe the same settled row. None is a new reply or text delta.
      expect(live.update([final], true, false, false, rendered)).toEqual([])
    })
  }
})

import { SpeechReplayCache } from './speech-replay-cache'

describe('independent cache coverage through final reconciliation', () => {
  for (const keepRun of [true, false]) {
    it(`keeps completed cache replayable with runtimeRunId ${keepRun ? 'retained' : 'omitted'}`, async () => {
      const live = new LiveSpeechReplies(), cache = new SpeechReplayCache()
      const signal = new AbortController().signal
      const streaming = row('original-run', true)
      live.observe([streaming], true, false)
      let visible = 'Original sentence.'
      const rendered = () => visible
      const emitted = [...live.update([streaming], true, false, true, rendered)]
      const final = row(keepRun ? 'original-run' : undefined, false)
      visible = 'Original sentence. Delayed tail.'
      emitted.push(...live.update([final], true, false, false, rendered))
      emitted.push(...live.update([final], true, false, false, rendered))
      for (const part of emitted) {
        const audio = cache.capture('principal/Bot/session/row/original-run', part.text, {
          identity: 'same-current-configuration', contentType: 'audio/wav',
          stream: new ReadableStream<Uint8Array>({ start(c) { c.enqueue(new Uint8Array([1, 2, 3])); c.close() } }),
        }, signal)
        await new Response(audio.stream).arrayBuffer()
      }
      expect(cache.status('principal/Bot/session/row/original-run', visible)).toBe('ready')
    })
  }
})
