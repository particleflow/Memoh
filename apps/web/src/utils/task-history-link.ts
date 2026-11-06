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

export async function revalidateTaskHistoryAnchor(anchor: TaskHistoryAnchor, bot: string, session: string, signal?: AbortSignal): Promise<void> {
  const current = await resolveTaskHistoryAnchor({ task_bot: anchor.directoryBotId, task_type: anchor.taskType, task_id: anchor.taskId, task_record: anchor.recordId, run: anchor.runId }, bot, session, signal)
  if (current?.turnId !== anchor.turnId) throw new Error('Task execution history changed')
}

// Re-read the original typed task on every route entrance, including refresh.
// URL values and restored panel params never establish execution ownership.
export async function resolveTaskHistoryAnchor(query: LocationQuery, bot: string, session: string, signal?: AbortSignal): Promise<TaskHistoryAnchor | undefined> {
  if (!hasTaskHistoryQuery(query)) return undefined
  const values = keys.map(key => query[key])
  if (values.some(value => typeof value !== 'string' || !value.trim())) throw new Error('Invalid task history link')
  const [directoryBotId, taskType, taskId, recordId, runId] = values as string[]
  if (!taskType || !isTaskType(taskType)) throw new Error('Invalid task type')
  signal?.throwIfAborted()
  const controller = new AbortController()
  const cancel = () => controller.abort(signal?.reason)
  signal?.addEventListener('abort', cancel, { once: true })
  const timeout = setTimeout(() => controller.abort(new DOMException('Task history validation timed out', 'TimeoutError')), 15000)
  let rejectAborted!: () => void
  // Bound the caller as well as the transport: an adapter may ignore abort or
  // finish parsing a response after the caller has selected another task.
  const aborted = new Promise<never>((_, reject) => {
    rejectAborted = () => reject(controller.signal.reason)
    controller.signal.addEventListener('abort', rejectAborted, { once: true })
  })
  let task: SessiontaskDirectoryTask
  try {
    const response = await Promise.race([getBotsByBotIdTasksByTaskTypeByRecordId({
      path: { bot_id: directoryBotId!, task_type: taskType, record_id: recordId! },
      cache: 'no-store', throwOnError: true, signal: controller.signal,
    }), aborted])
    controller.signal.throwIfAborted()
    task = response.data
  } finally {
    clearTimeout(timeout)
    signal?.removeEventListener('abort', cancel)
    controller.signal.removeEventListener('abort', rejectAborted)
  }
  const destination = task?.destination
  if (!task || task.task_type !== taskType || task.task_id !== taskId || task.record_id !== recordId
    || !destination || destination.bot_id !== bot || destination.session_id !== session || destination.run_id !== runId || !destination.turn_id) {
    throw new Error('Task execution history is unavailable')
  }
  return { directoryBotId: directoryBotId!, taskType: taskType!, taskId: taskId!, recordId: recordId!, runId: runId!, turnId: destination.turn_id }
}
