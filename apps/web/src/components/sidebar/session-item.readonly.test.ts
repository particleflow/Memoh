// @vitest-environment jsdom
import { createApp, h, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import SessionItem from './session-item.vue'
import en from '@/i18n/locales/en.json'

const session = Object.freeze({ id: 'old-history', bot_id: 'original-bot', title: 'Original historical record', channel_type: 'local', runtime_type: 'model' })
const selected = vi.fn(), opened = vi.fn(), renamed = vi.fn(), deleted = vi.fn()
let app: ReturnType<typeof createApp> | undefined
let root: HTMLDivElement

beforeEach(() => {
  vi.clearAllMocks()
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  root = document.createElement('div')
  document.body.append(root)
})
afterEach(() => { app?.unmount(); root.remove(); vi.unstubAllGlobals() })
async function mount(streaming = false) {
  app = createApp({ render: () => h(SessionItem, {
    session, isActive: false, streaming,
    onSelect: selected, onOpenNewTab: opened, onRename: renamed, onDelete: deleted,
  }) })
  app.use(createI18n({ legacy: false, locale: 'en', messages: { en } }))
  app.mount(root)
  await nextTick()
  return root.querySelector<HTMLElement>('[data-slot="sidebar-session-row"]')!
}

it('retains historical reading by click, Enter and Space without entering a deletion selection', async () => {
  const row = await mount()
  row.click()
  row.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
  row.dispatchEvent(new KeyboardEvent('keydown', { key: ' ', code: 'Space', bubbles: true }))
  expect(selected).toHaveBeenCalledTimes(3)
  expect(selected).toHaveBeenLastCalledWith(session)
  expect(row.getAttribute('role')).toBe('button')
  expect(root.querySelector('[role="checkbox"]')).toBeNull()
  expect(root.querySelector('button')).toBeNull()
  expect(renamed).not.toHaveBeenCalled()
  expect(deleted).not.toHaveBeenCalled()
})

it('offers only Open for historical context actions; no rename, delete or bulk selection', async () => {
  const row = await mount()
  row.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, clientX: 12, clientY: 12 }))
  await new Promise(resolve => setTimeout(resolve, 30))
  await nextTick()
  const actions = [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')]
  expect(actions.map(item => item.textContent?.trim())).toEqual(['Open'])
  actions[0]!.click()
  expect(opened).toHaveBeenCalledWith(session)
  expect(renamed).not.toHaveBeenCalled()
  expect(deleted).not.toHaveBeenCalled()
})

it('keeps the actual streaming indicator and historical title after removing the menu overlay', async () => {
  await mount(true)
  expect(root.textContent).toContain(session.title)
  expect(root.querySelector('[data-slot="sidebar-session-spinner"]')).not.toBeNull()
  expect(root.querySelector('button')).toBeNull()
})
