// @vitest-environment jsdom
import { createApp, h, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it, vi } from 'vitest'
import en from '@/i18n/locales/en.json'
import Trigger from './session-activity-trigger.vue'

const tabs = vi.hoisted(() => ({ selectSidebarView: vi.fn(), openMobileNav: vi.fn() }))
vi.mock('@/store/workspace-tabs', () => ({ useWorkspaceTabsStore: () => tabs }))
let app: ReturnType<typeof createApp> | undefined
afterEach(() => { app?.unmount(); document.body.innerHTML = ''; vi.restoreAllMocks(); vi.clearAllMocks() })

describe('task entry opens the one minimal activity list', () => {
  it('selects the existing sessions sidebar, without a task detail modal or business call', async () => {
    const fetch = vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('No request belongs to this trigger'))
    const root = document.createElement('div'); document.body.append(root)
    app = createApp({ render: () => h(Trigger) })
    app.use(createI18n({ legacy: false, locale: 'en', messages: { en } })); app.mount(root)
    root.querySelector('button')!.click(); await nextTick()
    expect(tabs.selectSidebarView).toHaveBeenCalledExactlyOnceWith('sessions')
    expect(tabs.openMobileNav).toHaveBeenCalledOnce()
    expect(document.querySelector('[role=dialog], [role=tablist], pre, code')).toBeNull()
    expect(fetch).not.toHaveBeenCalled()
  })
})
