import { computed, onScopeDispose, ref, shallowRef, watch, type Ref } from 'vue'
import { getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdDeliveries, type ComputertaskDeliveryPageFile, type SessiontaskTask } from '@memohai/sdk'
import { apiErrorStatus } from '@/utils/api-error'
import type { TaskScope } from './useSessionTasks'

export interface ComputerTaskDeliveryView {
  botId?: string
  files: ComputertaskDeliveryPageFile[]
  loading: boolean
  loaded: boolean
  error?: unknown
  refresh: () => Promise<void>
}

// Task metadata reads retire with their scope. The owner accompanies references
// so native media reads use this Bot, never a global current-Bot fallback.
// The media endpoint independently checks the logged-in user's Bot permission.
export function useComputerTaskDeliveries(scope: Ref<TaskScope | null>, selected: Ref<Pick<SessiontaskTask, 'task_type' | 'task_id'> | undefined>, enabled: Ref<boolean>) {
  const files = shallowRef<ComputertaskDeliveryPageFile[]>([])
  const loading = ref(false)
  const loaded = ref(false)
  const error = shallowRef<unknown>()
  let generation = 0
  let controller: AbortController | undefined

  function invalidate() {
    generation++
    controller?.abort()
    controller = undefined
    files.value = []
    loading.value = false
    loaded.value = false
    error.value = undefined
  }
  async function refresh() {
    if (!enabled.value || !scope.value || selected.value?.task_type !== 'computer_operation' || loading.value) return
    const current = scope.value
    const target = selected.value.task_id
    const epoch = generation
    const requestController = new AbortController()
    controller = requestController
    const hadSnapshot = loaded.value
    loading.value = true
    error.value = undefined
    loaded.value = false
    if (!hadSnapshot) files.value = []
    const entries = new Map<string, ComputertaskDeliveryPageFile>()
    let after: string | undefined
    try {
      // Pages are an internal read detail, not a new user action or a task
      // completion signal. Refresh from the start to see earlier entry updates.
      for (;;) {
        const timeout = setTimeout(() => requestController.abort(), 15000)
        let data
        try {
          ;({ data } = await getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdDeliveries({
            path: { bot_id: current.bot, session_id: current.session, task_type: 'computer_operation', task_id: target },
            query: { after }, signal: requestController.signal, cache: 'no-store', throwOnError: true,
          }))
        } finally { clearTimeout(timeout) }
        if (epoch !== generation) return
        if (data.version !== 1 || data.operation_id !== target || !Array.isArray(data.files)
          || typeof data.has_more !== 'boolean') throw new Error('Invalid delivery page')
        for (const file of data.files) {
          if (!file.delivery_id || entries.has(file.delivery_id) || !file.state) throw new Error('Invalid delivery entry')
          const reference = file.state === 'committed' ? file.reference : undefined
          if (file.state === 'committed' && (!reference || reference.version !== 1
            || !['upload', 'download'].includes(reference.direction ?? '')
            || !/^[0-9a-f]{64}$/.test(reference.content_hash ?? '')
            || !Number.isSafeInteger(reference.size_bytes) || reference.size_bytes! < 0
            || typeof reference.mime !== 'string')) throw new Error('Invalid delivery reference')
          entries.set(file.delivery_id, { delivery_id: file.delivery_id, state: file.state, reference })
        }
        // Keep the last complete view during a refresh so scrolling does not
        // jump to page one every poll. Any read failure still clears everything.
        if (!hadSnapshot || !data.has_more) files.value = [...entries.values()]
        if (!data.has_more) { loaded.value = true; break }
        if (!data.next_after || !/^(0|[1-9][0-9]*)$/.test(data.next_after)
          || BigInt(data.next_after) <= BigInt(after ?? '0')) throw new Error('Invalid delivery continuation')
        after = data.next_after
      }
    } catch (cause) {
      if (epoch === generation) {
        files.value = []
        error.value = cause
      }
    } finally {
      if (epoch === generation) loading.value = false
      if (controller === requestController) controller = undefined
    }
  }
  watch([scope, selected, enabled], () => { invalidate(); void refresh() }, { immediate: true, flush: 'sync' })
  const timer = setInterval(() => {
    if (![401, 403, 404].includes(apiErrorStatus(error.value) ?? 0)) void refresh()
  }, 5000)
  onScopeDispose(() => { clearInterval(timer); invalidate() })
  return computed<ComputerTaskDeliveryView>(() => ({ botId: enabled.value ? scope.value?.bot : undefined, files: files.value, loading: loading.value, loaded: loaded.value, error: error.value, refresh }))
}
