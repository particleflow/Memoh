import type { LocationQuery, LocationQueryRaw } from 'vue-router'
import { getBotsByBotIdTasksByTaskTypeByRecordId, type SessiontaskDirectoryTask } from '@memohai/sdk'

export interface TaskHistoryAnchor {
  directoryBotId: string
  taskType: string
  taskId: string
  recordId: string
  runId: string
  turnId: string
}

function isTaskType(value: string): value is SessiontaskDirectoryTask['task_type'] {
  return ['native_agent', 'computer_operation', 'bot_delegation', 'schedule_fire'].includes(value)
}

const keys = ['task_bot', 'task_type', 'task_id', 'task_record', 'run'] as const

export function taskHistoryQuery(bot: string, task: SessiontaskDirectoryTask): LocationQueryRaw {
  if (!task.destination?.run_id || !task.destination.turn_id) throw new Error('Task execution history is unavailable')
  return { task_bot: bot, task_type: task.task_type, task_id: task.task_id, task_record: task.record_id, run: task.destination.run_id }
}

export function hasTaskHistoryQuery(query: LocationQuery): boolean {
  return keys.some(key => query[key] !== undefined)
}

export async function revalidateTaskHistoryAnchor(anchor: TaskHistoryAnchor, bot: string, session: string): Promise<void> {
  const current = await resolveTaskHistoryAnchor({ task_bot: anchor.directoryBotId, task_type: anchor.taskType, task_id: anchor.taskId, task_record: anchor.recordId, run: anchor.runId }, bot, session)
  if (current?.turnId !== anchor.turnId) throw new Error('Task execution history changed')
}

// Re-read the original typed task on every route entrance, including refresh.
// URL values and restored panel params never establish execution ownership.
export async function resolveTaskHistoryAnchor(query: LocationQuery, bot: string, session: string): Promise<TaskHistoryAnchor | undefined> {
  if (!hasTaskHistoryQuery(query)) return undefined
  const values = keys.map(key => query[key])
  if (values.some(value => typeof value !== 'string' || !value.trim())) throw new Error('Invalid task history link')
  const [directoryBotId, taskType, taskId, recordId, runId] = values as string[]
  if (!taskType || !isTaskType(taskType)) throw new Error('Invalid task type')
  const { data: task } = await getBotsByBotIdTasksByTaskTypeByRecordId({
    path: { bot_id: directoryBotId!, task_type: taskType, record_id: recordId! }, cache: 'no-store', throwOnError: true,
  })
  const destination = task?.destination
  if (!task || task.task_type !== taskType || task.task_id !== taskId || task.record_id !== recordId
    || !destination || destination.bot_id !== bot || destination.session_id !== session || destination.run_id !== runId || !destination.turn_id) {
    throw new Error('Task execution history is unavailable')
  }
  return { directoryBotId: directoryBotId!, taskType: taskType!, taskId: taskId!, recordId: recordId!, runId: runId!, turnId: destination.turn_id }
}
