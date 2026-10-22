import { computed, onScopeDispose, ref, shallowRef, watch, type Ref } from 'vue'
import {
  postBotsByBotIdSessionsBySessionIdDelegationsByDelegationIdDelivery,
  type BotdelegationView,
} from '@memohai/sdk'
import { apiErrorStatus } from '@/utils/api-error'
import type { TaskScope } from './useSessionTasks'
import type { useBotDelegations } from './useBotDelegations'

// Hosts opt in by supplying this capability to the shared observation body.
// Its absence keeps existing observation-only hosts read-only.
export interface BotDelegationDeliveryAction {
  pending: boolean
  disabled: boolean
  error?: unknown
  response?: BotdelegationView
  retry: () => void
}

export function useBotDelegationDelivery(
  scope: Ref<TaskScope | null>,
  visible: Ref<boolean>,
  reader: Pick<ReturnType<typeof useBotDelegations>, 'detail' | 'selected' | 'error' | 'reload'>,
) {
  const pending = ref(false)
  const error = shallowRef<unknown>()
  const response = shallowRef<BotdelegationView>()
  let generation = 0
  let controller: AbortController | undefined
  const allowed = computed(() => {
    const task = reader.detail.value
    return !!scope.value && visible.value && !!task && task.delegation_id === reader.selected.value
      && (task.state === 'completed' || task.state === 'failed')
      && task.source_session_id === scope.value.session && task.delivery_state !== 'accepted'
      && !reader.error.value && ![401, 403, 404, 405].includes(apiErrorStatus(error.value) ?? 0)
  })

  function invalidate() {
    generation++
    // Aborting a local request does not undo a delivery accepted by the server.
    controller?.abort()
    controller = undefined
    pending.value = false
    error.value = undefined
    response.value = undefined
  }

  async function retry() {
    if (!allowed.value || pending.value || !scope.value || !reader.selected.value) return
    const current = scope.value
    const target = reader.selected.value
    const epoch = generation
    const requestController = new AbortController()
    controller = requestController
    pending.value = true
    error.value = undefined
    response.value = undefined
    const timeout = setTimeout(() => requestController.abort(), 15000)
    try {
      const { data } = await postBotsByBotIdSessionsBySessionIdDelegationsByDelegationIdDelivery({
        path: { bot_id: current.bot, session_id: current.session, delegation_id: target },
        signal: requestController.signal, cache: 'no-store', throwOnError: true,
      })
      if (epoch === generation) response.value = data
    } catch (cause) {
      if (epoch === generation) error.value = cause
    } finally {
      clearTimeout(timeout)
      if (controller === requestController) controller = undefined
      if (epoch === generation) {
        // Retire any pre-action GET before re-reading server state. A POST
        // acknowledgement is not proof that the supervisor processed the result.
        await reader.reload()
        if (epoch === generation) pending.value = false
      }
    }
  }

  watch([scope, visible, reader.selected], invalidate, { flush: 'sync' })
  onScopeDispose(invalidate)
  return computed<BotDelegationDeliveryAction>(() => ({
    pending: pending.value, disabled: !allowed.value, error: error.value,
    response: response.value, retry,
  }))
}
