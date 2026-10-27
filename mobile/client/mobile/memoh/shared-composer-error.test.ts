import { describe, expect, it } from 'vitest'
import { effectScope, ref } from 'vue'
import { runtimeFailureAlreadyVisible, watchVisibleComposerFailure } from './composer-error'
import type { ChatAssistantTurn, SendMessageResult } from '@/store/chat/types'

const failure: SendMessageResult = { ok: false, errorCode: 'runtime_run_failed', errorTurnId: 'original-turn', errorRunId: 'original-run', error: 'localized failure' }
const reply = (overrides: Partial<ChatAssistantTurn> = {}): ChatAssistantTurn => ({ id: 'reply', role: 'assistant', turnId: 'original-turn', runtimeRunId: 'original-run', streaming: false, timestamp: new Date().toISOString(), messages: [{ id: 0, type: 'error', code: 'runtime_run_failed', content: '' }], ...overrides })
describe('one identified runtime failure surface', () => {
  it('hides the composer duplicate only when the same run/turn failure is already in its reply', () => {
    expect(runtimeFailureAlreadyVisible(failure, [reply()])).toBe(true)
    expect(runtimeFailureAlreadyVisible(failure, [])).toBe(false)
  })
  it.each(['workspace.unreachable', 'model.not_found', 'http.forbidden', undefined])('keeps independent composer error %s', errorCode => {
    expect(runtimeFailureAlreadyVisible({ ...failure, errorCode }, [reply()])).toBe(false)
  })
  it('keeps a standalone failure and never deduplicates by the translated text alone', () => {
    expect(runtimeFailureAlreadyVisible({ errorCode: 'runtime_run_failed' }, [reply()])).toBe(false)
    expect(runtimeFailureAlreadyVisible(failure, [reply({ messages: [{ id: 0, type: 'error', code: 'other', content: 'localized failure' }] })])).toBe(false)
  })
  it('does not hide another run/turn or a user message with the same error code', () => {
    expect(runtimeFailureAlreadyVisible(failure, [reply({ runtimeRunId: 'different-run' })])).toBe(false)
    expect(runtimeFailureAlreadyVisible(failure, [reply({ turnId: 'different-turn' })])).toBe(false)
    expect(runtimeFailureAlreadyVisible(failure, [{ id: 'user', role: 'user', isSelf: true, text: 'localized failure', attachments: [], timestamp: '2026-10-27T06:00:00Z', streaming: false }])).toBe(false)
  })
})


it('does not resurrect the old composer banner after recovery/history replaces the reply projection', () => {
  const owner = effectScope(), copy = ref('localized failure'), current = ref<SendMessageResult | undefined>(failure), messages = ref<ChatAssistantTurn[]>([])
  owner.run(() => watchVisibleComposerFailure(() => current.value, () => messages.value, () => { copy.value = ''; current.value = undefined }))
  expect(copy.value).toBe('localized failure')
  messages.value = [reply()]; expect(copy.value).toBe('')
  messages.value = []; expect(copy.value).toBe('')
  current.value = { ...failure, errorCode: 'workspace.unreachable' }; copy.value = 'independent workspace error'
  messages.value = [reply()]; expect(copy.value).toBe('independent workspace error')
  owner.stop()
})
