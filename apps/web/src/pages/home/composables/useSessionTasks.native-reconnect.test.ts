// @vitest-environment jsdom
import { effectScope, nextTick, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { SessiontaskTask } from '@memohai/sdk'
import { useSessionTasks, type TaskScope } from './useSessionTasks'

const api = vi.hoisted(() => ({ list: vi.fn(), detail: vi.fn(), read: vi.fn(), send: vi.fn() }))
vi.mock('@memohai/sdk', () => ({
  getBotsByBotIdSessionsBySessionIdTasks: api.list,
  getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskId: api.detail,
  getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdContent: api.read,
  postBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdFollowup: api.send,
  getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdObservation: vi.fn().mockResolvedValue({ data: { operation_id: 'operation', native: { availability: 'online' } } }),
  getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdDeliveries: vi.fn().mockResolvedValue({ data: { version: 1, operation_id: 'operation', files: [], has_more: false } }),
}))
const task: SessiontaskTask = {
  task_type: 'computer_operation', task_id: 'operation', session_id: 'child', parent_session_id: 'specialist',
  created_at: '2026-10-22T00:00:00Z', updated_at: '2026-10-22T00:00:00Z', status: 'submitted',
  summary: 'Submitted synthetic task', content_available: false, attachments_available: false,
}
const content = () => ({ data: { operation_id: task.task_id, supported: true, followup_available: true,
  observation: { output: 'synthetic live sample', output_fidelity: 'terminal_snapshot_unverified', task_success_verified: false },
  followups: [] as { request_id: string; state: string }[],
} })
let owner: ReturnType<typeof effectScope>
async function settle() { await nextTick(); await vi.advanceTimersByTimeAsync(0); await nextTick() }
async function setup() {
  owner = effectScope()
  const scope = ref<TaskScope | null>({ bot: 'specialist-bot', session: 'specialist', selection: 'pc-a', identity: 'synthetic-login' })
  const visible = ref(true), paused = ref(false)
  const reader = owner.run(() => useSessionTasks(scope, visible, paused))!
  await settle(); reader.openTask(task); await settle()
  await reader.nativeTask.value.open()
  expect(reader.nativeTask.value.data?.observation?.output).toBe('synthetic live sample')
  return { reader, scope, visible, paused }
}
beforeEach(() => {
  vi.useFakeTimers(); sessionStorage.clear()
  vi.stubGlobal('crypto', { subtle: { digest: vi.fn(async (_: string, bytes: Uint8Array) => bytes.buffer) }, randomUUID: vi.fn(() => '00000000-0000-4000-8000-000000000001') })
  api.list.mockReset().mockResolvedValue({ data: { items: [task] } })
  api.detail.mockReset().mockResolvedValue({ data: task })
  api.read.mockReset().mockResolvedValue(content()); api.send.mockReset()
})
afterEach(() => { owner?.stop(); vi.restoreAllMocks(); vi.unstubAllGlobals(); vi.useRealTimers() })

describe('native content within the authorized task reader', () => {
  it('resumes an opened sample only after the same parent task recovers from a failed detail read', async () => {
    const { reader } = await setup()
    api.detail.mockRejectedValueOnce({ status: 503 })
    await reader.refresh(); await settle()
    expect(reader.detail.value).toBeUndefined()
    expect(reader.nativeTask.value.data).toBeUndefined()
    await reader.nativeTask.value.refresh()
    expect(api.read).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(5000); await settle()
    expect(reader.detail.value?.status).toBe('submitted')
    expect(reader.nativeTask.value.opened).toBe(true)
    expect(reader.nativeTask.value.data?.observation?.task_success_verified).toBe(false)
    expect(api.read).toHaveBeenLastCalledWith(expect.objectContaining({ path: {
      bot_id: 'specialist-bot', session_id: 'specialist', task_type: 'computer_operation', task_id: 'operation',
    } }))
    expect(api.send).not.toHaveBeenCalled()
  })

  it('retains the unknown follow-up UUID through parent revalidation without another prompt', async () => {
    const { reader, paused } = await setup()
    api.send.mockRejectedValueOnce(new TypeError('lost confirmation'))
    await reader.nativeTask.value.send('synthetic instruction', 'bound_session')
    const id = reader.nativeTask.value.requestId
    expect(id).toBeTruthy()
    paused.value = true; await settle()
    expect(reader.nativeTask.value.data).toBeUndefined()
    await reader.nativeTask.value.send('must not resend', 'bound_session')
    paused.value = false; await settle()
    expect(reader.nativeTask.value.opened).toBe(true)
    expect(reader.nativeTask.value.requestId).toBe(id)
    await reader.nativeTask.value.send('must still not resend', 'bound_session')
    expect(api.send).toHaveBeenCalledTimes(1)
    expect(reader.detail.value?.status).toBe('submitted')
  })

  it.each([401, 403, 404])('stops reads on parent %s until explicit successful revalidation', async status => {
    const { reader } = await setup()
    api.detail.mockRejectedValueOnce({ status })
    await reader.refresh(); await settle()
    const count = api.detail.mock.calls.length
    await vi.advanceTimersByTimeAsync(20000)
    expect(api.detail).toHaveBeenCalledTimes(count)
    expect(api.read).toHaveBeenCalledTimes(1)
    expect(reader.nativeTask.value.data).toBeUndefined()
    await reader.nativeTask.value.send('denied input', 'current_agent')
    expect(api.send).not.toHaveBeenCalled()
    await reader.refresh(); await settle()
    expect(reader.nativeTask.value.data?.observation?.output).toBe('synthetic live sample')
  })

  it.each(['close', 'identity', 'task'] as const)('does not restore a prior sample after %s during disconnection', async change => {
    const { reader, scope, visible } = await setup()
    api.detail.mockRejectedValueOnce(new TypeError('offline'))
    await reader.refresh(); await settle()
    if (change === 'close') visible.value = false
    else if (change === 'identity') scope.value = { ...scope.value!, identity: 'another-login' }
    else reader.openTask({ ...task, task_id: 'other-operation' })
    await settle()
    if (change === 'close') visible.value = true
    await settle(); await vi.advanceTimersByTimeAsync(5000)
    expect(reader.nativeTask.value.opened).toBe(false)
    expect(reader.nativeTask.value.data).toBeUndefined()
    expect(api.read).toHaveBeenCalledTimes(1)
  })

  it('rejects a late sample after parent invalidation and reads fresh data on recovery', async () => {
    const { reader, paused } = await setup()
    let finish!: (value: ReturnType<typeof content>) => void
    api.read.mockReturnValueOnce(new Promise<ReturnType<typeof content>>(resolve => { finish = resolve }))
    const pending = reader.nativeTask.value.refresh(); await settle()
    const signal = api.read.mock.lastCall![0].signal as AbortSignal
    paused.value = true; await settle()
    expect(signal.aborted).toBe(true)
    const stale = content(); stale.data.observation.output = 'stale sample'
    finish(stale); await pending
    expect(reader.nativeTask.value.data).toBeUndefined()
    paused.value = false; await settle()
    expect(reader.nativeTask.value.data?.observation?.output).toBe('synthetic live sample')
    expect(api.send).not.toHaveBeenCalled()
  })

  it('does not replace an uncertain in-flight UUID when the parent disconnects', async () => {
    const { reader, paused } = await setup()
    let finish!: (value: { data: { operation_id: string; state: string } }) => void
    api.send.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const pending = reader.nativeTask.value.send('synthetic instruction', 'bound_session'); await settle()
    const id = reader.nativeTask.value.requestId
    const signal = api.send.mock.lastCall![0].signal as AbortSignal
    paused.value = true; await settle()
    expect(signal.aborted).toBe(true)
    paused.value = false; await settle()
    expect(reader.nativeTask.value.requestId).toBe(id)
    finish({ data: { operation_id: task.task_id, state: 'accepted' } }); await pending
    expect(reader.nativeTask.value.response).toBeUndefined()
    await reader.nativeTask.value.send('must not repeat', 'bound_session')
    expect(api.send).toHaveBeenCalledTimes(1)
    const fresh = content(); fresh.data.followups = [{ request_id: id!, state: 'accepted' }]
    api.read.mockResolvedValue(fresh)
    await reader.nativeTask.value.refresh()
    expect(reader.nativeTask.value.requestId).toBeUndefined()
    expect(reader.nativeTask.value.response?.state).toBe('accepted')
    expect(reader.detail.value?.status).toBe('submitted')
  })
})
