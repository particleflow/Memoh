// @vitest-environment jsdom
import { createApp, h, nextTick, reactive, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { BotdelegationView, SessiontaskTask } from '@memohai/sdk'
import type { RuntimeCurrentRunView, SessionSummary } from '@/composables/api/useChat.types'
import en from '@/i18n/locales/en.json'
import SessionActivity from './session-activity.vue'

const api = vi.hoisted(() => ({ tasks: vi.fn(), delegations: vi.fn(), delegation: vi.fn(), session: vi.fn(), open: vi.fn(), close: vi.fn(), select: vi.fn(), push: vi.fn() }))
vi.mock('@memohai/sdk', () => ({
  getBotsByBotIdSessionsBySessionIdTasks: api.tasks,
  getBotsByBotIdSessionsBySessionIdDelegations: api.delegations,
  getBotsByBotIdSessionsBySessionIdDelegationsByDelegationId: api.delegation,
  getBotsByBotIdSessionsBySessionId: api.session,
}))
const bot = ref('bot'), session = ref('parent')
const hostVisible = ref(true)
const known = ref<SessionSummary[]>([])
const observations = reactive<Record<string, { synchronized: boolean, run: RuntimeCurrentRunView | null }>>({})
const sidebar = reactive({ sidebarOpen: true, sidebarView: 'sessions', api: {} })
vi.mock('@/store/chat-list', () => ({ useChatStore: () => ({
  get currentBotId() { return bot.value }, get sessionId() { return session.value },
  get knownSessions() { return known.value }, bots: [{ id: 'specialist', display_name: 'Image specialist' }],
  knownSessionSummary: (id: string) => known.value.find(value => value.id === id),
  runtimeObservationFor: (_bot: string, id: string) => observations[id],
  workspaceTargetSelectionFor: () => ({ targetId: 'native' }), selectBot: api.select,
}) }))
vi.mock('@/store/workspace-tabs', () => ({ useWorkspaceTabsStore: () => ({
  get sidebarOpen() { return sidebar.sidebarOpen }, get sidebarView() { return sidebar.sidebarView },
  get api() { return sidebar.api }, openSessionChat: api.open, closeMobileNav: api.close,
}) }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: api.push }) }))
let app: ReturnType<typeof createApp> | undefined, root: HTMLDivElement
const task = (id: string, status: SessiontaskTask['status']): SessiontaskTask => ({
  task_id: id, task_type: 'subagent', session_id: 'child', parent_session_id: 'parent', status,
  summary: 'Do not show this diagnostic paragraph', content_available: false, attachments_available: false, created_at: '', updated_at: '',
})
const delegation: BotdelegationView = {
  delegation_id: 'delegation', source_session_id: 'parent', target_bot_id: 'specialist', target_session_id: 'expert-session',
  target_run_id: 'expert-run', target_run_state: 'running', request_id: 'request', state: 'running', delivery_state: 'pending',
  attachments: [], created_at: '', updated_at: '',
}
async function settled() { await nextTick(); await vi.advanceTimersByTimeAsync(20); await nextTick() }
async function mount() {
  root = document.createElement('div'); document.body.append(root)
  app = createApp({ render: () => h(SessionActivity, { visible: hostVisible.value }) })
  app.use(createI18n({ legacy: false, locale: 'en', messages: { en } })); app.mount(root); await settled()
}
beforeEach(() => {
  vi.useFakeTimers(); localStorage.clear(); localStorage.setItem('token', 'test-identity')
  bot.value = 'bot'; session.value = 'parent'; sidebar.sidebarOpen = true; sidebar.sidebarView = 'sessions'
  hostVisible.value = true
  known.value = [{ id: 'parent', bot_id: 'bot', title: 'Generate snow landscape' }, { id: 'child', bot_id: 'bot', title: 'Review image reference' }]
  Object.keys(observations).forEach(key => { delete observations[key] })
  Object.values(api).forEach(mock => mock.mockReset())
  api.tasks.mockResolvedValue({ data: { items: [] } }); api.delegations.mockResolvedValue({ data: { items: [] } })
  api.delegation.mockResolvedValue({ data: delegation })
  api.session.mockResolvedValue({ data: { id: 'expert-session', bot_id: 'specialist', title: 'Specialist original session' } })
  api.select.mockImplementation(async (id: string) => { bot.value = id }); api.push.mockResolvedValue(undefined)
})
afterEach(() => { app?.unmount(); root?.remove(); vi.useRealTimers(); vi.restoreAllMocks() })

