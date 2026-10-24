import { computed, onScopeDispose, ref, shallowRef, watch, type Ref } from 'vue'
import { getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdObservation, type ComputertaskRecovery, type SessiontaskTask } from '@memohai/sdk'
import { apiErrorStatus } from '@/utils/api-error'
import type { TaskScope } from './useSessionTasks'

export interface ComputerTaskObservationView {
  data?: ComputertaskRecovery
  receivedAt?: string
  loading: boolean
  error?: unknown
  refresh: () => Promise<void>
}

// The existing recovery read does not collect output or write task state.
// Only the selected, visible task is observed; identity changes retire requests.
export function useComputerTaskObservation(scope: Ref<TaskScope | null>, selected: Ref<Pick<SessiontaskTask, 'task_type' | 'task_id'> | undefined>, enabled: Ref<boolean>) {
  const data = shallowRef<ComputertaskRecovery>()
  const receivedAt = ref<string>()
  const loading = ref(false)
  const error = shallowRef<unknown>()
  let generation = 0
  let controller: AbortController | undefined
  function invalidate() {
    generation++
    controller?.abort()
    controller = undefined
    data.value = undefined
    receivedAt.value = undefined
    error.value = undefined
    loading.value = false
  }
  async function refresh() {
    if (!enabled.value || !scope.value || selected.value?.task_type !== 'computer_operation' || loading.value) return
    const current = scope.value
    const target = selected.value.task_id
    const epoch = generation
    const request = new AbortController()
    controller = request
    const timeout = setTimeout(() => request.abort(), 15000)
    loading.value = true
    error.value = undefined
    try {
      const response = await getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdObservation({
        path: { bot_id: current.bot, session_id: current.session, task_type: 'computer_operation', task_id: target },
        signal: request.signal, cache: 'no-store', throwOnError: true,
      })
      if (epoch !== generation) return
      if (response.data.operation_id !== target || !response.data.native) throw new Error('Invalid task observation')
      data.value = response.data
      receivedAt.value = new Date().toLocaleTimeString()
    } catch (cause) {
      if (epoch === generation) {
        data.value = undefined
        receivedAt.value = undefined
        error.value = cause
      }
    } finally {
      clearTimeout(timeout)
      if (epoch === generation) loading.value = false
    }
  }
  watch([scope, selected, enabled], () => { invalidate(); void refresh() }, { immediate: true, flush: 'sync' })
  const timer = setInterval(() => {
    if (![401, 403, 404].includes(apiErrorStatus(error.value) ?? 0)) void refresh()
  }, 5000)
  onScopeDispose(() => { clearInterval(timer); invalidate() })
  return computed<ComputerTaskObservationView>(() => ({ data: data.value, receivedAt: receivedAt.value, loading: loading.value, error: error.value, refresh }))
}
