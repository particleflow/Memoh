import { computed, onScopeDispose, ref, shallowRef, watch, type Ref } from 'vue'
import { getBotsByBotIdSessionsBySessionId, getBotsByBotIdSessionsBySessionIdDelegations, getBotsByBotIdSessionsBySessionIdTasks } from '@memohai/sdk'
import { onAuthSessionCleared } from '@/lib/auth-session'
import { apiErrorStatus } from '@/utils/api-error'
import type { TaskScope } from './useSessionTasks'

export interface ConversationRelation {
  key: string
  kind: 'bot' | 'agent' | 'task'
  bot: string
  session: string
  name: string
  state: string
  parent?: string
}

export const relationSessionKey = (bot: string, session: string) => `session:${bot}:${session}`

// Use only persisted session ancestry and scoped delegation links. The global
// Bot/activity list is not a source of conversation membership or queue state.
export function useConversationRelations(scope: Ref<TaskScope | null>, visible: Ref<boolean>, botName: (id: string) => string, rootState: () => string) {
  const records = shallowRef<ConversationRelation[]>([]), error = shallowRef<unknown>()
  const loading = ref(false)
  let epoch = 0, controller: AbortController | undefined
  const denied = new Set<string>()
  const sessionKey = relationSessionKey
  function clear() {
    epoch++; controller?.abort(); controller = undefined
    records.value = []; error.value = undefined; loading.value = false; denied.clear()
  }
  async function refresh() {
    const current = scope.value
    if (!current || !visible.value || loading.value || denied.has(sessionKey(current.bot, current.session))) return
    const generation = epoch, control = new AbortController()
    controller = control; loading.value = true
    const timeout = setTimeout(() => control.abort(), 15000)
    const rows = new Map<string, ConversationRelation>()
    let partialError: unknown
    const sessions = new Map<string, Awaited<ReturnType<typeof getBotsByBotIdSessionsBySessionId<true>>>['data']>()
    const tasksRead = new Set<string>(), delegationsRead = new Set<string>()
    const queue = [{ bot: current.bot, session: current.session, tasks: true }]
    const options = { signal: control.signal, cache: 'no-store' as const, throwOnError: true as const }
    async function session(bot: string, id: string) {
      const key = sessionKey(bot, id)
      if (denied.has(key)) throw new Error('Session is not accessible')
      const cached = sessions.get(key)
      if (cached) return cached
      let data
      try {
        data = (await getBotsByBotIdSessionsBySessionId({ ...options, path: { bot_id: bot, session_id: id } })).data
      } catch (cause) {
        if ([401, 403, 404].includes(apiErrorStatus(cause) ?? 0)) denied.add(key)
        else partialError = cause
        throw cause
      }
      if (data.id !== id || data.bot_id !== bot) throw new Error('Session identity changed')
      sessions.set(key, data)
      return data
    }
    async function pages<T>(load: (cursor?: string) => Promise<{ items: T[], next_cursor?: string }>) {
      const result: T[] = [], cursors = new Set<string>()
      let cursor: string | undefined
      do {
        control.signal.throwIfAborted()
        const page = await load(cursor)
        result.push(...page.items)
        cursor = page.next_cursor || undefined
        if (cursor && cursors.has(cursor)) throw new Error('Repeated conversation cursor')
        if (cursor) cursors.add(cursor)
      } while (cursor)
      return result
    }
    try {
      await session(current.bot, current.session)
      rows.set(sessionKey(current.bot, current.session), { key: sessionKey(current.bot, current.session), kind: 'bot', bot: current.bot, session: current.session, name: botName(current.bot), state: rootState() })
      for (let index = 0; index < queue.length; index++) {
        const node = queue[index]!, key = sessionKey(node.bot, node.session)
        if (denied.has(key)) continue
        const path = { bot_id: node.bot, session_id: node.session }
        try {
          if (node.tasks && !tasksRead.has(key)) {
            tasksRead.add(key)
            const tasks = await pages(async cursor => (await getBotsByBotIdSessionsBySessionIdTasks({ ...options, path, query: { limit: 100, cursor } })).data)
            for (const task of tasks) {
              if (task.parent_session_id !== node.session || !task.session_id) continue
              let owner
              try { owner = await session(node.bot, task.session_id) } catch { continue }
              if (task.direct_parent_session_id && owner.parent_session_id !== task.direct_parent_session_id) continue
              const owning = sessionKey(node.bot, task.session_id)
              if (task.task_type === 'subagent') {
                if (task.session_id === node.session) continue
                rows.set(owning, { key: owning, kind: 'agent', bot: node.bot, session: task.session_id, name: owner.title || '', state: task.status,
                  parent: owner.parent_session_id ? sessionKey(node.bot, owner.parent_session_id) : undefined })
                queue.push({ bot: node.bot, session: task.session_id, tasks: false })
              } else {
                const taskKey = `task:${node.bot}:${task.task_id}`
                rows.set(taskKey, { key: taskKey, kind: 'task', bot: node.bot, session: task.session_id, name: owner.title || '', state: task.status, parent: owning })
              }
            }
          }
          if (delegationsRead.has(key)) continue
          delegationsRead.add(key)
          const delegations = await pages(async cursor => (await getBotsByBotIdSessionsBySessionIdDelegations({ ...options, path, query: { limit: 100, cursor } })).data)
          for (const link of delegations) {
            if (link.source_session_id !== node.session || !link.target_session_id) continue
            const target = sessionKey(link.target_bot_id, link.target_session_id)
            if (rows.has(target)) continue
            try { await session(link.target_bot_id, link.target_session_id) } catch { continue }
            rows.set(target, { key: target, kind: 'bot', bot: link.target_bot_id, session: link.target_session_id,
              name: botName(link.target_bot_id), state: link.target_run_state || link.state, parent: key })
            queue.push({ bot: link.target_bot_id, session: link.target_session_id, tasks: true })
          }
        } catch (cause) {
          if ([401, 403, 404].includes(apiErrorStatus(cause) ?? 0)) denied.add(key)
          else partialError = cause
          if (key === sessionKey(current.bot, current.session)) throw cause
        }
      }
      if (generation === epoch && !control.signal.aborted) { records.value = [...rows.values()]; error.value = partialError }
    } catch (cause) {
      if (generation === epoch) { records.value = []; error.value = cause }
    } finally {
      clearTimeout(timeout)
      if (generation === epoch) loading.value = false
      if (controller === control) controller = undefined
    }
  }
  const items = computed(() => {
    const byId = new Map(records.value.map(row => [row.key, row]))
    const root = scope.value && sessionKey(scope.value.bot, scope.value.session)
    return records.value.filter(row => {
      const path = new Set<string>()
      let key: string | undefined = row.key
      while (key && !path.has(key) && key !== root) { path.add(key); key = byId.get(key)?.parent }
      return !!root && key === root // orphan/cycle is not an invented relation
    })
  })
  watch([scope, visible], () => { clear(); void refresh() }, { immediate: true, flush: 'sync' })
  const timer = setInterval(() => { void refresh() }, 5000)
  const off = onAuthSessionCleared(clear)
  onScopeDispose(() => { clearInterval(timer); off(); clear() })
  const bots = computed(() => items.value.filter(row => row.kind === 'bot'))
  return { items, bots, error, loading }
}
