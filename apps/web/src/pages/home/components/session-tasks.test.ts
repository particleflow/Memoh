// @vitest-environment jsdom
import { createApp, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { SessiontaskTask } from '@memohai/sdk'
import { notifyAuthSessionCleared } from '@/lib/auth-session'
import en from '@/i18n/locales/en.json'
import SessionTasks from './session-tasks.vue'

const api = vi.hoisted(() => ({ list: vi.fn(), detail: vi.fn() }))
vi.mock('@memohai/sdk', () => ({
  getBotsByBotIdSessionsBySessionIdTasks: api.list,
  getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskId: api.detail,
}))

const text = '<img src="https://example.invalid/summary-probe" onerror="alert(1)">\n<script>alert(2)</script>\n[link](javascript:alert(3)) https://example.invalid/plain & <b>final</b>'
const task: SessiontaskTask = {
  task_type: 'computer_operation', task_id: 'operation', session_id: 'child', parent_session_id: 'parent',
  created_at: '2026-10-22T00:00:00Z', updated_at: '2026-10-22T00:00:00Z', status: 'process_exited',
  summary: 'receipt', content_available: true, attachments_available: false, pi_result: 'succeeded',
  result_summary: { text, truncated: false, source: 'pi_final_assistant' },
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
async function open() {
  root = document.createElement('div')
  document.body.append(root)
  app = createApp(SessionTasks, { botId: 'bot', sessionId: 'parent', selection: 'pc-a', visible: true })
  app.use(createI18n({ legacy: false, locale: 'en', messages: { en } }))
  app.mount(root)
  await settled()
  button('Tasks').click()
  await settled()
  button(/Computer task/).click()
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
  })
  afterEach(() => {
    app?.unmount()
    root?.remove()
    vi.unstubAllGlobals()
    vi.useRealTimers()
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
