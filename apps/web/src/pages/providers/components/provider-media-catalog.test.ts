// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, nextTick, reactive } from 'vue'
import type { App } from 'vue'
const mocks = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@memohai/sdk/client', () => ({ client: { get: mocks.get } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/utils/api-error', () => ({ resolveApiErrorMessage: (_error: unknown, fallback: string) => fallback }))
vi.mock('@felinic/ui', () => {
  const Pass = defineComponent({ setup: (_, { slots }) => () => h('section', slots.default?.()) })
  return {
    Button: defineComponent({ setup: (_, { slots, attrs }) => () => h('button', attrs, slots.default?.()) }),
    Dialog: Pass, DialogBody: Pass, DialogClose: Pass, DialogDescription: Pass, DialogFooter: Pass,
    DialogHeader: Pass, DialogPanel: Pass, DialogTitle: Pass, DialogTrigger: Pass, SettingsSection: Pass,
    SettingsRow: defineComponent({ props: { label: String, description: String }, setup: props => () => h('p', [props.label, props.description]) }),
  }
})
import ProviderMediaCatalog from './provider-media-catalog.vue'

let app: App | undefined
let host: HTMLDivElement
async function flush() { await Promise.resolve(); await nextTick(); await Promise.resolve(); await nextTick() }
function mount() {
  const props = reactive({ providerId: 'saved-provider-id', disabled: false })
  host = document.createElement('div'); document.body.append(host)
  app = createApp({ render: () => h(ProviderMediaCatalog, props) })
  app.config.globalProperties.$t = (key: string) => key
  app.mount(host)
  return props
}
function read() { ([...host.querySelectorAll('button')].find(button => button.textContent?.trim() === 'provider.mediaCatalog.read') as HTMLButtonElement).click() }
const catalog = {
  provider_id: 'saved-provider-id', protocol: 'magpie', checked_at: '2026-10-25T00:00:00Z',
  models: [{ id: 'synthetic/video', kind: 'video', input: ['text'], output: ['video'], capability_source: 'catalog_kind_and_modalities' }],
  transcription_status: 'unverified',
}
afterEach(() => { app?.unmount(); host?.remove(); mocks.get.mockReset() })
describe('read-only media catalog', () => {
  it('only explicitly reads the saved provider through the authenticated SDK client', async () => {
    mocks.get.mockResolvedValue({ data: catalog }); mount()
    expect(mocks.get).not.toHaveBeenCalled()
    read(); await flush()
    expect(mocks.get).toHaveBeenCalledExactlyOnceWith({
      url: '/providers/{id}/media-catalog', path: { id: 'saved-provider-id' }, query: { protocol: 'magpie' }, throwOnError: true,
    })
    expect(host.textContent).toContain('synthetic/video')
  })
  it('distinguishes a valid empty catalog from an error, and clears previous facts', async () => {
    mocks.get.mockResolvedValueOnce({ data: catalog }).mockRejectedValueOnce({ code: 'agent.provider_permission_denied' }).mockResolvedValueOnce({ data: { ...catalog, models: [] } })
    mount(); read(); await flush()
    read(); await flush()
    expect(host.textContent).toContain('provider.mediaCatalog.failed')
    expect(host.textContent).not.toContain('synthetic/video')
    expect(host.textContent).not.toContain('provider.mediaCatalog.empty')
    read(); await flush()
    expect(host.textContent).toContain('provider.mediaCatalog.empty')
    expect(host.textContent).not.toContain('provider.mediaCatalog.failed')
  })
  it('does not auto-read or save when the form is dirty and ignores stale in-flight results', async () => {
    let finish!: (value: unknown) => void
    mocks.get.mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const props = mount(); read(); await flush(); read()
    expect(mocks.get).toHaveBeenCalledTimes(1)
    props.providerId = 'different-provider'; await flush()
    finish({ data: catalog }); await flush()
    expect(host.textContent).not.toContain('synthetic/video')
    props.disabled = true; await flush(); read()
    expect(mocks.get).toHaveBeenCalledTimes(1)
    props.disabled = false; await flush()
    expect(mocks.get).toHaveBeenCalledTimes(1)
  })
})
