// @vitest-environment jsdom
import { createApp, h } from 'vue'
import { Dialog } from '@felinic/ui'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { BotdelegationView } from '@memohai/sdk'
import en from '@/i18n/locales/en.json'
import BotDelegationTasks from './bot-delegation-tasks.vue'

vi.mock('@memohai/sdk', () => ({
  getBotsByBotIdSessionsBySessionIdDelegations: vi.fn(),
  getBotsByBotIdSessionsBySessionIdDelegationsByDelegationId: vi.fn(),
}))
const task: BotdelegationView = {
  delegation_id: 'handoff-a', request_id: 'request-a', source_session_id: 'session-a', target_bot_id: 'specialist-a',
  state: 'completed', delivery_state: 'accepted', created_at: '2026-10-22T00:00:00Z', updated_at: '2026-10-22T00:00:00Z',
  summary: { text: '<img src="https://example.invalid/image" onerror="alert(1)"> [link](javascript:alert(2))', truncated: true, source: 'assistant_final' },
  attachments: [{ content_hash: 'synthetic-hash', url: '/bots/other/media/synthetic-hash', mime: 'image/png' }],
}
const mounts: { app: ReturnType<typeof createApp>, root: HTMLElement }[] = []
function mount(overrides: { items?: BotdelegationView[], detail?: BotdelegationView, selected?: string, error?: unknown, nextCursor?: string } = {}) {
  const root = document.createElement('div'); document.body.append(root)
  const app = createApp({ render: () => h(Dialog, {}, { default: () => h(BotDelegationTasks, { items: [], loading: false, ...overrides }) }) })
  app.use(createI18n({ legacy: false, locale: 'en', messages: { en } }))
  app.mount(root); mounts.push({ app, root }); return root
}
afterEach(() => mounts.splice(0).forEach(({ app, root }) => { app.unmount(); root.remove() }))

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
