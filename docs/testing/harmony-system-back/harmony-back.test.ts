// @vitest-environment jsdom
// Run in the generated Harmony Web context; imports the actual host adapter.
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { createPinia, disposePinia, setActivePinia } from 'pinia'
import { useChatSelectionStore } from '@/store/chat-selection'
import { handleHarmonyBack } from '@/lib/harmony-back'

function router(path = '/', name = 'home') {
  return { currentRoute: { value: { path, name } }, replace: vi.fn() } as unknown as Router
}
beforeEach(() => {
  document.body.innerHTML = ''
  vi.restoreAllMocks()
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(function (this: HTMLElement) {
    return this.hidden ? [] as unknown as DOMRectList : [new DOMRect(0, 0, 20, 20)] as unknown as DOMRectList
  })
})
describe('system back to existing Web owners', () => {
  it.each(['home', 'bot', 'Login', 'onboarding'])('keeps %s root available to system', name => {
    expect(handleHarmonyBack(router(name === 'onboarding' ? '/onboarding' : '/', name))).toBe(false)
  })
  it('consumes an explicit task link and returns only to its routed Bot contact', () => {
    const r = router('/bot/original-bot', 'bot')
    Object.assign(r.currentRoute.value, { params: { botName: 'original-bot' }, query: { session: 'original-task', model: 'old-task-model' } })
    localStorage.setItem('token', 'synthetic-retained-login')
    expect(handleHarmonyBack(r)).toBe(true)
    expect(r.replace).toHaveBeenCalledExactlyOnceWith({ name: 'bot', params: { botName: 'original-bot' } })
    expect(localStorage.getItem('token')).toBe('synthetic-retained-login')
  })
  it('uses one route write even when the previously selected Bot differs; root back stays unhandled', async () => {
    const r = createRouter({ history: createMemoryHistory(), routes: [{ name: 'bot', path: '/bot/:botName', component: {} }] })
    await r.push({ name: 'bot', params: { botName: 'original-bot' }, query: { session: 'original-task' } })
    const replace = vi.spyOn(r, 'replace'), back = vi.spyOn(r, 'back')
    // A stale selection must never be a second navigation source. The host
    // adapter receives only the existing route, not selection-store state.
    const pinia = createPinia(); setActivePinia(pinia)
    const selection = useChatSelectionStore()
    selection.setBot('previous-other-bot'); selection.setSession('previous-other-task', { explicitSelection: true })
    const setBot = vi.spyOn(selection, 'setBot'), setSession = vi.spyOn(selection, 'setSession')
    expect(handleHarmonyBack(r)).toBe(true)
    await vi.waitFor(() => expect(r.currentRoute.value.query).toEqual({}))
    expect(r.currentRoute.value.params.botName).toBe('original-bot')
    expect(replace).toHaveBeenCalledExactlyOnceWith({ name: 'bot', params: { botName: 'original-bot' } })
    expect(back).not.toHaveBeenCalled()
    expect(selection.currentBotId).toBe('previous-other-bot')
    expect(selection.sessionId).toBe('previous-other-task')
    expect(setBot).not.toHaveBeenCalled(); expect(setSession).not.toHaveBeenCalled()
    expect(handleHarmonyBack(r)).toBe(false)
    expect(replace).toHaveBeenCalledTimes(1)
    disposePinia(pinia)
  })
  it('dismisses an existing task-link dialog before changing the route', () => {
    const r = router('/bot/original-bot', 'bot')
    Object.assign(r.currentRoute.value, { params: { botName: 'original-bot' }, query: { session: 'original-task' } })
    document.body.innerHTML = '<div role="dialog" data-state="open"></div>'
    const escape = vi.fn(); window.addEventListener('keydown', escape)
    expect(handleHarmonyBack(r)).toBe(true)
    expect(escape).toHaveBeenCalledOnce(); expect(r.replace).not.toHaveBeenCalled()
    window.removeEventListener('keydown', escape)
  })
  it('leaves initial setup available to system and calls existing edit cancel', () => {
    expect(handleHarmonyBack()).toBe(false)
    document.body.innerHTML = '<button id="service-cancel">Return</button>'
    const click = vi.fn(); document.querySelector('button')!.onclick = click
    expect(handleHarmonyBack()).toBe(true); expect(click).toHaveBeenCalledOnce()
  })
  it('dismisses Reka layers before navigation; ignores closing layers', () => {
    const r = router('/settings/providers', 'providers'), escape = vi.fn()
    window.addEventListener('keydown', escape)
    document.body.innerHTML = '<div role="dialog" data-state="open"></div>'
    expect(handleHarmonyBack(r)).toBe(true); expect(escape).toHaveBeenCalledOnce()
    expect(r.replace).not.toHaveBeenCalled()
    document.querySelector('div')!.dataset.state = 'closed'
    expect(handleHarmonyBack(r)).toBe(true); expect(r.replace).toHaveBeenCalledWith('/settings')
    window.removeEventListener('keydown', escape)
  })
  it('closes custom media preview only, without Escape reaching lower modal', () => {
    document.body.innerHTML = '<div role="dialog"></div><div role="dialog" data-keyboard-scope="mediaLightbox"><button aria-label="Close"></button></div>'
    const click = vi.fn(), escape = vi.fn(); document.querySelector('button')!.onclick = click
    window.addEventListener('keydown', escape)
    expect(handleHarmonyBack(router())).toBe(true)
    expect(click).toHaveBeenCalledOnce(); expect(escape).not.toHaveBeenCalled()
    window.removeEventListener('keydown', escape)
  })
  it('returns a task-detail dialog to its list before closing the modal', () => {
    document.body.innerHTML = '<div role="dialog"><div data-slot="dialog-view-header"><button><svg class="lucide-chevron-left"></svg></button></div></div>'
    const click = vi.fn(), escape = vi.fn();document.querySelector('button')!.onclick = click
    window.addEventListener('keydown', escape)
    expect(handleHarmonyBack(router())).toBe(true)
    expect(click).toHaveBeenCalledOnce();expect(escape).not.toHaveBeenCalled()
    window.removeEventListener('keydown', escape)
  })
  it('uses settings back instead of a still-mounted background chat panel', () => {
    document.body.innerHTML = '<header><button id="background"><svg class="lucide-chevron-left"></svg></button></header><div data-settings-content-shell><button id="detail" data-settings-detail-back></button></div>'
    const detail = vi.fn(), background = vi.fn()
    document.getElementById('detail')!.onclick = detail;document.getElementById('background')!.onclick = background
    expect(handleHarmonyBack(router('/settings/providers', 'providers'))).toBe(true)
    expect(detail).toHaveBeenCalledOnce(); expect(background).not.toHaveBeenCalled()
  })
  it('does not treat arbitrary message chevrons as navigation', () => {
    document.body.innerHTML = '<article><button><svg class="lucide-chevron-left"></svg></button></article>'
    const click = vi.fn();document.querySelector('button')!.onclick = click
    expect(handleHarmonyBack(router())).toBe(false);expect(click).not.toHaveBeenCalled()
  })
  it('uses existing secondary-panel back without destroying its state', () => {
    document.body.innerHTML = '<header><button><svg class="lucide-chevron-left"></svg></button></header>'
    const click = vi.fn();document.querySelector('button')!.onclick = click
    expect(handleHarmonyBack(router())).toBe(true);expect(click).toHaveBeenCalledOnce()
  })
  it('returns cold settings and unknown subpage to app, never browser history', () => {
    for (const path of ['/settings', '/oauth/callback']) {
      const r = router(path, 'detail');expect(handleHarmonyBack(r)).toBe(true)
      expect(r.replace).toHaveBeenCalledWith({ name: 'home' })
    }
  })
})
