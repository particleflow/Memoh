// @vitest-environment jsdom
import { createApp, h, nextTick, ref, type Ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import en from '@/i18n/locales/en.json'
import NativeTaskContent from './native-task-content.vue'
import type { NativeTaskView } from '../composables/useNativeTask'
let app: ReturnType<typeof createApp>, root: HTMLDivElement
beforeEach(() => {
 root = document.createElement('div'); document.body.append(root)
 vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
})
afterEach(() => { app?.unmount(); root.remove(); vi.unstubAllGlobals() })
function mount(view: Ref<NativeTaskView>) {
 app = createApp({ setup: () => () => h('dialog', { open: true }, h(NativeTaskContent, { view: view.value, operationId: 'op' })) })
 app.use(createI18n({ legacy: false, locale: 'en', messages: { en } })); app.mount(root)
}
function view() { return ref<NativeTaskView>({ opened: true, loading: false, sending: false, open: vi.fn(), refresh: vi.fn(), send: vi.fn(), data: { operation_id: 'op', supported: true, followup_available: true, observation: { output: '<script>unsafe</script> synthetic output', output_fidelity: 'terminal_snapshot_unverified', truncated: true, status: 'done', completion_seq: '9' } } }) }
describe('shared native Agent controls', () => {
 it('shows escaped bounded terminal text, never success, inside a native dialog', async () => {
  const v = view(); mount(v); await nextTick()
  expect(root.textContent).toContain('terminal_snapshot_unverified')
  expect(root.textContent).toContain('Truncated')
  expect(root.querySelector('pre')?.textContent).toContain('<script>unsafe</script>')
  expect(root.querySelector('script')).toBeNull()
  v.value.response = { operation_id: 'op', state: 'accepted' }; await nextTick()
  expect(root.textContent).toContain('Input accepted. Work completion and output ownership remain unverified.')
 })
 it('hides continuation for unsupported batch and keeps pending unknown disabled', async () => {
  const v = view(); v.value.data = { operation_id: 'op', supported: false, followup_available: false }; mount(v); await nextTick()
  expect(root.textContent).toContain(en.chat.tasks.native.batch); expect(root.querySelector('form')).toBeNull()
  v.value.data = view().value.data; v.value.requestId = 'retained-request'; await nextTick()
  expect(root.querySelector('textarea')?.disabled).toBe(true)
  expect(root.querySelector<HTMLButtonElement>('button[type=submit]')?.disabled).toBe(true)
  expect(root.textContent).toContain('retained-request')
 })
 it('submits through the supplied capability with explicit mode and no action on empty input', async () => {
  const v = view(); mount(v); await nextTick()
  root.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  await new Promise(resolve => setTimeout(resolve, 0)); expect(v.value.send).not.toHaveBeenCalled()
  const text = root.querySelector('textarea')!; text.value = 'synthetic next instruction'; text.dispatchEvent(new Event('input', { bubbles: true })); await nextTick()
  root.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  await new Promise(resolve => setTimeout(resolve, 0))
  await vi.waitFor(() => expect(v.value.send).toHaveBeenCalledWith('synthetic next instruction', 'bound_session'))
 })
})
