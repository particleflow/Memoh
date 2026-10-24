// @vitest-environment jsdom
import { createApp, h, nextTick, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Dialog, DialogPanel, DialogTitle, DialogDescription } from '@felinic/ui'
import en from '@/i18n/locales/en.json'
import SessionTaskResults from './session-task-results.vue'
import { useSessionTasks, type TaskScope } from '../composables/useSessionTasks'
const api = vi.hoisted(() => ({ list: vi.fn(), detail: vi.fn(), files: vi.fn() }))
vi.mock('@memohai/sdk', () => ({
  getBotsByBotIdSessionsBySessionIdTasks: api.list,
  getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskId: api.detail,
  getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdDeliveries: api.files,
}))
let app: ReturnType<typeof createApp>
let root: HTMLDivElement
beforeEach(() => {
  vi.useFakeTimers()
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  const task = { task_id: 'task', task_type: 'computer_operation', status: 'unknown', content_available: false, attachments_available: false }
  api.list.mockResolvedValue({ data: { items: [task] } })
  api.detail.mockResolvedValue({ data: task })
  api.files.mockResolvedValue({ data: { version: 1, operation_id: 'task', has_more: false, files: [{ delivery_id: 'result', state: 'pending' }] } })
})
afterEach(() => { app?.unmount(); root?.remove(); vi.useRealTimers(); vi.unstubAllGlobals() })
describe.each(['web', 'native'])('shared task files in %s host', host => {
  it('uses the same scoped reader and shared body, without requiring a DialogRoot', async () => {
    root = document.createElement('div'); document.body.append(root)
    app = createApp({ setup() {
      const scope = ref<TaskScope | null>({ bot: 'bot', session: 'session', selection: 'native', identity: 'test' })
      const reader = useSessionTasks(scope, ref(true))
      const body = () => h(SessionTaskResults, {
        items: reader.items.value, detail: reader.detail.value, selected: reader.selected.value,
        loading: reader.loading.value, error: reader.error.value, deliveries: reader.deliveries.value,
        onOpenTask: reader.openTask, onRefresh: reader.refresh,
      })
      if (host === 'web') return () => h(Dialog, { open: true }, { default: () => h(DialogPanel, {}, { default: () => [
        h(DialogTitle, {}, { default: () => en.chat.tasks.title }),
        h(DialogDescription, {}, { default: () => en.chat.tasks.description }), body(),
      ] }) })
      return () => h('dialog', { open: true, 'aria-labelledby': 'task-title', 'aria-describedby': 'task-description' }, [
        h('h2', { id: 'task-title' }, en.chat.tasks.title), h('p', { id: 'task-description' }, en.chat.tasks.description), body(),
      ])
    } })
    app.use(createI18n({ legacy: false, locale: 'en', messages: { en } })); app.mount(root)
    await nextTick(); await vi.advanceTimersByTimeAsync(50); await nextTick()
    const dialog = document.querySelector(host === 'web' ? '[role=dialog]' : 'dialog')!
    expect(document.getElementById(dialog.getAttribute('aria-labelledby')!)?.textContent).toBe(en.chat.tasks.title)
    ;(dialog.querySelector('button') as HTMLButtonElement).click()
    await nextTick(); await vi.advanceTimersByTimeAsync(50); await nextTick()
    expect(dialog.textContent).toContain(en.chat.tasks.files.title)
    expect(dialog.textContent).toContain(en.chat.tasks.files.states.pending)
    expect(dialog.textContent).not.toContain(en.chat.tasks.files.states.committed)
    expect(dialog.querySelectorAll('a,img,iframe')).toHaveLength(0)
    expect(api.files.mock.lastCall![0].path.session_id).toBe('session')
  })
})
