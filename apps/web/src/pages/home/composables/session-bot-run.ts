import type { RuntimeCurrentRunView, UIMessage } from '@/composables/api/useChat.types'
import type { ChatMessage, ToolCallBlock } from '@/store/chat/types'
import { isRuntimeRunActive, runOwnsTurn } from '@/store/chat/runtime-projection'
import { hasToolResultError } from '../components/tool-result-error'

// Read only the existing native run and its exact turn identities. No timestamps,
// tool names or a parent's completion are used to invent a task relationship.
export function sessionBotRunContent(run: RuntimeCurrentRunView, history: ChatMessage[]) {
  const owned = history.filter(turn => turn.role !== 'system'
    && (turn.runtimeRunId === run.run_id || runOwnsTurn(run, turn.turnId ?? '')))
  // The chat transcript already reconciles terminal snapshots with persisted
  // receipts. Prefer that exact run's reconciled turns, without reimplementing
  // receipt recovery in this read-only view.
  const reconciled = owned.flatMap(turn => turn.role === 'assistant' && turn.runtimeRunId === run.run_id ? turn.messages : [])
  const messages: UIMessage[] = reconciled.length ? reconciled : run.messages?.length ? run.messages : owned.flatMap(turn =>
    turn.role === 'assistant' ? turn.messages : [])
  const prompt = (run.user_turns ?? (run.request_user_turn ? [run.request_user_turn] : []))
    .map(turn => turn.text).filter(Boolean).join('\n')
    || owned.filter(turn => turn.role === 'user').map(turn => turn.text).join('\n')
  const text = messages.filter(message => message.type === 'text').map(message => message.content).join('\n')
  const tools = messages.flatMap(message => {
    if (message.type !== 'tool') return []
    const result = message.output ?? ('result' in message ? message.result : undefined)
    const received = result !== undefined && result !== null
    const block: ToolCallBlock = { ...message, toolCallId: message.tool_call_id, toolName: message.name, result: result ?? null, done: received }
    const state = hasToolResultError(block) ? 'failed'
      : received ? 'received'
        : isRuntimeRunActive(run.status) && message.running ? 'running' : 'unconfirmed'
    return [{ block, state }]
  })
  return { prompt, text, tools }
}

export function sessionBotRunStatus(status: RuntimeCurrentRunView['status']) {
  if (status === 'admitting') return 'accepted'
  if (status === 'errored') return 'failed'
  if (status === 'aborting') return 'aborting'
  return status
}
