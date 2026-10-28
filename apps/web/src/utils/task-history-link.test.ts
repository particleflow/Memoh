import { beforeEach, expect, it, vi } from 'vitest'
import { resolveTaskHistoryAnchor, revalidateTaskHistoryAnchor, taskHistoryQuery } from './task-history-link'
const read = vi.hoisted(() => vi.fn())
vi.mock('@memohai/sdk', () => ({ getBotsByBotIdTasksByTaskTypeByRecordId: read }))
const task = { task_type: 'native_agent' as const, task_id: 'bg-original', record_id: 'original-occurrence', name: 'work', state: 'completed', created_at: new Date().toISOString(), destination: { bot_id: 'target', session_id: 'history', run_id: 'original-run', turn_id: 'original-turn' } }
const query = { task_bot: 'source', task_type: task.task_type, task_id: task.task_id, task_record: task.record_id, run: 'original-run' }
beforeEach(() => { read.mockReset().mockResolvedValue({ data: task }) })
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