describe('compact activity outside the task dialog', () => {
  it('does not keep a hidden desktop/mobile navigation host polling', async () => {
    hostVisible.value = false; await mount(); expect(api.tasks).not.toHaveBeenCalled()
    hostVisible.value = true; await settled(); expect(api.tasks).toHaveBeenCalledOnce()
    hostVisible.value = false; await settled(); await vi.advanceTimersByTimeAsync(15000)
    expect(api.tasks).toHaveBeenCalledOnce(); expect(root.querySelector('section')).toBeNull()
  })
  it('shows title and real status without modal, IDs, diagnostic text or terminal records', async () => {
    observations.parent = { synchronized: true, run: { run_id: 'opaque-run', turn_id: 'turn', generation: 'g', status: 'running', started_at: '', updated_at: '', messages: [] } }
    api.tasks.mockResolvedValue({ data: { items: [task('opaque-task', 'waiting_decision'), task('old-aborted', 'aborted'), task('uncertain', 'unknown')] } })
    await mount()
    expect(root.textContent).toContain('Generate snow landscape'); expect(root.textContent).toContain('Review image reference')
    expect(root.textContent).toContain(en.chat.tasks.statuses.running); expect(root.textContent).toContain(en.chat.tasks.statuses.waiting_decision)
    expect(root.textContent).not.toMatch(/opaque|old-aborted|uncertain|diagnostic|Run ID/)
    expect(root.querySelector('[role=dialog]')).toBeNull()
    root.querySelectorAll('button')[1]!.click(); await settled()
    expect(api.open).toHaveBeenCalledWith({ sessionId: 'child', title: 'Review image reference' })
  })
  it.each(['unknown', 'completed', 'aborted', 'failed', 'lost', 'process_exited', 'not_started', 'rejected'] as const)('does not put %s tasks in the active list', async status => {
    api.tasks.mockResolvedValue({ data: { items: [task('private-id', status)] } }); await mount()
    expect(root.querySelector('section')).toBeNull(); expect(root.textContent).not.toContain('No tasks')
  })
  it('reads existing continuation pages and deduplicates without making users page through history', async () => {
    api.tasks.mockImplementation(async ({ query }: { query: { cursor?: string } }) => query.cursor
      ? { data: { items: [task('active', 'running'), task('active', 'running')] } }
      : { data: { items: [task('terminal', 'completed')], next_cursor: 'next-original-cursor' } })
    await mount()
    expect(api.tasks).toHaveBeenCalledTimes(2); expect(root.querySelectorAll('button')).toHaveLength(1)
    expect(root.textContent).toContain('Review image reference')
  })
  it('uses the existing specialist relation and target-session authorization to open the original conversation', async () => {
    api.delegations.mockResolvedValue({ data: { items: [delegation] } }); await mount()
    root.querySelector('button')!.click(); await settled()
    expect(api.delegation).toHaveBeenCalledOnce(); expect(api.session).toHaveBeenCalledWith(expect.objectContaining({ path: { bot_id: 'specialist', session_id: 'expert-session' } }))
    expect(api.select).toHaveBeenCalledWith('specialist')
    expect(api.open).toHaveBeenCalledWith({ sessionId: 'expert-session', title: 'Specialist original session' })
  })
  it('opens a known active original session while the user is composing a new draft', async () => {
    session.value = ''
    observations.parent = { synchronized: true, run: { run_id: 'original', turn_id: 'turn', generation: 'g', status: 'running', started_at: '', updated_at: '', messages: [] } }
    await mount(); root.querySelector('button')!.click(); await settled()
    expect(api.open).toHaveBeenCalledWith({ sessionId: 'parent', title: 'Generate snow landscape' })
    expect(api.tasks).not.toHaveBeenCalled(); expect(api.delegations).not.toHaveBeenCalled()
  })
  it('does not invent a link without the recorded specialist session/run relation', async () => {
    api.delegations.mockResolvedValue({ data: { items: [{ ...delegation, target_run_id: undefined }] } }); await mount()
    expect(root.querySelector('button')!.disabled).toBe(true); root.querySelector('button')!.click()
    expect(api.open).not.toHaveBeenCalled(); expect(api.session).not.toHaveBeenCalled()
  })
  it('cancels an in-flight specialist navigation when the source list loses access', async () => {
    let finish!: (value: { data: BotdelegationView }) => void
    api.delegations.mockResolvedValueOnce({ data: { items: [delegation] } }).mockRejectedValue({ status: 403 })
    api.delegation.mockReturnValue(new Promise(resolve => { finish = resolve }))
    await mount(); root.querySelector('button')!.click(); await settled()
    expect(api.delegation).toHaveBeenCalledOnce()
    await vi.advanceTimersByTimeAsync(5000)
    finish({ data: delegation }); await settled()
    expect(api.session).not.toHaveBeenCalled(); expect(api.open).not.toHaveBeenCalled(); expect(api.select).not.toHaveBeenCalled()
  })
  it('does not mislabel unknown or terminal specialist runs as running', async () => {
    api.delegations.mockResolvedValue({ data: { items: [{ ...delegation, state: 'unknown' }, { ...delegation, target_run_state: 'completed' }] } }); await mount()
    expect(root.querySelector('section')).toBeNull()
  })
  it.each(['accepted', 'finishing'] as const)('preserves the existing projected specialist %s state', async status => {
    api.delegations.mockResolvedValue({ data: { items: [{ ...delegation, target_run_state: status }] } }); await mount()
    expect(root.querySelectorAll('button')).toHaveLength(1)
    expect(root.textContent).toContain(en.chat.tasks.statuses[status])
  })
  it('clears on a denial and stops automatic reads; no device observation or business call is registered', async () => {
    api.tasks.mockResolvedValueOnce({ data: { items: [task('active', 'running')] } }).mockRejectedValue({ status: 403 })
    await mount(); expect(root.querySelector('button')).not.toBeNull()
    await vi.advanceTimersByTimeAsync(5000); await nextTick(); expect(root.querySelector('section')).toBeNull()
    const count = api.tasks.mock.calls.length; await vi.advanceTimersByTimeAsync(15000); expect(api.tasks).toHaveBeenCalledTimes(count)
    expect(api.delegation).not.toHaveBeenCalled(); expect(api.session).not.toHaveBeenCalled()
  })
  it('drops a late response from another Bot/session and recovers the same native run after reconnect', async () => {
    let finish!: (data: { data: { items: SessiontaskTask[] } }) => void
    api.tasks.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    await mount(); bot.value = 'other'; session.value = 'other-session'; await settled()
    finish({ data: { items: [task('stale', 'running')] } }); await settled()
    expect(root.textContent).not.toContain('Review image reference')
    bot.value = 'bot'; session.value = 'parent'
    observations.parent = { synchronized: true, run: { run_id: 'same-run', turn_id: 'same-turn', generation: 'g', status: 'running', started_at: '', updated_at: '', messages: [] } }
    await settled(); expect(root.textContent).toContain('Generate snow landscape')
    const restored = observations.parent
    if (!restored) throw new Error('Missing original observation fixture')
    restored.synchronized = false; await nextTick(); expect(root.textContent).not.toContain('Generate snow landscape')
    restored.synchronized = true; await nextTick(); expect(root.textContent).toContain('Generate snow landscape')
    expect(api.open).not.toHaveBeenCalled()
  })
})
