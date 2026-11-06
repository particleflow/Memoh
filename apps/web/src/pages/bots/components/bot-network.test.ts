// @vitest-environment jsdom
import { createApp, nextTick, type App, type FunctionalComponent, type Slots } from 'vue'
import { createPinia } from 'pinia'
import { PiniaColada } from '@pinia/colada'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { NetworkBotStatus } from '@/pages/network/api'
import NetworkPage from './bot-network.vue'

const mocks = vi.hoisted(() => ({
  status: vi.fn<() => Promise<NetworkBotStatus>>(),
  settings: vi.fn(),
  save: vi.fn(),
}))
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key === 'bots.settings.networkState.connected' ? 'Connected' : key }),
}))
vi.mock('@memohai/sdk', () => ({
  getBotsByBotIdSettings: mocks.settings,
  putBotsByBotIdSettings: mocks.save,
}))
vi.mock('@/pages/network/api', () => ({
  getBotNetworkStatus: mocks.status,
  listOverlayProviderMeta: async () => [],
  listBotNetworkNodes: async () => ({ items: [] }),
  executeBotNetworkAction: vi.fn(),
}))
vi.mock('@/components/monaco-editor/index.vue', () => ({ default: () => null }))
vi.mock('./network-provider-select.vue', () => ({ default: () => null }))
vi.mock('./network-node-select.vue', () => ({ default: () => null }))
vi.mock('@felinic/ui', async () => {
  const { h } = await import('vue')
  const pass = (_: unknown, { slots }: { slots: Slots }) => h('div', [slots.default?.(), slots.content?.()])
  const Switch: FunctionalComponent<{ modelValue: boolean }, { 'update:modelValue': [value: boolean] }> =
    (props, { emit }) => h('button', { 'data-toggle': '', onClick: () => emit('update:modelValue', !props.modelValue) })
  Switch.props = ['modelValue']
  Switch.emits = ['update:modelValue']
  return {
    ...Object.fromEntries([
      'ActionCard', 'InputGroup', 'InputGroupAddon', 'InputGroupButton', 'InputGroupInput',
      'Label', 'NumberField', 'Input', 'Select', 'SelectContent', 'SelectItem', 'SelectTrigger',
      'SelectValue', 'Textarea', 'DialogBody', 'DialogContent', 'DialogHeader', 'DialogPanel',
      'DialogTitle', 'DialogDescription', 'DialogFooter', 'DialogClose', 'ConfirmPopover',
      'InlineLoadingRow', 'SectionGroup', 'SettingsRow', 'SettingsSection',
    ].map(name => [name, pass])),
    Dialog: () => null,
    Button: (_: unknown, { attrs, slots }: { attrs: Record<string, unknown>; slots: Slots }) =>
      h('button', attrs, slots.default?.()),
    Switch,
    toast: { success: vi.fn(), error: vi.fn() },
  }
})

let app: App | undefined
let host: HTMLDivElement
async function flush() {
  for (let i = 0; i < 4; i += 1) {
    await vi.advanceTimersByTimeAsync(0)
    await nextTick()
  }
}
async function mount() {
  host = document.createElement('div')
  document.body.append(host)
  app = createApp(NetworkPage, { botId: 'synthetic-bot' })
  app.config.globalProperties.$t = (key: string) => key
  app.use(createPinia()).use(PiniaColada)
  app.mount(host)
  await flush()
}
beforeEach(() => {
  vi.useFakeTimers()
  vi.clearAllMocks()
  mocks.settings.mockResolvedValue({ data: {
    overlay_enabled: true, overlay_provider: 'synthetic', overlay_config: { hostname: 'fixture' },
    compaction_target_percent: null,
  } })
  mocks.save.mockResolvedValue({ data: {} })
})
afterEach(() => {
  app?.unmount()
  host?.remove()
  vi.useRealTimers()
})

describe('network settings query contract', () => {
  it('polls transient status, skips an in-flight request, and stops when connected', async () => {
    const refresh = Promise.withResolvers<NetworkBotStatus>()
    mocks.status.mockResolvedValueOnce({ state: 'starting' }).mockReturnValueOnce(refresh.promise)
    await mount()
    expect(mocks.status).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(5000)
    expect(mocks.status).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(10000)
    expect(mocks.status).toHaveBeenCalledTimes(2)
    refresh.resolve({ state: 'ready' })
    await flush()
    expect(host.textContent).toContain('Connected')
    await vi.advanceTimersByTimeAsync(10000)
    expect(mocks.status).toHaveBeenCalledTimes(2)
  })

  it('clears the status timer when the page unmounts', async () => {
    mocks.status.mockResolvedValue({ state: 'starting' })
    await mount()
    app?.unmount()
    app = undefined
    await vi.advanceTimersByTimeAsync(10000)
    expect(mocks.status).toHaveBeenCalledTimes(1)
  })

  it('saves only network request fields without copying nullable read-only settings', async () => {
    mocks.status.mockResolvedValue({ state: 'ready' })
    await mount()
    host.querySelector<HTMLButtonElement>('[data-toggle]')?.click()
    await flush()
    const save = [...host.querySelectorAll('button')].find(button => button.textContent?.includes('common.save'))
    expect(save).toBeDefined()
    save?.click()
    await flush()
    expect(mocks.save).toHaveBeenCalledWith({
      path: { bot_id: 'synthetic-bot' },
      body: { overlay_enabled: false, overlay_provider: 'synthetic', overlay_config: { hostname: 'fixture' } },
      throwOnError: true,
    })
  })
})
