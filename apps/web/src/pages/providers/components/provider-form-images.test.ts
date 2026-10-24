// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, nextTick, ref } from 'vue'
import type { App, Slots } from 'vue'
import type { ProvidersGetResponse } from '@memohai/sdk'

const feedback = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, te: () => false }) }))
vi.mock('@memohai/sdk', () => ({
  deleteProvidersByIdOauthToken: vi.fn(),
  getProvidersByIdOauthAuthorize: vi.fn(),
  getProvidersByIdOauthStatus: vi.fn(),
  postProvidersByIdOauthPoll: vi.fn(),
  postProvidersByIdTest: vi.fn(),
}))
vi.mock('@/composables/useProviderModelCatalog', () => ({
  useProviderModelCatalog: () => ({ syncProviderModelCatalog: vi.fn() }),
}))
vi.mock('@felinic/ui', async () => {
  const { Field } = await import('vee-validate')
  const Pass = (_props: unknown, { slots }: { slots: Slots }) => h('div', Object.values(slots).map(slot => slot?.()))
  const FieldWrapper = defineComponent({
    props: { name: { type: String, required: true } },
    setup: (props, { slots }) => () => h('div', { 'data-field': props.name }, [h(Field, { name: props.name }, slots)]),
  })
  const Input = defineComponent({
    props: { modelValue: String },
    emits: ['update:modelValue'],
    setup: (props, { attrs, emit }) => () => h('input', {
      ...attrs, value: props.modelValue,
      onInput: (event: Event) => emit('update:modelValue', (event.target as HTMLInputElement).value),
    }),
  })
  const Select = defineComponent({
    props: { modelValue: String, disabled: Boolean },
    emits: ['update:modelValue'],
    setup: (props, { slots, emit }) => () => h('select', {
      value: props.modelValue, disabled: props.disabled,
      onChange: (event: Event) => emit('update:modelValue', (event.target as HTMLSelectElement).value),
    }, slots.default?.()),
  })
  const Switch = defineComponent({
    props: { modelValue: Boolean, disabled: Boolean },
    emits: ['update:modelValue'],
    setup: (props, { attrs, emit }) => () => h('input', {
      ...attrs, type: 'checkbox', checked: props.modelValue, disabled: props.disabled,
      onChange: (event: Event) => emit('update:modelValue', (event.target as HTMLInputElement).checked),
    }),
  })
  const SelectItem = defineComponent({
    props: { value: String },
    setup: (props, { slots }) => () => h('option', { value: props.value }, slots.default?.()),
  })
  return {
    AutoHeight: Pass, Button: Pass, ConfirmPopover: Pass, DeviceCodePanel: Pass,
    SettingsRow: Pass, SettingsSection: Pass, FormControl: Pass, FormField: FieldWrapper,
    FormItem: Pass, FormMessage: Pass, HoverCard: Pass, HoverCardContent: Pass,
    HoverCardTrigger: Pass, Input, LabelSwap: Pass, Select,
    SelectContent: (_props: unknown, { slots }: { slots: Slots }) => slots.default?.(),
    Switch, SelectItem, SelectTrigger: () => null, SelectValue: () => null, Spinner: Pass,
    toast: feedback,
  }
})
vi.mock('@/components/loading-button/index.vue', () => ({ default: { template: '<button><slot /></button>' } }))
vi.mock('@/components/check-draw-icon/index.vue', () => ({ default: { template: '<span />' } }))

const mounted: App[] = []
afterEach(() => {
  mounted.splice(0).forEach(app => app.unmount())
  document.body.innerHTML = ''
  vi.clearAllMocks()
})

async function flush() {
  await nextTick()
  await Promise.resolve()
  await nextTick()
}

