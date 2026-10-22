// @vitest-environment jsdom
import { createApp, h, nextTick, ref, type Component } from 'vue'
import { Dialog, DialogPanel, DialogTitle, DialogDescription } from '@felinic/ui'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { BotdelegationView } from '@memohai/sdk'
import en from '@/i18n/locales/en.json'
import BotDelegationTasks from './bot-delegation-tasks.vue'
import { useBotDelegations } from '../composables/useBotDelegations'
import type { TaskScope } from '../composables/useSessionTasks'

const api = vi.hoisted(() => ({ list: vi.fn(), detail: vi.fn() }))
vi.mock('@memohai/sdk', () => ({
  getBotsByBotIdSessionsBySessionIdDelegations: api.list,
  getBotsByBotIdSessionsBySessionIdDelegationsByDelegationId: api.detail,
}))
const task: BotdelegationView = {
  delegation_id: 'handoff-a', request_id: 'request-a', source_session_id: 'session-a', target_bot_id: 'specialist-a',
  state: 'completed', delivery_state: 'accepted', created_at: '2026-10-22T00:00:00Z', updated_at: '2026-10-22T00:00:00Z',
  summary: { text: '<img src="https://example.invalid/image" onerror="alert(1)"> [link](javascript:alert(2))', truncated: true, source: 'assistant_final' },
  attachments: [{ content_hash: 'synthetic-hash', url: '/bots/other/media/synthetic-hash', mime: 'image/png' }],
}
const mounts: { app: ReturnType<typeof createApp>, root: HTMLElement }[] = []
function mountComponent(component: Component, props?: Record<string, unknown>) {
  const root = document.createElement('div'); document.body.append(root)
  const app = createApp(component, props)
  app.use(createI18n({ legacy: false, locale: 'en', messages: { en } }))
  app.mount(root); mounts.push({ app, root }); return root
}
function mount(overrides: { items?: BotdelegationView[], detail?: BotdelegationView, selected?: string, error?: unknown, nextCursor?: string } = {}) {
  return mountComponent(BotDelegationTasks, { items: [], loading: false, ...overrides })
}
beforeEach(() => {
  vi.useFakeTimers()
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  api.list.mockReset().mockResolvedValue({ data: { items: [task] } })
  api.detail.mockReset().mockResolvedValue({ data: task })
})
afterEach(() => {
  mounts.splice(0).forEach(({ app, root }) => { app.unmount(); root.remove() })
  vi.unstubAllGlobals(); vi.useRealTimers()
})

describe('specialist handoff content and states', () => {
  it('renders only plain final text, keeps execution and delivery separate, and exposes no attachment fallback', () => {
    const root = mount({ selected: task.delegation_id, detail: task })
    expect(root.textContent).toContain('Execution completed')
    expect(root.textContent).toContain('Return delivery · Accepted')
    expect(root.textContent).toContain('does not confirm that the supervisor has processed')
    expect(root.textContent).toContain(task.summary!.text)
    expect(root.textContent).toContain(en.chat.tasks.truncated)
    expect(root.textContent).toContain('Downloads are not available')
    expect(root.querySelectorAll('a,img,iframe,video,script,audio')).toHaveLength(0)
    expect(root.textContent).not.toContain('synthetic-hash')
    expect(root.textContent).not.toContain('/bots/other')
    expect(root.querySelectorAll('button')).toHaveLength(0)
  })

  it.each(['accepted', 'running', 'unknown', 'failed'] as const)('does not promote %s or stale summary fields to a completed result', (state) => {
    const root = mount({ selected: task.delegation_id, detail: { ...task, state, delivery_state: 'pending' } })
    expect(root.textContent).not.toContain(task.summary!.text)
    expect(root.textContent).toContain('No final visible reply is available.')
    expect(root.textContent).not.toContain('Execution completed')
    expect(root.textContent).not.toContain('returned reference')
    if (state === 'accepted' || state === 'running') expect(root.textContent).toContain('Live work and tool logs are not available')
  })

  it('distinguishes a successful empty list from unsupported and inaccessible APIs', () => {
    expect(mount().textContent).toContain(en.chat.delegations.empty)
    const unsupported = mount({ error: { code: 'http.not_found', status: 404 } })
    expect(unsupported.textContent).toContain(en.chat.delegations.unsupported)
    expect(unsupported.textContent).not.toContain(en.chat.delegations.empty)
    const hidden = mount({ error: { code: 'session.not_found', status: 404 } })
    expect(hidden.textContent).toContain(en.chat.delegations.inaccessible)
    expect(hidden.textContent).not.toContain(en.chat.delegations.unsupported)
  })

  it('keeps empty filtered pages pageable', () => {
    const root = mount({ nextCursor: 'opaque' })
    expect(root.textContent).toContain(en.chat.tasks.filteredPage)
    expect(root.textContent).not.toContain(en.chat.delegations.empty)
    expect(root.textContent).toContain(en.chat.tasks.next)
  })

  it.each([
    { code: 'http.unauthorized', status: 401 },
    { code: 'http.forbidden', status: 403 },
    { code: 'workspace.unreachable', status: 503 },
    { code: 'http.internal_server_error', status: 500 },
  ])('uses localized protocol diagnostics for $code without exposing server text', (error) => {
    const root = mount({ error: { ...error, detail: 'PRIVATE_DIAGNOSTIC https://example.invalid/private' } })
    expect(root.textContent).not.toContain('PRIVATE_DIAGNOSTIC')
    expect(root.textContent).not.toContain('example.invalid')
    expect(root.textContent).not.toContain(en.chat.delegations.empty)
    expect(root.querySelectorAll('button')).toHaveLength(1)
    expect(root.querySelector('button')?.textContent).toContain(en.chat.tasks.reload)
  })

  it('does not describe a network failure as an empty queue or unsupported API', () => {
    const root = mount({ error: new TypeError('PRIVATE_NETWORK') })
    expect(root.textContent).toContain(en.common.networkError)
    expect(root.textContent).not.toContain('PRIVATE_NETWORK')
    expect(root.textContent).not.toContain(en.chat.delegations.empty)
    expect(root.textContent).not.toContain(en.chat.delegations.unsupported)
  })
})

