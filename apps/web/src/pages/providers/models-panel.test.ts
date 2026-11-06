// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, h, nextTick } from 'vue'
import type { Component, Ref, Slots } from 'vue'

const mocks = vi.hoisted(() => ({
  providerData: undefined as unknown as Ref<Array<Record<string, unknown>> | undefined>,
  providersLoading: undefined as unknown as Ref<boolean>,
  modelData: undefined as unknown as Ref<Array<Record<string, unknown>>>,
  templateData: undefined as unknown as Ref<Array<Record<string, unknown>> | undefined>,
  templatesLoading: undefined as unknown as Ref<boolean>,
  route: { query: {} as Record<string, string> },
  replace: vi.fn(),
  realQueries: false,
  fetchProviders: vi.fn(),
}))

vi.mock('vue-router', () => ({
  useRoute: () => mocks.route,
  useRouter: () => ({ replace: mocks.replace }),
}))

vi.mock('@pinia/colada', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@pinia/colada')>()
  const { ref } = await import('vue')
  mocks.providerData = ref()
  mocks.providersLoading = ref(false)
  mocks.modelData = ref([])
  mocks.templateData = ref()
  mocks.templatesLoading = ref(false)
  return {
    ...actual,
    useQuery: (options: { key: () => string[], query: () => Promise<unknown> }) => {
      if (mocks.realQueries) return actual.useQuery(options)
      const { key } = options
      if (key()[0] === 'providers') return { data: mocks.providerData, isLoading: mocks.providersLoading }
      if (key()[0] === 'provider-templates') return { data: mocks.templateData, isLoading: mocks.templatesLoading }
      return { data: mocks.modelData, isLoading: ref(false) }
    },
  }
})

