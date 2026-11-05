import { onScopeDispose, ref, shallowRef, watch, type Ref } from 'vue'
import { getBotsByBotIdTasks, type SessiontaskDirectoryTask } from '@memohai/sdk'
import { onAuthSessionCleared } from '@/lib/auth-session'

const key = (row: SessiontaskDirectoryTask) => `${row.task_type}:${row.record_id}`

// The Bot task window follows recent activity: every refresh re-reads the
// newest page (the server orders rows by updated_at) and merges it over the
// accumulated view, while the rows the caller scrolled to load earlier stay
// pinned below the live window. A restart rebuilds the whole list.
export function useBotTaskDirectory(bot: Ref<string>, identity: Ref<string>, visible: Ref<boolean>) {
  const items = shallowRef<SessiontaskDirectoryTask[]>([])
  const loading = ref(false), loadingOlder = ref(false), error = shallowRef<unknown>(), nextCursor = ref('')
  const older = new Set<string>()
  let generation = 0, controller: AbortController | undefined
  function clear() {
    generation++; controller?.abort(); controller = undefined
    items.value = []; loading.value = false; loadingOlder.value = false; error.value = undefined
    nextCursor.value = ''; older.clear()
  }
  async function fetchPage(cursor: string, control: AbortController) {
    const { data } = await getBotsByBotIdTasks({ path: { bot_id: bot.value }, query: { limit: 20, cursor: cursor || undefined }, signal: control.signal, cache: 'no-store', throwOnError: true })
    if (data.next_cursor && data.next_cursor === cursor) throw new Error('Repeated task cursor')
    return data
  }
  async function restart() {
    if (!bot.value || !identity.value || !visible.value || loading.value) return
    const epoch = generation, control = new AbortController()
    controller = control; loading.value = true
    const timeout = setTimeout(() => control.abort(), 15000)
    try {
      const data = await fetchPage('', control)
      if (epoch !== generation || control.signal.aborted) return
      items.value = data.items; nextCursor.value = data.next_cursor || ''; older.clear(); error.value = undefined
    } catch (cause) {
      if (epoch === generation) { items.value = []; nextCursor.value = ''; older.clear(); error.value = cause }
    } finally {
      clearTimeout(timeout)
      if (epoch === generation) { loading.value = false; controller = undefined }
    }
  }
  // Merge the live window: newly active rows arrive in server order on top and
  // rows that slid out of the newest page disappear until scrolled to again.
  async function refresh() {
    if (!bot.value || !identity.value || !visible.value || loading.value) return
    const epoch = generation, control = new AbortController()
    controller = control; loading.value = true
    const timeout = setTimeout(() => control.abort(), 15000)
    try {
      const data = await fetchPage('', control)
      if (epoch !== generation || control.signal.aborted) return
      const head = new Set(data.items.map(key))
      items.value = [...data.items, ...items.value.filter(row => !head.has(key(row)) && older.has(key(row)))]
      nextCursor.value = data.next_cursor || ''; error.value = undefined
    } catch (cause) {
      if (epoch === generation) { items.value = []; nextCursor.value = ''; older.clear(); error.value = cause }
    } finally {
      clearTimeout(timeout)
      if (epoch === generation) { loading.value = false; controller = undefined }
    }
  }
  // Scrolling to the bottom appends the next older page instead of replacing
  // the view; the pagination cursor chain stays intact while it loads.
  async function loadOlder() {
    if (!bot.value || !identity.value || !visible.value || loading.value || loadingOlder.value || !nextCursor.value) return
    const epoch = generation, control = new AbortController()
    controller = control; loadingOlder.value = true
    const timeout = setTimeout(() => control.abort(), 15000)
    try {
      const data = await fetchPage(nextCursor.value, control)
      if (epoch !== generation || control.signal.aborted) return
      const known = new Set(items.value.map(key))
      const appended = data.items.filter(row => !known.has(key(row)))
      for (const row of appended) older.add(key(row))
      if (appended.length) items.value = [...items.value, ...appended]
      nextCursor.value = data.next_cursor || ''
    } catch (cause) {
      if (epoch === generation) error.value = cause
    } finally {
      clearTimeout(timeout)
      if (epoch === generation) { loadingOlder.value = false; controller = undefined }
    }
  }
  // A scope change must invalidate any in-flight page before reading again.
  watch([bot, identity, visible], () => { clear(); void restart() }, { immediate: true, flush: 'sync' })
  const timer = setInterval(() => { void refresh() }, 5000)
  const off = onAuthSessionCleared(clear)
  onScopeDispose(() => { clear(); clearInterval(timer); off() })
  return { items, loading, loadingOlder, error, nextCursor, refresh, restart, loadOlder }
}