describe.each(['web', 'native'] as const)('shared body in a %s dialog host', (host) => {
  it('mounts the same reader/body with host-owned accessible title and description', async () => {
    mountComponent({
      setup() {
        const scope = ref<TaskScope | null>({ bot: 'supervisor', session: 'session', identity: 'synthetic', selection: 'native' })
        const reader = useBotDelegations(scope, ref(true))
        const body = () => h(BotDelegationTasks, {
          items: reader.items.value, detail: reader.detail.value, selected: reader.selected.value,
          cursor: reader.cursor.value, nextCursor: reader.nextCursor.value, loading: reader.loading.value, error: reader.error.value,
          onOpenTask: reader.openTask, onPage: reader.page, onRefresh: reader.refresh,
        })
        if (host === 'web') return () => h(Dialog, { open: true }, { default: () => h(DialogPanel, {}, { default: () => [
          h(DialogTitle, {}, { default: () => en.chat.delegations.title }),
          h(DialogDescription, {}, { default: () => en.chat.delegations.description }), body(),
        ] }) })
        // Native HTML dialog has no Reka DialogRoot context. Its host owns the
        // accessibility relationship and lifecycle, not the shared task body.
        return () => h('dialog', { open: true, 'aria-labelledby': 'native-title', 'aria-describedby': 'native-description' }, [
          h('h2', { id: 'native-title' }, en.chat.delegations.title),
          h('p', { id: 'native-description' }, en.chat.delegations.description), body(),
        ])
      },
    })
    await nextTick(); await vi.advanceTimersByTimeAsync(50); await nextTick()
    const dialog = document.querySelector(host === 'web' ? '[role=dialog]' : 'dialog')!
    expect(dialog).not.toBeNull()
    const title = document.getElementById(dialog.getAttribute('aria-labelledby')!)
    const description = document.getElementById(dialog.getAttribute('aria-describedby')!)
    expect(title?.textContent).toBe(en.chat.delegations.title)
    expect(description?.textContent).toBe(en.chat.delegations.description)
    const item = [...dialog.querySelectorAll('button')].find(el => el.textContent?.includes('Target Bot'))!
    expect(item).toBeDefined(); item.click()
    await nextTick(); await vi.advanceTimersByTimeAsync(50); await nextTick()
    expect(dialog.textContent).toContain(task.summary!.text)
    expect(dialog.textContent).toContain('Return delivery · Accepted')
    expect(dialog.querySelectorAll('a,img,iframe')).toHaveLength(0)
    expect(api.list).toHaveBeenCalledTimes(1)
    expect(api.detail).toHaveBeenCalledTimes(1)
  })
})
