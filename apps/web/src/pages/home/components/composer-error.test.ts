import { describe, expect, it } from 'vitest'
import { runtimeFailureAlreadyVisible } from './composer-error'
import type { ChatMessage, SendMessageResult } from '@/store/chat/types'

const failure: SendMessageResult = { ok: false, errorCode: 'runtime_run_failed', errorTurnId: 'original-turn', errorRunId: 'original-run', error: 'localized failure' }
const reply = (overrides: Partial<ChatMessage> = {}): ChatMessage => ({ id: 'reply', role: 'assistant', turnId: 'original-turn', runtimeRunId: 'original-run', text: '', attachments: [], timestamp: new Date(), messages: [{ type: 'error', code: 'runtime_run_failed', content: '' }], ...overrides })
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
    expect(runtimeFailureAlreadyVisible(failure, [reply({ messages: [{ type: 'error', code: 'other', content: 'localized failure' }] })])).toBe(false)
  })
  it('does not hide another run/turn or a user message with the same error code', () => {
    expect(runtimeFailureAlreadyVisible(failure, [reply({ runtimeRunId: 'different-run' })])).toBe(false)
    expect(runtimeFailureAlreadyVisible(failure, [reply({ turnId: 'different-turn' })])).toBe(false)
    expect(runtimeFailureAlreadyVisible(failure, [reply({ role: 'user' })])).toBe(false)
  })
})
