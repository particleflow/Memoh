// @vitest-environment jsdom
/* eslint-disable vue/one-component-per-file */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, nextTick, type App, type Ref, type Slots } from 'vue'
import StoragePage from './index.vue'

const api = vi.hoisted(() => ({ list: vi.fn(), read: vi.fn(), create: vi.fn(), save: vi.fn() }))
const session = vi.hoisted(() => ({ token: undefined as Ref<string> | undefined }))
vi.mock('@vueuse/core', async () => {
  const { ref } = await import('vue')
  return { useLocalStorage: () => session.token ??= ref('synthetic-account') }
})
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@memohai/sdk', () => ({
  getUsersMeRuntimes: api.list, getUsersMeRuntimesByIdWorkspaceStorage: api.read,
  postUsersMeRuntimesByIdWorkspaceStorageDirectories: api.create,
  putUsersMeRuntimesByIdWorkspaceStorage: api.save,
}))
vi.mock('@/components/file-manager/directory-picker.vue', () => ({
  default: defineComponent({
    props: { directoryLoader: { type: Function, default: undefined }, selectedPath: { type: String, default: '' } }, emits: ['select'],
    setup: (props, { emit }) => () => h('button', { onClick: async () => {
      await props.directoryLoader?.('.')
      emit('select', 'existing-project')
    } }, 'Existing directory'),
  }),
}))
vi.mock('@felinic/ui', () => {
  const Wrapper = (props: { label?: string }, { slots }: { slots: Slots }) => h('div', [props.label, slots.default?.()])
  return {
    PageShell: Wrapper, SettingsSection: Wrapper, SettingsRow: Wrapper, FormStack: Wrapper, FieldStack: Wrapper,
    Button: defineComponent({ setup: (_, { slots, attrs }) => () => h('button', attrs, slots.default?.()) }),
    Input: defineComponent({ props: { modelValue: { type: String, default: '' } }, emits: ['update:modelValue'], setup: (props, { emit }) => () => h('input', { value: props.modelValue, onInput: (e: Event) => emit('update:modelValue', (e.target as HTMLInputElement).value) }) }),
    FormDialogShell: defineComponent({
      props: { open: Boolean, submitDisabled: Boolean }, emits: ['submit', 'update:open'],
      setup: (props, { slots, emit }) => () => props.open ? h('div', [slots.body?.(), h('button', { disabled: props.submitDisabled, onClick: () => emit('submit') }, 'Save directory'), h('button', { onClick: () => emit('update:open', false) }, 'Cancel')]) : null,
    }),
    toast: { success: vi.fn() },
  }
})

let app: App | undefined, root: HTMLDivElement
async function flush() { for (let i = 0; i < 5; i++) { await Promise.resolve(); await nextTick() } }
function button(text: string) { return [...root.querySelectorAll('button')].find(b => b.textContent === text)! }
async function mount() { root = document.createElement('div'); document.body.append(root); app = createApp(StoragePage); app.mount(root); await flush() }
beforeEach(() => {
  vi.resetAllMocks(); session.token = undefined
  api.list.mockResolvedValue({ data: [{ id: 'computer', name: 'Synthetic PC', provider: 'memoh', online: true }] })
  api.read.mockResolvedValue({ data: { path: '.', selected_path: '.', directories: [] } })
  api.create.mockResolvedValue({}); api.save.mockResolvedValue({})
})
afterEach(() => { app?.unmount(); document.body.innerHTML = '' })

describe('owned NAS workspace selection', () => {
  it('browses the owned computer API, selects an existing directory and writes only on Save', async () => {
    await mount(); button('workspaceStorage.choose').click(); await flush()
    expect(api.save).not.toHaveBeenCalled(); expect(api.create).not.toHaveBeenCalled()
    button('Existing directory').click(); await flush()
    button('Save directory').click(); await flush()
    expect(api.save).toHaveBeenCalledOnce()
    expect(api.save).toHaveBeenCalledWith(expect.objectContaining({ path: { id: 'computer' }, body: { path: 'existing-project' } }))
    expect(api.read.mock.calls.every(([options]) => !JSON.stringify(options.path).includes('bot'))).toBe(true)
  })
  it('creates one child under the selection without changing its saved binding', async () => {
    await mount(); button('workspaceStorage.choose').click(); await flush()
    const input = root.querySelector('input')!; input.value = 'new-project'; input.dispatchEvent(new Event('input')); await flush()
    button('workspaceStorage.create').click(); await flush()
    expect(api.create).toHaveBeenCalledOnce()
    expect(api.create).toHaveBeenCalledWith(expect.objectContaining({ body: { path: 'new-project' } }))
    expect(api.save).not.toHaveBeenCalled()
    button('Cancel').click(); await flush(); expect(api.save).not.toHaveBeenCalled()
  })
  it('disables NAS selection for an offline computer and never requests its directories', async () => {
    api.list.mockResolvedValue({ data: [{ id: 'computer', name: 'PC', provider: 'memoh', online: false }] })
    await mount(); expect(button('workspaceStorage.choose').disabled).toBe(true); expect(api.read).not.toHaveBeenCalled()
  })
  it('disables NAS creation and Save when the existing share disappears', async () => {
    await mount(); button('workspaceStorage.choose').click(); await flush()
    const input = root.querySelector('input')!; input.value = 'new-project'; input.dispatchEvent(new Event('input')); await flush()
    api.create.mockRejectedValueOnce({ code: 'workspace.storage_unavailable', status: 503 })
    button('workspaceStorage.create').click(); await flush()
    expect(button('Save directory').disabled).toBe(true)
    expect(api.save).not.toHaveBeenCalled()
  })
  it('keeps Save disabled on share failure and drops a late response after Cancel', async () => {
    api.read.mockRejectedValueOnce({ status: 503 })
    await mount(); button('workspaceStorage.choose').click(); await flush()
    expect(button('Save directory').disabled).toBe(true); expect(api.save).not.toHaveBeenCalled()
    button('Cancel').click(); await flush()
    let finish!: (value: unknown) => void
    api.read.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    button('workspaceStorage.choose').click(); await flush(); button('Cancel').click(); await flush()
    finish({ data: { path: '.', selected_path: 'late', directories: [] } }); await flush()
    expect(root.textContent).not.toContain('Existing directory'); expect(api.save).not.toHaveBeenCalled()
  })
})
