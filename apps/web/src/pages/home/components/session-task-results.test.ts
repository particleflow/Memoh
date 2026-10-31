// @vitest-environment jsdom
import { createApp, h, nextTick, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Dialog, DialogPanel, DialogTitle, DialogDescription } from '@felinic/ui'
import en from '@/i18n/locales/en.json'
import SessionTaskResults from './session-task-results.vue'
import type { ComputerTaskDeliveryView } from '../composables/useComputerTaskDeliveries'
import { client } from '@memohai/sdk/client'
import { openAssetPreviewKey } from '../composables/useFileManagerProvider'
vi.mock('@/store/chat-list', () => ({ useChatStore: () => ({ currentBotId: 'wrong-global-bot' }) }))
import { useSessionTasks, type TaskScope } from '../composables/useSessionTasks'
const api = vi.hoisted(() => ({ list: vi.fn(), detail: vi.fn(), files: vi.fn() }))
const transfer = vi.hoisted(() => ({ save: vi.fn().mockResolvedValue(undefined) }))
vi.mock('@/lib/media-transfer', () => ({ saveMediaDownload: transfer.save, MediaRecoveryError: class extends Error {} }))
vi.mock('@memohai/sdk', () => ({
  getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdObservation: vi.fn().mockResolvedValue({ data: { operation_id: 'task', native: { availability: 'connection_unavailable' } } }),
  getBotsByBotIdSessionsBySessionIdTasks: api.list,
  getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskId: api.detail,
  getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdDeliveries: api.files,
}))
let app: ReturnType<typeof createApp>
let root: HTMLDivElement
beforeEach(() => {
  vi.useFakeTimers()
  transfer.save.mockClear()
  client.setConfig({ baseUrl: '/api' })
  localStorage.setItem('token', 'synthetic-token')
  vi.stubGlobal('requestAnimationFrame', (fn: FrameRequestCallback) => { fn(0); return 0 })
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  const task = { task_id: 'task', task_type: 'computer_operation', status: 'unknown', content_available: false, attachments_available: false }
  api.list.mockResolvedValue({ data: { items: [task] } })
  api.detail.mockResolvedValue({ data: task })
  api.files.mockResolvedValue({ data: { version: 1, operation_id: 'task', has_more: false, files: [{ delivery_id: 'result', state: 'pending' }] } })
})
afterEach(() => { app?.unmount(); root?.remove(); vi.useRealTimers(); vi.restoreAllMocks(); vi.unstubAllGlobals() })
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

const hash = 'a'.repeat(64)
describe.each(['web', 'native'])('native committed attachment access in %s host', host => {
  it('uses the reader Bot and native preview/download, hides pending refs and clears on denial', async () => {
    const preview = vi.fn(), opened = vi.fn()
    const deliveries = ref<ComputerTaskDeliveryView>({ botId: 'specialist', files: [
      { delivery_id: 'output', state: 'committed', reference: { version: 1, direction: 'upload', content_hash: hash, size_bytes: 42, mime: 'text/plain' } },
      { delivery_id: 'waiting', state: 'pending', reference: { version: 1, direction: 'upload', content_hash: hash, size_bytes: 42, mime: 'text/plain' } },
    ], loaded: true, loading: false, refresh: async () => {} })
    root = document.createElement('div'); document.body.append(root)
    app = createApp({ render: () => h(host === 'native' ? 'dialog' : 'section', { open: true }, [h(SessionTaskResults, {
      items: [], selected: { task_type: 'computer_operation', task_id: 'task' },
      detail: { task_type: 'computer_operation', task_id: 'task', status: 'unknown', content_available: false, attachments_available: false, session_id: 'session', parent_session_id: '', created_at: '2026-10-24T00:00:00Z', updated_at: '2026-10-24T00:00:00Z', summary: '' },
      loading: false, deliveries: deliveries.value, onAssetOpened: opened,
    })]) })
    if (host === 'web') app.provide(openAssetPreviewKey, preview)
    app.use(createI18n({ legacy: false, locale: 'en', messages: { en } })); app.mount(root)
    await nextTick()
    const link = root.querySelector('a')!
    expect(root.querySelectorAll('a')).toHaveLength(1)
    expect(link.getAttribute('href')).toBe(`/api/bots/specialist/media/${hash}?token=synthetic-token`)
    expect(link.download).toBe('output')
    const download = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function(this: HTMLAnchorElement) {
      expect(this.pathname).toBe(`/api/bots/specialist/media/${hash}`)
      expect(this.download).toBe('output')
    })
    root.querySelector('button[title="output"]')!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    if (host === 'web') {
      expect(preview).toHaveBeenCalledWith(expect.objectContaining({ botId: 'specialist', contentHash: hash, src: undefined }))
      expect(opened).toHaveBeenCalledOnce()
      expect(transfer.save).not.toHaveBeenCalled()
    } else {
      // This dialog-only host has no preview injection or phone byte bridge.
      // Its fallback must use the shared durable downloader with the reader
      // identity, rather than a bare URL or the global Bot.
      expect(transfer.save).toHaveBeenCalledExactlyOnceWith('specialist', hash, 42, undefined, 'output', expect.any(AbortSignal))
    }
    expect(download).not.toHaveBeenCalled()
    deliveries.value = { ...deliveries.value, error: { status: 403 } }
    await nextTick()
    expect(root.querySelectorAll('a')).toHaveLength(0)
    deliveries.value = { ...deliveries.value, error: undefined, botId: undefined }
    await nextTick()
    expect(root.querySelectorAll('a')).toHaveLength(0) // Never fall back to the global Bot.
  })

  it('reuses the media lightbox inside the host, then retires it on a scope change', async () => {
    const deliveries = ref<ComputerTaskDeliveryView>({ botId: 'specialist', files: [
      { delivery_id: 'picture', state: 'committed', reference: { version: 1, direction: 'upload', content_hash: hash, size_bytes: 42, mime: 'image/png' } },
    ], loaded: true, loading: false, refresh: async () => {} })
    root = document.createElement('div'); document.body.append(root)
    app = createApp({ render: () => h(host === 'native' ? 'dialog' : 'section', { open: true }, [h(SessionTaskResults, {
      items: [], selected: { task_type: 'computer_operation', task_id: 'task' },
      detail: { task_type: 'computer_operation', task_id: 'task', status: 'unknown', content_available: false, attachments_available: false, session_id: 'session', parent_session_id: '', created_at: '2026-10-24T00:00:00Z', updated_at: '2026-10-24T00:00:00Z', summary: '' },
      loading: false, deliveries: deliveries.value,
    })]) })
    app.use(createI18n({ legacy: false, locale: 'en', messages: { en } })); app.mount(root)
    await nextTick()
    root.querySelector('img')!.closest('button')!.click(); await nextTick()
    expect(root.querySelector('[data-keyboard-scope="mediaLightbox"] img')?.getAttribute('src')).toBe(`/api/bots/specialist/media/${hash}?token=synthetic-token`)
    deliveries.value = { ...deliveries.value, files: [...deliveries.value.files] }
    await nextTick()
    expect(root.querySelector('[data-keyboard-scope="mediaLightbox"]')).not.toBeNull() // Routine polling preserves the open preview.
    deliveries.value = { ...deliveries.value, botId: 'changed', files: [] }
    await nextTick(); await vi.advanceTimersByTimeAsync(400)
    expect(root.querySelector('[data-keyboard-scope="mediaLightbox"]')).toBeNull()
  })
})

