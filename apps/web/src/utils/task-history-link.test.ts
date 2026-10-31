import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { resolveTaskHistoryAnchor, revalidateTaskHistoryAnchor, taskHistoryQuery } from './task-history-link'
const read = vi.hoisted(() => vi.fn())
vi.mock('@memohai/sdk', () => ({ getBotsByBotIdTasksByTaskTypeByRecordId: read }))
const task = { task_type: 'native_agent' as const, task_id: 'bg-original', record_id: 'original-occurrence', name: 'work', state: 'completed', created_at: new Date().toISOString(), destination: { bot_id: 'target', session_id: 'history', run_id: 'original-run', turn_id: 'original-turn' } }
const query = { task_bot: 'source', task_type: task.task_type, task_id: task.task_id, task_record: task.record_id, run: 'original-run' }
beforeEach(() => { read.mockReset().mockResolvedValue({ data: task }) })
afterEach(() => { vi.useRealTimers() })
it('rechecks the exact typed occurrence and target on refresh and restored-panel activation', async () => {
  expect(taskHistoryQuery('source', task)).toEqual(query)
  const anchor = await resolveTaskHistoryAnchor(query, 'target', 'history')
  expect(anchor?.turnId).toBe('original-turn')
  expect(read.mock.calls[0]![0].path).toEqual({ bot_id: 'source', task_type: 'native_agent', record_id: 'original-occurrence' })
  read.mockRejectedValue({ status: 403 })
  await expect(revalidateTaskHistoryAnchor(anchor!, 'target', 'history')).rejects.toMatchObject({ status: 403 })
})
it.each(['bot', 'session', 'run', 'task', 'type', 'occurrence', 'missing-run'] as const)('rejects a mismatched %s without falling back to the session', async mismatch => {
  const current = structuredClone(task)
  if (mismatch === 'bot') current.destination.bot_id = 'wrong'
  if (mismatch === 'session') current.destination.session_id = 'wrong'
  if (mismatch === 'run') current.destination.run_id = 'latest-run'
  if (mismatch === 'task') current.task_id = 'wrong'
  if (mismatch === 'type') current.task_type = 'schedule_fire' as never
  if (mismatch === 'occurrence') current.record_id = 'wrong'
  if (mismatch === 'missing-run') current.destination.turn_id = ''
  read.mockResolvedValue({ data: current })
  await expect(resolveTaskHistoryAnchor(query, 'target', 'history')).rejects.toThrow()
})
it('allows ordinary history and rejects partial or array-valued task links', async () => {
  expect(await resolveTaskHistoryAnchor({}, 'bot', 'session')).toBeUndefined()
  await expect(resolveTaskHistoryAnchor({ run: 'some-run' }, 'bot', 'session')).rejects.toThrow()
  await expect(resolveTaskHistoryAnchor({ ...query, run: ['one', 'two'] }, 'target', 'history')).rejects.toThrow()
  expect(read).not.toHaveBeenCalled()
})

it('bounds a stalled identity read and aborts its transport without authorizing history', async () => {
  vi.useFakeTimers()
  read.mockImplementation(() => new Promise(() => {}))
  const result = resolveTaskHistoryAnchor(query, 'target', 'history')
  const rejected = expect(result).rejects.toMatchObject({ name: 'TimeoutError' })
  await vi.advanceTimersByTimeAsync(15000)
  await rejected
  expect(read.mock.calls[0]![0].signal.aborted).toBe(true)
  expect(vi.getTimerCount()).toBe(0)
})

it('cancels immediately even if a transport ignores abort, and ignores its late success', async () => {
  let finish!: (value: { data: typeof task }) => void
  read.mockImplementation(() => new Promise(resolve => { finish = resolve }))
  const controller = new AbortController()
  const result = resolveTaskHistoryAnchor(query, 'target', 'history', controller.signal)
  const rejected = expect(result).rejects.toMatchObject({ name: 'AbortError' })
  controller.abort()
  await rejected
  expect(read.mock.calls[0]![0].signal.aborted).toBe(true)
  finish({ data: task })
  await expect(result).rejects.toMatchObject({ name: 'AbortError' })
})

it('does not issue a read for an already canceled caller and cleans up a completed deadline', async () => {
  vi.useFakeTimers()
  const controller = new AbortController(); controller.abort()
  await expect(resolveTaskHistoryAnchor(query, 'target', 'history', controller.signal)).rejects.toMatchObject({ name: 'AbortError' })
  expect(read).not.toHaveBeenCalled()
  await resolveTaskHistoryAnchor(query, 'target', 'history')
  expect(vi.getTimerCount()).toBe(0)
})
