// @vitest-environment jsdom
import { effectScope, nextTick, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { SessiontaskTask } from '@memohai/sdk'
import { useComputerTaskObservation } from './useComputerTaskObservation'
import type { TaskScope } from './useSessionTasks'
const api = vi.hoisted(() => vi.fn())
vi.mock('@memohai/sdk', () => ({ getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdObservation: api }))
let owner: ReturnType<typeof effectScope>
const reply = (id = 'op-a', availability = 'online') => ({ data: { operation_id: id, state: 'unknown', business_outcome: 'unknown', native: { availability, agent_status: 'working', reference_match: 'observed' } } })
async function settle() { await nextTick(); await vi.advanceTimersByTimeAsync(0); await nextTick() }
function deferred() { let resolve!: (value: ReturnType<typeof reply>) => void; let reject!: (cause: unknown) => void; const promise = new Promise<ReturnType<typeof reply>>((yes, no) => { resolve = yes; reject = no }); return { promise, resolve, reject } }
function setup() {
  const scope = ref<TaskScope | null>({ bot: 'bot-a', session: 'root-a', selection: 'native', identity: 'synthetic' })
  const selected = ref<Pick<SessiontaskTask, 'task_type' | 'task_id'> | undefined>({ task_type: 'computer_operation', task_id: 'op-a' })
  const enabled = ref(true)
  owner = effectScope()
  const view = owner.run(() => useComputerTaskObservation(scope, selected, enabled))!
  return { scope, selected, enabled, view }
}
beforeEach(() => { vi.useFakeTimers(); api.mockReset().mockResolvedValue(reply()) })
afterEach(() => { owner?.stop(); vi.useRealTimers() })
describe('read-only current computer observation', () => {
  it('uses the authenticated session route, single flight and visible polling only', async () => {
    const pending = deferred(); api.mockReturnValueOnce(pending.promise)
    const { view, enabled } = setup(); await settle()
    await vi.advanceTimersByTimeAsync(5000); await view.value.refresh()
    expect(api).toHaveBeenCalledTimes(1)
    expect(api.mock.lastCall![0]).toMatchObject({ path: { bot_id: 'bot-a', session_id: 'root-a', task_type: 'computer_operation', task_id: 'op-a' }, cache: 'no-store' })
    pending.resolve(reply()); await settle()
    expect(view.value.data?.business_outcome).toBe('unknown')
    expect(view.value.receivedAt).toBeTruthy()
    await vi.advanceTimersByTimeAsync(5000); expect(api).toHaveBeenCalledTimes(2)
    enabled.value = false; await settle(); expect(view.value.data).toBeUndefined()
    await vi.advanceTimersByTimeAsync(15000); expect(api).toHaveBeenCalledTimes(2)
  })
  it.each([401, 403, 404])('clears a previous observation after %s and needs explicit reload', async status => {
    const { view } = setup(); await settle(); expect(view.value.data).toBeTruthy()
    api.mockRejectedValue({ status }); await vi.advanceTimersByTimeAsync(5000)
    expect(view.value.data).toBeUndefined(); expect(view.value.receivedAt).toBeUndefined()
    await vi.advanceTimersByTimeAsync(15000); expect(api).toHaveBeenCalledTimes(2)
    api.mockResolvedValue(reply()); await view.value.refresh(); expect(view.value.data).toBeTruthy()
  })
  it('clears a disconnected read and recovers via the existing poll without resubmission', async () => {
    const { view } = setup(); await settle()
    api.mockRejectedValueOnce(new TypeError('offline'))
    await vi.advanceTimersByTimeAsync(5000); expect(view.value.data).toBeUndefined()
    await vi.advanceTimersByTimeAsync(5000); expect(view.value.data?.native?.agent_status).toBe('working')
    expect(api).toHaveBeenCalledTimes(3)
  })
  it.each(['resolve', 'reject'] as const)('retires old %s after A → B → A, including identity changes', async finish => {
    const old = deferred(); api.mockReturnValueOnce(old.promise)
    const { scope, view } = setup(); await settle()
    const signal = api.mock.calls[0]![0].signal as AbortSignal
    scope.value = { bot: 'bot-b', session: 'root-b', selection: 'native', identity: 'other' }; await settle()
    scope.value = { bot: 'bot-a', session: 'root-a', selection: 'native', identity: 'new-login' }; await settle()
    expect(signal.aborted).toBe(true)
    if (finish === 'resolve') old.resolve(reply('op-a', 'offline'))
    else old.reject({ status: 403 })
    await settle()
    expect(view.value.data?.native?.availability).toBe('online'); expect(view.value.error).toBeUndefined()
  })
  it('rejects a mismatched operation and never observes subagents', async () => {
    api.mockResolvedValueOnce(reply('foreign-operation'))
    const { view, selected } = setup(); await settle()
    expect(view.value.data).toBeUndefined(); expect(view.value.error).toBeTruthy()
    selected.value = { task_type: 'subagent', task_id: 'child' }; await settle()
    await vi.advanceTimersByTimeAsync(10000); expect(api).toHaveBeenCalledTimes(1)
  })
})
