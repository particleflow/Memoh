// @vitest-environment jsdom
import { createApp, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import en from '@/i18n/locales/en.json'
import { notifyAuthSessionCleared } from '@/lib/auth-session'
import Directory from './bot-task-directory.vue'
const api = vi.hoisted(() => ({ list: vi.fn(), detail: vi.fn(), session: vi.fn(), push: vi.fn() }))
vi.mock('@memohai/sdk', () => ({ getBotsByBotIdTasks: api.list, getBotsByBotIdTasksByTaskTypeByRecordId: api.detail, getBotsByBotIdSessionsBySessionId: api.session }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: api.push }) }))
const row = (id: string, state = 'failed') => ({ task_type: 'native_agent', task_id: 'bg_original', record_id: id, name: 'Same task', state, created_at: '2026-01-01', destination: { bot_id: 'bot', session_id: 'execution', run_id: id, turn_id: 'turn-'+id } })
let app: ReturnType<typeof createApp> | undefined, root: HTMLDivElement
async function settle() { await nextTick(); for (let i = 0; i < 80; i++) await Promise.resolve(); await nextTick() }
async function mount() {
  root = document.createElement('div'); document.body.append(root)
  app = createApp(Directory, { botId: 'bot' }); app.use(createI18n({ legacy: false, locale: 'en', messages: { en } })); app.mount(root); await settle()
  root.querySelector('button')!.click(); await settle()
}
beforeEach(() => {
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  localStorage.clear(); localStorage.setItem('token', 'fixture-account')
  api.list.mockReset().mockResolvedValue({ data: { items: [row('run-1'), row('run-2', 'unknown')] } })
  api.detail.mockReset().mockImplementation(({ path }) => Promise.resolve({ data: row(path.record_id) }))
  api.session.mockReset().mockResolvedValue({ data: { id: 'execution', bot_id: 'bot', title: 'Original execution' } })
  api.push.mockReset().mockResolvedValue(undefined)
})
afterEach(() => { app?.unmount(); root?.remove(); document.body.innerHTML = ''; vi.unstubAllGlobals() })
it('lists real same-name historical tasks and rechecks the clicked occurrence before opening execution', async () => {
  await mount(); const dialog = document.querySelector('[role="dialog"]')!
  const rows = dialog.querySelectorAll<HTMLButtonElement>('li > button')
  expect(rows).toHaveLength(2)
  expect(rows[0]!.textContent).toContain(en.chat.tasks.statuses.failed)
  expect(rows[1]!.textContent).toContain(en.chat.tasks.statuses.unknown)
  expect(dialog.textContent).not.toContain('bg_original'); expect(dialog.textContent).not.toContain('run-2')
  rows[1]!.click(); await settle()
  expect(api.detail.mock.calls[0]![0].path).toEqual({ bot_id: 'bot', task_type: 'native_agent', record_id: 'run-2' })
  expect(api.push).toHaveBeenCalledExactlyOnceWith({ name: 'bot', params: { botName: 'bot' }, query: { session: 'execution', task_bot: 'bot', task_type: 'native_agent', task_id: 'bg_original', task_record: 'run-2', run: 'run-2' } })
})
it('keeps failed tasks without an execution destination visible and disabled', async () => {
  api.list.mockResolvedValue({ data: { items: [{ ...row('fire'), task_type: 'schedule_fire', destination: undefined }] } })
  await mount(); const button = document.querySelector<HTMLButtonElement>('[role="dialog"] li > button')!
  expect(button.disabled).toBe(true); expect(button.textContent).toContain(en.chat.tasks.statuses.failed)
  expect(api.session).not.toHaveBeenCalled()
})
it('does not navigate after task permissions are revoked', async () => {
  api.detail.mockRejectedValue({ status: 403 }); await mount()
  document.querySelector<HTMLButtonElement>('[role="dialog"] li > button')!.click(); await settle()
  expect(api.session).not.toHaveBeenCalled(); expect(api.push).not.toHaveBeenCalled()
})
it('does not navigate after logout while execution history authorization is pending', async () => {
  let finish!: (value: unknown) => void
  api.session.mockImplementation(() => new Promise(resolve => { finish = resolve }))
  await mount()
  document.querySelector<HTMLButtonElement>('[role="dialog"] li > button')!.click(); await settle()
  expect(api.session).toHaveBeenCalledOnce()
  notifyAuthSessionCleared('logout'); finish({ data: { id: 'execution', bot_id: 'bot' } }); await settle()
  expect(api.push).not.toHaveBeenCalled()
})