async function mountForm(config: Record<string, unknown> = {}, clientType = 'openai-completions') {
  const ProviderForm = (await import('./provider-form.vue')).default
  const provider = ref<ProvidersGetResponse>({
    id: 'synthetic-provider', name: 'Synthetic provider', client_type: clientType, enable: true,
    config: { base_url: 'https://provider.example.invalid/v1', api_key: 'synthetic********', ...config },
  })
  const saving = ref(false)
  const save = vi.fn().mockResolvedValue(undefined)
  const root = document.createElement('div')
  document.body.append(root)
  const app = createApp(defineComponent({
    setup: () => () => h(ProviderForm, { provider: provider.value, editLoading: saving.value, saveProvider: save, ensureProvider: vi.fn() }),
  }))
  app.config.globalProperties.$t = (key: string) => key
  app.mount(root)
  mounted.push(app)
  await flush()
  const control = () => root.querySelector<HTMLSelectElement>('[data-field="images_protocol"] select')
  const choose = async (value: string) => {
    const select = control()!
    select.value = value
    select.dispatchEvent(new Event('change', { bubbles: true }))
    await flush()
  }
  const rename = async () => {
    const input = root.querySelector<HTMLInputElement>('[data-field="name"] input')!
    input.value = 'Renamed synthetic provider'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await flush()
  }
  const submit = async () => {
    root.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    await vi.waitFor(() => expect(save).toHaveBeenCalledOnce())
    await flush()
    return save.mock.calls[0]![0] as { config: Record<string, unknown>, client_type: string }
  }
  const references = () => root.querySelector<HTMLInputElement>('[data-field="images_edits"] input')
  const setReferences = async (enabled: boolean) => {
    const input = references()!
    input.checked = enabled
    input.dispatchEvent(new Event('change', { bubbles: true }))
    await flush()
  }
  return { root, provider, saving, save, control, choose, rename, submit, references, setReferences }
}

describe('provider Images protocol configuration', () => {
  it.each(['openai-completions', 'openai-responses'])('keeps the legacy behavior and blank password for %s until selected', async (clientType) => {
    const form = await mountForm({}, clientType)
    expect(form.control()?.value).toBe('preserve')
    expect(form.root.querySelector<HTMLInputElement>('[type="password"]')?.value).toBe('')
    await form.rename()
    const payload = await form.submit()
    expect(payload.config).not.toHaveProperty('images_api')
    expect(payload.config).not.toHaveProperty('api_key')
    expect(payload.client_type).toBe(clientType)
  })

  it('saves only the explicit protocol and displays the persisted selection after reopening', async () => {
    const form = await mountForm()
    await form.choose('openai-images')
    expect(form.save).not.toHaveBeenCalled()
    const payload = await form.submit()
    expect(payload.config.images_api).toEqual({ protocol: 'openai-images' })
    expect(payload.config).not.toHaveProperty('api_key')
    expect(payload.client_type).toBe('openai-completions')
    expect(feedback.success).toHaveBeenCalledOnce()
    expect(form.control()?.querySelector('option[value="preserve"]')).toBeNull()
    const reopened = await mountForm({ images_api: payload.config.images_api })
    expect(reopened.control()?.value).toBe('openai-images')
  })

  it('preserves every stored image policy when explicitly changing the protocol', async () => {
    const images = { protocol: 'previous-protocol', edits: true, model_mode: 'omit', edit_encoding: 'multipart', extension: { retained: true } }
    const form = await mountForm({ images_api: images })
    expect(form.control()?.value).toBe('preserve')
    await form.choose('openai-images')
    const payload = await form.submit()
    expect(payload.config.images_api).toEqual({ ...images, protocol: 'openai-images' })
    expect(form.provider.value.config?.images_api).toEqual(images)
  })

  it('does not rewrite configured image policies when saving an unrelated field', async () => {
    const form = await mountForm({ images_api: { protocol: 'openai-images', edits: true, model_mode: 'configured', edit_encoding: 'json-images' } })
    expect(form.control()?.value).toBe('openai-images')
    await form.rename()
    expect((await form.submit()).config).not.toHaveProperty('images_api')
  })

  it.each(['anthropic-messages', 'google-generative-ai'])('does not offer or submit Images configuration for %s', async (clientType) => {
    const form = await mountForm({}, clientType)
    expect(form.control()).toBeNull()
    await form.rename()
    expect((await form.submit()).config).not.toHaveProperty('images_api')
  })

  it('observes clean refetches but preserves a local selection until saved', async () => {
    const form = await mountForm()
    await form.choose('openai-images')
    form.provider.value = { ...form.provider.value, name: 'Refetched name' }
    await flush()
    expect(form.control()?.value).toBe('openai-images')
    form.provider.value = { ...form.provider.value, id: 'another-synthetic-provider' }
    await flush()
    expect(form.control()?.value).toBe('preserve')
    form.provider.value = { ...form.provider.value, config: { ...form.provider.value.config, images_api: { protocol: 'openai-images' } } }
    await flush()
    expect(form.control()?.value).toBe('openai-images')
  })

  it('retains the unsaved selection on an authorization failure without retrying', async () => {
    const form = await mountForm()
    form.save.mockRejectedValue({ status: 401 })
    await form.choose('openai-images')
    await form.submit()
    await vi.waitFor(() => expect(feedback.error).toHaveBeenCalledOnce())
    expect(feedback.success).not.toHaveBeenCalled()
    expect(form.control()?.value).toBe('openai-images')
    expect(form.provider.value.config).not.toHaveProperty('images_api')
    expect(form.save).toHaveBeenCalledOnce()
  })

  it('disables the protocol control during the existing save operation', async () => {
    const form = await mountForm()
    form.saving.value = true
    await flush()
    expect(form.control()?.disabled).toBe(true)
  })
})


