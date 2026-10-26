import { onScopeDispose, ref, shallowRef, watch, type Ref } from 'vue'
import { getBotsByBotIdActivity, type SessiontaskActivity } from '@memohai/sdk'
import { onAuthSessionCleared } from '@/lib/auth-session'
import { apiErrorStatus, isApiErrorCode, parseMemohError } from '@/utils/api-error'

// One persisted read per accessible Bot/page, never one connection/transcript
// per historical session. Selection and the current transcript are not inputs.
export function useBotActivity(botIds: Ref<string[]>, identity: Ref<string>, visible: Ref<boolean>) {
  const items = shallowRef<SessiontaskActivity[]>([]), unsupported = ref(false)
  const errors = new Map<string, unknown>(), rows = new Map<string, SessiontaskActivity[]>()
  let generation = 0, loading = false, controller: AbortController | undefined
  function clear() {
    generation++; controller?.abort(); controller = undefined; loading = false
    rows.clear(); errors.clear(); items.value = []; unsupported.value = false
  }
  function publish() {
    items.value = [...rows.values()].flat()
    unsupported.value = botIds.value.length > 0 && botIds.value.every(bot => {
      const error = errors.get(bot)
      // A hidden/deleted Bot's typed404 is a resource denial, not an old
      // server. Only a missing route (or legacy untyped404) enables fallback.
      return isApiErrorCode(error, 'http.not_found') || (apiErrorStatus(error) === 404 && !parseMemohError(error)?.code)
    })
  }
  async function refresh() {
    if (!identity.value || !visible.value || loading) return
    const epoch = generation, control = new AbortController(), bots = [...new Set(botIds.value)]
    controller = control; loading = true
    const timeout = setTimeout(() => control.abort(), 15000)
    let position = 0
    async function worker() {
      while (position < bots.length) {
        const bot = bots[position++]!
        if ([401, 403, 404].includes(apiErrorStatus(errors.get(bot)) ?? 0)) continue
        try {
          let cursor: string | undefined
          const all = new Map<string, SessiontaskActivity>(), seen = new Set<string>()
          do {
            control.signal.throwIfAborted()
            const { data } = await getBotsByBotIdActivity({ path: { bot_id: bot }, query: { limit: 100, cursor }, signal: control.signal, cache: 'no-store', throwOnError: true })
            control.signal.throwIfAborted()
            for (const item of data.items) if (item.bot_id === bot) all.set(item.kind + ':' + item.id, item)
            cursor = data.next_cursor || undefined
            if (cursor && seen.has(cursor)) throw new Error('Repeated activity cursor')
            if (cursor) seen.add(cursor)
          } while (cursor)
          if (epoch === generation) { rows.set(bot, [...all.values()]); errors.delete(bot); publish() }
        } catch (error) {
          if (epoch === generation) { rows.delete(bot); errors.set(bot, error); publish() }
        }
      }
    }
    try { await Promise.all(Array.from({ length: Math.min(3, bots.length) }, worker)) }
    finally { clearTimeout(timeout); if (epoch === generation) loading = false }
  }
  watch([() => [...botIds.value].sort().join('\0'), identity, visible], () => { clear(); void refresh() }, { immediate: true, flush: 'sync' })
  const timer = setInterval(() => { void refresh() }, 5000)
  const off = onAuthSessionCleared(clear)
  onScopeDispose(() => { clear(); clearInterval(timer); off() })
  return { items, unsupported }
}
