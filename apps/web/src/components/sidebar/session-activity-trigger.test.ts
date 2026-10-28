// @vitest-environment jsdom
import { createApp, nextTick, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import en from '@/i18n/locales/en.json'
import SessionActivityTrigger from './session-activity-trigger.vue'

const api = vi.hoisted(() => ({ session: vi.fn(), tasks: vi.fn(), delegations: vi.fn(), select: vi.fn(), push: vi.fn(), open: vi.fn(), close: vi.fn(), global: vi.fn() }))
vi.mock('@memohai/sdk', () => ({ getBotsByBotIdSessionsBySessionId: api.session, getBotsByBotIdSessionsBySessionIdTasks: api.tasks, getBotsByBotIdSessionsBySessionIdDelegations: api.delegations }))
const bot = ref('bot')
vi.mock('@/store/chat-list', () => ({ useChatStore: () => ({
  get currentBotId() { return bot.value },
  bots: [{ id: 'bot', display_name: 'Supervisor' }, { id: 'expert', display_name: 'Image expert' }, { id: 'other', display_name: 'Unrelated Bot' }],
  runtimeObservationFor: () => ({ synchronized: true, run: { status: 'running' } }), selectBot: api.select,
}) }))
vi.mock('@/pages/home/composables/useChatViewContext', () => ({ useChatViewTarget: () => ref({ botId: 'bot', sessionId: 'root', viewId: 'chat' }) }))
vi.mock('@/store/workspace-tabs', () => ({ useWorkspaceTabsStore: () => ({ api: {}, openSessionChat: api.open, closeMobileNav: api.close, openMobileNav: api.global }) }))
vi.mock('vue-router', async importOriginal => ({ ...await importOriginal<typeof import('vue-router')>(), useRouter: () => ({ push: api.push }) }))
let app: ReturnType<typeof createApp> | undefined, root: HTMLDivElement
const sessions: Record<string, { id: string, bot_id: string, title: string, parent_session_id?: string }> = {
  root: { bot_id: 'bot', id: 'root', title: 'Conversation' },
  a: { bot_id: 'bot', id: 'a', title: 'Review', parent_session_id: 'root' },
  b: { bot_id: 'bot', id: 'b', title: 'Deep Agent', parent_session_id: 'a' },
  expert: { bot_id: 'expert', id: 'expert', title: 'Expert conversation' },
}
const task = (id: string, parent: string) => ({ task_type: 'subagent', task_id: `opaque-${id}`, session_id: id, parent_session_id: 'root', direct_parent_session_id: parent, status: 'running', summary: 'PRIVATE prompt body', result_summary: { text: 'PRIVATE result body' } })
async function settle() { await nextTick(); for (let i = 0; i < 70; i++) await Promise.resolve(); await nextTick() }
function button(text: string) { return [...document.querySelectorAll<HTMLButtonElement>('button')].find(node => node.textContent?.trim() === text)! }
async function mount(kind: 'tasks' | 'bots' = 'tasks') {
  root = document.createElement('div'); document.body.append(root)
  app = createApp(SessionActivityTrigger, { kind, botId: 'bot', sessionId: 'root' })
  app.use(createI18n({ legacy: false, locale: 'en', messages: { en } })); app.mount(root); await settle()
  button(kind === 'tasks' ? en.chat.tasks.title : en.bots.title).click(); await settle()
}
beforeEach(() => {
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  localStorage.clear(); localStorage.setItem('token', 'fixture-account'); bot.value = 'bot'
  api.session.mockReset().mockImplementation(({ path }) => Promise.resolve({ data: sessions[path.session_id] }))
  api.tasks.mockReset().mockImplementation(({ path }) => Promise.resolve({ data: { items: path.session_id === 'root' ? [task('a', 'root'), task('b', 'a')] : [] } }))
  api.delegations.mockReset().mockImplementation(({ path }) => Promise.resolve({ data: { items: path.session_id === 'root' ? [{ source_session_id: 'root', target_bot_id: 'expert', target_session_id: 'expert', state: 'running', target_run_state: 'waiting_decision', delegation_id: 'original-delegation' }] : [] } }))
  api.select.mockReset().mockImplementation(async id => { bot.value = id })
  api.open.mockReset(); api.close.mockReset(); api.global.mockReset(); api.push.mockReset().mockResolvedValue(undefined)
})
afterEach(() => { app?.unmount(); root?.remove(); app = undefined; document.body.innerHTML = ''; localStorage.clear(); vi.unstubAllGlobals() })

describe('conversation monitor entry', () => {
  it('opens only this conversation\'s task Agents, with names/states and genuine Agent nesting', async () => {
    await mount()
    const dialog = document.querySelector('[role="dialog"]')!
    expect(dialog.textContent).toContain('Review'); expect(dialog.textContent).toContain('Deep Agent')
    expect(dialog.querySelectorAll('ul ul')).toHaveLength(1)
    for (const hidden of ['Supervisor', 'Image expert', 'Unrelated Bot', 'PRIVATE', 'opaque-', 'original-delegation']) expect(dialog.textContent).not.toContain(hidden)
    expect(api.global).not.toHaveBeenCalled(); expect(api.open).not.toHaveBeenCalled()
  })
  it('opens the exact downstream Agent session on a whole-row click', async () => {
    await mount()
    const row = [...document.querySelectorAll<HTMLButtonElement>('button')].find(node => node.textContent?.includes('Deep Agent'))!
    row.click(); await settle()
    expect(api.select).toHaveBeenCalledWith('bot', { sessionId: 'b' })
    expect(api.push).toHaveBeenCalledWith({ name: 'bot', params: { botName: 'bot' }, query: { session: 'b' } })
    expect(api.open).toHaveBeenCalledExactlyOnceWith({ sessionId: 'b', title: 'Deep Agent' })
    expect(api.global).not.toHaveBeenCalled()
  })
  it('preserves distinct same-name Agents and routes the clicked sibling rather than its Bot or first Agent', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/bot/:botName', name: 'bot', component: {} }] })
    await router.push({ name: 'bot', params: { botName: 'bot' }, query: { session: 'root' } })
    api.push.mockImplementation(target => router.push(target))
    api.session.mockImplementation(({ path }) => Promise.resolve({ data: path.session_id === 'root' ? sessions.root
      : { id: path.session_id, bot_id: 'bot', title: 'Same Agent', parent_session_id: 'root' } }))
    api.tasks.mockResolvedValue({ data: { items: [task('a', 'root'), { ...task('b', 'root'), status: 'unknown' }] } })
    api.delegations.mockResolvedValue({ data: { items: [] } })
    await mount()
    const rows = [...document.querySelectorAll<HTMLButtonElement>('[role="dialog"] li > button')]
    expect(rows).toHaveLength(2)
    expect(rows.every(row => row.textContent?.includes('Same Agent'))).toBe(true)
    expect(rows[0]!.textContent).toContain(en.chat.tasks.statuses.running)
    expect(rows[1]!.textContent).toContain(en.chat.tasks.statuses.unknown)
    rows[1]!.click(); await settle()
    expect(api.select).toHaveBeenCalledExactlyOnceWith('bot', { sessionId: 'b' })
    expect(router.currentRoute.value.fullPath).toBe('/bot/bot?session=b')
    expect(api.open).toHaveBeenCalledExactlyOnceWith({ sessionId: 'b', title: 'Same Agent' })
  })
  it('keeps a linked Bot\'s task Agent without making the linked Bot a task entry', async () => {
    api.session.mockImplementation(({ path }) => Promise.resolve({ data: path.session_id === 'expert-child'
      ? { id: 'expert-child', bot_id: 'expert', title: 'Expert task Agent', parent_session_id: 'expert' } : sessions[path.session_id] }))
    api.tasks.mockImplementation(({ path }) => Promise.resolve({ data: { items: path.session_id === 'expert'
      ? [{ ...task('expert-child', 'expert'), parent_session_id: 'expert', status: 'waiting_decision' }] : [] } }))
    await mount()
    const dialog = document.querySelector('[role="dialog"]')!
    expect(dialog.textContent).not.toContain('Image expert'); expect(dialog.textContent).not.toContain('Supervisor')
    const row = dialog.querySelector<HTMLButtonElement>('li > button')!
    expect(row.textContent).toContain('Expert task Agent'); expect(row.textContent).toContain(en.chat.tasks.statuses.waiting_decision)
    row.click(); await settle()
    expect(api.select).toHaveBeenCalledExactlyOnceWith('expert', { sessionId: 'expert-child' })
    expect(api.push).toHaveBeenCalledWith({ name: 'bot', params: { botName: 'expert' }, query: { session: 'expert-child' } })
    expect(api.open).toHaveBeenCalledExactlyOnceWith({ sessionId: 'expert-child', title: 'Expert task Agent' })
  })
  it('shows the existing empty state when only Bot and old computer receipts have no task Agent session', async () => {
    api.tasks.mockResolvedValue({ data: { items: [{ task_type: 'computer_operation', task_id: 'old-receipt', session_id: 'root', parent_session_id: 'root', status: 'submitted' }] } })
    await mount()
    const dialog = document.querySelector('[role="dialog"]')!
    expect(dialog.textContent).toContain(en.chat.tasks.empty)
    expect(dialog.querySelectorAll('li > button')).toHaveLength(0)
    expect(api.select).not.toHaveBeenCalled(); expect(api.open).not.toHaveBeenCalled()
  })
  it.each([401, 403, 404])('does not substitute a Bot or another Agent for a missing/inaccessible old Agent (%s)', async status => {
    api.tasks.mockResolvedValue({ data: { items: [task('old-agent', 'root')] } })
    api.session.mockImplementation(({ path }) => path.session_id === 'old-agent' ? Promise.reject({ status }) : Promise.resolve({ data: sessions[path.session_id] }))
    await mount()
    const dialog = document.querySelector('[role="dialog"]')!
    expect(dialog.querySelectorAll('li > button')).toHaveLength(0)
    expect(api.select).not.toHaveBeenCalled(); expect(api.push).not.toHaveBeenCalled(); expect(api.open).not.toHaveBeenCalled()
  })
  it.each([401, 403, 404])('rechecks the clicked Agent and never falls back after navigation denial (%s)', async status => {
    await mount()
    api.session.mockRejectedValue({ status })
    const row = [...document.querySelectorAll<HTMLButtonElement>('button')].find(node => node.textContent?.includes('Deep Agent'))!
    row.click(); await settle()
    expect(api.select).not.toHaveBeenCalled(); expect(api.push).not.toHaveBeenCalled(); expect(api.open).not.toHaveBeenCalled()
  })
  it.each([{ id: 'a', bot_id: 'bot' }, { id: 'b', bot_id: 'expert' }])('rejects a changed Agent session identity $id/$bot_id before routing', async returned => {
    await mount()
    api.session.mockResolvedValue({ data: returned })
    const row = [...document.querySelectorAll<HTMLButtonElement>('button')].find(node => node.textContent?.includes('Deep Agent'))!
    row.click(); await settle()
    expect(api.select).not.toHaveBeenCalled(); expect(api.push).not.toHaveBeenCalled(); expect(api.open).not.toHaveBeenCalled()
  })
  it('keeps an unnamed old Agent as an Agent entry without substituting the Bot name or prompt', async () => {
    api.session.mockImplementation(({ path }) => Promise.resolve({ data: path.session_id === 'a' ? { ...sessions.a, title: '' } : sessions[path.session_id] }))
    api.tasks.mockResolvedValue({ data: { items: [task('a', 'root')] } })
    await mount()
    const dialog = document.querySelector('[role="dialog"]')!
    expect(dialog.textContent).toContain(en.chat.tasks.types.subagent)
    expect(dialog.textContent).not.toContain('Supervisor'); expect(dialog.textContent).not.toContain('PRIVATE')
    dialog.querySelector<HTMLButtonElement>('li > button')!.click(); await settle()
    expect(api.select).toHaveBeenCalledExactlyOnceWith('bot', { sessionId: 'a' })
    expect(api.open).toHaveBeenCalledExactlyOnceWith({ sessionId: 'a', title: '' })
  })
  it('limits the Bot entry to this conversation and navigates its linked original session', async () => {
    await mount('bots')
    const dialog = document.querySelector('[role="dialog"]')!
    expect(dialog.textContent).toContain('Supervisor'); expect(dialog.textContent).toContain('Image expert')
    expect(dialog.textContent).not.toContain('Review'); expect(dialog.textContent).not.toContain('Unrelated Bot')
    const row = [...dialog.querySelectorAll<HTMLButtonElement>('button')].find(node => node.textContent?.includes('Image expert'))!
    row.click(); await settle()
    expect(api.select).toHaveBeenCalledWith('expert', { sessionId: 'expert' })
    expect(api.push).toHaveBeenCalledWith({ name: 'bot', params: { botName: 'expert' }, query: { session: 'expert' } })
    expect(api.open).toHaveBeenCalledExactlyOnceWith({ sessionId: 'expert', title: 'Expert conversation' })
  })
  it('keeps one pending navigation for repeated clicks and verifies the returned session identity', async () => {
    await mount()
    let resolve!: (value: unknown) => void
    api.session.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    const row = [...document.querySelectorAll<HTMLButtonElement>('button')].find(node => node.textContent?.includes('Deep Agent'))!
    const calls = api.session.mock.calls.length
    row.click(); row.click(); await settle()
    expect(api.session).toHaveBeenCalledTimes(calls + 1)
    resolve({ data: sessions.b }); await settle(); expect(api.open).toHaveBeenCalledTimes(1)
  })
})
