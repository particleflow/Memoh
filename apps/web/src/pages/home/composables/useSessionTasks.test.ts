import { effectScope, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { SessiontaskTask } from '@memohai/sdk'
import { useSessionTasks, type TaskScope } from './useSessionTasks'

const api = vi.hoisted(() => ({ list: vi.fn(), detail: vi.fn() }))
vi.mock('@memohai/sdk', () => ({
  getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdObservation: vi.fn().mockResolvedValue({ data: { operation_id: 'operation', native: { availability: 'connection_unavailable' } } }),
  getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdDeliveries: vi.fn().mockResolvedValue({ data: { version: 1, operation_id: 'operation', files: [], has_more: false } }),
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
  const paused = ref(false)
  const reader = owner.run(() => useSessionTasks(scope, visible, paused))!
  return { owner, scope, visible, paused, reader }
}
async function settled() { await Promise.resolve(); await Promise.resolve() }
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

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

  it.each(['success', 'rejection'] as const)('pauses reads and discards a late %s before resuming the selected detail', async (outcome) => {
    const { reader, paused } = setup()
    await settled()
    reader.openTask(task)
    await settled()
    const old = deferred<{ data: SessiontaskTask }>()
    api.detail.mockReturnValueOnce(old.promise)
    void reader.refresh()
    const signal = api.detail.mock.lastCall![0].signal as AbortSignal
    paused.value = true
    expect(signal.aborted).toBe(true)
    expect(reader.detail.value).toBeUndefined()
    expect(reader.items.value).toEqual([])
    expect(reader.selected.value?.task_id).toBe(task.task_id)
    await reader.refresh()
    await vi.advanceTimersByTimeAsync(20000)
    expect(api.detail).toHaveBeenCalledTimes(2)

    const fresh = { ...task, status: 'waiting_decision', result_summary: undefined }
    api.detail.mockResolvedValue({ data: fresh })
    paused.value = false
    await settled()
    if (outcome === 'success') old.resolve({ data: task })
    else old.reject({ status: 403 })
    await settled()
    expect(reader.detail.value).toEqual(fresh)
    expect(reader.error.value).toBeUndefined()
    expect(api.list).toHaveBeenCalledTimes(1)
    expect(api.detail).toHaveBeenCalledTimes(3)
  })

  it('keeps the current page during a pause but erases navigation on a scope change', async () => {
    const { reader, paused, scope } = setup()
    await settled()
    reader.page('opaque-page-two')
    await settled()
    paused.value = true
    expect(reader.cursor.value).toBe('opaque-page-two')
    paused.value = false
    await settled()
    expect(api.list.mock.lastCall![0].query.cursor).toBe('opaque-page-two')
    reader.openTask(task)
    await settled()
    paused.value = true
    scope.value = { ...scope.value!, identity: 'another-identity' }
    expect(reader.selected.value).toBeUndefined()
    expect(reader.cursor.value).toBeUndefined()
    const calls = api.list.mock.calls.length + api.detail.mock.calls.length
    await vi.advanceTimersByTimeAsync(10000)
    expect(api.list.mock.calls.length + api.detail.mock.calls.length).toBe(calls)
    paused.value = false
    await settled()
    expect(api.list.mock.lastCall![0].query.cursor).toBeUndefined()
    expect(reader.detail.value).toBeUndefined()
  })

  describe.each(['identity', 'bot', 'session', 'selection'] as const)('%s changes', (field) => {
    it.each(['list', 'detail'] as const)('rejects a late %s success without replacing the new scope', async (kind) => {
      const { reader, scope } = setup()
      await settled()
      const old = deferred<{ data: SessiontaskTask & { items: SessiontaskTask[], next_cursor: string } }>()
      api[kind].mockReturnValueOnce(old.promise)
      if (kind === 'detail') reader.openTask(task)
      else void reader.refresh()
      const oldSignal = api[kind].mock.lastCall![0].signal as AbortSignal

      const fresh = { ...task, task_id: 'new-operation', result_summary: { ...task.result_summary!, text: 'new identity final' } }
      api.list.mockResolvedValue({ data: { items: [fresh] } })
      api.detail.mockResolvedValue({ data: fresh })
      scope.value = { ...scope.value!, [field]: 'changed' }
      expect(oldSignal.aborted).toBe(true)
      expect(reader.items.value).toEqual([])
      expect(reader.detail.value).toBeUndefined()
      expect(reader.selected.value).toBeUndefined()
      expect(reader.cursor.value).toBeUndefined()
      await settled()
      expect(reader.items.value).toEqual([fresh])
      reader.openTask(fresh)
      await settled()
      // Deliberately resolve despite abort: generation checks must also protect
      // against adapters/requests that cannot be cancelled in time.
      old.resolve({ data: { ...task, items: [task], next_cursor: 'old-cursor' } })
      await settled()
      expect(reader.detail.value).toEqual(fresh)
      expect(reader.nextCursor.value).toBeUndefined()
      expect(reader.error.value).toBeUndefined()
    })

    it.each(['list', 'detail'] as const)('ignores a late %s rejection after a fresh authorized result', async (kind) => {
      const { reader, scope } = setup()
      await settled()
      const old = deferred<never>()
      api[kind].mockReturnValueOnce(old.promise)
      if (kind === 'detail') reader.openTask(task)
      else void reader.refresh()
      scope.value = { ...scope.value!, [field]: 'changed' }
      await settled()
      reader.openTask(task)
      await settled()
      old.reject({ status: 401 })
      await settled()
      expect(reader.detail.value).toEqual(task)
      expect(reader.error.value).toBeUndefined()
      expect(reader.loading.value).toBe(false)
    })
  })

  it.each([401, 403, 404])('pauses automatic reads after %i until explicit reload', async (status) => {
    const { reader } = setup()
    await settled()
    reader.openTask(task)
    await settled()
    api.detail.mockRejectedValueOnce({ status })
    await reader.refresh()
    const calls = api.detail.mock.calls.length
    await vi.advanceTimersByTimeAsync(20000)
    expect(api.detail).toHaveBeenCalledTimes(calls)
    expect(reader.detail.value).toBeUndefined()
    await reader.refresh()
    expect(reader.detail.value).toEqual(task)
    await vi.advanceTimersByTimeAsync(5000)
    expect(api.detail).toHaveBeenCalledTimes(calls + 2)
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
