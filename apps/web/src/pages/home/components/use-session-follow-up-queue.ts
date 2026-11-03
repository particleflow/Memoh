import { computed, getCurrentScope, onScopeDispose, ref, toValue, watch, type MaybeRefOrGetter } from 'vue'
import {
  deleteFollowUpQueueItem,
  deleteSteerQueueItem,
  fetchSessionQueues,
  queueItemText,
  reorderFollowUpQueue,
  reorderSteerQueue,
  promoteFollowUpQueueItemToSteer,
  updateSteerQueueItem,
  updateFollowUpQueueItem,
  type SessionQueueItem,
} from '@/composables/api/useChat.chat-api'
import { isApiErrorCode } from '@/utils/api-error'

export interface EditableFollowUpQueueItem extends SessionQueueItem {
  text: string
  queueKind: 'follow-up' | 'steer'
}

/**
 * Fallback refresh cadence while a run is active and items are pending. The
 * primary trigger is the runtime projection (a steer being claimed or applied,
 * the run reaching a terminal state); this timer only covers a missed event.
 */
export const QUEUE_FALLBACK_REFRESH_MS = 10_000

function editable(item: SessionQueueItem, queueKind: EditableFollowUpQueueItem['queueKind']): EditableFollowUpQueueItem {
  return { ...item, text: queueItemText(item), queueKind }
}

