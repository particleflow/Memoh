import type { ChatMessage, SendMessageResult } from '@/store/chat/types'

// Deduplicate only the same identified run failure already visible at its
// reply. No translated-text matching and no blanket removal of composer errors.
export function runtimeFailureAlreadyVisible(failure: Pick<SendMessageResult, 'errorCode' | 'errorTurnId' | 'errorRunId'> | undefined, messages: readonly ChatMessage[]): boolean {
  if (failure?.errorCode !== 'runtime_run_failed' || (!failure.errorTurnId && !failure.errorRunId)) return false
  return messages.some(message => message.role === 'assistant'
    && (!failure.errorTurnId || message.turnId === failure.errorTurnId)
    && (!failure.errorRunId || message.runtimeRunId === failure.errorRunId)
    && message.messages.some(block => block.type === 'error' && block.code === 'runtime_run_failed'))
}
