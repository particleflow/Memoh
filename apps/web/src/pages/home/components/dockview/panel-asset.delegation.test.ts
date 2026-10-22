// @vitest-environment jsdom
import { createApp, h, nextTick, ref, type App } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import PanelAsset from './panel-asset.vue'

let credential = 'first'
vi.mock('@/lib/api-client', () => ({
  sdkAuthQuery: () => ({ token: credential }),
  sdkApiUrl: ({ url, path, query }: { url: string, path?: Record<string, string>, query: { token: string } }) => `${path ? url.replace('{bot_id}', path.bot_id).replace('{content_hash}', path.content_hash) : url}?token=${query.token}`,
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
      let actual = ''
      const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
        actual = this.getAttribute('href') ?? ''
      })
      host.querySelector('button')!.click()
      expect(click).toHaveBeenCalledOnce()
      expect(actual).toBe(`${scoped}?token=${token}`)
      click.mockRestore()
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
    let actual = ''
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      actual = this.getAttribute('href') ?? ''
    })
    host.querySelector('button')!.click()
    expect(click).toHaveBeenCalledOnce()
    expect(actual).toBe(`/bots/supervisor/media/${hash}?token=${credential}`)
  })
})