describe('provider reference-image capability configuration', () => {
  it.each(['openai-completions', 'openai-responses'])('defaults off and requires explicit Images selection (%s)', async (clientType) => {
    const form = await mountForm({}, clientType)
    expect(form.references()?.checked).toBe(false)
    expect(form.references()?.disabled).toBe(true)
    await form.choose('openai-images')
    expect(form.references()?.disabled).toBe(false)
    expect(form.references()?.checked).toBe(false)
    expect((await form.submit()).config.images_api).toEqual({ protocol: 'openai-images' })
  })

  it('persists only the existing boolean and retains image policies and blank password', async () => {
    const images = { protocol: 'openai-images', model_mode: 'configured', edit_encoding: 'json-images' }
    const form = await mountForm({ images_api: images })
    await form.setReferences(true)
    const payload = await form.submit()
    expect(payload.config.images_api).toEqual({ ...images, edits: true })
    expect(payload.config.base_url).toBe('https://provider.example.invalid/v1')
    expect(payload.config).not.toHaveProperty('api_key')
    expect(payload.client_type).toBe('openai-completions')
    expect(form.provider.value.config?.images_api).toEqual(images)
    const reopened = await mountForm({ images_api: payload.config.images_api })
    expect(reopened.references()?.checked).toBe(true)
    expect(reopened.root.querySelector<HTMLInputElement>('[type="password"]')?.value).toBe('')
  })

  it('can explicitly disable references without deleting the protocol or legacy fields', async () => {
    const images = { protocol: 'openai-images', edits: true, model_mode: 'configured', edit_encoding: 'json-images' }
    const form = await mountForm({ images_api: images })
    await form.setReferences(false)
    expect((await form.submit()).config.images_api).toEqual({ ...images, edits: false })
  })

  it.each(['anthropic-messages', 'google-generative-ai'])('hides the Images capability for unsupported client type %s', async (clientType) => {
    const form = await mountForm({}, clientType)
    expect(form.references()).toBeNull()
  })

  it('observes a refetch without overwriting a dirty reference selection', async () => {
    const form = await mountForm({ images_api: { protocol: 'openai-images' } })
    await form.setReferences(true)
    form.provider.value = { ...form.provider.value, name: 'Refetched name' }
    await flush()
    expect(form.references()?.checked).toBe(true)
    form.provider.value = { ...form.provider.value, id: 'second-synthetic-provider' }
    await flush()
    expect(form.references()?.checked).toBe(false)
    form.provider.value = { ...form.provider.value, config: { ...form.provider.value.config, images_api: { protocol: 'openai-images', edits: true } } }
    await flush()
    expect(form.references()?.checked).toBe(true)
  })

  it('preserves the pending switch on a rejected save without retrying', async () => {
    const form = await mountForm({ images_api: { protocol: 'openai-images' } })
    form.save.mockRejectedValue({ status: 401 })
    await form.setReferences(true)
    await form.submit()
    await vi.waitFor(() => expect(feedback.error).toHaveBeenCalledOnce())
    expect(feedback.success).not.toHaveBeenCalled()
    expect(form.references()?.checked).toBe(true)
    expect(form.provider.value.config?.images_api).toEqual({ protocol: 'openai-images' })
  })

  it('disables references while saving', async () => {
    const form = await mountForm({ images_api: { protocol: 'openai-images' } })
    form.saving.value = true
    await flush()
    expect(form.references()?.disabled).toBe(true)
  })

  it.each([
    { model_mode: 'omit' },
    { model_mode: 'configured' },
    { edit_encoding: 'multipart' },
  ])('does not silently migrate incompatible legacy image policy %j', async (legacy) => {
    const images = { protocol: 'openai-images', ...legacy }
    const form = await mountForm({ images_api: images })
    await form.setReferences(true)
    form.root.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    await vi.waitFor(() => expect(form.references()?.getAttribute('aria-invalid')).toBe('true'))
    expect(form.save).not.toHaveBeenCalled()
    expect(form.provider.value.config?.images_api).toEqual(images)
  })
})