describe.each(['web', 'native'])('computer observation in %s host', host => {
  it('keeps native status separate from the task outcome and clears unavailable identity', async () => {
    root = document.createElement('div'); document.body.append(root)
    const detail = { task_type: 'computer_operation' as const, task_id: 'operation', session_id: 'session', parent_session_id: 'root', created_at: '', updated_at: '', status: 'submitted' as const, summary: '', content_available: false, attachments_available: false }
    const observation = ref<import('../composables/useComputerTaskObservation').ComputerTaskObservationView>({
      loading: false, refresh: vi.fn().mockResolvedValue(undefined), receivedAt: '12:00:00',
      data: { operation_id: 'operation', target_id: 'synthetic-pc', workspace_id: 'w-observed', pane_id: 'p-observed', state: 'submitted', reconciliation: { revision: '9007199254740993123', receipt_state: 'validated' }, business_outcome: 'unknown', native: { availability: 'online', agent_status: 'working', reference_match: 'observed' } },
    })
    app = createApp({ setup() {
      const body = () => h(SessionTaskResults, { items: [], detail, selected: detail, loading: false, observation: observation.value })
      if (host === 'web') return () => h(Dialog, { open: true }, { default: () => h(DialogPanel, {}, { default: () => [
        h(DialogTitle, {}, { default: () => 'Synthetic task' }), h(DialogDescription, {}, { default: () => 'Synthetic observation' }), body(),
      ] }) })
      return () => h('dialog', { open: true, 'aria-label': 'Synthetic task' }, [body()])
    } })
    app.use(createI18n({ legacy: false, locale: 'en', messages: { en } })); app.mount(root)
    await nextTick(); await vi.advanceTimersByTimeAsync(50); await nextTick()
    const dialog = document.querySelector(host === 'web' ? '[role=dialog]' : 'dialog')!
    expect(dialog.textContent).toContain('Native Agent: Working')
    expect(dialog.textContent).toContain('w-observed')
    expect(dialog.textContent).toContain('9007199254740993123')
    observation.value = { ...observation.value, data: { ...observation.value.data, native: { availability: 'online', agent_status: 'done', reference_match: 'observed' } } }
    await nextTick()
    expect(dialog.textContent).toContain(en.chat.tasks.observation.states.done)
    expect(dialog.textContent).toContain(en.chat.tasks.statuses.submitted)
    expect(dialog.textContent).not.toContain(en.chat.tasks.businessUnknown)
    expect(dialog.textContent).toContain(en.chat.tasks.observation.checkpoints.fields.business_outcome)
    observation.value = { ...observation.value, error: { status: 403 } } // Even stale data must be hidden on access loss.
    await nextTick()
    expect(dialog.textContent).not.toContain('9007199254740993123')
    expect(dialog.textContent).not.toContain('w-observed')
    observation.value = { ...observation.value, data: undefined, error: { status: 404, code: 'http.not_found' } }
    await nextTick()
    expect(dialog.textContent).toContain(en.errors.http.not_found)
    expect(dialog.textContent).not.toContain('w-observed')
    const refresh = [...dialog.querySelectorAll('button')].find(button => button.textContent?.includes(en.chat.tasks.observation.refresh))!
    refresh.click(); expect(observation.value.refresh).toHaveBeenCalledOnce()
  })
})