export function useSessionFollowUpQueue(
  botId: MaybeRefOrGetter<string | null | undefined>,
  sessionId: MaybeRefOrGetter<string | null | undefined>,
  active: MaybeRefOrGetter<boolean> = false,
  /**
   * Any value whose change means the server-side queue may have moved: the
   * runtime's steer/continuation signature, or a local enqueue. Changing it
   * schedules one refresh; the caller does not need to poll.
   */
  changeSignal: MaybeRefOrGetter<unknown> = undefined,
) {
  const items = ref<EditableFollowUpQueueItem[]>([])
  const loading = ref(false)
  const steerSupported = ref(false)
  const busy = ref(new Set<string>())
  const hasItems = computed(() => items.value.length > 0)
  let requestVersion = 0
  let scopeVersion = 0
  let refreshTimer: ReturnType<typeof setInterval> | undefined

  function queueTarget() {
    return {
      bot: String(toValue(botId) ?? '').trim(),
      session: String(toValue(sessionId) ?? '').trim(),
      scope: scopeVersion,
    }
  }

  const markBusy = (id: string, value: boolean) => {
    const next = new Set(busy.value)
    if (value) next.add(id)
    else next.delete(id)
    busy.value = next
  }

  function merge(steers: SessionQueueItem[], followUps: SessionQueueItem[]): EditableFollowUpQueueItem[] {
    // Steers are always consumed before follow-ups. Keeping both in one visual
    // list makes a promoted follow-up stay visible immediately, while the
    // server APIs and pending order remain separate.
    return [
      ...steers.map(item => editable(item, 'steer')),
      ...followUps.map(item => editable(item, 'follow-up')),
    ]
  }

  function ofKind(kind: EditableFollowUpQueueItem['queueKind']): SessionQueueItem[] {
    return items.value.filter(entry => entry.queueKind === kind)
  }

  function dropLocal(id: string) {
    items.value = items.value.filter(entry => entry.item_id !== id)
  }

  /**
   * The server answered that this item is no longer pending. The list holds a
   * stale row, so the action is complete once the row is gone: drop it, stay
   * silent, and let the regular refresh reconcile anything else. Retrying or
   * reporting would only ask again about an item the server no longer has.
   */
  function resolvesStaleItem(error: unknown, id: string): boolean {
    if (!isApiErrorCode(error, 'queue_item_not_pending')) return false
    dropLocal(id)
    return true
  }

  async function refresh() {
    const { bot, session } = queueTarget()
    const version = ++requestVersion
    if (!bot || !session) {
      loading.value = false
      items.value = []
      return
    }
    loading.value = true
    try {
      const response = await fetchSessionQueues(bot, session)
      if (version === requestVersion) {
        items.value = merge(response.steer ?? [], response.follow_up ?? [])
        steerSupported.value = response.steer_supported === true
      }
    } finally {
      if (version === requestVersion) loading.value = false
    }
  }

  async function update(item: EditableFollowUpQueueItem) {
    const { bot, session, scope } = queueTarget()
    const id = item.item_id?.trim()
    const text = item.text.trim()
    if (!bot || !session || !id) return
    if (!text) {
      await refresh()
      return
    }
    markBusy(id, true)
    try {
      const updated = item.queueKind === 'steer'
        ? await updateSteerQueueItem(bot, session, id, text)
        : await updateFollowUpQueueItem(bot, session, id, text)
      if (scope !== scopeVersion) return
      const index = items.value.findIndex(entry => entry.item_id === id)
      if (index >= 0) items.value[index] = editable(updated, item.queueKind)
    } catch (error) {
      if (scope !== scopeVersion) return
      if (resolvesStaleItem(error, id)) return
      await refresh()
      if (scope !== scopeVersion) return
      throw error
    } finally {
      if (scope === scopeVersion) markBusy(id, false)
    }
  }

  async function remove(item: EditableFollowUpQueueItem) {
    const { bot, session, scope } = queueTarget()
    const id = item.item_id?.trim()
    if (!bot || !session || !id) return
    markBusy(id, true)
    try {
      if (item.queueKind === 'steer') await deleteSteerQueueItem(bot, session, id)
      else await deleteFollowUpQueueItem(bot, session, id)
      if (scope !== scopeVersion) return
      items.value = items.value.filter(entry => entry.item_id !== id)
    } catch (error) {
      if (scope !== scopeVersion) return
      if (resolvesStaleItem(error, id)) return
      await refresh()
      if (scope !== scopeVersion) return
      throw error
    } finally {
      if (scope === scopeVersion) markBusy(id, false)
    }
  }

  async function steer(item: EditableFollowUpQueueItem) {
    const { bot, session, scope } = queueTarget()
    const id = item.item_id?.trim()
    if (!bot || !session || !id) return
    markBusy(id, true)
    try {
      await promoteFollowUpQueueItemToSteer(bot, session, id)
      if (scope !== scopeVersion) return
      const index = items.value.findIndex(entry => entry.item_id === id)
      if (index >= 0) items.value[index] = editable(items.value[index]!, 'steer')
      await refresh()
    } catch (error) {
      if (scope !== scopeVersion) return
      if (resolvesStaleItem(error, id)) return
      await refresh()
      if (scope !== scopeVersion) return
      throw error
    } finally {
      if (scope === scopeVersion) markBusy(id, false)
    }
  }

  async function reorder(oldIndex: number, newIndex: number) {
    const moved = items.value[oldIndex]
    if (!moved?.item_id || oldIndex === newIndex) return
    const ordered = items.value.slice()
    ordered.splice(oldIndex, 1)
    ordered.splice(newIndex, 0, moved)
    items.value = ordered
    const beforeId = ordered
      .slice(newIndex + 1)
      .find(entry => entry.queueKind === moved.queueKind)?.item_id ?? ''
    const { bot, session, scope } = queueTarget()
    if (!bot || !session) return
    markBusy(moved.item_id, true)
    try {
      // The server answers with the reordered queue of one kind only. Keep the
      // other kind as-is rather than dropping it until the next refresh.
      if (moved.queueKind === 'steer') {
        const response = await reorderSteerQueue(bot, session, moved.item_id, beforeId)
        if (scope !== scopeVersion) return
        items.value = merge(response, ofKind('follow-up'))
      } else {
        const response = await reorderFollowUpQueue(bot, session, moved.item_id, beforeId)
        if (scope !== scopeVersion) return
        items.value = merge(ofKind('steer'), response)
      }
    } catch (error) {
      if (scope !== scopeVersion) return
      if (resolvesStaleItem(error, moved.item_id)) return
      await refresh()
      if (scope !== scopeVersion) return
      throw error
    } finally {
      if (scope === scopeVersion) markBusy(moved.item_id, false)
    }
  }

  watch([() => toValue(botId), () => toValue(sessionId)], () => {
    // A new session owns a new queue view. Late responses from any previous
    // visit (including A → B → A) must not mutate it.
    scopeVersion++
    items.value = []
    busy.value = new Set()
    steerSupported.value = false
    void refresh().catch(() => {})
  }, { immediate: true, flush: 'sync' })
  watch(() => toValue(changeSignal), () => { void refresh().catch(() => {}) })
  if (typeof document !== 'undefined' && typeof document.addEventListener === 'function') {
    // The fallback timer pauses while the host is hidden, and a hidden host
    // can miss the runtime boundary that would have refreshed the list. Coming
    // back visible rebuilds from the server's queue instead of trusting rows
    // that aged behind a backgrounded app.
    const onVisible = () => {
      if (document.visibilityState === 'visible' && hasItems.value) void refresh().catch(() => {})
    }
    document.addEventListener('visibilitychange', onVisible)
    if (getCurrentScope()) onScopeDispose(() => document.removeEventListener('visibilitychange', onVisible))
  }

  function stopAutoRefresh() {
    if (refreshTimer === undefined) return
    clearInterval(refreshTimer)
    refreshTimer = undefined
  }

  function syncAutoRefresh() {
    stopAutoRefresh()
    if (!toValue(active) || !hasItems.value) return
    refreshTimer = setInterval(() => {
      if (loading.value || (typeof document !== 'undefined' && document.visibilityState === 'hidden')) return
      void refresh().catch(() => {})
    }, QUEUE_FALLBACK_REFRESH_MS)
  }

  watch([() => toValue(active), hasItems], syncAutoRefresh)
  if (getCurrentScope()) onScopeDispose(() => {
    scopeVersion++
    requestVersion++
    stopAutoRefresh()
  })

  return { items, loading, steerSupported, busy, hasItems, refresh, update, remove, steer, reorder }
}