vi.mock('@memohai/sdk', () => ({
  getModels: vi.fn(),
  getProviders: mocks.fetchProviders,
  getProviderTemplates: vi.fn(),
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

vi.mock('lucide-vue-next', () => ({
  Boxes: () => h('span'),
  Box: () => h('span'),
  ChevronRight: () => h('span'),
  Plus: () => h('span'),
  Search: () => h('span'),
}))

vi.mock('@felinic/ui', () => {
  const Passthrough = (_props: Record<string, unknown>, { slots }: { slots: Slots }) => h('div', slots.default?.())
  const BackendCard = {
    props: ['name'],
    emits: ['click'],
    setup(props: { name: string }, { emit }: { emit: (event: 'click') => void }) {
      return () => h('button', {
        'data-provider': props.name,
        'onClick': () => emit('click'),
      }, props.name)
    },
  }
  const DetailPane = {
    props: ['backLabel', 'width', 'loading'],
    emits: ['back'],
    setup(
      props: { loading?: boolean },
      { slots, emit }: { slots: Slots, emit: (event: 'back') => void },
    ) {
      return () => h('div', { 'data-testid': 'detail-pane' }, [
        h('button', { 'data-testid': 'back', onClick: () => emit('back') }, 'back'),
        props.loading
          ? h('div', { 'data-testid': 'detail-pane-skeleton' })
          : slots.default?.(),
      ])
    },
  }
  const SwapTransition = (_props: Record<string, unknown>, { slots }: { slots: Slots }) => h('div', slots.default?.())
  const PageShell = (_props: Record<string, unknown>, { slots }: { slots: Slots }) => h('div', [slots.actions?.(), slots.default?.()])
  return {
    BackendCard,
    DetailPane,
    SwapTransition,
    PageShell,
    Button: Passthrough,
    Empty: Passthrough,
    EmptyContent: Passthrough,
    EmptyDescription: Passthrough,
    EmptyHeader: Passthrough,
    EmptyMedia: Passthrough,
    EmptyTitle: Passthrough,
    InputGroup: Passthrough,
    InputGroupAddon: Passthrough,
    InputGroupInput: Passthrough,
    Skeleton: (props: { class?: string }) => h('div', { 'data-slot': 'skeleton', class: props.class }),
  }
})

vi.mock('@/components/add-provider/index.vue', () => ({
  default: {
    props: ['open'],
    emits: ['update:open'],
    setup(props: { open?: boolean }) {
      return () => h('div', {
        'data-testid': 'add-provider-dialog',
        'data-open': String(props.open ?? false),
      })
    },
  },
}))
vi.mock('@/components/provider-icon/index.vue', () => ({ default: () => h('span') }))
vi.mock('@/components/backend-card-grid-skeleton/index.vue', () => ({ default: () => h('div', { 'data-slot': 'skeleton' }) }))
vi.mock('./model-setting.vue', () => ({
  default: {
    props: ['provider'],
    emits: ['materialized'],
    setup(
      props: { provider?: { id?: string, name?: string, provider_template_id?: string } },
      { emit }: { emit: (event: 'materialized', provider: Record<string, unknown>) => void },
    ) {
      return () => h('div', { 'data-testid': 'provider-detail' }, [
        `${props.provider?.id}:${props.provider?.name}`,
        h('button', {
          'data-testid': 'materialize-provider',
          'onClick': () => emit('materialized', {
            id: 'provider-anthropic',
            name: props.provider?.name,
            provider_template_id: props.provider?.provider_template_id,
          }),
        }),
      ])
    },
  },
}))

let ModelsPanel: Component

async function mountPage() {
  const root = document.createElement('div')
  document.body.append(root)
  // The container page owns the search box; the panel only receives its query.
  // eslint-disable-next-line vue/one-component-per-file -- Existing route-state test mount.
  const app = createApp(ModelsPanel, { searchQuery: '' })
  app.mount(root)
  await nextTick()
  return { app, root }
}

describe('models panel route state', () => {
  beforeEach(async () => {
    vi.resetModules()
    ModelsPanel = (await import('./models-panel.vue')).default
    mocks.providerData.value = [
      { id: 'provider-one', name: 'One', enable: true },
      { id: 'provider-two', name: 'Two', enable: true },
    ]
    mocks.providersLoading.value = false
    mocks.modelData.value = []
    mocks.templateData.value = []
    mocks.templatesLoading.value = false
    mocks.realQueries = false
    mocks.fetchProviders.mockReset()
    mocks.route.query = {}
    mocks.replace.mockReset()
  })

  afterEach(() => {
    document.body.innerHTML = ''
  })

  it.each(['success', 'failure', 'missing-key'] as const)('refreshes a fresh sanitized snapshot on mount (%s)', async (outcome) => {
    mocks.realQueries = true
    mocks.route.query = { provider: 'provider-one' }
    const { createPinia } = await import('pinia')
    const { hydrateQueryCache, PiniaColada, useQueryCache } = await import('@pinia/colada')
    const { QUERY_CACHE_STORAGE_KEY, saveQueryCacheToDiskNow } = await import('@/lib/query-cache-persistence')
    const { getModels, getProviderTemplates } = await import('@memohai/sdk')
    vi.mocked(getModels).mockResolvedValue({ data: [] } as never)
    vi.mocked(getProviderTemplates).mockResolvedValue({ data: [] } as never)
    const fresh = { id: 'provider-one', name: 'Fresh provider', client_type: 'openai-completions', config: { api_key: 'synthetic********' } }
    const seed = createPinia()
    // eslint-disable-next-line vue/one-component-per-file -- Independent cache owner before reload.
    const seedApp = createApp({ render: () => null }).use(seed).use(PiniaColada)
    const seedCache = seedApp.runWithContext(() => useQueryCache())
    seedCache.setQueryData(['providers'], [{ ...fresh, name: 'Cached provider' }])
    const values = new Map<string, string>()
    const storage: Storage = {
      get length() { return values.size },
      clear: () => values.clear(),
      key: (index: number) => [...values.keys()][index] ?? null,
      removeItem: (key: string) => { values.delete(key) },
      getItem: (key: string) => values.get(key) ?? null,
      setItem: (key: string, value: string) => { values.set(key, value) },
    }
    saveQueryCacheToDiskNow(seedCache, storage)
    const disk = storage.getItem(QUERY_CACHE_STORAGE_KEY)!
    expect(disk).not.toContain('api_key')
    expect(disk).not.toContain('synthetic********')

    let resolve!: (value: unknown) => void
    let reject!: (reason: Error) => void
    mocks.fetchProviders.mockImplementation(() => new Promise((yes, no) => { resolve = yes; reject = no }))
    const root = document.createElement('div')
    document.body.append(root)
    const pinia = createPinia()
    // Freshness is intentionally long: the mount must refresh, not wait for age.
    // eslint-disable-next-line vue/one-component-per-file -- New app restores the serialized cache.
    const app = createApp(ModelsPanel, { searchQuery: '' }).use(pinia).use(PiniaColada, { queryOptions: { staleTime: Infinity } })
    const cache = app.runWithContext(() => useQueryCache())
    hydrateQueryCache(cache, JSON.parse(disk))
    app.mount(root)
    try {
      await vi.waitFor(() => expect(mocks.fetchProviders).toHaveBeenCalledOnce())
      expect(root.textContent).toContain('Cached provider')
      expect(cache.getQueryData<typeof fresh[]>(['providers'])?.[0]?.config).not.toHaveProperty('api_key')
      if (outcome === 'failure') reject(new Error('Synthetic catalog unavailable'))
      else resolve({ data: [outcome === 'success' ? fresh : { ...fresh, config: {} }] })
      await vi.waitFor(() => expect(cache.getEntries({ key: ['providers'] })[0]?.asyncStatus.value).toBe('idle'))
      if (outcome === 'success') {
        expect(root.textContent).toContain('Fresh provider')
        expect(cache.getQueryData<typeof fresh[]>(['providers'])?.[0]?.config.api_key).toBe('synthetic********')
      } else {
        expect(cache.getQueryData<typeof fresh[]>(['providers'])?.[0]?.config).not.toHaveProperty('api_key')
      }
      saveQueryCacheToDiskNow(cache, storage)
      expect(storage.getItem(QUERY_CACHE_STORAGE_KEY) ?? '').not.toContain('api_key')
      expect(mocks.fetchProviders).toHaveBeenCalledOnce()
    } finally {
      app.unmount()
    }
  })

  it.each(['providers', 'templates'] as const)('keeps cached cards mounted while %s refresh', async (source) => {
    const { app, root } = await mountPage()
    const cards = [...root.querySelectorAll('[data-provider]')]
    expect(cards).toHaveLength(2)

    const loading = source === 'providers' ? mocks.providersLoading : mocks.templatesLoading
    loading.value = true
    await nextTick()

    expect([...root.querySelectorAll('[data-provider]')]).toEqual(cards)
    expect(root.querySelector('[data-slot="skeleton"]')).toBeNull()
    loading.value = false
    await nextTick()
    expect([...root.querySelectorAll('[data-provider]')]).toEqual(cards)
    app.unmount()
  })

  it.each(['providers', 'templates'] as const)('waits for unresolved %s before showing cards', async (source) => {
    const data = source === 'providers' ? mocks.providerData : mocks.templateData
    const loading = source === 'providers' ? mocks.providersLoading : mocks.templatesLoading
    data.value = undefined
    loading.value = true
    const { app, root } = await mountPage()

    expect(root.querySelector('[data-slot="skeleton"]')).not.toBeNull()
    expect(root.querySelector('[data-provider]')).toBeNull()
    data.value = [{ id: 'loaded', name: 'Loaded' }]
    loading.value = false
    await nextTick()
    expect(root.querySelector('[data-slot="skeleton"]')).toBeNull()
    expect(root.querySelector('[data-provider]')).not.toBeNull()
    app.unmount()
  })

  it('leaves the skeleton when an initial request fails without data', async () => {
    mocks.templateData.value = undefined
    mocks.templatesLoading.value = true
    const { app, root } = await mountPage()
    expect(root.querySelector('[data-slot="skeleton"]')).not.toBeNull()

    // A rejected query returns to idle and keeps its data undefined.
    mocks.templatesLoading.value = false
    await nextTick()
    expect(root.querySelector('[data-slot="skeleton"]')).toBeNull()
    expect(root.querySelectorAll('[data-provider]')).toHaveLength(2)
    app.unmount()
  })

  it('shows a detail skeleton while the URL provider is still loading', async () => {
    mocks.route.query = { provider: 'provider-two' }
    mocks.providerData.value = undefined
    mocks.providersLoading.value = true

    const { app, root } = await mountPage()

    expect(root.querySelector('[data-testid="detail-pane-skeleton"]')).not.toBeNull()
    expect(root.querySelector('[data-testid="provider-detail"]')).toBeNull()
    expect(mocks.replace).not.toHaveBeenCalled()
    app.unmount()
  })

  it('waits for a stale provider query to refresh before resolving the URL', async () => {
    mocks.route.query = { provider: 'provider-two' }
    mocks.providerData.value = [
      { id: 'provider-one', name: 'One', enable: true },
    ]
    mocks.providersLoading.value = true

    const { app, root } = await mountPage()

    expect(mocks.replace).not.toHaveBeenCalled()
    expect(root.querySelector('[data-testid="detail-pane-skeleton"]')).not.toBeNull()
    expect(root.querySelector('[data-testid="provider-detail"]')).toBeNull()

    mocks.providerData.value = [
      { id: 'provider-one', name: 'One', enable: true },
      { id: 'provider-two', name: 'Two', enable: true },
    ]
    mocks.providersLoading.value = false
    await nextTick()

    expect(root.querySelector('[data-testid="provider-detail"]')?.textContent).toBe('provider-two:Two')
    expect(root.querySelector('[data-testid="detail-pane-skeleton"]')).toBeNull()
    app.unmount()
  })

  it('writes the selected provider ID instead of a shared detail flag', async () => {
    const { app, root } = await mountPage()

    ;(root.querySelector('[data-provider="Two"]') as HTMLButtonElement).click()
    await nextTick()

    expect(mocks.replace).toHaveBeenCalledWith({ query: { provider: 'provider-two' } })
    expect(root.querySelector('[data-testid="provider-detail"]')?.textContent).toBe('provider-two:Two')
    app.unmount()
  })

  it('opens an unconfigured template as a draft detail without opening the add dialog', async () => {
    mocks.templateData.value = [{
      id: 'template-anthropic',
      name: 'Anthropic',
      driver: 'anthropic-messages',
      default_config: { base_url: 'https://api.anthropic.com' },
      configured: false,
    }]
    const { app, root } = await mountPage()

    ;(root.querySelector('[data-provider="Anthropic"]') as HTMLButtonElement).click()
    await nextTick()

    expect(mocks.replace).toHaveBeenCalledWith({ query: { provider: 'template:template-anthropic' } })
    expect(root.querySelector('[data-testid="provider-detail"]')?.textContent).toBe('undefined:Anthropic')
    expect(root.querySelector('[data-testid="add-provider-dialog"]')).toBeNull()
    app.unmount()
  })

  it('keeps the template route and detail open after the draft is materialized', async () => {
    mocks.templateData.value = [{
      id: 'template-anthropic',
      name: 'Anthropic',
      driver: 'anthropic-messages',
      default_config: { base_url: 'https://api.anthropic.com' },
      configured: false,
    }]
    const { app, root } = await mountPage()

    ;(root.querySelector('[data-provider="Anthropic"]') as HTMLButtonElement).click()
    await nextTick()
    ;(root.querySelector('[data-testid="materialize-provider"]') as HTMLButtonElement).click()
    await nextTick()

    expect(mocks.replace).toHaveBeenLastCalledWith({ query: { provider: 'template:template-anthropic' } })
    expect(root.querySelector('[data-testid="provider-detail"]')?.textContent).toContain('provider-anthropic:Anthropic')
    app.unmount()
  })

  it('returns to the list when the URL references a missing provider', async () => {
    mocks.route.query = { provider: 'missing', tab: 'kept' }

    const { app, root } = await mountPage()

    expect(mocks.replace).toHaveBeenCalledWith({ query: { tab: 'kept' } })
    expect(root.querySelector('[data-testid="provider-detail"]')).toBeNull()
    app.unmount()
  })
})
