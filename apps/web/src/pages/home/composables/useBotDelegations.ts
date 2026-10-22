import { onScopeDispose, ref, shallowRef, watch, type Ref } from 'vue'
import {
  getBotsByBotIdSessionsBySessionIdDelegations,
  getBotsByBotIdSessionsBySessionIdDelegationsByDelegationId,
  type BotdelegationView,
} from '@memohai/sdk'
import { apiErrorStatus, parseMemohError } from '@/utils/api-error'
import type { TaskScope } from './useSessionTasks'

export function delegationAPIUnsupported(error: unknown): boolean {
  const code = parseMemohError(error)?.code
  // Pre-delegation servers return the framework route-not-found Problem.
  // A scoped session.not_found denial is deliberately NOT an unsupported API.
  return code === 'http.not_found' || (!code && apiErrorStatus(error) === 404)
}

function pausePolling(error: unknown): boolean {
  return delegationAPIUnsupported(error) || [401, 403, 404].includes(apiErrorStatus(error) ?? 0)
}

// This reader has its own cursor and no mutations. Re-reading a result must
// never submit the specialist task or invoke the delivery-retry endpoint.
export function useBotDelegations(scope: Ref<TaskScope | null>, visible: Ref<boolean>) {
  const items = shallowRef<BotdelegationView[]>([])
  const detail = shallowRef<BotdelegationView>()
  const selected = ref<string>()
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
    const timeout = setTimeout(() => requestController.abort(), 15000)
    loading.value = true
    error.value = undefined
    try {
      const path = { bot_id: current.bot, session_id: current.session }
      const options = { signal: requestController.signal, cache: 'no-store' as const, throwOnError: true as const }
      if (target) {
        const { data } = await getBotsByBotIdSessionsBySessionIdDelegationsByDelegationId({
          ...options, path: { ...path, delegation_id: target },
        })
        if (epoch === generation) detail.value = data
      } else {
        const { data } = await getBotsByBotIdSessionsBySessionIdDelegations({
          ...options, path, query: { limit: 20, cursor: cursor.value },
        })
        if (epoch === generation) {
          items.value = data.items
          nextCursor.value = data.next_cursor
        }
      }
    } catch (cause) {
      if (epoch === generation) {
        items.value = []
        detail.value = undefined
        nextCursor.value = undefined
        error.value = cause
      }
    } finally {
      clearTimeout(timeout)
      if (epoch === generation) loading.value = false
      if (controller === requestController) controller = undefined
    }
  }

  function reload() {
    invalidate()
    return refresh()
  }

  function openTask(task: BotdelegationView) {
    if (selected.value === task.delegation_id) return
    invalidate()
    selected.value = task.delegation_id
    void refresh()
  }
  function back() {
    if (!selected.value) return
    invalidate()
    selected.value = undefined
    void refresh()
  }
  function page(next?: string) {
    if (loading.value || cursor.value === next) return
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
  const timer = setInterval(() => { if (!pausePolling(error.value)) void refresh() }, 5000)
  onScopeDispose(() => { clearInterval(timer); invalidate() })
  return { items, detail, selected, cursor, nextCursor, loading, error, openTask, back, page, refresh, reload }
}
