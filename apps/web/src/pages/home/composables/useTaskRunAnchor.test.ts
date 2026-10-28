import { effectScope, nextTick, ref } from 'vue'
import { afterEach, expect, it, vi } from 'vitest'
import type { ChatMessage } from '@/store/chat-list'
import type { TaskHistoryAnchor } from '@/utils/task-history-link'
import { useTaskRunAnchor } from './useTaskRunAnchor'
const scopes: ReturnType<typeof effectScope>[] = []
afterEach(() => { scopes.splice(0).forEach(s => s.stop()) })
const message = (turn: string): ChatMessage => ({ id: turn+'-message', turnId: turn, role: 'assistant', messages: [], timestamp: '', streaming: false })
const anchor = (turn: string): TaskHistoryAnchor => ({ directoryBotId: 'source', taskType: 'native_agent', taskId: turn, recordId: turn+'-occurrence', runId: turn+'-run', turnId: turn })
async function settle() { for (let i=0;i<20;i++) { await nextTick(); await Promise.resolve() } }
function fixture() {
  const selected = ref(anchor('old')), messages = ref<ChatMessage[]>([message('new')]), visible = ref(true), more = ref(true)
  const load = vi.fn(async () => { more.value=false; messages.value.unshift(message('old')); return 1 })
  const validate = vi.fn(async () => {}), scroll = vi.fn(async () => true)
  const scope=effectScope(); scopes.push(scope)
  const state=scope.run(() => useTaskRunAnchor({ anchor: () => selected.value, session: () => 'session', visible: () => visible.value, messages: () => messages.value, loading: () => false, loadingOlder: () => false, hasMore: () => more.value, loadOlder: load, validate, scroll }))!
  return { selected, messages, visible, more, load, validate, scroll, state }
}
it('pages to the old run, stays there during new live messages, and switches runs in the same session', async () => {
  const f=fixture(); await settle()
  expect(f.state.status.value).toBe('located'); expect(f.scroll).toHaveBeenLastCalledWith('old-message')
  f.messages.value.push(message('latest')); await settle(); expect(f.scroll).toHaveBeenCalledTimes(1)
  f.selected.value=anchor('new'); await settle(); expect(f.scroll).toHaveBeenLastCalledWith('new-message')
})
it('shows unavailable for missing history instead of scrolling to latest, then accepts the exact live turn', async () => {
  const f=fixture(); f.load.mockImplementation(async () => { f.more.value=false; return 0 }); await settle()
  expect(f.state.status.value).toBe('unavailable'); expect(f.scroll).not.toHaveBeenCalled()
  f.messages.value.push(message('old')); await settle(); expect(f.scroll).toHaveBeenLastCalledWith('old-message')
})
it('rechecks restored panels before showing them', async () => {
  const f=fixture(); await settle(); f.visible.value=false; await settle()
  f.validate.mockRejectedValue({ status: 403 }); f.visible.value=true; await settle()
  expect(f.state.status.value).toBe('unavailable'); expect(f.scroll).toHaveBeenCalledTimes(1)
})

it('ignores a stale authorization completion when another run was selected', async () => {
  const f=fixture(); await settle(); f.visible.value=false; await settle()
  let release!: () => void
  f.validate.mockImplementationOnce(() => new Promise<void>(resolve => { release=resolve }))
  f.visible.value=true; await settle()
  f.selected.value=anchor('new'); await settle()
  release(); await settle()
  expect(f.scroll).toHaveBeenCalledTimes(2)
  expect(f.scroll).toHaveBeenLastCalledWith('new-message')
  expect(f.state.status.value).toBe('located')
})
