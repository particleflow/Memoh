import { computed, onScopeDispose, ref, shallowRef, watch, type Ref } from 'vue'
import { getBotsByBotIdSessionsBySessionId, getBotsByBotIdSessionsBySessionIdDelegationsByDelegationId } from '@memohai/sdk'
import type { TaskScope } from './useSessionTasks'
import type { useBotDelegations } from './useBotDelegations'

export interface BotDelegationHistoryAction {
  pending: boolean
  error?: unknown
  open: () => Promise<void>
}

// Navigation rechecks the source relation and the target's ordinary session
// permission. It never dispatches a turn or treats a UUID as an access grant.
export function useBotDelegationHistory(scope: Ref<TaskScope | null>, visible: Ref<boolean>, reader: ReturnType<typeof useBotDelegations>, navigate: (target: { bot: string, session: string, title?: string, identity: string }) => Promise<void>) {
  const pending = ref(false), error = shallowRef<unknown>()
  let epoch = 0, controller: AbortController | undefined
  function invalidate() { epoch++; controller?.abort(); pending.value = false; error.value = undefined }
  async function open() {
    const current = scope.value, task = reader.detail.value
    if (!current || !visible.value || pending.value || reader.error.value || reader.loading.value
      || !task?.target_run_id || !task.target_session_id || task.source_session_id !== current.session
      || task.delegation_id !== reader.selected.value) return
    const generation = epoch, control = new AbortController()
    controller = control; pending.value = true; error.value = undefined
    const timeout = setTimeout(() => control.abort(), 15000)
    try {
      const { data: fresh } = await getBotsByBotIdSessionsBySessionIdDelegationsByDelegationId({
        path: { bot_id: current.bot, session_id: current.session, delegation_id: task.delegation_id },
        signal: control.signal, cache: 'no-store', throwOnError: true,
      })
      if (generation !== epoch) return
      if (fresh.delegation_id !== task.delegation_id || fresh.source_session_id !== current.session
        || fresh.target_bot_id !== task.target_bot_id || fresh.target_session_id !== task.target_session_id
        || fresh.target_run_id !== task.target_run_id) throw new Error('Delegation run changed')
      const { data: session } = await getBotsByBotIdSessionsBySessionId({
        path: { bot_id: fresh.target_bot_id, session_id: fresh.target_session_id! },
        signal: control.signal, cache: 'no-store', throwOnError: true,
      })
      if (generation !== epoch) return
      if (session.id !== fresh.target_session_id || session.bot_id !== fresh.target_bot_id || session.parent_session_id) throw new Error('Invalid specialist session')
      await navigate({ bot: fresh.target_bot_id, session: session.id, title: session.title, identity: current.identity })
    } catch (cause) { if (generation === epoch) error.value = cause }
    finally { clearTimeout(timeout); if (generation === epoch) pending.value = false }
  }
  watch([scope, visible, reader.selected], invalidate, { flush: 'sync' })
  watch(() => [reader.detail.value?.delegation_id, reader.detail.value?.target_bot_id, reader.detail.value?.target_session_id, reader.detail.value?.target_run_id].join(':'), invalidate, { flush: 'sync' })
  watch(reader.error, cause => { if (cause) invalidate() }, { flush: 'sync' })
  onScopeDispose(invalidate)
  return computed<BotDelegationHistoryAction>(() => ({ pending: pending.value, error: error.value, open }))
}
