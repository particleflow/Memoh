import { describe, expect, it } from 'vitest'
import type { RuntimeCurrentRunView, UIToolMessage } from '@/composables/api/useChat.types'
import type { ChatMessage, ToolCallBlock } from '@/store/chat/types'
import { sessionBotRunContent, sessionBotRunStatus } from './session-bot-run'

const call: UIToolMessage = { id: 1, type: 'tool', name: 'generate_image', input: { prompt: 'Synthetic mountain' }, tool_call_id: 'call-a', running: true }
const run: RuntimeCurrentRunView = { run_id: 'run-a', turn_id: 'turn-a', generation: 'generation-a', status: 'running', started_at: '2026-10-25T00:00:00Z', updated_at: '2026-10-25T00:00:01Z', messages: [call] }
const block: ToolCallBlock = { ...call, toolCallId: call.tool_call_id, toolName: call.name, done: false, result: null }
const history: ChatMessage[] = [
  { id: 'user-a', role: 'user', turnId: 'turn-a', text: 'Synthetic prompt', timestamp: run.started_at, streaming: false, isSelf: true, attachments: [] },
  { id: 'assistant-a', role: 'assistant', turnId: 'turn-a', timestamp: run.updated_at, streaming: false, messages: [block, { id: 2, type: 'text', content: 'Visible reply' }, { id: 3, type: 'reasoning', content: 'PRIVATE reasoning' }] },
  { id: 'assistant-b', role: 'assistant', turnId: 'turn-b', runtimeRunId: 'run-b', timestamp: run.updated_at, streaming: false, messages: [{ ...block, input: { prompt: 'Unrelated same-name call' } }] },
]

describe('native Bot run content projection', () => {
  it.each(['aborted', 'completed', 'errored', 'lost'] as const)('does not confirm a missing tool result from run state %s', status => {
    const result = sessionBotRunContent({ ...run, status }, history)
    expect(result.tools[0]?.state).toBe('unconfirmed')
    expect(result.tools[0]?.block.done).toBe(false)
    expect(result.tools[0]?.block.toolCallId).toBe('call-a')
  })
  it('uses actual live tool state and result flags independently of the parent', () => {
    expect(sessionBotRunContent(run, []).tools[0]?.state).toBe('running')
    expect(sessionBotRunContent({ ...run, messages: [{ ...call, running: false }] }, []).tools[0]?.state).toBe('unconfirmed')
    expect(sessionBotRunContent({ ...run, messages: [{ ...call, output: { content: 'Result' } }] }, []).tools[0]?.state).toBe('received')
    expect(sessionBotRunContent({ ...run, messages: [{ ...call, output: { isError: true, content: 'Failure' } }] }, []).tools[0]?.state).toBe('failed')
  })
  it('recovers exact persisted turn content when a ledger snapshot has no messages', () => {
    const result = sessionBotRunContent({ ...run, status: 'aborted', messages: [] }, history)
    expect(result.prompt).toBe('Synthetic prompt')
    expect(result.text).toBe('Visible reply')
    expect(result.tools).toHaveLength(1)
    expect(result.tools[0]?.block.input).toEqual(call.input)
    expect(JSON.stringify(result)).not.toContain('PRIVATE reasoning')
    expect(JSON.stringify(result)).not.toContain('Unrelated same-name call')
  })
  it('does not merge old persisted tools into an authoritative live snapshot', () => {
    const result = sessionBotRunContent({ ...run, messages: [{ id: 4, type: 'text', content: 'Current reply' }] }, history)
    expect(result.text).toBe('Current reply')
    expect(result.tools).toEqual([])
  })
  it('uses the existing exact-run transcript reconciliation for a preserved receipt', () => {
    const known: ChatMessage = { id: 'known', role: 'assistant', turnId: run.turn_id, runtimeRunId: run.run_id,
      timestamp: run.updated_at, streaming: false, messages: [{ ...block, done: true, running: false, result: { content: 'Persisted receipt' } }] }
    const result = sessionBotRunContent({ ...run, status: 'aborted' }, [known])
    expect(result.tools[0]?.state).toBe('received')
    expect(result.tools[0]?.block.result).toEqual({ content: 'Persisted receipt' })
  })
  it('retains runtime status distinctions without inventing success', () => {
    expect(sessionBotRunStatus('admitting')).toBe('accepted')
    expect(sessionBotRunStatus('errored')).toBe('failed')
    expect(sessionBotRunStatus('aborting')).toBe('aborting')
    expect(sessionBotRunStatus('lost')).toBe('lost')
  })
})
