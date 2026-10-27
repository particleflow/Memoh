// @vitest-environment jsdom
import { effectScope, nextTick, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ChatAssistantTurn, ChatMessage } from '@/store/chat/types'
import { notifyAuthSessionCleared } from '@/lib/auth-session'
import { acceptedUserInputRecovery, useAcceptedUserInputRecovery, type UserInputRecoveryScope } from './useAcceptedUserInputRecovery'
const api = vi.hoisted(() => ({ recover: vi.fn(), observe: vi.fn() }))
vi.mock('@memohai/sdk', () => ({ postBotsByBotIdSessionsBySessionIdDecisionsByDecisionIdRecover: api.recover, getBotsByBotIdSessionsBySessionIdInvocationsByInvocationId: api.observe }))
const original = (): ChatAssistantTurn => ({ id: 'original-message', role: 'assistant', timestamp: '2026-10-27T07:00:00Z', streaming: false, turnId: 'original-turn', runtimeRunId: 'original-run', messages: [
  { id: 1, type: 'tool', name: 'ask_user', input: {}, tool_call_id: 'original-call', running: false, toolCallId: 'original-call', toolName: 'ask_user', result: null, done: true, userInput: { user_input_id: 'original-decision', status: 'submitted', answers: [] } },
  { id: 2, type: 'error', code: 'runtime_run_failed', content: '' },
] })
const admitted = { original_run_id: 'original-run', decision_id: 'original-decision', recovery_run_id: 'recovery-run', invocation_id: 'recovery-invocation', state: 'accepted', started: true, replayed: false }
const status = (state = 'running') => ({ data: { found: true, session_id: 'session', invocation_id: 'recovery-invocation', run_id: 'recovery-run', state } })
const owners: ReturnType<typeof effectScope>[] = []
function mount() {
  const scope = ref<UserInputRecoveryScope>({ bot: 'bot', session: 'session', enabled: true, visible: true }), message = ref<ChatMessage>(original()), token = ref('synthetic-account')
  const owner = effectScope(); owners.push(owner)
  return { scope, message, token, recovery: owner.run(() => useAcceptedUserInputRecovery(scope, message, token))! }
}
async function settle() { for (let i = 0; i < 15; i++) await Promise.resolve(); await nextTick() }
beforeEach(() => { vi.useFakeTimers(); api.recover.mockReset().mockResolvedValue({ data: admitted }); api.observe.mockReset().mockResolvedValue(status()) })
afterEach(() => { owners.splice(0).forEach(owner => owner.stop()); vi.useRealTimers() })
describe('accepted-answer recovery', () => {
  it('requires the same failed reply and exactly one submitted decision, not an unanswered input', () => {
    expect(acceptedUserInputRecovery(original())).toEqual({ run: 'original-run', decision: 'original-decision' })
    for (const state of ['pending', 'canceled', 'expired']) {
      const message = original(); const tool = message.messages[0]; if (tool?.type === 'tool' && tool.userInput) tool.userInput.status = state
      expect(acceptedUserInputRecovery(message)).toBeUndefined()
    }
    const message = original(); message.runtimeRunId = undefined
    expect(acceptedUserInputRecovery(message)).toBeUndefined()
    expect(acceptedUserInputRecovery(message, 'verified-original-run')?.run).toBe('verified-original-run')
  })
  it('does not mutate history or automatically submit on mount; a double click sends one original identity request', async () => {
    let done!: (value: unknown) => void; api.recover.mockImplementationOnce(() => new Promise(resolve => { done = resolve }))
    const { recovery, message } = mount(), snapshot = JSON.stringify(message.value)
    expect(api.recover).not.toHaveBeenCalled()
    const pending = recovery.recover(); await recovery.recover()
    expect(api.recover).toHaveBeenCalledTimes(1)
    expect(api.recover.mock.calls[0]![0]).toMatchObject({ path: { bot_id: 'bot', session_id: 'session', decision_id: 'original-decision' }, body: { original_run_id: 'original-run' } })
    done({ data: admitted }); await pending; await settle()
    expect(recovery.state.value).toBe('running'); expect(recovery.receipt.value?.recovery_run_id).toBe('recovery-run')
    expect(JSON.stringify(message.value)).toBe(snapshot)
  })
  it('202 is admission only; later clicks and reconnect observe the same run without repeating POST', async () => {
    api.observe.mockResolvedValue(status('accepted'))
    const { recovery } = mount(); await recovery.recover(); await settle()
    expect(recovery.state.value).toBe('accepted'); expect(recovery.state.value).not.toBe('completed')
    await recovery.recover(); await settle(); expect(api.recover).toHaveBeenCalledTimes(1)
    api.observe.mockRejectedValueOnce({ status: 503 }); await vi.advanceTimersByTimeAsync(5000); await settle()
    expect(recovery.state.value).toBe('unknown')
    api.observe.mockResolvedValue(status('completed')); await vi.advanceTimersByTimeAsync(5000); await settle()
    expect(recovery.state.value).toBe('completed'); expect(api.recover).toHaveBeenCalledTimes(1)
    expect(api.observe.mock.calls.every(([call]) => call.path.invocation_id === 'recovery-invocation' && call.path.session_id === 'session')).toBe(true)
  })
  it.each([401, 403, 404, 409])('keeps denial %s inline and never falls back to an answer or whole-turn retry', async status => {
    api.recover.mockRejectedValue({ status }); const { recovery, message } = mount(), before = JSON.stringify(message.value)
    await recovery.recover(); await recovery.recover(); await vi.advanceTimersByTimeAsync(15000)
    expect(recovery.blocked.value).toBe(true); expect(api.recover).toHaveBeenCalledTimes(1); expect(api.observe).not.toHaveBeenCalled()
    expect(JSON.stringify(message.value)).toBe(before)
  })
  it('does not automatically repeat an unknown POST; explicit retry retains the exact original decision/run', async () => {
    api.recover.mockRejectedValueOnce(new TypeError('synthetic connection lost')).mockResolvedValueOnce({ data: { ...admitted, started: false, replayed: true } })
    const { recovery } = mount(); await recovery.recover(); await vi.advanceTimersByTimeAsync(15000)
    expect(recovery.state.value).toBe('unknown'); expect(api.recover).toHaveBeenCalledTimes(1)
    await recovery.recover(); await settle(); expect(recovery.receipt.value?.recovery_run_id).toBe('recovery-run')
    expect(api.recover.mock.calls.map(([call]) => call.body)).toEqual([{ original_run_id: 'original-run' }, { original_run_id: 'original-run' }])
    expect(api.recover.mock.calls.every(([call]) => call.path.decision_id === 'original-decision')).toBe(true)
  })
  it('does not infer accepted state from the raw stale transport field', () => {
    const message = original(), block = message.messages[0]
    if (block?.type !== 'tool' || !block.userInput) throw new Error('Expected input fixture')
    block.user_input = { ...block.userInput, status: 'submitted' }; block.userInput.status = 'pending'
    expect(acceptedUserInputRecovery(message)).toBeUndefined()
  })
  it.each(['bot', 'session'] as const)('ignores a late receipt after switching %s or logging in again with the same token', async field => {
    let done!: (value: unknown) => void; api.recover.mockImplementationOnce(() => new Promise(resolve => { done = resolve }))
    const { recovery, scope } = mount(); const pending = recovery.recover(); scope.value = { ...scope.value, [field]: 'another-scope' }
    done({ data: admitted }); await pending; await settle(); expect(recovery.receipt.value).toBeUndefined(); expect(api.observe).not.toHaveBeenCalled()
    await recovery.recover(); await settle(); notifyAuthSessionCleared('login'); expect(recovery.receipt.value).toBeUndefined(); expect(recovery.state.value).toBe('')
  })
  it('rejects foreign response identity and retains a failed recovery without resubmission', async () => {
    api.recover.mockResolvedValueOnce({ data: { ...admitted, decision_id: 'foreign' } })
    const first = mount(); await first.recovery.recover(); expect(first.recovery.blocked.value).toBe(true); expect(api.observe).not.toHaveBeenCalled()
    const second = mount(); api.observe.mockResolvedValue(status('failed')); await second.recovery.recover(); await settle()
    expect(second.recovery.state.value).toBe('failed'); const count = api.recover.mock.calls.length
    await second.recovery.recover(); expect(api.recover).toHaveBeenCalledTimes(count); expect(second.message.value).toEqual(original())
  })
  it('retains admission across transcript refresh and view hiding, without creating a new recovery', async () => {
    const { recovery, scope, message } = mount(); await recovery.recover(); await settle()
    message.value = JSON.parse(JSON.stringify(message.value)); await settle(); expect(recovery.receipt.value?.recovery_run_id).toBe('recovery-run')
    scope.value = { ...scope.value, visible: false }; await vi.advanceTimersByTimeAsync(10000); const count = api.observe.mock.calls.length
    scope.value = { ...scope.value, visible: true }; await settle(); expect(api.observe).toHaveBeenCalledTimes(count + 1); expect(api.recover).toHaveBeenCalledTimes(1)
  })
})
