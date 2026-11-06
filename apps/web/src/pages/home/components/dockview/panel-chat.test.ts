// @vitest-environment jsdom
import { createApp, nextTick, reactive, ref } from 'vue'
import { createPinia, defineStore } from 'pinia'
import type { DockviewApi, DockviewPanelApi } from 'dockview-vue'
import { afterEach, expect, it, vi } from 'vitest'
import PanelChat from './panel-chat.vue'

const calls = vi.hoisted(() => ({ bind: vi.fn(), mount: vi.fn(), unbind: vi.fn() }))
const useChat = defineStore('panel-owner-fixture', {
  state: () => ({ currentBotId: 'source' }),
  actions: { bindChatView: calls.bind, unbindChatView: calls.unbind, focusChatView() {} },
})
vi.mock('@/store/chat-list', () => ({ useChatStore: () => useChat() }))
vi.mock('./use-panel-visible', () => ({ usePanelVisible: () => ref(true), usePanelFocused: () => ref(false) }))
vi.mock('../chat-pane.vue', () => ({ default: {
  props: ['sessionId'],
  setup(props: { sessionId?: string }) { calls.mount(useChat().currentBotId, props.sessionId) },
  template: '<div data-pane />',
} }))
let app: ReturnType<typeof createApp> | undefined, root: HTMLDivElement
afterEach(() => { app?.unmount(); root?.remove(); calls.bind.mockClear(); calls.mount.mockClear(); calls.unbind.mockClear() })
async function mount() {
  const params = reactive({ params: { sessionId: 'source-session' }, api: { id: 'chat:1' } as DockviewPanelApi, containerApi: {} as DockviewApi })
  root = document.createElement('div'); document.body.append(root)
  app = createApp(PanelChat, { params }); app.use(createPinia()); app.mount(root); await nextTick()
  return params
}

it('never binds or remounts an old Bot panel under the next Bot before dock cleanup', async () => {
  await mount()
  useChat().currentBotId = 'expert'; await nextTick()
  expect(calls.bind).toHaveBeenCalledExactlyOnceWith('chat:1', { botId: 'source', sessionId: 'source-session', viewId: 'chat:1' }, true)
  expect(calls.mount).toHaveBeenCalledExactlyOnceWith('source', 'source-session')
  expect(root.querySelector('[data-pane]')).toBeNull()
})

it('keeps same-Bot tab changes and the original panel when returning before disposal', async () => {
  const params = await mount()
  params.params.sessionId = 'second-source-session'; await nextTick()
  expect(calls.bind).toHaveBeenLastCalledWith('chat:1', { botId: 'source', sessionId: 'second-source-session', viewId: 'chat:1' }, true)
  useChat().currentBotId = 'expert'; await nextTick()
  useChat().currentBotId = 'source'; await nextTick()
  expect(root.querySelector('[data-pane]')).not.toBeNull()
  expect(calls.bind.mock.calls.every(([, target]) => target.botId === 'source')).toBe(true)
})
