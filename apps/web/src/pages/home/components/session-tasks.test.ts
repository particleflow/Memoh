// @vitest-environment jsdom
import { createApp, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { BotdelegationView, SessiontaskTask } from '@memohai/sdk'
import { notifyAuthSessionCleared } from '@/lib/auth-session'
import en from '@/i18n/locales/en.json'
import SessionTasks from './session-tasks.vue'

const api = vi.hoisted(() => ({ list: vi.fn(), detail: vi.fn(), delegations: vi.fn(), delegationDetail: vi.fn() }))
vi.mock('@memohai/sdk', () => ({
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
  button('Tasks').click()
  await settled()
  if (group === 'delegations') {
    await selectGroup('Specialist Bot handoffs')
    if (selectDetail) button(/Target Bot/).click()
  } else if (selectDetail) button(/Computer task/).click()
  await settled()
}

describe('task detail identity and rendering boundary', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
    localStorage.clear()
    localStorage.setItem('token', 'synthetic-identity')
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
