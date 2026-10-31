import { computed, onScopeDispose, ref, shallowRef, watch, type Ref } from 'vue'
import { getBotsByBotIdSessionsBySessionIdInvocationsByInvocationId, postBotsByBotIdSessionsBySessionIdDecisionsByDecisionIdRecover, type ApplicationUserInputRecoveryResult } from '@memohai/sdk'
import type { ChatMessage } from '@/store/chat/types'
import { authSessionGeneration, onAuthSessionCleared } from '@/lib/auth-session'
import { apiErrorStatus } from '@/utils/api-error'

export interface UserInputRecoveryScope { bot: string; session: string; run?: string; enabled: boolean; visible: boolean }
export function acceptedUserInputRecovery(message: ChatMessage, run?: string) {
  if (message.role !== 'assistant' || message.streaming || !message.messages.some(block => block.type === 'error' && block.code === 'runtime_run_failed')) return
  const inputs = message.messages.flatMap(block => block.type === 'tool' && block.userInput?.status === 'submitted' && block.userInput.user_input_id ? [block.userInput.user_input_id] : [])
  const decisions = [...new Set(inputs)], originalRun = message.runtimeRunId || run
  if (decisions.length !== 1 || !originalRun) return
  return { run: originalRun, decision: decisions[0]! }
}
const terminal = new Set(['completed', 'failed', 'aborted', 'lost'])
const denied = new Set([400, 401, 403, 404, 409])

// Request state only: the server's original decision/run and admission CAS own
// idempotence. Reconnect performs GET, never answer submission or turn retry.
export function useAcceptedUserInputRecovery(scope: Ref<UserInputRecoveryScope>, message: Ref<ChatMessage>, token: Ref<string>) {
  const captured = shallowRef<ReturnType<typeof acceptedUserInputRecovery>>()
  const receipt = shallowRef<ApplicationUserInputRecoveryResult>()
  const state = ref(''), error = shallowRef<unknown>(), busy = ref(false), blocked = ref(false)
  const candidate = computed(() => scope.value.enabled && token.value ? acceptedUserInputRecovery(message.value, scope.value.run || captured.value?.run) : undefined)
  let epoch = 0, controller: AbortController | undefined, timer: ReturnType<typeof setTimeout> | undefined
  function stop() { controller?.abort(); controller = undefined; clearTimeout(timer); timer = undefined }
  function clear() { epoch++; stop(); captured.value = undefined; receipt.value = undefined; state.value = ''; error.value = undefined; busy.value = false; blocked.value = false }
  watch(() => JSON.stringify([scope.value.bot, scope.value.session, scope.value.enabled, token.value, message.value.id, message.value.turnId, message.value.role === 'assistant' ? message.value.messages.flatMap(block => block.type === 'tool' && block.userInput ? [block.userInput.user_input_id, block.userInput.status] : []) : []]), clear, { flush: 'sync' })
  const off = onAuthSessionCleared(clear)
  const active = (generation: number, auth: number) => generation === epoch && auth === authSessionGeneration() && !!token.value
  function later() {
    clearTimeout(timer)
    if (receipt.value && !terminal.has(state.value) && !blocked.value && scope.value.visible) timer = setTimeout(() => { void observe() }, 5000)
  }
  async function observe() {
    const saved = receipt.value, current = scope.value
    if (!saved?.invocation_id || !saved.recovery_run_id || busy.value || !current.visible || blocked.value || !token.value) return
    const generation = epoch, auth = authSessionGeneration(), control = new AbortController()
    controller = control; busy.value = true
    const timeout = setTimeout(() => control.abort(), 15000)
    try {
      const { data } = await getBotsByBotIdSessionsBySessionIdInvocationsByInvocationId({ path: { bot_id: current.bot, session_id: current.session, invocation_id: saved.invocation_id }, signal: control.signal, cache: 'no-store', throwOnError: true })
      if (!active(generation, auth)) return
      if (!data.found || data.session_id !== current.session || data.invocation_id !== saved.invocation_id || data.run_id !== saved.recovery_run_id) { blocked.value = true; state.value = 'unknown'; return }
      state.value = data.state || 'unknown'; error.value = undefined
    } catch (cause) {
      if (!active(generation, auth)) return
      state.value = 'unknown'; error.value = cause
      if (denied.has(apiErrorStatus(cause) ?? 0)) blocked.value = true
    } finally {
      clearTimeout(timeout)
      if (active(generation, auth)) { busy.value = false; later() }
      if (controller === control) controller = undefined
    }
  }
  async function recover() {
    if (busy.value || blocked.value || !candidate.value || !scope.value.visible) return
    if (receipt.value) { await observe(); return }
    const original = { ...candidate.value }, current = { ...scope.value }, generation = epoch, auth = authSessionGeneration()
    captured.value = original; busy.value = true; error.value = undefined
    const control = new AbortController(); controller = control
    const timeout = setTimeout(() => control.abort(), 15000)
    try {
      const { data } = await postBotsByBotIdSessionsBySessionIdDecisionsByDecisionIdRecover({ path: { bot_id: current.bot, session_id: current.session, decision_id: original.decision }, body: { original_run_id: original.run }, signal: control.signal, cache: 'no-store', throwOnError: true })
      if (!active(generation, auth)) return
      if (data.original_run_id !== original.run || data.decision_id !== original.decision || !data.recovery_run_id || !data.invocation_id) { blocked.value = true; state.value = 'unknown'; return }
      receipt.value = data; state.value = data.state || 'accepted'
    } catch (cause) {
      if (!active(generation, auth)) return
      error.value = cause; state.value = 'unknown'
      if (denied.has(apiErrorStatus(cause) ?? 0)) blocked.value = true
    } finally {
      clearTimeout(timeout)
      if (active(generation, auth)) { busy.value = false; if (receipt.value) void observe() }
      if (controller === control) controller = undefined
    }
  }
  watch(() => scope.value.visible, visible => { if (!visible) { clearTimeout(timer); timer = undefined } else if (receipt.value && !terminal.has(state.value)) void observe() })
  onScopeDispose(() => { off(); clear() })
  return { candidate, receipt, state, error, busy, blocked, recover }
}
