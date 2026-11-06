// @vitest-environment jsdom
import { createApp, defineComponent, h, KeepAlive, nextTick, ref } from 'vue'
import { expect, it, vi } from 'vitest'
import type { ChatMessage } from '@/store/chat-list'
import { useTaskRunAnchor } from './useTaskRunAnchor'

it('reauthorizes an actual cached pane even when visible props never changed', async () => {
  const shown = ref(true), validate = vi.fn(async () => {}), scroll = vi.fn(async () => true)
  let state!: ReturnType<typeof useTaskRunAnchor>
  const pane = defineComponent({ setup() {
    state=useTaskRunAnchor({
      anchor: () => ({ directoryBotId: 'bot', taskType: 'native_agent', taskId: 'task', recordId: 'record', runId: 'run', turnId: 'turn' }),
      session: () => 'session', visible: () => true,
      messages: () => [{ id: 'message', turnId: 'turn', role: 'user' } as ChatMessage],
      loading: () => false, loadingOlder: () => false, hasMore: () => false,
      validate, scroll, loadOlder: async () => 0,
    })
    return () => h('div', state.status.value)
  } })
  const root=document.createElement('div');document.body.append(root)
  const app=createApp({ render: () => h(KeepAlive, () => shown.value ? h(pane) : null) });app.mount(root)
  async function settle() { for(let i=0;i<20;i++) { await nextTick(); await Promise.resolve() } }
  try {
    await settle(); expect(state.status.value).toBe('located')
    shown.value=false; await settle(); const scrolls=scroll.mock.calls.length
    validate.mockRejectedValue({ status: 403 }); shown.value=true; await settle()
    expect(state.status.value).toBe('unavailable');expect(scroll).toHaveBeenCalledTimes(scrolls)
  } finally { app.unmount();root.remove() }
})
