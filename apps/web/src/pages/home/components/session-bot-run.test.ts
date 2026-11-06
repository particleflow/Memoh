// @vitest-environment jsdom
import { createApp, h, nextTick, reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { RuntimeCurrentRunView } from '@/composables/api/useChat.types'
import type { ChatMessage } from '@/store/chat/types'
import en from '@/i18n/locales/en.json'
import SessionBotRun from './session-bot-run.vue'

const store = vi.hoisted(() => ({ runtimeObservationFor: vi.fn(), chatView: vi.fn() }))
vi.mock('@/store/chat-list', () => ({ useChatStore: () => store }))
let app: ReturnType<typeof createApp> | undefined
let root: HTMLDivElement
const original: RuntimeCurrentRunView = {
  run_id: 'native-run-a', turn_id: 'turn-a', generation: 'generation-a', status: 'running',
  started_at: '2026-10-25T00:00:00Z', updated_at: '2026-10-25T00:00:01Z',
  messages: [{ id: 1, type: 'tool', name: 'generate_image', tool_call_id: 'call-a', running: true, input: { prompt: '<img src=x onerror=alert(1)>' } }],
}
async function mount() {
  const state = reactive<{ synchronized: boolean, run: RuntimeCurrentRunView | null }>({ synchronized: true, run: { ...original } })
  const props = reactive({ botId: 'bot-a', sessionId: 'session-a', visible: true })
  store.runtimeObservationFor.mockImplementation((bot: string, session: string) => bot === 'bot-a' && session === 'session-a' ? state : { synchronized: false, run: null })
  store.chatView.mockReturnValue({ transcript: { visibleMessages: { value: [] as ChatMessage[] } } })
  root = document.createElement('div'); document.body.append(root)
  app = createApp({ render: () => h(SessionBotRun, props) })
  app.use(createI18n({ legacy: false, locale: 'en', messages: { en } }))
  app.mount(root); await nextTick()
  return { state, props }
}
async function expand() {
  root.querySelector('button')!.click(); await nextTick(); await nextTick()
}
afterEach(() => { app?.unmount(); root?.remove(); vi.clearAllMocks() })

describe('shared Bot run content without a DialogRoot', () => {
  it('shows a real run and tool identity, then an aborted run with an unconfirmed result', async () => {
    const { state } = await mount(); await expand()
    expect(root.textContent).toContain('native-run-a')
    expect(root.textContent).toContain('call-a')
    expect(root.textContent).toContain(en.chat.botRun.tools.running)
    expect(root.querySelector('img')).toBeNull()
    state.run = { ...original, status: 'aborted' }; await nextTick()
    expect(root.textContent).toContain(en.chat.tasks.statuses.aborted)
    expect(root.textContent).toContain(en.chat.botRun.tools.unconfirmed)
    expect(root.textContent).not.toContain(en.chat.botRun.tools.running)
    expect(root.textContent).not.toContain(en.chat.botRun.tools.received)
  })
  it('clears stale state on disconnect and restores the same run only after synchronization', async () => {
    const { state } = await mount(); await expand()
    state.synchronized = false; await nextTick()
    expect(root.textContent).not.toContain('native-run-a')
    expect(root.textContent).not.toContain('call-a')
    expect(root.textContent).toContain(en.chat.botRun.unavailable)
    state.synchronized = true; await nextTick(); await expand()
    expect(root.textContent).toContain('native-run-a')
    expect(root.textContent).toContain('call-a')
  })
  it('does not show cached content for another Bot/session or a hidden host', async () => {
    const { props } = await mount(); await expand()
    props.sessionId = 'session-b'; await nextTick()
    expect(root.textContent).not.toContain('native-run-a')
    props.sessionId = 'session-a'; props.botId = 'bot-b'; await nextTick()
    expect(root.textContent).not.toContain('native-run-a')
    props.botId = 'bot-a'; props.visible = false; await nextTick()
    expect(root.textContent).not.toContain('native-run-a')
  })
  it('separates an authoritative empty snapshot and configuration activity from task runs', async () => {
    const { state } = await mount()
    state.run = null; await nextTick()
    expect(root.textContent).toContain(en.chat.botRun.empty)
    state.run = { ...original, configuration_only: true }; await nextTick()
    expect(root.textContent).toContain(en.chat.botRun.configuration)
    expect(root.querySelector('button')).toBeNull()
    expect(root.textContent).not.toContain('native-run-a')
  })
})
