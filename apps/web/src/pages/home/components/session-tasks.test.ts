// @vitest-environment jsdom
import { createApp, h, nextTick, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { BotdelegationView, SessiontaskTask } from '@memohai/sdk'
import { notifyAuthSessionCleared } from '@/lib/auth-session'
import en from '@/i18n/locales/en.json'
import SessionTasks from './session-tasks.vue'
import { client } from '@memohai/sdk/client'
const navigation = vi.hoisted(() => ({ bot: 'wrong-global-bot', select: vi.fn(), push: vi.fn(), open: vi.fn() }))
vi.mock('@/store/chat-list', () => ({ useChatStore: () => ({ get currentBotId() { return navigation.bot }, selectBot: navigation.select, runtimeObservationFor: () => ({ synchronized: false, run: null }) }) }))
vi.mock('@/store/workspace-tabs', () => ({ useWorkspaceTabsStore: () => ({ api: {}, openSessionChat: navigation.open }) }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: navigation.push }) }))

const api = vi.hoisted(() => ({ list: vi.fn(), detail: vi.fn(), delegations: vi.fn(), delegationDetail: vi.fn(), retryDelivery: vi.fn(), deliveries: vi.fn(), session: vi.fn() }))
vi.mock('@memohai/sdk', () => ({
  getBotsByBotIdSessionsBySessionId: api.session,
  getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdObservation: vi.fn().mockResolvedValue({ data: { operation_id: 'operation', native: { availability: 'connection_unavailable' } } }),
  getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdDeliveries: api.deliveries,
  postBotsByBotIdSessionsBySessionIdDelegationsByDelegationIdDelivery: api.retryDelivery,
  getBotsByBotIdSessionsBySessionIdTasks: api.list,
  getBotsByBotIdSessionsBySessionIdDelegations: api.delegations,
  getBotsByBotIdSessionsBySessionIdDelegationsByDelegationId: api.delegationDetail,
  getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskId: api.detail,
}))

const text = '<img src="https://example.invalid/summary-probe" onerror="alert(1)">\n<script>alert(2)</script>\n[link](javascript:alert(3)) https://example.invalid/plain & <b>final</b>'
const task: SessiontaskTask = {
  task_type: 'computer_operation', task_id: 'operation', session_id: 'child', parent_session_id: 'parent',
  created_at: '2026-10-22T00:00:00Z', updated_at: '2026-10-22T00:00:00Z', status: 'process_exited',
  summary: 'receipt', content_available: true, attachments_available: false, pi_result: 'succeeded',
  result_summary: { text, truncated: false, source: 'pi_final_assistant' },
}
const delegation: BotdelegationView = {
  delegation_id: 'handoff-a', request_id: 'request-a', source_session_id: 'parent', target_bot_id: 'specialist-a',
  state: 'completed', delivery_state: 'accepted', attachments: [], created_at: '2026-10-22T00:00:00Z', updated_at: '2026-10-22T00:00:00Z',
  summary: { text: 'Specialist final reply', source: 'assistant_final' },
}
let app: ReturnType<typeof createApp> | undefined
let root: HTMLDivElement
async function settled() { await nextTick(); await vi.advanceTimersByTimeAsync(50); await nextTick() }
function button(label: string | RegExp) {
  const found = [...document.querySelectorAll('button')].find(b => typeof label === 'string'
    ? b.textContent?.trim() === label : label.test(b.textContent ?? ''))
  expect(found).toBeDefined()
  return found!
}
async function selectGroup(name: string) {
  const tab = [...document.querySelectorAll('[role=tab]')].find(el => el.textContent?.includes(name))
  expect(tab).toBeDefined()
  tab!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
  await settled()
}
async function open(group = 'execution', selectDetail = true) {
  root = document.createElement('div')
  document.body.append(root)
  app = createApp(SessionTasks, { botId: 'bot', sessionId: 'parent', selection: 'pc-a', visible: true })
  app.use(createI18n({ legacy: false, locale: 'en', messages: { en } }))
  app.mount(root)
  await settled()
  button(en.chat.tasks.records).click()
  await settled()
  if (group === 'delegations') {
    await selectGroup('Specialist Bot handoffs')
    if (selectDetail) button(/Target Bot/).click()
  } else if (selectDetail) button(/Computer task/).click()
  await settled()
}

