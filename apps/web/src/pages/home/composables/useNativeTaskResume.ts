import { computed, onScopeDispose, ref, watch, type Ref } from 'vue'
import { postBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdResume, type HandlersNativeTaskResumeInput } from '@memohai/sdk'
import { authSessionGeneration, onAuthSessionCleared } from '@/lib/auth-session'
import { structuredToolResult } from '@/store/chat-list.normalize'
import type { ToolCallBlock } from '@/store/chat-list'
import { apiErrorStatus } from '@/utils/api-error'

export interface NativeTaskResumeScope {
  bot: string
  session: string
  identity: string
}
export type NativeTaskResumeStatus = 'ready' | 'sending' | 'submitted' | 'unchanged' | 'unknown' | 'unavailable' | 'blocked' | 'unsupported' | 'unauthorized' | 'forbidden' | 'storageUnavailable'
const phases = new Set(['batch_creating_workspace', 'batch_workspace_adopted', 'batch_tab_adopted'])
const inputKeys = new Set(['kind', 'task', 'target_id', 'work_dir', 'artifact', 'artifacts'])
function record(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}
function artifact(value: unknown): boolean {
  return record(value) && Object.entries(value).every(([key, item]) => ['mime', 'source_ref'].includes(key) && typeof item === 'string')
}

// This is a display hint only. The server rechecks hash, target, admission and CAS.
export function originalNativeTaskResume(block: ToolCallBlock) {
  if (block.toolName !== 'computer_task_submit' || !block.done) return
  const input = block.input, receipt = structuredToolResult(block.result)
  if (!record(input) || Object.keys(input).some(key => !inputKeys.has(key))
    || input.kind !== 'herdr_pi_task' || typeof input.task !== 'string' || !input.task.trim()
    || ['target_id', 'work_dir'].some(key => key in input && typeof input[key] !== 'string')
    || ('artifact' in input && !artifact(input.artifact))
    || ('artifacts' in input && (!Array.isArray(input.artifacts) || !input.artifacts.every(artifact)))) return
  if (receipt.kind !== 'herdr_pi_task' || receipt.state !== 'unknown' || !phases.has(String(receipt.phase))
    || typeof receipt.operation_id !== 'string' || !receipt.operation_id.trim()
    || typeof receipt.target_id !== 'string' || !receipt.target_id.trim()
    || (input.target_id !== undefined && input.target_id !== receipt.target_id)) return
  // Snapshot the complete original JSON; never reconstruct missing arguments.
  return { operation: receipt.operation_id, body: JSON.parse(JSON.stringify(input)) as HandlersNativeTaskResumeInput }
}

export function useNativeTaskResume(scope: Ref<NativeTaskResumeScope | null>, block: Ref<ToolCallBlock>, token: Ref<string>) {
  const status = ref<NativeTaskResumeStatus>('unavailable')
  const candidate = computed(() => originalNativeTaskResume(block.value))
  const canSend = ref(false)
  let generation = 0, key: string | undefined, controller: AbortController | undefined
  function invalidate() {
    generation++; controller?.abort(); controller = undefined
    key = undefined; canSend.value = false; status.value = 'unavailable'
  }
  async function prepare() {
    invalidate()
    const epoch = generation, authEpoch = authSessionGeneration(), current = scope.value, original = candidate.value
    if (!current?.bot || !current.session || !current.identity || !token.value || !original) return
    try {
      const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(current.identity))
      if (epoch !== generation || authEpoch !== authSessionGeneration()) return
      const userHash = Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, '0')).join('')
      key = `memoh:native-resume:${userHash}:${encodeURIComponent(current.bot)}:${encodeURIComponent(current.session)}:${encodeURIComponent(original.operation)}`
      const pending = sessionStorage.getItem(key)
      status.value = pending === null ? 'ready' : pending === 'submitted' || pending === 'unchanged' ? pending : 'unknown'
      canSend.value = pending === null
    } catch { if (epoch === generation) status.value = 'storageUnavailable' }
  }
  async function send() {
    if (!canSend.value || !key || !scope.value || !candidate.value || !token.value) return
    const epoch = generation, authEpoch = authSessionGeneration(), login = token.value
    const current = { ...scope.value }, original = candidate.value, storageKey = key
    const active = () => epoch === generation && authEpoch === authSessionGeneration() && token.value === login
    canSend.value = false
    try {
      // Both transcript and Tasks can display this same operation at once.
      const existing = sessionStorage.getItem(storageKey)
      if (existing !== null) { status.value = existing === 'submitted' || existing === 'unchanged' ? existing : 'unknown'; return }
      sessionStorage.setItem(storageKey, 'unknown')
    } catch { status.value = 'storageUnavailable'; return }
    status.value = 'sending'
    controller = new AbortController()
    const control = controller, timeout = setTimeout(() => control.abort(), 45000)
    try {
      const { data } = await postBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdResume({
        path: { bot_id: current.bot, session_id: current.session, task_type: 'computer_operation', task_id: original.operation },
        body: original.body, signal: control.signal, cache: 'no-store', throwOnError: true,
      })
      if (!active()) return
      if (data.operation_id !== original.operation || data.automatic_retry !== false || data.business_outcome !== 'unknown') {
        status.value = 'unknown'; return
      }
      const outcome = data.outcome
      if (outcome === 'unavailable' || outcome === 'blocked') {
        sessionStorage.removeItem(storageKey); status.value = outcome; canSend.value = true
      } else {
        status.value = outcome === 'submitted' || outcome === 'unchanged' ? outcome : 'unknown'
        sessionStorage.setItem(storageKey, status.value)
      }
    } catch (cause) {
      if (!active()) return
      const code = apiErrorStatus(cause)
      // A refusal can be explicitly rechecked. Network/5xx/abort stays uncertain.
      if (code && [400, 401, 403, 404, 409].includes(code)) {
        try {
          sessionStorage.removeItem(storageKey)
          status.value = code === 401 ? 'unauthorized' : code === 403 ? 'forbidden' : code === 404 ? 'unsupported' : 'blocked'
          canSend.value = code !== 404
        } catch { status.value = 'unknown' }
      } else status.value = 'unknown'
    } finally { clearTimeout(timeout) }
  }
  watch([() => scope.value?.bot, () => scope.value?.session, () => scope.value?.identity, () => JSON.stringify(candidate.value), token],
    () => { void prepare() }, { immediate: true, flush: 'sync' })
  const stopAuth = onAuthSessionCleared(() => { void prepare() })
  onScopeDispose(() => { stopAuth(); invalidate() })
  return { status, canSend, send }
}
