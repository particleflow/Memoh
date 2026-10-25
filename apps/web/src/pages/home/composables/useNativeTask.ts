import { computed, onScopeDispose, ref, shallowRef, watch, type Ref } from 'vue'
import { getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdContent, postBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdFollowup, type ComputertaskNativeContent, type HandlersNativeTaskFollowupResponse, type SessiontaskTask } from '@memohai/sdk'
import { apiErrorStatus } from '@/utils/api-error'
import type { TaskScope } from './useSessionTasks'

export interface NativeTaskView {
  opened: boolean
  pollingPaused?: boolean
  data?: ComputertaskNativeContent
  loading: boolean
  sending: boolean
  requestId?: string
  response?: HandlersNativeTaskFollowupResponse
  error?: unknown
  open: () => Promise<void>
  refresh: () => Promise<void>
  send: (task: string, mode: 'current_agent' | 'bound_session') => Promise<void>
}

// Only an explicitly opened terminal sample polls. It is never appended to the
// task's evidence ledger. Storage retains one request UUID before any POST;
// it contains no token or prompt, and unknown outcomes cannot mint another ID.
export function useNativeTask(scope: Ref<TaskScope | null>, selected: Ref<Pick<SessiontaskTask, 'task_type' | 'task_id'> | undefined>, enabled: Ref<boolean>, paused: Ref<boolean> = ref(false)) {
  const pollingPaused = ref(false)
  const opened = ref(false), loading = ref(false), sending = ref(false)
  const data = shallowRef<ComputertaskNativeContent>(), error = shallowRef<unknown>()
  const actionError = shallowRef<unknown>()
  const response = shallowRef<HandlersNativeTaskFollowupResponse>(), requestId = ref<string>()
  let generation = 0, storageKey: string | undefined
  let reader: AbortController | undefined, sender: AbortController | undefined
  const usable = () => enabled.value && !paused.value && scope.value && selected.value?.task_type === 'computer_operation'
  const path = () => ({ bot_id: scope.value!.bot, session_id: scope.value!.session, task_type: 'computer_operation' as const, task_id: selected.value!.task_id })
  function invalidate() {
    generation++; reader?.abort(); sender?.abort()
    opened.value = loading.value = sending.value = pollingPaused.value = false
    data.value = error.value = actionError.value = response.value = undefined
    requestId.value = storageKey = undefined
  }
  async function prepareKey(epoch: number) {
    if (!scope.value || !selected.value) return false
    const current = scope.value, id = selected.value.task_id
    const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(current.identity))
    if (epoch !== generation) return false
    const key = `memoh:native-followup:${Array.from(new Uint8Array(digest), b => b.toString(16).padStart(2, '0')).join('')}:${current.bot}:${current.session}:${id}`
    const pending = sessionStorage.getItem(key) || undefined
    storageKey = key; requestId.value = pending
    return true
  }
  async function refresh() {
    if (!usable() || !opened.value || loading.value) return
    pollingPaused.value = false
    const epoch = generation, target = selected.value!.task_id
    reader = new AbortController(); const control = reader
    const timeout = setTimeout(() => control.abort(), 15000)
    loading.value = true
    try {
      if (!storageKey) {
        try { if (!await prepareKey(epoch)) return }
        catch (cause) { actionError.value = cause } // Reading never requires writable browser storage.
      }
      const result = await getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdContent({ path: path(), signal: control.signal, cache: 'no-store', throwOnError: true })
      if (epoch !== generation) return
      if (result.data.operation_id !== target) throw new Error('Invalid native task response')
      data.value = result.data; error.value = undefined
      pollingPaused.value = result.data.availability === 'authentication_unavailable'
      const receipt = result.data.followups?.find(item => item.request_id === requestId.value)
      if (receipt) {
        response.value = { operation_id: target, state: receipt.state, followup: receipt }; actionError.value = undefined
        if (['accepted', 'rejected'].includes(receipt.state ?? '') && storageKey) {
          sessionStorage.removeItem(storageKey); requestId.value = undefined
        }
      }
    } catch (cause) {
      if (epoch === generation) { data.value = undefined; error.value = cause; pollingPaused.value = [401, 403, 404].includes(apiErrorStatus(cause) ?? 0) }
    } finally { clearTimeout(timeout); if (epoch === generation) loading.value = false }
  }
  async function open() { if (!usable()) return; opened.value = true; await refresh() }
  async function send(task: string, mode: 'current_agent' | 'bound_session') {
    if (!usable() || !data.value?.followup_available || requestId.value || sending.value || !task.trim()) return
    const epoch = generation, target = selected.value!.task_id
    sending.value = true
    try {
      if (!storageKey && !await prepareKey(epoch)) return
      if (requestId.value || !storageKey) return
      const id = crypto.randomUUID()
      sessionStorage.setItem(storageKey, id) // If storage fails, no input is sent.
      requestId.value = id; response.value = undefined; actionError.value = undefined
      sender = new AbortController(); const control = sender
      const timeout = setTimeout(() => control.abort(), 45000)
      try {
        const result = await postBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdFollowup({ path: path(), body: { request_id: id, mode, task }, signal: control.signal, cache: 'no-store', throwOnError: true })
        if (epoch !== generation) return
        if (result.data.operation_id !== target) throw new Error('Invalid follow-up response')
        response.value = result.data
        if (['accepted', 'rejected', 'unsupported'].includes(result.data.state ?? '') && storageKey) {
          sessionStorage.removeItem(storageKey); requestId.value = undefined
        }
      } finally { clearTimeout(timeout) }
    } catch (cause) { if (epoch === generation) actionError.value = cause }
    finally { if (epoch === generation) { sending.value = false; await refresh() } }
  }
  watch([scope, selected, enabled], invalidate, { flush: 'sync' })
  // Parent authorization temporarily disappearing clears output and aborts
  // requests, but does not undo the user's choice to watch this same task.
  // A refreshed parent must validate access before any content read resumes.
  watch(paused, () => {
    const wasOpened = opened.value, wasPollingPaused = pollingPaused.value
    invalidate()
    opened.value = wasOpened; pollingPaused.value = wasPollingPaused
    if (!paused.value && !pollingPaused.value) void refresh()
  }, { flush: 'sync' })
  const timer = setInterval(() => { if (!pollingPaused.value && data.value?.supported !== false) void refresh() }, 5000)
  onScopeDispose(() => { clearInterval(timer); invalidate() })
  return computed<NativeTaskView>(() => ({ opened: opened.value, pollingPaused: pollingPaused.value, data: data.value, loading: loading.value, sending: sending.value, requestId: requestId.value, response: response.value, error: actionError.value ?? error.value, open, refresh, send }))
}
