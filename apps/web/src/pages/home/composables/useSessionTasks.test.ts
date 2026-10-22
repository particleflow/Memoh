import { effectScope, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { SessiontaskTask } from '@memohai/sdk'
import { useSessionTasks, type TaskScope } from './useSessionTasks'

const api = vi.hoisted(() => ({ list: vi.fn(), detail: vi.fn() }))
vi.mock('@memohai/sdk', () => ({
  getBotsByBotIdSessionsBySessionIdTasks: api.list,
  getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskId: api.detail,
}))

const task: SessiontaskTask = {
  task_type: 'computer_operation', task_id: 'operation', session_id: 'child', parent_session_id: 'parent',
  created_at: '2026-10-22T00:00:00Z', updated_at: '2026-10-22T00:00:00Z', status: 'process_exited',
  summary: 'receipt', content_available: true, attachments_available: false, pi_result: 'succeeded',
  result_summary: { text: 'final text', truncated: false, source: 'pi_final_assistant' },
}
const scopes: ReturnType<typeof effectScope>[] = []
function setup() {
  const owner = effectScope()
  scopes.push(owner)
  const scope = ref<TaskScope | null>({ bot: 'bot', session: 'parent', selection: 'pc-a', identity: 'synthetic' })
  const visible = ref(true)
  const reader = owner.run(() => useSessionTasks(scope, visible))!
  return { owner, scope, visible, reader }
}
async function settled() { await Promise.resolve(); await Promise.resolve() }

describe('authorization-scoped task view', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    api.list.mockReset().mockResolvedValue({ data: { items: [task] } })
    api.detail.mockReset().mockResolvedValue({ data: task })
  })
  afterEach(() => { scopes.splice(0).forEach(s => s.stop()); vi.useRealTimers() })

  it('loads fresh detail and erases summary after denial without fallback', async () => {
    const { reader } = setup()
    await settled()
    reader.openTask(task)
    await settled()
    expect(reader.detail.value?.result_summary?.text).toBe('final text')
    expect(reader.selected.value).toEqual({ task_type: 'computer_operation', task_id: 'operation' })
    api.detail.mockRejectedValueOnce({ status: 404, code: 'session.not_found' })
    await reader.refresh()
    expect(reader.detail.value).toBeUndefined()
    expect(reader.items.value).toEqual([])
    expect(reader.error.value).toEqual({ status: 404, code: 'session.not_found' })
    expect(api.detail).toHaveBeenCalledWith(expect.objectContaining({ cache: 'no-store', path: { bot_id: 'bot', session_id: 'parent', task_type: 'computer_operation', task_id: 'operation' } }))
  })

  it('ignores a late authorized response after scope changes or close', async () => {
    let finish!: (value: { data: SessiontaskTask }) => void
    api.detail.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const { reader, scope, visible } = setup()
    await settled()
    reader.openTask(task)
    scope.value = { ...scope.value!, selection: 'pc-b' }
    await settled()
    visible.value = false
    finish({ data: task })
    await settled()
    expect(reader.detail.value).toBeUndefined()
    expect(reader.items.value).toEqual([])
    expect(reader.selected.value).toBeUndefined()
    const calls = api.list.mock.calls.length
    await vi.advanceTimersByTimeAsync(15000)
    expect(api.list).toHaveBeenCalledTimes(calls)
  })

  it('keeps an empty scan page continuation and sends it unchanged', async () => {
    api.list.mockResolvedValueOnce({ data: { items: [], next_cursor: 'opaque' } })
    const { reader } = setup()
    await settled()
    expect(reader.nextCursor.value).toBe('opaque')
    reader.page(reader.nextCursor.value)
    await settled()
    expect(api.list).toHaveBeenLastCalledWith(expect.objectContaining({ query: { limit: 20, cursor: 'opaque' } }))
    expect(reader.items.value).toEqual([task])
    reader.page()
    await settled()
    expect(reader.cursor.value).toBeUndefined()
  })

  it('clears on every failed refresh and refreshes only while visible', async () => {
    const { reader, visible, owner } = setup()
    await settled()
    api.list.mockRejectedValueOnce({ status: 403 })
    await vi.advanceTimersByTimeAsync(5000)
    expect(reader.items.value).toEqual([])
    visible.value = false
    const calls = api.list.mock.calls.length
    await vi.advanceTimersByTimeAsync(5000)
    expect(api.list).toHaveBeenCalledTimes(calls)
    visible.value = true
    await settled()
    expect(reader.items.value).toEqual([task])
    owner.stop()
    expect(reader.items.value).toEqual([])
  })
})
