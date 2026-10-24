import { effectScope, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ComputertaskDeliveryPage, ComputertaskDeliveryPageFile, SessiontaskTask } from '@memohai/sdk'
import { useComputerTaskDeliveries } from './useComputerTaskDeliveries'
import type { TaskScope } from './useSessionTasks'

const api = vi.hoisted(() => vi.fn())
vi.mock('@memohai/sdk', () => ({ getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdDeliveries: api }))
const reference = { version: 1, direction: 'upload', content_hash: 'a'.repeat(64), size_bytes: 42, mime: 'text/plain' }
const file = (id: string, state = 'committed'): ComputertaskDeliveryPageFile => ({ delivery_id: id, state, reference })
const page = (files: ComputertaskDeliveryPageFile[], has_more = false, next_after = '') => ({
  data: { version: 1, operation_id: 'operation', files, has_more, next_after } satisfies ComputertaskDeliveryPage,
})
const owners: ReturnType<typeof effectScope>[] = []
function setup() {
  const owner = effectScope(); owners.push(owner)
  const scope = ref<TaskScope | null>({ bot: 'bot', session: 'session', selection: 'pc', identity: 'synthetic' })
  const selected = ref<Pick<SessiontaskTask, 'task_type' | 'task_id'>>({ task_type: 'computer_operation', task_id: 'operation' })
  const enabled = ref(true)
  const reader = owner.run(() => useComputerTaskDeliveries(scope, selected, enabled))!
  return { owner, scope, selected, enabled, reader }
}
async function settled() { for (let n = 0; n < 8; n++) await Promise.resolve() }
function deferred() {
  let resolve!: (v: ReturnType<typeof page>) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<ReturnType<typeof page>>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
beforeEach(() => { vi.useFakeTimers(); api.mockReset().mockResolvedValue(page([])) })
afterEach(() => { owners.splice(0).forEach(s => s.stop()); vi.useRealTimers() })

describe('computer delivery metadata observation', () => {
  it('automatically drains pages serially, strips uncommitted references and does not complete a task', async () => {
    const tail = deferred()
    api.mockResolvedValueOnce(page([file('first'), file('waiting', 'pending')], true, '2')).mockReturnValueOnce(tail.promise)
    const { reader } = setup(); await settled()
    expect(reader.value.files).toEqual([file('first'), { delivery_id: 'waiting', state: 'pending', reference: undefined }])
    expect(reader.value.loading).toBe(true)
    expect(api).toHaveBeenCalledTimes(2)
    expect(api.mock.lastCall![0]).toMatchObject({ path: { bot_id: 'bot', session_id: 'session', task_type: 'computer_operation', task_id: 'operation' }, query: { after: '2' }, cache: 'no-store' })
    tail.resolve(page([file('last', 'failed')], false, '3')); await settled()
    expect(reader.value.loaded).toBe(true)
    expect(reader.value.files.map(f => f.state)).toEqual(['committed', 'pending', 'failed'])
    expect(reader.value).not.toHaveProperty('taskStatus')
    expect(reader.value).not.toHaveProperty('download')
  })

  it('keeps the completed view during page refreshes, then replaces it atomically', async () => {
    api.mockResolvedValueOnce(page([file('old-first'), file('old-last')]))
    const { reader } = setup(); await settled()
    const tail = deferred()
    api.mockResolvedValueOnce(page([file('new-first')], true, '1')).mockReturnValueOnce(tail.promise)
    void reader.value.refresh(); await settled()
    expect(reader.value.files.map(f => f.delivery_id)).toEqual(['old-first', 'old-last'])
    await reader.value.refresh()
    expect(api).toHaveBeenCalledTimes(3)
    tail.resolve(page([file('new-last')])); await settled()
    expect(reader.value.files.map(f => f.delivery_id)).toEqual(['new-first', 'new-last'])
  })

  it('reads from the start on the next poll for updated entries and late inserts', async () => {
    api.mockResolvedValueOnce(page([file('first', 'pending')]))
    const { reader } = setup(); await settled()
    api.mockResolvedValue(page([file('first'), file('late')]))
    await vi.advanceTimersByTimeAsync(5000)
    expect(api.mock.lastCall![0].query).toEqual({ after: undefined })
    expect(reader.value.files.map(f => f.delivery_id)).toEqual(['first', 'late'])
    expect(reader.value.files[0]?.reference).toEqual(reference)
  })

  it('clears a partial read on network failure, recovers without a dispatch or user page action', async () => {
    api.mockResolvedValueOnce(page([file('first')], true, '1')).mockRejectedValueOnce(new TypeError('PRIVATE_NETWORK'))
    const { reader } = setup(); await settled()
    expect(reader.value.files).toEqual([])
    expect(reader.value.loaded).toBe(false)
    api.mockResolvedValue(page([file('first'), file('second')]))
    await vi.advanceTimersByTimeAsync(5000)
    expect(reader.value.error).toBeUndefined()
    expect(reader.value.files).toHaveLength(2)
  })

  it.each([401, 403, 404])('stops polling after %i and allows explicit reload', async status => {
    api.mockRejectedValueOnce({ status })
    const { reader } = setup(); await settled()
    await vi.advanceTimersByTimeAsync(20000)
    expect(api).toHaveBeenCalledTimes(1)
    expect(reader.value.files).toEqual([])
    await reader.value.refresh()
    expect(reader.value.loaded).toBe(true)
  })

  it.each(['resolve', 'reject'] as const)('discards a late %s across A→B→A, including the same operation ID', async outcome => {
    const old = deferred(); api.mockReturnValueOnce(old.promise)
    const { scope, reader } = setup()
    const signal = api.mock.lastCall![0].signal as AbortSignal
    scope.value = { ...scope.value!, session: 'B' }; await settled()
    api.mockResolvedValue(page([file('new')]))
    scope.value = { ...scope.value!, session: 'session' }; await settled()
    expect(signal.aborted).toBe(true)
    if (outcome === 'resolve') old.resolve(page([file('stale')]))
    else old.reject({ status: 403 })
    await settled()
    expect(reader.value.files.map(f => f.delivery_id)).toEqual(['new'])
    expect(reader.value.error).toBeUndefined()
  })

  it.each(['bot', 'session', 'selection', 'identity'] as const)('retires in-flight reads when %s changes', async field => {
    const old = deferred(); api.mockReturnValueOnce(old.promise)
    const { scope, reader } = setup()
    scope.value = { ...scope.value!, [field]: 'changed' }; await settled()
    old.resolve(page([file('stale')])); await settled()
    expect(reader.value.files).toEqual([])
  })

  it('does not read subagents, hidden/denied parents, or after disposal', async () => {
    const { enabled, selected, reader, owner } = setup(); await settled()
    enabled.value = false
    await vi.advanceTimersByTimeAsync(10000)
    expect(reader.value.files).toEqual([])
    expect(api).toHaveBeenCalledTimes(1)
    selected.value = { task_type: 'subagent', task_id: 'child' }; enabled.value = true
    await vi.advanceTimersByTimeAsync(5000)
    expect(api).toHaveBeenCalledTimes(1)
    owner.stop(); await vi.advanceTimersByTimeAsync(10000)
    expect(api).toHaveBeenCalledTimes(1)
  })

  it.each([
    { ...page([file('first')], true, '0').data },
    { ...page([file('first')]).data, version: 2 },
    { ...page([file('first')]).data, operation_id: 'other' },
    { ...page([{ ...file('first'), reference: { ...reference, content_hash: 'not-a-hash' } }]).data },
  ])('fails closed on invalid page/reference, without looping', async data => {
    api.mockResolvedValue({ data })
    const { reader } = setup(); await settled()
    expect(reader.value.error).toBeDefined()
    expect(reader.value.files).toEqual([])
    expect(api).toHaveBeenCalledTimes(1)
  })
})
