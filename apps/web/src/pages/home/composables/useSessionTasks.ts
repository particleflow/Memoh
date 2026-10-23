import { onScopeDispose, ref, shallowRef, watch, type Ref } from 'vue'
import {
  getBotsByBotIdSessionsBySessionIdTasks,
  getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskId,
  type SessiontaskTask,
} from '@memohai/sdk'
import { apiErrorStatus } from '@/utils/api-error'

export interface TaskScope {
  bot: string
  session: string
  selection: string
  identity: string
}

// Authorization-scoped content stays in this mounted view only. In particular,
// a query-cache success must never reappear after a denial or a scope change.
export function useSessionTasks(scope: Ref<TaskScope | null>, visible: Ref<boolean>) {
  const items = shallowRef<SessiontaskTask[]>([])
  const detail = shallowRef<SessiontaskTask>()
  const selected = shallowRef<Pick<SessiontaskTask, 'task_type' | 'task_id'>>()
  const cursor = ref<string>()
  const nextCursor = ref<string>()
  const loading = ref(false)
  const error = shallowRef<unknown>()
  let generation = 0
  let controller: AbortController | undefined

  function invalidate() {
    generation++
    controller?.abort()
    controller = undefined
    items.value = []
    detail.value = undefined
    nextCursor.value = undefined
    error.value = undefined
    loading.value = false
  }

  async function refresh() {
    if (!scope.value || !visible.value || loading.value) return
    const current = scope.value
    const target = selected.value
    const epoch = generation
    const requestController = new AbortController()
    controller = requestController
    const signal = requestController.signal
    const timeout = setTimeout(() => requestController.abort(), 15000)
    loading.value = true
    error.value = undefined
    try {
      const path = { bot_id: current.bot, session_id: current.session }
      if (target) {
        const { data } = await getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskId({
          path: { ...path, task_type: target.task_type, task_id: target.task_id },
          signal, cache: 'no-store', throwOnError: true,
        })
        if (epoch === generation) detail.value = data
      } else {
        const { data } = await getBotsByBotIdSessionsBySessionIdTasks({
          path, query: { limit: 20, cursor: cursor.value },
          signal, cache: 'no-store', throwOnError: true,
        })
        if (epoch === generation) {
          items.value = data.items
          nextCursor.value = data.next_cursor
        }
      }
    } catch (cause) {
      if (epoch === generation) {
        // Also clear on network/5xx errors: showing old success as current is
        // misleading, and a later denial must not leave content behind.
        items.value = []
        detail.value = undefined
        nextCursor.value = undefined
        error.value = cause
      }
    } finally {
      clearTimeout(timeout)
      if (epoch === generation) loading.value = false
    }
  }

  function openTask(task: SessiontaskTask) {
    invalidate()
    selected.value = { task_type: task.task_type, task_id: task.task_id }
    void refresh()
  }
  function back() {
    invalidate()
    selected.value = undefined
    void refresh()
  }
  function page(next?: string) {
    invalidate()
    cursor.value = next
    void refresh()
  }
  watch([scope, visible], () => {
    invalidate()
    selected.value = undefined
    cursor.value = undefined
    void refresh()
  }, { immediate: true, flush: 'sync' })
  // A denial/missing route needs explicit Reload or a changed scope, not a
  // five-second retry loop. Transient failures can still recover by polling.
  const timer = setInterval(() => {
    if (![401, 403, 404].includes(apiErrorStatus(error.value) ?? 0)) void refresh()
  }, 5000)
  onScopeDispose(() => { clearInterval(timer); invalidate() })
  return { items, detail, selected, cursor, nextCursor, loading, error, openTask, back, page, refresh }
}