describe('task detail identity and rendering boundary', () => {
  beforeEach(() => {
  navigation.bot = 'wrong-global-bot'
  navigation.open.mockReset(); navigation.push.mockReset().mockResolvedValue(undefined)
  navigation.select.mockReset().mockImplementation(async (bot: string) => { navigation.bot = bot })
  api.session.mockReset().mockResolvedValue({ data: { id: 'specialist-session', bot_id: 'specialist-a', title: 'Synthetic specialist' } })
    vi.useFakeTimers()
    vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
    client.setConfig({ baseUrl: '/api' })
    localStorage.clear()
    localStorage.setItem('token', 'synthetic-identity')
    api.deliveries.mockReset().mockResolvedValue({ data: { version: 1, operation_id: 'operation', files: [], has_more: false } })
    api.retryDelivery.mockReset().mockResolvedValue({ data: delegation })
    api.list.mockReset().mockResolvedValue({ data: { items: [task] } })
    api.detail.mockReset().mockResolvedValue({ data: task })
    api.delegations.mockReset().mockResolvedValue({ data: { items: [delegation] } })
    api.delegationDetail.mockReset().mockResolvedValue({ data: delegation })
  })
  afterEach(() => {
    app?.unmount()
    root?.remove()
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  it('shows the specialist root run with no children and opens its native history without dispatch', async () => {
    const withRun = { ...delegation, target_session_id: 'specialist-session', target_run_id: 'native-run-a', target_run_state: 'completed' as const }
    api.delegationDetail.mockResolvedValue({ data: withRun })
    api.list.mockResolvedValue({ data: { items: [] } })
    await open('delegations')
    expect(document.body.textContent).toContain('native-run-a')
    button(en.chat.delegations.openTasks).click(); await settled()
    expect(document.body.textContent).toContain(en.chat.delegations.nativeRun)
    expect(document.body.textContent).toContain('native-run-a')
    expect(document.body.textContent).toContain(en.chat.delegations.childTasks)
    button(en.chat.delegations.openHistory).click(); await settled()
    expect(api.session).toHaveBeenCalledWith(expect.objectContaining({ path: { bot_id: 'specialist-a', session_id: 'specialist-session' }, cache: 'no-store' }))
    expect(navigation.select).toHaveBeenCalledWith('specialist-a')
    expect(navigation.open).toHaveBeenCalledWith({ sessionId: 'specialist-session', title: 'Synthetic specialist' })
    expect(api.retryDelivery).not.toHaveBeenCalled()
  })

  it.each([401, 403, 404, 503])('keeps native history closed after target session rejection %s', async status => {
    api.delegationDetail.mockResolvedValue({ data: { ...delegation, target_session_id: 'specialist-session', target_run_id: 'native-run-a' } })
    api.session.mockRejectedValue({ status, message: 'PRIVATE' })
    await open('delegations')
    button(en.chat.delegations.openHistory).click(); await settled()
    expect(navigation.select).not.toHaveBeenCalled()
    expect(navigation.open).not.toHaveBeenCalled()
    expect(document.body.textContent).not.toContain('PRIVATE')
  })

  it('clears an unconfirmed root run on disconnect and restores the same delegation/run after refresh', async () => {
    const withRun = { ...delegation, target_session_id: 'specialist-session', target_run_id: 'native-run-a', target_run_state: 'completed' as const }
    api.delegationDetail.mockResolvedValue({ data: withRun })
    api.list.mockResolvedValue({ data: { items: [] } })
    await open('delegations')
    button(en.chat.delegations.openTasks).click(); await settled()
    expect(document.body.textContent).toContain('native-run-a')
    api.delegationDetail.mockRejectedValue({ status: 503 })
    await vi.advanceTimersByTimeAsync(5000); await settled()
    expect(document.body.textContent).not.toContain('native-run-a')
    api.delegationDetail.mockResolvedValue({ data: withRun })
    button(en.chat.tasks.reload).click(); await settled()
    expect(document.body.textContent).toContain('native-run-a')
    expect(api.delegationDetail.mock.lastCall![0].path.delegation_id).toBe('handoff-a')
    expect(api.retryDelivery).not.toHaveBeenCalled()
    expect(navigation.select).not.toHaveBeenCalled()
  })

  it('retires pending native history navigation on logout', async () => {
    api.delegationDetail.mockResolvedValue({ data: { ...delegation, target_session_id: 'specialist-session', target_run_id: 'native-run-a' } })
    let resolve!: (value: unknown) => void
    api.session.mockReturnValue(new Promise(done => { resolve = done }))
    await open('delegations')
    button(en.chat.delegations.openHistory).click(); await settled()
    notifyAuthSessionCleared('logout'); await settled()
    resolve({ data: { id: 'specialist-session', bot_id: 'specialist-a' } }); await settled()
    expect(navigation.select).not.toHaveBeenCalled()
    expect(navigation.open).not.toHaveBeenCalled()
  })

  it('rejects a changed admitted run during native history revalidation', async () => {
    const withRun = { ...delegation, target_session_id: 'specialist-session', target_run_id: 'native-run-a' }
    api.delegationDetail.mockResolvedValue({ data: withRun })
    await open('delegations')
    api.delegationDetail.mockResolvedValue({ data: { ...withRun, target_run_id: 'different-run' } })
    button(en.chat.delegations.openHistory).click(); await settled()
    expect(api.session).not.toHaveBeenCalled()
    expect(navigation.open).not.toHaveBeenCalled()
  })

  it('shows all recorded files automatically, with pending and failed separate from task completion', async () => {
    api.detail.mockResolvedValue({ data: { ...task, status: 'unknown', pi_result: undefined } })
    api.deliveries.mockResolvedValueOnce({ data: { version: 1, operation_id: 'operation', has_more: true, next_after: '16', files: [
      { delivery_id: 'ready-result', state: 'committed', reference: { version: 1, direction: 'upload', content_hash: 'a'.repeat(64), size_bytes: 42, mime: 'text/plain' } },
      { delivery_id: 'waiting-result', state: 'pending' },
    ] } }).mockResolvedValueOnce({ data: { version: 1, operation_id: 'operation', has_more: false, files: [{ delivery_id: 'failed-result', state: 'failed' }] } })
    await open()
    expect(document.body.textContent).toContain('ready-result')
    expect(document.body.textContent).toContain('waiting-result')
    expect(document.body.textContent).toContain('failed-result')
    expect(document.body.textContent).toContain(en.chat.tasks.files.states.pending)
    expect(document.body.textContent).toContain(en.chat.tasks.files.states.failed)
    expect(document.body.textContent).toContain(en.chat.tasks.statuses.unknown)
    expect(document.body.textContent).not.toContain(en.chat.tasks.statuses.completed)
    const links = document.querySelector('[role=dialog]')!.querySelectorAll('a')
    expect(links).toHaveLength(1)
    expect(links[0]!.getAttribute('href')).toBe(`/api/bots/bot/media/${'a'.repeat(64)}?token=synthetic-identity`)
    expect(links[0]!.getAttribute('download')).toBe('ready-result')
    expect(api.deliveries.mock.lastCall![0].query.after).toBe('16')
  })

  it.each([
    { status: 404, code: 'http.not_found', expected: en.chat.tasks.files.unsupported },
    { status: 404, code: 'session.not_found', expected: en.chat.tasks.inaccessible },
    { status: 403, code: 'http.forbidden', expected: en.errors.http.forbidden },
  ])('keeps task summary but distinguishes file observation failure: $code', async ({ status, code, expected }) => {
    api.deliveries.mockRejectedValueOnce({ status, code, detail: 'PRIVATE_FAILURE' })
    await open()
    expect(document.body.textContent).toContain(text)
    expect(document.body.textContent).toContain(expected)
    expect(document.body.textContent).not.toContain(en.chat.tasks.files.empty)
    expect(document.body.textContent).not.toContain('PRIVATE_FAILURE')
    const calls = api.deliveries.mock.calls.length
    await vi.advanceTimersByTimeAsync(10000)
    expect(api.deliveries).toHaveBeenCalledTimes(calls)
    button('Reload view').click(); await settled()
    expect(document.body.textContent).toContain(en.chat.tasks.files.empty)
  })

  it('opens only the server-returned specialist session, retains detail across polls, and navigates back without dispatch', async () => {
    api.delegationDetail.mockResolvedValue({ data: { ...delegation, target_session_id: 'specialist-session' } })
    await open('delegations')
    const trigger = button(en.chat.delegations.openTasks)
    trigger.click(); trigger.click(); await settled()
    expect(api.list.mock.lastCall![0].path).toEqual({ bot_id: 'specialist-a', session_id: 'specialist-session' })
    expect(api.list).toHaveBeenCalledTimes(2)
    button(/Computer task/).click(); await settled()
    expect(api.detail.mock.lastCall![0].path).toEqual({ bot_id: 'specialist-a', session_id: 'specialist-session', task_type: 'computer_operation', task_id: 'operation' })
    await vi.advanceTimersByTimeAsync(5000); await settled()
    expect(document.body.textContent).toContain(text)
    expect(api.delegationDetail.mock.calls.length).toBeGreaterThan(1)
    expect(api.list).toHaveBeenCalledTimes(2)
    const back = () => (document.querySelector('[aria-label="Back"]') as HTMLButtonElement).click()
    back(); await settled()
    expect(document.body.textContent).not.toContain(text)
    expect(document.body.textContent).toContain(en.chat.delegations.downstreamTitle)
    back(); await settled()
    expect(document.body.textContent).toContain('Specialist final reply')
    expect(document.body.textContent).not.toContain(en.chat.delegations.downstreamTitle)
    expect(api.retryDelivery).not.toHaveBeenCalled()
  })

  it.each([
    { error: { status: 404, code: 'http.not_found' }, expected: en.chat.tasks.unsupported },
    { error: { status: 404, code: 'session.not_found' }, expected: en.chat.tasks.inaccessible },
    { error: { status: 403, code: 'http.forbidden' }, expected: en.errors.http.forbidden },
    { error: { status: 401, code: 'http.unauthorized' }, expected: en.errors.http.unauthorized },
    { error: { status: 503, code: 'workspace.unreachable' }, expected: en.errors.workspace.unreachable },
    { error: new TypeError('PRIVATE_NETWORK'), expected: en.common.networkError },
  ])('distinguishes downstream read failure from an empty task list: $expected', async ({ error, expected }) => {
    api.delegationDetail.mockResolvedValue({ data: { ...delegation, target_session_id: 'specialist-session' } })
    await open('delegations')
    api.list.mockRejectedValueOnce(error)
    button(en.chat.delegations.openTasks).click(); await settled()
    expect(document.body.textContent).toContain(expected)
    expect(document.body.textContent).not.toContain(en.chat.tasks.empty)
    expect(document.body.textContent).not.toContain('PRIVATE_NETWORK')
    expect(api.retryDelivery).not.toHaveBeenCalled()
  })

  it('does not invent a specialist session for an accepted handoff without one', async () => {
    await open('delegations')
    expect(document.body.textContent).not.toContain(en.chat.delegations.openTasks)
    expect(api.list).toHaveBeenCalledTimes(1)
  })

  it.each([
    { cause: new TypeError('PRIVATE_TRANSIENT_NETWORK'), recovery: 'reload' },
    { cause: new TypeError('PRIVATE_TRANSIENT_NETWORK'), recovery: 'poll' },
    { cause: { status: 503, code: 'http.unavailable', detail: 'PRIVATE_UPSTREAM' }, recovery: 'reload' },
    { cause: { status: 503, code: 'http.unavailable', detail: 'PRIVATE_UPSTREAM' }, recovery: 'poll' },
  ])('resumes the same downstream detail after a transient parent read failure via $recovery without redispatch', async ({ cause, recovery }) => {
    const relation = { ...delegation, target_session_id: 'specialist-session' }
    api.delegationDetail.mockResolvedValue({ data: relation })
    await open('delegations')
    button(en.chat.delegations.openTasks).click(); await settled()
    button(/Computer task/).click(); await settled()
    expect(document.body.textContent).toContain(text)

    api.delegationDetail.mockRejectedValue(cause)
    await vi.advanceTimersByTimeAsync(5000); await settled()
    expect(document.body.textContent).toContain(en.chat.delegations.downstreamTitle)
    expect(document.body.textContent).not.toContain(text)
    expect(document.body.textContent).not.toContain('PRIVATE_')
    expect(document.body.textContent).not.toContain(en.chat.tasks.empty)
    const childReads = api.list.mock.calls.length + api.detail.mock.calls.length
    await vi.advanceTimersByTimeAsync(5000); await settled()
    expect(api.list.mock.calls.length + api.detail.mock.calls.length).toBe(childReads)

    api.delegationDetail.mockResolvedValue({ data: relation })
    api.detail.mockResolvedValue({ data: { ...task, status: 'waiting_decision', pi_result: undefined,
      content_available: false, result_summary: undefined } })
    if (recovery === 'reload') button(en.chat.tasks.reload).click()
    else await vi.advanceTimersByTimeAsync(5000)
    await settled()
    expect(document.body.textContent).toContain(en.chat.delegations.downstreamTitle)
    expect(document.body.textContent).toContain(en.chat.tasks.statuses.waiting_decision)
    expect(document.body.textContent).not.toContain(text)
    expect(api.detail.mock.lastCall![0].path).toEqual({ bot_id: 'specialist-a', session_id: 'specialist-session', task_type: 'computer_operation', task_id: 'operation' })
    expect(api.list).toHaveBeenCalledTimes(2)
    expect(api.retryDelivery).not.toHaveBeenCalled()
  })

  it.each(['relation-denied', 'relation-unauthorized', 'relation-forbidden', 'relation-changed', 'logout', 'scope-changed'])('retires downstream content and ignores a late response after %s', async (reason) => {
    api.delegationDetail.mockResolvedValue({ data: { ...delegation, target_session_id: 'specialist-session' } })
    const props = ref({ botId: 'bot', sessionId: 'parent', selection: 'pc-a', visible: true })
    root = document.createElement('div'); document.body.append(root)
    app = createApp({ setup: () => () => h(SessionTasks, props.value) })
    app.use(createI18n({ legacy: false, locale: 'en', messages: { en } })); app.mount(root)
    await settled(); button(en.chat.tasks.records).click(); await settled()
    await selectGroup('Specialist Bot handoffs'); button(/Target Bot/).click(); await settled()
    let finish!: (value: { data: { items: SessiontaskTask[] } }) => void
    api.list.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    button(en.chat.delegations.openTasks).click(); await settled()
    const signal = api.list.mock.lastCall![0].signal as AbortSignal
    if (reason.startsWith('relation-')) {
      if (reason === 'relation-changed') api.delegationDetail.mockResolvedValue({ data: { ...delegation, target_session_id: 'another-session' } })
      else api.delegationDetail.mockRejectedValue({
        status: reason === 'relation-unauthorized' ? 401 : reason === 'relation-forbidden' ? 403 : 404,
        code: 'session.not_found', detail: 'PRIVATE',
      })
      await vi.advanceTimersByTimeAsync(5000)
    } else if (reason === 'logout') notifyAuthSessionCleared('logout')
    else props.value = { ...props.value, botId: 'other-bot', sessionId: 'other-session' }
    await settled(); expect(signal.aborted).toBe(true)
    finish({ data: { items: [{ ...task, task_id: 'late-private-task' }] } }); await settled()
    expect(document.body.textContent).not.toContain('late-pri')
    expect(document.body.textContent).not.toContain(en.chat.delegations.downstreamTitle)
    expect(document.body.textContent).not.toContain('PRIVATE')
    expect(api.retryDelivery).not.toHaveBeenCalled()
  })

  it('wires the shared retry action through the existing SDK and never reports supervisor completion', async () => {
    api.delegationDetail.mockResolvedValue({ data: { ...delegation, delivery_state: 'unknown' } })
    await open('delegations')
    expect(api.retryDelivery).not.toHaveBeenCalled()
    api.delegationDetail.mockResolvedValue({ data: delegation })
    button(en.chat.delegations.retryDelivery).click(); await settled()
    expect(api.retryDelivery).toHaveBeenCalledTimes(1)
    expect(api.retryDelivery.mock.calls[0]![0].path).toEqual({ bot_id: 'bot', session_id: 'parent', delegation_id: 'handoff-a' })
    expect(document.body.textContent).not.toContain(en.chat.delegations.deliveryAccepted)
    expect(document.body.textContent).toContain('Delivery request response · Accepted')
    expect([...document.querySelectorAll('button')].some(b => b.textContent?.trim() === en.chat.delegations.retryDelivery)).toBe(false)
  })

  it('opens the specialist group without polling the hidden execution group', async () => {
    await open('delegations')
    expect(document.body.textContent).toContain('Specialist final reply')
    expect(api.delegations).toHaveBeenCalledTimes(1)
    expect(api.list).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(5000)
    expect(api.list).toHaveBeenCalledTimes(1)
    expect(api.delegationDetail).toHaveBeenCalledTimes(2)
  })

  it('keeps delegation page two and execution task cursors separate across tab round trips', async () => {
    api.list.mockImplementation(({ query }: { query: { cursor?: string } }) => Promise.resolve({
      data: {
        items: [{ ...task, task_id: query.cursor ? 'exec-two' : 'exec-one' }],
        next_cursor: query.cursor ? 'execution-page-3' : 'execution-page-2',
      },
    }))
    api.delegations.mockImplementation(({ query }: { query: { cursor?: string } }) => Promise.resolve({
      data: {
        items: [{ ...delegation, target_bot_id: query.cursor ? 'bot-two' : 'bot-one' }],
        next_cursor: query.cursor ? 'delegation-page-3' : 'delegation-page-2',
      },
    }))
    await open('delegations', false)
    button(en.chat.tasks.next).click(); await settled()
    expect(document.body.textContent).toContain('Target Bot bot-two')
    expect(api.delegations.mock.lastCall![0].query.cursor).toBe('delegation-page-2')

    await selectGroup('Execution tasks')
    expect(api.list.mock.lastCall![0].query.cursor).toBeUndefined()
    expect(document.body.textContent).toContain('exec-one')
    expect(document.body.textContent).not.toContain('Target Bot bot-two')
    button(en.chat.tasks.next).click(); await settled()
    expect(api.list.mock.lastCall![0].query.cursor).toBe('execution-page-2')
    expect(document.body.textContent).toContain('exec-two')

    await selectGroup('Specialist Bot handoffs')
    expect(api.delegations.mock.lastCall![0].query.cursor).toBeUndefined()
    expect(document.body.textContent).toContain('Target Bot bot-one')
    expect(document.body.textContent).not.toContain('exec-two')
    button(en.chat.tasks.next).click(); await settled()
    expect(api.delegations.mock.lastCall![0].query.cursor).toBe('delegation-page-2')
    expect(api.list.mock.calls.map(([options]) => options.query.cursor)).toEqual([undefined, undefined, 'execution-page-2'])
    expect(api.delegations.mock.calls.map(([options]) => options.query.cursor)).toEqual([undefined, 'delegation-page-2', undefined, 'delegation-page-2'])
    expect(api.detail).not.toHaveBeenCalled()
    expect(api.delegationDetail).not.toHaveBeenCalled()
  })

  it('keeps the Web shell description and restores trigger focus on Escape', async () => {
    await open('delegations')
    const dialog = document.querySelector('[role=dialog]')!
    expect(document.getElementById(dialog.getAttribute('aria-labelledby')!)?.textContent).toBe(en.chat.tasks.detail)
    expect(document.getElementById(dialog.getAttribute('aria-describedby')!)?.textContent).toBe(en.chat.delegations.description)
    expect(dialog.contains(document.activeElement)).toBe(true)
    document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await settled(); await vi.advanceTimersByTimeAsync(300); await nextTick()
    expect(document.querySelector('[role=dialog]')).toBeNull()
    expect(document.activeElement).toBe(button(en.chat.tasks.records))
  })

  it('closes specialist detail and ignores its late reply after same-tab logout', async () => {
    let finish!: (value: { data: BotdelegationView }) => void
    api.delegationDetail.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    await open('delegations')
    const signal = api.delegationDetail.mock.lastCall![0].signal as AbortSignal
    notifyAuthSessionCleared('logout')
    expect(signal.aborted).toBe(true)
    finish({ data: delegation }); await settled()
    expect(document.querySelector('[role=dialog]')).toBeNull()
    expect(document.body.textContent).not.toContain('Specialist final reply')
    await vi.advanceTimersByTimeAsync(15000)
    expect(api.delegationDetail).toHaveBeenCalledTimes(1)
  })

  it('projects the child own completed run, then removes its output when the next run is pending', async () => {
    const child: SessiontaskTask = { ...task, task_type: 'subagent', task_id: 'child', status: 'completed', run_id: 'own-run', pi_result: undefined, process_state: undefined, result_summary: { text: 'child own output', source: 'subagent_assistant', truncated: false } }
    api.list.mockResolvedValue({ data: { items: [child] } })
    api.detail.mockResolvedValue({ data: child })
    await open('execution', false)
    button(/Subagent/).click(); await settled()
    expect(document.body.textContent).toContain('Run completed')
    expect(document.body.textContent).toContain('child own output')
    expect(document.body.textContent).not.toContain('Updated by polling')
    api.detail.mockResolvedValue({ data: { ...child, status: 'accepted', run_id: 'next-run', content_available: false, result_summary: undefined } })
    await vi.advanceTimersByTimeAsync(5000); await settled()
    expect(document.body.textContent).toContain('Accepted; waiting to run')
    expect(document.body.textContent).not.toContain('child own output')
  })

  it('renders HTML, Markdown and URLs literally without active elements', async () => {
    await open()
    const dialog = document.querySelector('[role="dialog"]')!
    expect(dialog.textContent).toContain(text)
    expect(dialog.querySelectorAll('img,script,a,iframe,video,audio,object,embed')).toHaveLength(0)
    expect(dialog.querySelector('.whitespace-pre-wrap')?.textContent).toBe(text)
  })

  it('invalidates pending detail when another tab replaces the identity token', async () => {
    let finish!: (value: { data: SessiontaskTask }) => void
    api.detail.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    await open()
    const signal = api.detail.mock.lastCall![0].signal as AbortSignal
    api.list.mockResolvedValue({ data: { items: [] } })
    localStorage.setItem('token', 'other-synthetic-identity')
    window.dispatchEvent(new StorageEvent('storage', {
      key: 'token', oldValue: 'synthetic-identity', newValue: 'other-synthetic-identity', storageArea: localStorage,
    }))
    await settled()
    expect(signal.aborted).toBe(true)
    finish({ data: task })
    await settled()
    expect(api.list).toHaveBeenCalledTimes(2)
    expect(document.body.textContent).not.toContain(text)
  })

  it.each(['login', 'logout', 'unauthorized', 'token-cleared'] as const)('closes and aborts on %s before any token update or parent unmount', async (reason) => {
    let finish!: (value: { data: SessiontaskTask }) => void
    api.detail.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    await open()
    const signal = api.detail.mock.lastCall![0].signal as AbortSignal
    // The login notification precedes token replacement; API 401 handling can
    // notify without a storage event. The mounted child must clear directly.
    notifyAuthSessionCleared(reason)
    expect(signal.aborted).toBe(true)
    finish({ data: task })
    await settled()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(document.body.textContent).not.toContain(text)
    const calls = api.list.mock.calls.length + api.detail.mock.calls.length
    await vi.advanceTimersByTimeAsync(15000)
    expect(api.list.mock.calls.length + api.detail.mock.calls.length).toBe(calls)
  })
})
