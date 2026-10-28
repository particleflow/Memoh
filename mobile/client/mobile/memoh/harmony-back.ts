import type { Router } from 'vue-router'
import { isKeyboardElementVisible } from './keyboard-context'

// The native page is a single WebView: its system back event must first reach
// the existing Web owners. Never reload the document or pop ArkWeb history.
export function handleHarmonyBack(router?: Router): boolean {
  const visible = (element: HTMLElement) => isKeyboardElementVisible(element)
    && element.getClientRects().length > 0 && element.dataset.state !== 'closed'
  const layers = [...document.querySelectorAll<HTMLElement>(
    '[role="dialog"], [role="alertdialog"], [role="menu"], [role="listbox"], [data-slot="popover-content"]',
  )].filter(visible)
  if (layers.length) {
    // Custom media lightboxes are above Reka's modal layer. Invoke their own
    // close action so an Escape cannot also dismiss a dialog underneath.
    const lightbox = layers.findLast(element => element.dataset.keyboardScope === 'mediaLightbox')
    if (lightbox) lightbox.querySelector<HTMLButtonElement>('button[aria-label="Close"]')?.click()
    else {
      const top = layers.at(-1)!
      const detailBack = [...top.querySelectorAll<HTMLButtonElement>('[data-slot="dialog-view-header"] button')]
        .find(button => visible(button) && !button.disabled && button.querySelector('svg.lucide-chevron-left'))
      if (detailBack) detailBack.click() // Task / form drill-in before modal dismissal.
      else window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    }
    return true // The layer owner may deliberately veto dismissal (e.g. saving).
  }
  const cancel = document.getElementById('service-cancel') as HTMLButtonElement | null
  if (cancel && visible(cancel)) { cancel.click(); return true }
  if (!router) return false // Initial configuration / failed boot has no parent.

  // Reuse in-page back actions (settings detail/list, secondary chat panels).
  // Their owners retain KeepAlive, drafts, native receipts and navigation guards.
  const route = router.currentRoute.value
  const surface = route.path.startsWith('/settings')
    ? document.querySelector('[data-settings-content-shell]') ?? document : document
  const buttons = [...surface.querySelectorAll<HTMLElement>('button, a, [role="button"]')]
  const back = buttons.find(element => visible(element) && !element.hasAttribute('disabled')
    && (element.hasAttribute('data-settings-detail-back') || !!element.closest('header, [data-settings-content-shell]'))
    && (element.hasAttribute('data-settings-detail-back')
      || !!element.querySelector('svg.lucide-chevron-left')))
  if (back) { back.click(); return true }

  // An explicit execution/history URL is below this Bot's fixed contact.
  // Let that route own selection; selecting the Bot first races the old URL.
  if (route.name === 'bot' && typeof route.query?.session === 'string' && route.query.session.trim()
    && typeof route.params?.botName === 'string' && route.params.botName.trim()) {
    void router.replace({ name: 'bot', params: { botName: route.params.botName } })
    return true
  }
  if (route.path.startsWith('/settings/')) { void router.replace('/settings'); return true }
  if (route.path === '/settings') { void router.replace({ name: 'home' }); return true }
  if (route.name === 'Login' || route.name === 'home' || route.name === 'bot'
    || route.path === '/onboarding') return false
  void router.replace({ name: 'home' })
  return true
}

export function installHarmonyBack(router?: Router): void {
  ;(window as Window & { __memohBack?: () => boolean }).__memohBack = () => handleHarmonyBack(router)
}
