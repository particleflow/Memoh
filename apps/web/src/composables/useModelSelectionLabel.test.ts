// @vitest-environment jsdom
import { createApp, defineComponent, h, nextTick, ref } from 'vue'
import { createPinia } from 'pinia'
import { PiniaColada } from '@pinia/colada'
import { createI18n } from 'vue-i18n'
import { afterEach, expect, it, vi } from 'vitest'
import { useModelSelectionLabel } from './useModelSelectionLabel'
import en from '@/i18n/locales/en.json'
import type { ModelsGetResponse } from '@memohai/sdk'

const lookup = vi.hoisted(() => vi.fn())
vi.mock('@memohai/sdk', () => ({ getModelsById: lookup }))
const id = '00000000-0000-0000-0000-000000000101'
const model: ModelsGetResponse = { id, model_id: 'fixture/alpha', name: 'Fixture Alpha', type: 'chat' }
let app: ReturnType<typeof createApp> | undefined
let root: HTMLDivElement | undefined

afterEach(() => { app?.unmount(); root?.remove(); lookup.mockReset() })

function mount(initial: ModelsGetResponse[]) {
  const selected = ref(id)
  const models = ref(initial)
  root = document.createElement('div')
  document.body.append(root)
  app = createApp(defineComponent({
    setup() {
      const { label, isUnavailable } = useModelSelectionLabel(selected, models)
      return () => h('span', { 'data-unavailable': String(isUnavailable.value) }, label.value)
    },
  }))
  app.use(createPinia()).use(PiniaColada).use(createI18n({ legacy: false, locale: 'en', messages: { en } }))
  app.mount(root)
  return { selected, models }
}

it('retains the selected name and marks it unavailable without changing selection or restoring a menu row', async () => {
  lookup.mockRejectedValue(new Error('offline'))
  const state = mount([model])
  expect(root?.textContent).toBe('Fixture Alpha')
  state.models.value = []
  await nextTick()
  expect(root?.textContent).toBe('Fixture Alpha (unavailable)')
  expect(root?.querySelector('span')?.dataset.unavailable).toBe('true')
  expect(state.selected.value).toBe(id)
  expect(state.models.value).toEqual([])
  state.models.value = [model]
  await nextTick()
  expect(root?.textContent).toBe('Fixture Alpha')
  expect(root?.querySelector('span')?.dataset.unavailable).toBe('false')
})

it('restores a missing model name after reload without making it selectable', async () => {
  lookup.mockResolvedValue({ data: model })
  const state = mount([])
  await vi.waitFor(() => expect(root?.textContent).toBe('Fixture Alpha (unavailable)'))
  expect(lookup).toHaveBeenCalledWith(expect.objectContaining({ path: { id } }))
  expect(state.selected.value).toBe(id)
  expect(state.models.value).toEqual([])
})

it('does not render an unknown internal UUID when the model row no longer exists', async () => {
  lookup.mockRejectedValue(new Error('not found'))
  mount([])
  await vi.waitFor(() => expect(root?.textContent).toBe('Unavailable model'))
  expect(root?.textContent).not.toContain(id)
})
