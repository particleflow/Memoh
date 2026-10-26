// @vitest-environment jsdom
// Run in the generated Harmony Web context; imports the actual host adapter.
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Router } from 'vue-router'
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
