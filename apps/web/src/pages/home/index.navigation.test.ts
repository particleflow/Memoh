// @vitest-environment jsdom
import { createApp, nextTick, reactive } from 'vue'
import { createPinia, defineStore } from 'pinia'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import Home from './index.vue'

const api = vi.hoisted(() => ({ bot: vi.fn(), session: vi.fn(), lookup: vi.fn(), open: vi.fn(), select: vi.fn(), task: vi.fn() }))
vi.mock('@/composables/api/useBot', () => ({ fetchBot: api.bot }))
vi.mock('@/composables/api/useChat', () => ({ fetchSession: api.session }))
vi.mock('@memohai/sdk', () => ({ getBotsByBotIdTasksByTaskTypeByRecordId: api.task, getBotsById: api.lookup, getBotsByBotIdAgents: vi.fn() }))
vi.mock('@felinic/ui', () => ({ PanePlaceholder: { template: '<div><slot /></div>' }, toast: { error: vi.fn() } }))
vi.mock('./components/chat-workspace.vue', () => ({ default: { template: '<div data-chat />' } }))
vi.mock('./components/contact-choice-dialog.vue', () => ({ default: { render: () => null } }))
vi.mock('./composables/useComposerPrefillResolver', () => ({ useComposerPrefillResolver: vi.fn() }))
vi.mock('@/store/composer-prefill', () => ({ useComposerPrefillStore: () => ({ pending: null }) }))
let workspace: ReturnType<typeof workspaceFixture>
function workspaceFixture() {
  return reactive({ activeId: '', activePanelIsChat: true, api: { activePanel: null as null | { id: string, params: Record<string, unknown> } }, openSessionChatPinned: api.open })
}
vi.mock('@/store/workspace-tabs', () => ({ useWorkspaceTabsStore: () => workspace }))
function activatePanel(id: string, params: Record<string, unknown>) {
  workspace.api.activePanel = { id, params }; workspace.activeId = id
}
const useChat = defineStore('navigation-fixture', {
  state: () => ({ currentBotId: 'source', bots: [{ id: 'source', name: 'source' }, { id: 'expert', name: 'expert' }], activeSession: null, loadingChats: false }),
  actions: {
    async selectBot(botId: string, options?: { sessionId?: string }) { this.currentBotId = botId; await api.select(botId, options) },
    cancelContactChoice() {}, isSessionTentative() { return false },
  },
})
vi.mock('@/store/chat-list', () => ({ useChatStore: () => useChat() }))
let app: ReturnType<typeof createApp> | undefined, root: HTMLDivElement
async function settle() { for (let i = 0; i < 40; i++) { await Promise.resolve(); await nextTick() } }
async function mount(path: string) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/bot/:botName', name: 'bot', component: Home }, { path: '/settings', name: 'settings', component: { render: () => null } }] })
  await router.push(path)
  root = document.createElement('div'); document.body.append(root)
  app = createApp(Home); app.use(createPinia()); app.use(router)
  app.use(createI18n({ legacy: false, locale: 'en', messages: { en: {} }, missingWarn: false, fallbackWarn: false }))
  app.mount(root)
  return router
}
beforeEach(() => {
  workspace = workspaceFixture()
  api.bot.mockReset(); api.session.mockReset().mockImplementation(async (bot, session) => ({ id: session, bot_id: bot, title: session }))
  api.lookup.mockReset().mockImplementation(async ({ path }) => ({ data: { name: path.id } }))
  api.open.mockReset(); api.select.mockReset(); api.task.mockReset()
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
  expect(api.open).toHaveBeenCalledExactlyOnceWith({ sessionId: 'expert-history', title: 'expert-history', taskAnchor: undefined })
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

it('returns to the contact when the same Bot URL loses its task deep link', async () => {
  const router = await mount('/bot/source?session=task-history'); await settle()
  api.select.mockClear(); api.open.mockClear()
  await router.push('/bot/source'); await settle()
  expect(api.select).toHaveBeenCalledExactlyOnceWith('source', undefined)
  expect(api.open).not.toHaveBeenCalled()
})

it('resolves a same-Bot cold entrance even when the stored Bot already matches', async () => {
  await mount('/bot/source'); await settle()
  expect(api.select).toHaveBeenCalledExactlyOnceWith('source', undefined)
})

it.each(['?session=', '?session=first&session=second'])('rejects malformed task links without opening contact (%s)', async query => {
  await mount('/bot/source' + query); await settle()
  expect(api.select).not.toHaveBeenCalled()
  expect(api.open).not.toHaveBeenCalled()
  expect(api.session).not.toHaveBeenCalled()
})

const runQuery = (id: string) => `?session=history&task_bot=source&task_type=native_agent&task_id=${id}&task_record=${id}&run=${id}`
function taskResponse(id: string) {
  return { data: { task_type: 'native_agent', task_id: id, record_id: id, destination: { bot_id: 'source', session_id: 'history', run_id: id, turn_id: id+'-turn' } } }
}
it('changes runs within one session without letting a late selection restore the old anchor', async () => {
  api.task.mockImplementation(async ({ path }) => taskResponse(path.record_id))
  let release!: () => void
  api.select.mockImplementationOnce(() => new Promise<void>(resolve => { release=resolve }))
  const router=await mount('/bot/source'+runQuery('old')); await settle()
  await router.push('/bot/source'+runQuery('new')); await settle()
  release(); await settle()
  expect(api.open).toHaveBeenCalledTimes(1)
  expect(api.open.mock.calls[0]?.[0].taskAnchor).toMatchObject({ runId: 'new', turnId: 'new-turn' })
  expect(router.currentRoute.value.query.run).toBe('new')
})
it.each(['wrong-run', 'denied', 'missing-session'])('rejects an untrusted task link before selecting history (%s)', async kind => {
  api.task.mockResolvedValue(taskResponse('old'))
  if (kind==='denied') api.task.mockRejectedValue({ status: 403 })
  const path=kind==='missing-session' ? '/bot/source?run=old' : '/bot/source'+runQuery(kind==='wrong-run' ? 'new' : 'old')
  await mount(path); await settle()
  expect(api.open).not.toHaveBeenCalled(); expect(api.select).not.toHaveBeenCalled(); expect(api.session).not.toHaveBeenCalled()
})

it('does not reopen contact for unrelated URL query changes', async () => {
  const router=await mount('/bot/source'); await settle(); api.select.mockClear()
  await router.replace('/bot/source?view=unchanged'); await settle()
  expect(api.select).not.toHaveBeenCalled()
})

it('cancels a pending task check on another task route without accepting late authorization', async () => {
  let release!: (value: ReturnType<typeof taskResponse>) => void
  api.task.mockImplementationOnce(() => new Promise(resolve => { release=resolve }))
    .mockImplementation(async ({ path }) => taskResponse(path.record_id))
  const router=await mount('/bot/source'+runQuery('old')); await settle()
  const signal=api.task.mock.calls[0]![0].signal as AbortSignal
  await router.push('/bot/source'+runQuery('new')); await settle()
  expect(signal.aborted).toBe(true)
  expect(api.open).toHaveBeenCalledTimes(1)
  expect(api.open.mock.calls[0]![0].taskAnchor.runId).toBe('new')
  release(taskResponse('old')); await settle()
  expect(api.open).toHaveBeenCalledTimes(1)
  expect(root.querySelector('[data-chat]')?.classList.contains('invisible')).toBe(false)
})

it.each(['settings', 'dispose'])('cancels task validation when leaving the history owner (%s)', async destination => {
  api.task.mockImplementation(() => new Promise(() => {}))
  const router=await mount('/bot/source'+runQuery('old')); await settle()
  const signal=api.task.mock.calls[0]![0].signal as AbortSignal
  if (destination==='settings') await router.push('/settings')
  else { app!.unmount(); app=undefined }
  await settle()
  expect(signal.aborted).toBe(true)
  expect(api.open).not.toHaveBeenCalled()
})


it('clears the old task link when the contact tab becomes active and preserves unrelated query', async () => {
  api.task.mockImplementation(async ({ path }) => taskResponse(path.record_id))
  const router = await mount('/bot/source' + runQuery('old') + '&view=retained'); await settle()
  api.select.mockClear()
  activatePanel('contact', { sessionId: 'contact-C', contactView: true }); await settle()
  expect(router.currentRoute.value.fullPath).toBe('/bot/source?view=retained')
  expect(api.select).toHaveBeenCalledExactlyOnceWith('source', undefined)
})

it('restores the original task anchor on task tab activation and rechecks authorization', async () => {
  const router = await mount('/bot/source'); await settle()
  api.task.mockResolvedValue(taskResponse('old'))
  activatePanel('task', { sessionId: 'history', taskAnchor: { directoryBotId: 'source', taskType: 'native_agent', taskId: 'old', recordId: 'old', runId: 'old', turnId: 'old-turn' } }); await settle()
  expect(router.currentRoute.value.query).toEqual({ session: 'history', task_bot: 'source', task_type: 'native_agent', task_id: 'old', task_record: 'old', run: 'old' })
  expect(api.task).toHaveBeenCalledOnce()
  expect(api.open.mock.calls.at(-1)?.[0].taskAnchor.runId).toBe('old')
})

it('does not rewrite a pending or denied task entrance from a restored contact tab', async () => {
  let reject!: (error: unknown) => void
  api.task.mockImplementation(() => new Promise((_, fail) => { reject = fail }))
  const router = await mount('/bot/source' + runQuery('old')); await settle()
  activatePanel('restored-contact', { sessionId: 'contact-C', contactView: true }); await settle()
  expect(router.currentRoute.value.query.run).toBe('old')
  reject({ status: 403 }); await settle()
  expect(router.currentRoute.value.query.run).toBe('old')
  expect(api.select).not.toHaveBeenCalled()
})

it('ignores non-chat tab activation while a task link is selected', async () => {
  const router = await mount('/bot/source?session=history'); await settle()
  workspace.activePanelIsChat = false
  activatePanel('file', {}); await settle()
  expect(router.currentRoute.value.query.session).toBe('history')
})

it('retains a denied task tab link without falling back to contact', async () => {
  const router = await mount('/bot/source'); await settle(); api.select.mockClear()
  api.task.mockRejectedValue({ status: 403 })
  activatePanel('task', { sessionId: 'history', taskAnchor: { directoryBotId: 'source', taskType: 'native_agent', taskId: 'old', recordId: 'old', runId: 'old', turnId: 'old-turn' } }); await settle()
  expect(router.currentRoute.value.query.run).toBe('old')
  expect(api.select).not.toHaveBeenCalled()
  expect(api.open).not.toHaveBeenCalled()
})

it('does not let a late tab-to-URL lookup overwrite a newer route', async () => {
  const router = await mount('/bot/source?session=history'); await settle()
  useChat().bots = []
  let release!: (value: { data: { name: string } }) => void
  api.lookup.mockImplementationOnce(() => new Promise(resolve => { release = resolve }))
  activatePanel('contact', { sessionId: 'contact-C', contactView: true }); await settle()
  await router.push('/settings'); await settle()
  release({ data: { name: 'source' } }); await settle()
  expect(router.currentRoute.value.fullPath).toBe('/settings')
})

it('does not let a late tab-to-URL lookup overwrite the next active tab', async () => {
  const router = await mount('/bot/source?session=history'); await settle()
  useChat().bots = []
  let release!: (value: { data: { name: string } }) => void
  api.lookup.mockImplementationOnce(() => new Promise(resolve => { release = resolve }))
  activatePanel('contact', { sessionId: 'contact-C', contactView: true }); await settle()
  workspace.activePanelIsChat = false
  activatePanel('file', {}); await settle()
  release({ data: { name: 'source' } }); await settle()
  expect(router.currentRoute.value.query.session).toBe('history')
})
