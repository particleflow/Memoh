// @vitest-environment jsdom
import { compile, createApp, nextTick, ref } from 'vue'
import { parse } from 'vue/compiler-sfc'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import en from '@/i18n/locales/en.json'
import Directory from './bot-task-directory.vue'
import paneSource from './chat-pane.vue?raw'

const api = vi.hoisted(() => ({ list: vi.fn(), detail: vi.fn(), session: vi.fn(), push: vi.fn() }))
vi.mock('@memohai/sdk', () => ({ getBotsByBotIdTasks: api.list, getBotsByBotIdTasksByTaskTypeByRecordId: api.detail, getBotsByBotIdSessionsBySessionId: api.session }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: api.push }) }))

// Exercise the actual pane template's read-only/composer boundary with the real
// directory. Its unrelated transcript/media children and pane setup are fixtures.
const template = parse(paneSource).descriptor.template!.content
const render = compile(template)
const task = { task_type: 'bot_delegation', task_id: 'handoff', record_id: 'handoff', name: 'Original delegated task', state: 'completed', created_at: '', destination: { bot_id: 'recipient', session_id: 'original-session', run_id: 'original-run', turn_id: 'original-turn' } }
let app: ReturnType<typeof createApp> | undefined, root: HTMLDivElement
const settle = async () => { for (let i = 0; i < 40; i++) await Promise.resolve(); await nextTick() }

beforeEach(() => {
  localStorage.clear(); localStorage.setItem('token', 'fixture-account')
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  api.list.mockReset().mockResolvedValue({ data: { items: [task] } })
  api.detail.mockReset().mockResolvedValue({ data: task })
  api.session.mockReset().mockResolvedValue({ data: { id: 'original-session', bot_id: 'recipient', title: 'Original task' } })
  api.push.mockReset().mockResolvedValue(undefined)
})
afterEach(() => { app?.unmount(); app = undefined; root?.remove(); vi.unstubAllGlobals() })

async function mount(bot: string | null = 'recipient') {
  root = document.createElement('div'); document.body.append(root)
  app = createApp({ setup: () => ({
    currentBotId: ref(bot), activeChatReadOnly: true, isVisible: true,
    dropHandlers: {}, taskAnchor: undefined,
    replaceDraftConfirm: { open: ref(false), setOpen: vi.fn(), answer: vi.fn() },
  }), render })
  for (const name of new Set(template.match(/(?<=<)[A-Z]\w*/g))) {
    if (name !== 'BotTaskDirectory') app.component(name, () => null)
  }
  app.component('BotTaskDirectory', Directory)
  app.config.warnHandler = () => {} // Other pane state is outside this boundary.
  app.use(createI18n({ legacy: false, locale: 'en', messages: { en } }))
  app.mount(root); await settle()
}

it('opens the recipient directory from a read-only task without mounting an input or resolving contact', async () => {
  await mount()
  expect(root.querySelector('textarea, input, form')).toBeNull()
  expect(api.list).not.toHaveBeenCalled()
  const trigger = root.querySelector<HTMLButtonElement>('button')!
  expect(trigger).not.toBeNull()
  expect(trigger?.textContent).toContain(en.chat.tasks.title)
  trigger.click(); await settle()
  expect(api.list.mock.calls[0]![0].path).toEqual({ bot_id: 'recipient' })
  const row = document.querySelector<HTMLButtonElement>('[role="dialog"] li > button')!
  expect(row.textContent).toContain(task.name)
  row.click(); await settle()
  expect(api.detail.mock.calls[0]![0].path).toEqual({ bot_id: 'recipient', task_type: 'bot_delegation', record_id: 'handoff' })
  expect(api.push).toHaveBeenCalledExactlyOnceWith({ name: 'bot', params: { botName: 'recipient' }, query: { session: 'original-session', task_bot: 'recipient', task_type: 'bot_delegation', task_id: 'handoff', task_record: 'handoff', run: 'original-run' } })
  expect(root.querySelector('textarea, input, form')).toBeNull()
})

it('does not expose a directory when there is no Bot identity', async () => {
  await mount(null)
  expect(root.querySelector('button')).toBeNull()
  expect(api.list).not.toHaveBeenCalled()
})
