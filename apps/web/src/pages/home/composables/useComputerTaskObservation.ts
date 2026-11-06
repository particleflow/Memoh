import { computed, onScopeDispose, ref, shallowRef, watch, type Ref } from 'vue'
import { getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdObservation, type ComputertaskRecovery, type SessiontaskTask } from '@memohai/sdk'
import { apiErrorStatus } from '@/utils/api-error'
import type { TaskScope } from './useSessionTasks'

export interface ComputerTaskObservationView {
  data?: ComputertaskRecovery
  receivedAt?: string
  loading: boolean
  pollingPaused?: boolean
  error?: unknown
  refresh: () => Promise<void>
}

// The existing recovery read does not collect output or write task state.
// The existing refresh action opts into native reads for this selected task.
// Merely opening persisted summaries or attachment references never opts in.
export function useComputerTaskObservation(scope: Ref<TaskScope | null>, selected: Ref<Pick<SessiontaskTask, 'task_type' | 'task_id'> | undefined>, enabled: Ref<boolean>) {
  const data = shallowRef<ComputertaskRecovery>()
  const receivedAt = ref<string>()
  const loading = ref(false)
  const error = shallowRef<unknown>()
  const opened = ref(false)
  const pollingPaused = ref(false)
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
  async function read() {
    if (!opened.value || pollingPaused.value || !enabled.value || !scope.value || selected.value?.task_type !== 'computer_operation' || loading.value) return
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
      pollingPaused.value = response.data.native.availability === 'authentication_unavailable'
      receivedAt.value = new Date().toLocaleTimeString()
    } catch (cause) {
      if (epoch === generation) {
        data.value = undefined
        receivedAt.value = undefined
        error.value = cause
        pollingPaused.value = [401, 403, 404].includes(apiErrorStatus(cause) ?? 0)
      }
    } finally {
      clearTimeout(timeout)
      if (epoch === generation) loading.value = false
    }
  }
  async function refresh() {
    if (!enabled.value || !scope.value || selected.value?.task_type !== 'computer_operation' || loading.value) return
    opened.value = true
    pollingPaused.value = false
    await read()
  }
  watch([scope, selected], () => {
    invalidate(); opened.value = false; pollingPaused.value = false
  }, { flush: 'sync' })
  // Parent summary revalidation can temporarily disable this reader. Retain
  // the explicit choice and any auth stop for the same identity, never restart
  // failed authentication merely because the persisted summary is available.
  watch(enabled, () => { invalidate(); void read() }, { flush: 'sync' })
  const timer = setInterval(() => { void read() }, 5000)
  onScopeDispose(() => { clearInterval(timer); invalidate() })
  return computed<ComputerTaskObservationView>(() => ({ data: data.value, receivedAt: receivedAt.value, loading: loading.value, error: error.value, pollingPaused: pollingPaused.value, refresh }))
}
