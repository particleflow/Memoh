// @vitest-environment jsdom
import { createApp, nextTick } from 'vue'
import { createPinia, defineStore } from 'pinia'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import Home from './index.vue'

const api = vi.hoisted(() => ({ bot: vi.fn(), session: vi.fn(), lookup: vi.fn(), open: vi.fn(), select: vi.fn() }))
vi.mock('@/composables/api/useBot', () => ({ fetchBot: api.bot }))
vi.mock('@/composables/api/useChat', () => ({ fetchSession: api.session }))
vi.mock('@memohai/sdk', () => ({ getBotsById: api.lookup, getBotsByBotIdAgents: vi.fn() }))
vi.mock('@felinic/ui', () => ({ PanePlaceholder: { template: '<div><slot /></div>' }, toast: { error: vi.fn() } }))
vi.mock('./components/chat-workspace.vue', () => ({ default: { template: '<div data-chat />' } }))
vi.mock('./components/contact-choice-dialog.vue', () => ({ default: { render: () => null } }))
vi.mock('./composables/useComposerPrefillResolver', () => ({ useComposerPrefillResolver: vi.fn() }))
vi.mock('@/store/composer-prefill', () => ({ useComposerPrefillStore: () => ({ pending: null }) }))
vi.mock('@/store/workspace-tabs', () => ({ useWorkspaceTabsStore: () => ({ api: {}, openSessionChatPinned: api.open }) }))
const useChat = defineStore('navigation-fixture', {
  state: () => ({ currentBotId: 'source', bots: [{ id: 'source', name: 'source' }, { id: 'expert', name: 'expert' }], activeSession: null, loadingChats: false }),
  actions: {
    async selectBot(botId: string, options?: { sessionId?: string }) { this.currentBotId = botId; api.select(botId, options) },
    cancelContactChoice() {}, isSessionTentative() { return false },
  },
})
vi.mock('@/store/chat-list', () => ({ useChatStore: () => useChat() }))
let app: ReturnType<typeof createApp> | undefined, root: HTMLDivElement
async function settle() { for (let i = 0; i < 40; i++) { await Promise.resolve(); await nextTick() } }
async function mount(path: string) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/bot/:botName', name: 'bot', component: Home }] })
  await router.push(path)
  root = document.createElement('div'); document.body.append(root)
  app = createApp(Home); app.use(createPinia()); app.use(router)
  app.use(createI18n({ legacy: false, locale: 'en', messages: { en: {} }, missingWarn: false, fallbackWarn: false }))
  app.mount(root)
  return router
}
beforeEach(() => {
  api.bot.mockReset(); api.session.mockReset().mockImplementation(async (bot, session) => ({ id: session, bot_id: bot, title: session }))
  api.lookup.mockReset().mockImplementation(async ({ path }) => ({ data: { name: path.id } }))
  api.open.mockReset(); api.select.mockReset()
})
afterEach(() => { app?.unmount(); root?.remove(); app = undefined })

it('does not combine a late source Bot lookup with the new expert session', async () => {
  let resolve!: (bot: { id: string }) => void
  api.bot.mockImplementationOnce(() => new Promise(done => { resolve = done }))
  const router = await mount('/bot/source-alias?session=source-history')
  await settle(); expect(api.bot).toHaveBeenCalledWith('source-alias')
  await router.push('/bot/expert?session=expert-history'); await settle()
  resolve({ id: 'source' }); await settle()
  expect(api.session).not.toHaveBeenCalledWith('source', 'expert-history')
  expect(api.session).toHaveBeenCalledWith('expert', 'expert-history')
  expect(router.currentRoute.value.fullPath).toBe('/bot/expert?session=expert-history')
  expect(api.open).toHaveBeenCalledExactlyOnceWith({ sessionId: 'expert-history', title: 'expert-history' })
})

it('does not replace a newer expert route after an earlier Bot-name lookup completes', async () => {
  let resolve!: (value: { data: { name: string } }) => void
  const router = await mount('/bot/source?session=source-history'); await settle()
  const chat = useChat(); chat.bots = []
  api.lookup.mockImplementationOnce(() => new Promise(done => { resolve = done }))
  chat.currentBotId = 'source-uncached'; await settle()
  chat.bots = [{ id: 'expert', name: 'expert' }]
  await router.push('/bot/expert?session=expert-history'); await settle()
  resolve({ data: { name: 'source-uncached' } }); await settle()
  expect(router.currentRoute.value.fullPath).toBe('/bot/expert?session=expert-history')
})

it.each([401, 403, 404])('preserves a denied historical route without selecting a different session (HTTP %s)', async status => {
  api.session.mockRejectedValue({ status })
  const router = await mount('/bot/expert?session=expert-history'); await settle()
  expect(api.session).toHaveBeenCalledExactlyOnceWith('expert', 'expert-history')
  expect(api.select).not.toHaveBeenCalled()
  expect(api.open).not.toHaveBeenCalled()
  expect(router.currentRoute.value.fullPath).toBe('/bot/expert?session=expert-history')
})
