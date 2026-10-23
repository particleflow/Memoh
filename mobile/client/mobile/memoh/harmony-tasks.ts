import { getBotsByBotIdSessionsBySessionIdTasks, getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskId, type SessiontaskTask } from './harmony-tasks-sdk'

// Deployed 6ddf61dc retains bounded final-assistant text. Never fall back to raw messages, operation
// output, media URLs or session metadata when these endpoints are unavailable.
export type Task = SessiontaskTask
export interface TaskPage { items: Task[]; cursor: string }
export class TaskReadError extends Error {
  readonly status: number
  constructor(status: number) { super('Task read unavailable'); this.status = status }
}
// Like the upstream task reader, bound every read to 15 seconds. Explicitly
// settle cancellation as well: a suspended or non-conforming transport must
// not leave the view busy forever or accept a late result after the deadline.
async function readTask<T>(signal: AbortSignal, run: (signal: AbortSignal) => Promise<T>): Promise<T> {
  signal.throwIfAborted()
  const controller = new AbortController()
  let stop: (reason: unknown) => void = () => {}
  const canceled = new Promise<never>((_, reject) => { stop = reject })
  const abort = () => { controller.abort(); stop(signal.reason) }
  signal.addEventListener('abort', abort, { once: true })
  const timer = setTimeout(() => { controller.abort(); stop(new TaskReadError(408)) }, 15000)
  try { return await Promise.race([run(controller.signal), canceled]) }
  finally { clearTimeout(timer); signal.removeEventListener('abort', abort) }
}
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
const statuses = ['unknown', 'submitted', 'process_exited', 'not_started', 'rejected']
const piResults = ['unknown', 'not_applicable', 'succeeded', 'failed', 'aborted', 'incomplete', 'not_started', 'invalid_protocol', 'output_limit'] as const
function piResult(value: unknown): NonNullable<Task['pi_result']> {
  return typeof value === 'string' && (piResults as readonly string[]).includes(value) ? value as NonNullable<Task['pi_result']> : 'unknown'
}
function invalid(): never { throw new TaskReadError(0) }
function task(value: unknown, parentId: string): Task {
  if (!value || typeof value !== 'object') return invalid()
  const row = value as Task
  if (!uuid.test(row.task_id) || !uuid.test(row.session_id) || row.parent_session_id !== parentId
    || !['subagent', 'computer_operation'].includes(row.task_type) || !statuses.includes(row.status)
    || typeof row.summary !== 'string' || typeof row.created_at !== 'string' || typeof row.updated_at !== 'string'
    || !Number.isFinite(Date.parse(row.created_at)) || !Number.isFinite(Date.parse(row.updated_at))
    || (row.content_available !== undefined && typeof row.content_available !== 'boolean')
    || (row.attachments_available !== undefined && row.attachments_available !== false)) return invalid()
  // Explicit copy: no arbitrary titles, metadata, raw output or new fields enter
  // the view. parent_session_id denotes the requested root, not the owner edge.
  const result: Task = { task_type: row.task_type, task_id: row.task_id, session_id: row.session_id,
    parent_session_id: row.parent_session_id, status: row.status, summary: row.summary,
    created_at: row.created_at, updated_at: row.updated_at, content_available: false, attachments_available: false }
  if (row.task_type === 'subagent') {
    if (row.status !== 'unknown') return invalid()
  } else {
    if (!row.target_id || !uuid.test(row.target_id) || typeof row.revision !== 'string' || !/^\d+$/.test(row.revision)
      || typeof row.cancel_requested !== 'boolean' || row.business_state !== 'unknown'
      || !['foreground_bash', 'herdr_pi_task', 'unknown'].includes(row.operation_kind || '')
      || !['unknown', 'exited', 'not_started'].includes(row.process_state || '')) return invalid()
    Object.assign(result, { target_id: row.target_id, revision: row.revision, cancel_requested: row.cancel_requested,
      operation_kind: row.operation_kind, process_state: row.process_state, pi_result: piResult(row.pi_result), business_state: row.business_state })
    const summary = row.result_summary
    // Availability never follows a success/exit status. Only the reviewed,
    // bounded field is accepted; unfamiliar results and malformed content stay
    // unavailable without hiding independently valid receipt facts.
    if (row.operation_kind === 'herdr_pi_task' && row.content_available === true
      && !['unknown', 'not_applicable', 'invalid_protocol', 'output_limit'].includes(result.pi_result!)
      && summary && summary.source === 'pi_final_assistant' && typeof summary.text === 'string' && summary.text.length > 0 && summary.text.length <= 2048
      && typeof summary.truncated === 'boolean' && new TextEncoder().encode(summary.text).byteLength <= 2048) {
      result.content_available = true
      result.result_summary = { text: summary.text, truncated: summary.truncated, source: 'pi_final_assistant' }
    }
  }
  return result
}
export function taskKey(row: Task): string { return `${row.task_type}:${row.task_id}` }
export function taskTitle(row: Task): string {
  if (row.task_type === 'subagent') return '子 Agent'
  return row.operation_kind === 'herdr_pi_task' ? '电脑 Agent 任务' : row.operation_kind === 'foreground_bash' ? '电脑命令' : '电脑任务'
}
export function taskStatus(row: Task): string {
  return { unknown: '状态未知', submitted: '已提交', process_exited: '进程已退出', not_started: '未启动', rejected: '已拒绝' }[row.status]
}
export function taskPiResult(row: Task): string {
  return { unknown: '未知', not_applicable: '不适用', succeeded: '成功', failed: '失败', aborted: '已中止',
    incomplete: '未完整结束', not_started: '未启动', invalid_protocol: '协议结果无效', output_limit: '输出超出限制' }[piResult(row.pi_result)]
}
export function taskError(cause: unknown): string {
  const status = cause instanceof TaskReadError ? cause.status : 0
  if (status === 404) return '任务接口尚未部署，或此会话／任务已不可访问。请确认服务版本和会话权限后刷新。'
  if (status === 401 || status === 403) return '无权读取此会话的任务，请确认登录和 Bot 权限。'
  if (status === 400) return '分页已失效，请刷新任务列表。'
  if (status === 408) return '读取任务超时，请检查连接后刷新。'
  return '暂时无法读取任务安全概要，请刷新后重试。'
}
function scope(botId: string, parentId: string) { if (!uuid.test(botId) || !uuid.test(parentId)) invalid() }
export async function sessionTasks(botId: string, parentId: string, cursor: string, signal: AbortSignal): Promise<TaskPage> {
  scope(botId, parentId)
  const result = await readTask(signal, requestSignal => getBotsByBotIdSessionsBySessionIdTasks({ path: { bot_id: botId, session_id: parentId },
    query: { limit: 20, ...(cursor ? { cursor } : {}) }, signal: requestSignal, cache: 'no-store', throwOnError: false }))
  signal.throwIfAborted()
  if (!result.response?.ok) throw new TaskReadError(result.response?.status || 0)
  const page = result.data
  if (!page || !Array.isArray(page.items) || page.items.length > 100
    || (page.next_cursor !== undefined && typeof page.next_cursor !== 'string')
    || (cursor && page.next_cursor === cursor)) return invalid()
  return { items: page.items.map(row => task(row, parentId)), cursor: page.next_cursor || '' }
}
export async function sessionTaskDetail(botId: string, parentId: string, row: Task, signal: AbortSignal): Promise<Task> {
  scope(botId, parentId)
  const result = await readTask(signal, requestSignal => getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskId({
    path: { bot_id: botId, session_id: parentId, task_type: row.task_type, task_id: row.task_id },
    signal: requestSignal, cache: 'no-store', throwOnError: false }))
  signal.throwIfAborted()
  if (!result.response?.ok) throw new TaskReadError(result.response?.status || 0)
  const item = task(result.data, parentId)
  if (taskKey(item) !== taskKey(row)) return invalid()
  return item
}
