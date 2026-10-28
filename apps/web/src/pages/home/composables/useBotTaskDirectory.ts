import { onScopeDispose, ref, shallowRef, watch, type Ref } from 'vue'
import { getBotsByBotIdTasks, type SessiontaskDirectoryTask } from '@memohai/sdk'
import { onAuthSessionCleared } from '@/lib/auth-session'

export function useBotTaskDirectory(bot: Ref<string>, identity: Ref<string>, visible: Ref<boolean>) {
  const items = shallowRef<SessiontaskDirectoryTask[]>([])
  const loading = ref(false), error = shallowRef<unknown>(), nextCursor = ref(''), cursor = ref('')
  let generation = 0, controller: AbortController | undefined
  function clear() {
    generation++; controller?.abort(); controller = undefined
    items.value = []; loading.value = false; error.value = undefined
    cursor.value = ''; nextCursor.value = ''
  }
  async function refresh() {
    if (!bot.value || !identity.value || !visible.value || loading.value) return
    const epoch = generation, control = new AbortController()
    controller = control; loading.value = true
    const timeout = setTimeout(() => control.abort(), 15000)
    try {
      const { data } = await getBotsByBotIdTasks({ path: { bot_id: bot.value }, query: { limit: 20, cursor: cursor.value || undefined }, signal: control.signal, cache: 'no-store', throwOnError: true })
      if (epoch !== generation || control.signal.aborted) return
      if (data.next_cursor && data.next_cursor === cursor.value) throw new Error('Repeated task cursor')
      items.value = data.items; nextCursor.value = data.next_cursor || ''; error.value = undefined
    } catch (cause) {
      if (epoch === generation) { items.value = []; nextCursor.value = ''; error.value = cause }
    } finally {
      clearTimeout(timeout)
      if (epoch === generation) { loading.value = false; controller = undefined }
    }
  }
  function next() {
    if (loading.value || !nextCursor.value) return
    cursor.value = nextCursor.value; items.value = []; nextCursor.value = ''; void refresh()
  }
  function newest() { clear(); void refresh() }
  watch([bot, identity, visible], newest, { immediate: true, flush: 'sync' })
  const timer = setInterval(() => { void refresh() }, 5000)
  const off = onAuthSessionCleared(clear)
  onScopeDispose(() => { clear(); clearInterval(timer); off() })
  return { items, loading, error, nextCursor, cursor, refresh, next, newest }
}
