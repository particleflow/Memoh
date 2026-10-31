// @vitest-environment jsdom
import { createApp, h, nextTick, ref, type App } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import PanelAsset from './panel-asset.vue'

const transfer = vi.hoisted(() => ({ save: vi.fn().mockResolvedValue(undefined) }))
vi.mock('@/lib/media-transfer', () => ({ saveMediaDownload: transfer.save, MediaRecoveryError: class extends Error {} }))
let credential = 'first'
vi.mock('@/lib/api-client', () => ({
  sdkAuthQuery: () => ({ token: credential }),
  sdkApiUrl: ({ url, path, query }: { url: string, path?: Record<string, string>, query: { token: string } }) => {
    const resolved = path
      ? Object.entries(path).reduce((result, [key, value]) => result.replace(`{${key}}`, value), url)
      : url
    return `${resolved}?token=${query.token}`
  },
}))
vi.mock('@/store/chat-list', () => ({ useChatStore: () => ({ currentBotId: 'supervisor' }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('./use-panel-visible', () => ({ usePanelVisible: () => ref(true) }))
vi.mock('./panel-frame.vue', () => ({ default: { template: '<div><slot /></div>' } }))
vi.mock('@/components/monaco-editor/index.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@felinic/ui', () => ({
  Button: { template: '<button><slot /></button>' },
  PanePlaceholder: { template: '<div><slot /><slot name="action" /></div>' },
  toast: { error: vi.fn() },
}))

const hash = 'a'.repeat(64)
const id = '10000000-0000-4000-8000-000000000001'
const scoped = `/bots/${id}/sessions/${id}/delegations/${id}/attachments/${hash}`
let app: App | undefined
let host: HTMLDivElement

afterEach(() => {
  app?.unmount()
  host?.remove()
  vi.restoreAllMocks()
  transfer.save.mockClear()
})

async function render(delegationUrl?: string) {
  host = document.createElement('div')
  document.body.append(host)
  // Dock persistence roundtrip must store a scope path, never a bearer URL.
  const params = JSON.parse(JSON.stringify({ name: 'file', botId: 'supervisor', contentHash: hash, delegationUrl }))
  app = createApp({ render: () => h(PanelAsset, { params: { params, api: {} as never, containerApi: {} as never } }) })
  app.mount(host)
  await nextTick()
}

describe('delegated asset preview downloads', () => {
  it('retains conversation scope and refreshes credentials after restoring the dock', async () => {
    for (const token of ['first', 'renewed']) {
      credential = token
      await render(scoped)
      host.querySelector('button')!.click()
      await nextTick()
      expect(transfer.save).toHaveBeenCalledWith('supervisor', hash, undefined, scoped, 'file', expect.any(AbortSignal))
      transfer.save.mockClear()
      app?.unmount()
      app = undefined
      host.remove()
    }
  })

  it.each([`https://example.invalid${scoped}`, `${scoped}?token=stale`, scoped.replace(hash, 'b'.repeat(64))])('rejects malformed scope without a Bot media fallback: %s', async (value) => {
    await render(value)
    expect(host.querySelector('button')).toBeNull()
  })

  it('preserves ordinary owner-scoped media downloads', async () => {
    await render()
    host.querySelector('button')!.click()
    await nextTick()
    expect(transfer.save).toHaveBeenCalledWith('supervisor', hash, undefined, undefined, 'file', expect.any(AbortSignal))
  })
})
