import { getCurrentInstance, onActivated, onDeactivated, onScopeDispose, ref, watch } from 'vue'
import type { ChatMessage } from '@/store/chat-list'
import type { TaskHistoryAnchor } from '@/utils/task-history-link'

export function useTaskRunAnchor(options: {
  anchor: () => TaskHistoryAnchor | undefined
  session: () => string | null | undefined
  visible: () => boolean
  messages: () => ChatMessage[]
  loading: () => boolean
  loadingOlder: () => boolean
  hasMore: () => boolean
  validate: (anchor: TaskHistoryAnchor, signal: AbortSignal) => Promise<void>
  loadOlder: () => Promise<number>
  scroll: (id: string) => Promise<boolean>
}) {
  const status = ref<'pending' | 'located' | 'unavailable'>('pending')
  let generation = 0, disposed = false, deactivated = false
  let active: AbortController | undefined
  function invalidate() {
    generation++
    active?.abort()
    active = undefined
    status.value = 'pending'
  }
  async function locate() {
    const anchor = options.anchor()
    if (!anchor || !options.visible() || options.loading() || options.loadingOlder() || active || status.value === 'located' || disposed || deactivated) return
    const version = generation
    const controller = new AbortController()
    active = controller
    const current = () => !disposed && !deactivated && !controller.signal.aborted
      && active === controller && generation === version && options.visible()
    try {
      await options.validate(anchor, controller.signal)
      while (current()) {
        const target = options.messages().find(message => (message.role === 'user' || message.role === 'assistant') && message.turnId === anchor.turnId)
        if (target) {
          // Reveal before scrolling; scrollToMessage waits for the DOM update.
          status.value = 'located'
          if (!await options.scroll(target.id) && current()) status.value = 'unavailable'
          return
        }
        if (!options.hasMore() || await options.loadOlder() === 0) {
          if (current()) status.value = 'unavailable'
          return
        }
      }
    } catch {
      if (current()) status.value = 'unavailable'
    } finally {
      // A superseded request cannot release the new request's admission slot.
      if (active === controller) active = undefined
    }
  }
  watch([options.anchor, options.session], () => {
    invalidate()
    void locate()
  }, { immediate: true, flush: 'sync' })
  watch(options.visible, visible => { invalidate(); if (visible) void locate() }, { flush: 'sync' })
  // A live run may acquire its first history turn after the route was opened.
  watch(() => [options.loading(), options.loadingOlder(), options.messages().length], () => { void locate() }, { flush: 'post' })
  // Dockview's KeepAlive can deactivate a pane without delivering visible=false.
  // Treat reactivation as a fresh admission even when its props are unchanged.
  if (getCurrentInstance()) {
    onDeactivated(() => { deactivated = true; invalidate() })
    onActivated(() => { deactivated = false; invalidate(); void locate() })
  }
  onScopeDispose(() => { disposed = true; invalidate() })
  return { status }
}
