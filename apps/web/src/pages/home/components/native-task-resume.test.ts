// @vitest-environment jsdom
import { createApp, h, nextTick, reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ToolCallBlock } from '@/store/chat-list'
import en from '@/i18n/locales/en.json'
import { provideNativeTaskResumeScope } from '../composables/native-task-resume-scope'
import ToolCallDetailGeneric from './tool-call-detail-generic.vue'

const api = vi.hoisted(() => ({ send: vi.fn() }))
vi.mock('@memohai/sdk', () => ({ postBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdResume: api.send }))
vi.mock('@/store/user', () => ({ useUserStore: () => ({ userInfo: { id: 'synthetic-user' } }) }))
let app: ReturnType<typeof createApp> | undefined, root: HTMLDivElement
const original = (): ToolCallBlock => ({ id: 1, type: 'tool', name: 'computer_task_submit', tool_call_id: 'call-a', running: false, toolCallId: 'call-a', toolName: 'computer_task_submit', done: true,
  input: { kind: 'herdr_pi_task', task: 'original synthetic instruction' },
  result: { structuredContent: { kind: 'herdr_pi_task', operation_id: 'op-a', target_id: 'target-a', state: 'unknown', phase: 'batch_creating_workspace' } },
})
async function settle() { await nextTick(); await new Promise(resolve => setTimeout(resolve, 0)); await nextTick() }
async function mount(block = original(), withScope = true) {
  const host = reactive({ botId: 'bot-a', sessionId: 'session-a', visible: true })
  root = document.createElement('div'); document.body.append(root)
  app = createApp({ setup() {
    if (withScope) provideNativeTaskResumeScope(host)
    return () => h(ToolCallDetailGeneric, { block })
  } })
  app.use(createI18n({ legacy: false, locale: 'en', messages: { en } }))
  app.mount(root); await settle()
  return host
}
beforeEach(() => {
  sessionStorage.clear(); localStorage.clear(); localStorage.setItem('token', 'synthetic-account-token')
  api.send.mockReset().mockResolvedValue({ data: { operation_id: 'op-a', outcome: 'submitted', automatic_retry: false, business_outcome: 'unknown' } })
  vi.stubGlobal('crypto', { subtle: { digest: vi.fn(async (_: string, bytes: Uint8Array) => bytes.buffer) } })
})
afterEach(() => { app?.unmount(); root?.remove(); vi.unstubAllGlobals() })

describe('resume action inside original tool details', () => {
  it('retains original input/result, sends only an explicit click, and describes acknowledgement without claiming success', async () => {
    await mount()
    expect(root.textContent).toContain('original synthetic instruction')
    expect(root.textContent).toContain('batch_creating_workspace')
    expect(api.send).not.toHaveBeenCalled()
    const button = root.querySelector('button')!
    expect(button.textContent).toContain(en.chat.tasks.resume.action)
    button.click(); await settle()
    expect(api.send).toHaveBeenCalledTimes(1)
    expect(root.querySelector('[role="status"]')?.textContent?.trim()).toBe(en.chat.tasks.resume.submitted)
    expect(root.querySelector('button')).toBeNull()
  })
  it('keeps missing original arguments and unscoped details read only', async () => {
    const block = original(); block.input = null
    await mount(block)
    expect(root.querySelector('button')).toBeNull()
    expect(root.textContent).toContain(en.chat.tasks.resume.unavailable)
    app?.unmount(); root.remove(); await mount(original(), false)
    expect(root.querySelector('button')).toBeNull(); expect(api.send).not.toHaveBeenCalled()
  })
  it('reports a disconnected Runtime as unavailable and does not repeat the POST', async () => {
    api.send.mockResolvedValue({ data: { operation_id: 'op-a', outcome: 'unavailable', automatic_retry: false, business_outcome: 'unknown' } })
    await mount(); root.querySelector('button')!.click(); await settle(); await settle()
    expect(root.textContent).toContain(en.chat.tasks.resume.unavailable)
    expect(api.send).toHaveBeenCalledTimes(1)
  })
  it('clears a pending acknowledgement when the host scope changes', async () => {
    let resolve!: (value: unknown) => void
    api.send.mockReturnValueOnce(new Promise(yes => { resolve = yes }))
    const host = await mount(); root.querySelector('button')!.click(); await settle()
    host.visible = false; await settle()
    resolve({ data: { operation_id: 'op-a', outcome: 'submitted', automatic_retry: false, business_outcome: 'unknown' } }); await settle()
    expect(root.textContent).not.toContain(en.chat.tasks.resume.submitted)
    expect(root.querySelector('button')).toBeNull()
  })
})
