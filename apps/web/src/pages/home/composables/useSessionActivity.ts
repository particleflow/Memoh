import { onScopeDispose, ref, shallowRef, watch, type Ref } from 'vue'
import { getBotsByBotIdSessionsBySessionIdTasks, getBotsByBotIdSessionsBySessionIdDelegations, type SessiontaskTask, type BotdelegationView } from '@memohai/sdk'
import { onAuthSessionCleared } from '@/lib/auth-session'
import { apiErrorStatus } from '@/utils/api-error'
import type { TaskScope } from './useSessionTasks'

// A read-only projection of the existing lists. Read their original cursors so
// a recent terminal page cannot hide an older still-active task. No device
// observation, task submission or result collection is performed here.
export function useSessionActivity(scope: Ref<TaskScope | null>, visible: Ref<boolean>) {
  const tasks = shallowRef<SessiontaskTask[]>([])
  const delegations = shallowRef<BotdelegationView[]>([])
  const taskError = shallowRef<unknown>(), delegationError = shallowRef<unknown>()
  const loading = ref(false)
  let epoch = 0, controller: AbortController | undefined
  function clear() {
    epoch++; controller?.abort(); controller = undefined
    tasks.value = []; delegations.value = []; taskError.value = undefined; delegationError.value = undefined; loading.value = false
  }
  const paused = (error: unknown) => [401, 403, 404].includes(apiErrorStatus(error) ?? 0)
  async function pages<T>(load: (cursor?: string) => Promise<{ items: T[], next_cursor?: string }>, signal: AbortSignal) {
    const rows: T[] = [], seen = new Set<string>()
    let cursor: string | undefined
    do {
      signal.throwIfAborted()
      const page = await load(cursor)
      signal.throwIfAborted()
      rows.push(...page.items)
      cursor = page.next_cursor || undefined
      if (cursor && seen.has(cursor)) throw new Error('Repeated activity cursor')
      if (cursor) seen.add(cursor)
    } while (cursor)
    return rows
  }
  async function refresh() {
    const current = scope.value
    if (!current || !visible.value || loading.value) return
    const generation = epoch, control = new AbortController()
    controller = control; loading.value = true
    const timeout = setTimeout(() => control.abort(), 15000)
    const path = { bot_id: current.bot, session_id: current.session }
    const options = { path, signal: control.signal, cache: 'no-store' as const, throwOnError: true as const }
    try {
      await Promise.allSettled([
        (async () => {
          if (paused(taskError.value)) return
          try {
            const rows = await pages(async cursor => (await getBotsByBotIdSessionsBySessionIdTasks({ ...options, query: { limit: 100, cursor } })).data, control.signal)
            if (generation === epoch) { tasks.value = rows; taskError.value = undefined }
          } catch (error) { if (generation === epoch) { tasks.value = []; taskError.value = error } }
        })(),
        (async () => {
          if (paused(delegationError.value)) return
          try {
            const rows = await pages(async cursor => (await getBotsByBotIdSessionsBySessionIdDelegations({ ...options, query: { limit: 100, cursor } })).data, control.signal)
            if (generation === epoch) { delegations.value = rows; delegationError.value = undefined }
          } catch (error) { if (generation === epoch) { delegations.value = []; delegationError.value = error } }
        })(),
      ])
    } finally {
      clearTimeout(timeout)
      if (generation === epoch) loading.value = false
      if (controller === control) controller = undefined
    }
  }
  watch([scope, visible], () => { clear(); void refresh() }, { immediate: true, flush: 'sync' })
  const timer = setInterval(() => { void refresh() }, 5000)
  const off = onAuthSessionCleared(clear)
  onScopeDispose(() => { clearInterval(timer); off(); clear() })
  return { tasks, delegations, taskError, delegationError, loading }
}
