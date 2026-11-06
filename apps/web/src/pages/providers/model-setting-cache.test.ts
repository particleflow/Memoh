// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, ref } from 'vue'
import type { App, PropType, Slots } from 'vue'
import { createPinia } from 'pinia'
import { hydrateQueryCache, PiniaColada, useQuery, useQueryCache } from '@pinia/colada'
import type { ProvidersGetResponse } from '@memohai/sdk'
import { cancelPendingQueryCacheSave, QUERY_CACHE_STORAGE_KEY, saveQueryCacheToDiskNow } from '@/lib/query-cache-persistence'

const api = vi.hoisted(() => ({ providers: vi.fn(), put: vi.fn(), models: vi.fn() }))
vi.mock('@memohai/sdk', () => ({
  putProvidersById: api.put, getProvidersByIdModels: api.models,
  deleteModelsById: vi.fn(), deleteProvidersById: vi.fn(),
  postProvidersByIdImportModels: vi.fn(), postProvidersFromTemplate: vi.fn(),
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/composables/useProviderTemplateModels', () => ({
  useProviderTemplateModels: () => ({ models: ref([]) }),
}))
vi.mock('@/components/provider-icon/index.vue', () => ({ default: () => h('span') }))
vi.mock('@/components/provider-identity-card/index.vue', () => ({ default: () => h('div') }))
vi.mock('./components/model-list.vue', () => ({ default: () => h('div') }))
vi.mock('./components/provider-form.vue', () => ({
  // The parent receives the same form callback; query/mutation/cache are real.
  // eslint-disable-next-line vue/one-component-per-file -- Child form test double.
  default: defineComponent({
    props: { saveProvider: { type: Function as PropType<(value: Record<string, unknown>) => Promise<unknown>>, required: true } },
    setup(props) {
      const state = ref('idle')
      return () => h('button', {
        onClick: async () => {
          state.value = 'saving'
          try {
            await props.saveProvider({ config: { images_api: { protocol: 'openai-images' } } })
            state.value = 'saved'
          } catch {
            state.value = 'failed'
          }
        },
      }, state.value)
    },
  }),
}))
vi.mock('@felinic/ui', () => ({
  SettingsShell: (_props: unknown, { slots }: { slots: Slots }) => h('div', slots.default?.()),
  Button: () => h('button'), ConfirmPopover: () => h('div'), Switch: () => h('div'),
  toast: { success: vi.fn(), error: vi.fn() },
}))

const original: ProvidersGetResponse = {
  id: 'synthetic-provider', name: 'Synthetic provider', client_type: 'openai-completions', enable: true,
  config: { base_url: 'https://provider.example.invalid/v1', api_key: 'synthetic********' },
}
const updated: ProvidersGetResponse = {
  ...original, config: { ...original.config, images_api: { protocol: 'openai-images' } },
}
const apps: App[] = []
beforeEach(() => {
  vi.resetAllMocks()
  const storage = new Map<string, string>()
  vi.stubGlobal('localStorage', {
    get length() { return storage.size },
    getItem: (key: string) => storage.get(key) ?? null,
    setItem: (key: string, value: string) => storage.set(key, value),
    removeItem: (key: string) => storage.delete(key),
    clear: () => storage.clear(),
    key: (index: number) => [...storage.keys()][index] ?? null,
  } satisfies Storage)
  localStorage.setItem('token', 'synthetic-login-identity')
  api.providers.mockResolvedValue([original])
  api.put.mockResolvedValue({ data: updated })
  api.models.mockResolvedValue({ data: [] })
})
afterEach(() => {
  apps.splice(0).forEach(app => app.unmount())
  cancelPendingQueryCacheSave()
  localStorage.clear()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

async function mountSetting() {
  const ModelSetting = (await import('./model-setting.vue')).default
  const pinia = createPinia()
  // eslint-disable-next-line vue/one-component-per-file -- Parent harness observes the real provider query.
  const Wrapper = defineComponent({
    setup() {
      const { data } = useQuery({ key: ['providers'], query: (): Promise<ProvidersGetResponse[]> => api.providers(), staleTime: Infinity })
      return () => data.value?.[0] ? h(ModelSetting, { provider: data.value[0] }) : h('div')
    },
  })
  const root = document.createElement('div')
  document.body.append(root)
  const app = createApp(Wrapper)
  app.config.globalProperties.$t = (key: string) => key
  app.use(pinia).use(PiniaColada)
  app.mount(root)
  apps.push(app)
  await vi.waitFor(() => expect(root.querySelector('button')?.textContent).toBe('idle'))
  const cache = useQueryCache(pinia)
  saveQueryCacheToDiskNow(cache)
  expect(localStorage.getItem(QUERY_CACHE_STORAGE_KEY)).toContain('Synthetic provider')
  return { root, cache, button: root.querySelector('button')! }
}

describe('Provider save display-cache consistency', () => {
  it('waits for the provider refresh and flushes the existing sanitized cache before reporting saved', async () => {
    const { button } = await mountSetting()
    let resolveRefresh!: (value: ProvidersGetResponse[]) => void
    api.providers.mockImplementationOnce(() => new Promise(resolve => { resolveRefresh = resolve }))
    button.click()
    await vi.waitFor(() => expect(api.providers).toHaveBeenCalledTimes(2))
    expect(button.textContent).toBe('saving')
    expect(localStorage.getItem(QUERY_CACHE_STORAGE_KEY)).not.toContain('openai-images')
    resolveRefresh([updated])
    await vi.waitFor(() => expect(button.textContent).toBe('saved'))

    const disk = localStorage.getItem(QUERY_CACHE_STORAGE_KEY)!
    expect(disk).toContain('openai-images')
    expect(disk).not.toContain('api_key')
    expect(disk).not.toContain('synthetic********')
    const restored = useQueryCache(createPinia())
    hydrateQueryCache(restored, JSON.parse(disk))
    expect(restored.getQueryData<ProvidersGetResponse[]>(['providers'])?.[0]?.config?.images_api).toEqual({ protocol: 'openai-images' })
    expect(localStorage.getItem('token')).toBe('synthetic-login-identity')
    expect(api.put).toHaveBeenCalledOnce()
    expect(api.put.mock.calls[0]![0].body).toEqual({ config: { images_api: { protocol: 'openai-images' } } })
  })

  it('preserves a successful PUT outcome when the refresh fails and excludes its stale entry from disk', async () => {
    const { button } = await mountSetting()
    api.providers.mockRejectedValueOnce(new Error('Synthetic refresh failure'))
    button.click()
    await vi.waitFor(() => expect(button.textContent).toBe('saved'))
    await vi.waitFor(() => expect(api.providers).toHaveBeenCalledTimes(2))
    expect(localStorage.getItem(QUERY_CACHE_STORAGE_KEY) ?? '').not.toContain('Synthetic provider')
    expect(localStorage.getItem(QUERY_CACHE_STORAGE_KEY) ?? '').not.toContain('synthetic********')
    expect(api.put).toHaveBeenCalledOnce()
    expect(localStorage.getItem('token')).toBe('synthetic-login-identity')
  })

  it('keeps a rejected PUT as a failed save even when the subsequent refresh succeeds', async () => {
    const { button } = await mountSetting()
    api.put.mockRejectedValueOnce(new Error('Synthetic PUT failure'))
    button.click()
    await vi.waitFor(() => expect(button.textContent).toBe('failed'))
    expect(localStorage.getItem(QUERY_CACHE_STORAGE_KEY)).not.toContain('openai-images')
    expect(api.put).toHaveBeenCalledOnce()
  })
})
