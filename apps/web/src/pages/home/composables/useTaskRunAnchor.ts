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
  validate: (anchor: TaskHistoryAnchor) => Promise<void>
  loadOlder: () => Promise<number>
  scroll: (id: string) => Promise<boolean>
}) {
  const status = ref<'pending' | 'located' | 'unavailable'>('pending')
  let generation = 0, busy = false, disposed = false, deactivated = false
  async function locate() {
    const anchor = options.anchor()
    if (!anchor || !options.visible() || options.loading() || options.loadingOlder() || busy || status.value === 'located' || disposed || deactivated) return
    const version = generation
    const current = () => !disposed && !deactivated && generation === version && options.visible()
    busy = true
    try {
      await options.validate(anchor)
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
      busy = false
      if (!disposed && version !== generation) void locate()
    }
  }
  watch([options.anchor, options.session], () => {
    generation++
    status.value = 'pending'
    void locate()
  }, { immediate: true })
  watch(options.visible, visible => { generation++; if (visible) { status.value = 'pending'; void locate() } })
  // A live run may acquire its first history turn after the route was opened.
  watch(() => [options.loading(), options.loadingOlder(), options.messages().length], () => { void locate() }, { flush: 'post' })
  // Dockview's KeepAlive can deactivate a pane without delivering visible=false.
  // Treat reactivation as a fresh admission even when its props are unchanged.
  if (getCurrentInstance()) {
    onDeactivated(() => { deactivated = true; generation++; status.value = 'pending' })
    onActivated(() => { deactivated = false; generation++; status.value = 'pending'; void locate() })
  }
  onScopeDispose(() => { disposed = true; generation++ })
  return { status }
}
