import { effectScope, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { BotdelegationView, SessiontaskTask } from '@memohai/sdk'
import { useSessionTasks, type TaskScope } from './useSessionTasks'
import { useBotDelegations } from './useBotDelegations'

const api = vi.hoisted(() => ({ tasks: vi.fn(), task: vi.fn(), delegations: vi.fn(), delegation: vi.fn() }))
vi.mock('@memohai/sdk', () => ({
  getBotsByBotIdSessionsBySessionIdTasks: api.tasks,
  getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskId: api.task,
  getBotsByBotIdSessionsBySessionIdDelegations: api.delegations,
  getBotsByBotIdSessionsBySessionIdDelegationsByDelegationId: api.delegation,
}))
const task: SessiontaskTask = {
  task_type: 'computer_operation', task_id: '55555555-5555-4555-8555-555555555555',
  session_id: '33333333-3333-4333-8333-333333333333', parent_session_id: '22222222-2222-4222-8222-222222222222',
  created_at: '2026-10-24T00:00:00Z', updated_at: '2026-10-24T00:00:00Z',
  status: 'process_exited', summary: 'bounded receipt', content_available: true,
  attachments_available: false, pi_result: 'incomplete',
}
const delegation: BotdelegationView = {
  delegation_id: '66666666-6666-4666-8666-666666666666', request_id: '77777777-7777-4777-8777-777777777777',
  source_session_id: task.parent_session_id, target_bot_id: '44444444-4444-4444-8444-444444444444',
  state: 'failed', delivery_state: 'pending', attachments: [],
  created_at: task.created_at, updated_at: task.updated_at,
}
const owners: ReturnType<typeof effectScope>[] = []
const opaque = 'opaque-second-page+/=keep-exact';
function setup(kind: 'tasks' | 'delegations') {
  const owner = effectScope(); owners.push(owner)
  const scope = ref<TaskScope | null>({
    bot: '11111111-1111-4111-8111-111111111111', session: task.parent_session_id,
    selection: 'native', identity: 'synthetic-audit-identity',
  })
  const visible = ref(true)
  if (kind === 'tasks') {
    const reader = owner.run(() => useSessionTasks(scope, visible))!
    return { owner, reader, open: () => reader.openTask(task) }
  }
  const reader = owner.run(() => useBotDelegations(scope, visible))!
  return { owner, reader, open: () => reader.openTask(delegation) }
}
async function settled() { await Promise.resolve(); await Promise.resolve() }
beforeEach(() => {
  vi.useFakeTimers()
  api.tasks.mockReset().mockResolvedValue({ data: { items: [task] } })
  api.task.mockReset().mockResolvedValue({ data: task })
  api.delegations.mockReset().mockResolvedValue({ data: { items: [delegation] } })
  api.delegation.mockReset().mockResolvedValue({ data: delegation })
})
afterEach(() => { owners.splice(0).forEach(owner => owner.stop()); vi.useRealTimers() })

describe.each(['tasks', 'delegations'] as const)('%s existing HTTP page contract', (kind) => {
  const fixture = kind === 'tasks' ? task : delegation
  const list = () => api[kind]

  it('reads only the requested bounded page and does not accumulate or drain the queue', async () => {
    const first = Array.from({ length: 20 }, () => ({ ...fixture }))
    list().mockResolvedValueOnce({ data: { items: first, next_cursor: opaque } })
    const { reader } = setup(kind); await settled()
    expect(list()).toHaveBeenCalledTimes(1)
    expect(reader.items.value).toHaveLength(20)
    expect(reader.nextCursor.value).toBe(opaque)
    reader.page(reader.nextCursor.value); await settled()
    expect(list()).toHaveBeenCalledTimes(2)
    expect(list()).toHaveBeenLastCalledWith(expect.objectContaining({ query: { limit: 20, cursor: opaque } }))
    expect(reader.items.value).toEqual([fixture])
    expect(reader.nextCursor.value).toBeUndefined()
  })

  it('continues an empty filtered page without interpreting it as exhaustion', async () => {
    list().mockResolvedValueOnce({ data: { items: [], next_cursor: opaque } })
    const { reader } = setup(kind); await settled()
    expect(reader.items.value).toEqual([])
    expect(reader.nextCursor.value).toBe(opaque)
    reader.page(reader.nextCursor.value); await settled()
    expect(reader.items.value).toEqual([fixture])
  })

  it('clears stale content on a later-page failure and reloads the same cursor', async () => {
    list().mockResolvedValueOnce({ data: { items: [fixture], next_cursor: opaque } })
    const { reader } = setup(kind); await settled()
    list().mockRejectedValueOnce({ status: 503 })
    reader.page(reader.nextCursor.value); await settled()
    expect(reader.cursor.value).toBe(opaque)
    expect(reader.items.value).toEqual([])
    expect(reader.error.value).toEqual({ status: 503 })
    expect(reader.nextCursor.value).toBeUndefined()
    await reader.refresh()
    expect(list()).toHaveBeenLastCalledWith(expect.objectContaining({ query: { limit: 20, cursor: opaque } }))
    expect(reader.items.value).toEqual([fixture])
    expect(reader.error.value).toBeUndefined()
  })

  it('retains the current page when returning from a task detail in the same mounted view', async () => {
    const { reader, open } = setup(kind); await settled()
    reader.page(opaque); await settled()
    open(); await settled()
    reader.back(); await settled()
    expect(list()).toHaveBeenLastCalledWith(expect.objectContaining({ query: { limit: 20, cursor: opaque } }))
  })

  it('starts a fresh authorized first page after disposal/remount, without claiming durable cursor recovery', async () => {
    const old = setup(kind); await settled()
    old.reader.page(opaque); await settled(); old.open(); await settled()
    old.owner.stop()
    const fresh = setup(kind); await settled()
    expect(fresh.reader.cursor.value).toBeUndefined()
    expect(fresh.reader.selected.value).toBeUndefined()
    expect(list()).toHaveBeenLastCalledWith(expect.objectContaining({ query: { limit: 20, cursor: undefined } }))
    expect(fresh.reader.items.value).toEqual([fixture])
  })

  it('preserves individual failed or incomplete outcomes without an aggregate completion result', async () => {
    const successful = kind === 'tasks' ? { ...task, pi_result: 'succeeded' } : { ...delegation, state: 'completed', delivery_state: 'pending' }
    list().mockResolvedValueOnce({ data: { items: [fixture, successful] } })
    const { reader } = setup(kind); await settled()
    expect(reader.items.value).toEqual([fixture, successful])
    expect(reader).not.toHaveProperty('complete')
    expect(reader).not.toHaveProperty('has_more')
    expect(list()).toHaveBeenCalledTimes(1)
  })
})
