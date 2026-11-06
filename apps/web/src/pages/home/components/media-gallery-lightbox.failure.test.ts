// @vitest-environment jsdom
import { createApp, h, nextTick, ref, type App } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import MediaGalleryLightbox from './media-gallery-lightbox.vue'

vi.mock('@/composables/useKeyboardCommand', () => ({ useKeyboardCommand: vi.fn() }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

let app: App | undefined
afterEach(() => { app?.unmount(); document.body.replaceChildren() })

describe('media preview failure', () => {
  it('shows one inaccessible state and does not request the failed URL again on navigation or reopening', async () => {
    const host = document.createElement('div')
    document.body.append(host)
    const index = ref<number | null>(0)
    const id = '10000000-0000-4000-8000-000000000001'
    const src = `/bots/${id}/sessions/${id}/delegations/${id}/attachments/${'a'.repeat(64)}`
    const items = [{ src, type: 'image' as const, name: 'original.png' }, { src: 'data:image/png;base64,synthetic', type: 'image' as const, name: 'other.png' }]
    const snapshot = JSON.stringify(items)
    app = createApp({ render: () => h(MediaGalleryLightbox, { items, openIndex: index.value, 'onUpdate:openIndex': value => { index.value = value } }) })
    app.mount(host)
    await nextTick()
    document.querySelector('img')!.dispatchEvent(new Event('error'))
    await nextTick()
    expect(document.querySelector('img')).toBeNull()
    expect(document.querySelector('[role="status"]')?.textContent).toBe('chat.attachmentUnavailable')
    index.value = 1
    await nextTick()
    expect(document.querySelector('img')?.getAttribute('src')).toBe(items[1]!.src)
    index.value = 0
    await nextTick()
    expect(document.querySelector('img')).toBeNull()
    index.value = null
    await nextTick()
    index.value = 0
    await nextTick()
    expect(document.querySelector('img')).toBeNull()
    expect(document.querySelectorAll('[role="status"]')).toHaveLength(1)
    expect(JSON.stringify(items)).toBe(snapshot)
  })
})
