// @vitest-environment jsdom
import { createApp, h, nextTick, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { AttachmentItem } from '@/store/chat-list'
import AttachmentBlock from './attachment-block.vue'
import { openAssetPreviewKey, type OpenAssetPreview } from '../composables/useFileManagerProvider'

vi.mock('@/store/chat-list', () => ({ useChatStore: () => ({ currentBotId: 'bot-a' }) }))
vi.mock('../composables/useMediaGallery', () => ({
  resolveUrl: (att: AttachmentItem) => att.url || '',
}))
vi.mock('@felinic/ui', () => ({ Skeleton: 'span' }))
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string, args?: { count: number }) => `${key}:${args?.count ?? ''}` }),
}))

let app: App | undefined
let host: HTMLDivElement
const fetchMock = vi.fn()

beforeEach(() => {
  vi.clearAllMocks()
  vi.stubGlobal('fetch', fetchMock)
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => { callback(0); return 0 })
})
afterEach(() => {
  app?.unmount()
  app = undefined
  host?.remove()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

async function renderAttachment(size: number | undefined, name = 'history.txt', extra: Partial<AttachmentItem> = {}, open?: OpenAssetPreview) {
  host = document.createElement('div')
  document.body.append(host)
  app = createApp({
    render: () => h(AttachmentBlock, {
      block: { id: 1, type: 'attachments', attachments: [{
        type: 'file', name, mime: 'text/plain', size, url: 'https://example.invalid/synthetic', ...extra,
      }] },
    }),
  })
  if (open) app.provide(openAssetPreviewKey, open)
  app.mount(host)
  await nextTick()
  await nextTick()
}

describe('history attachment automatic line counts', () => {
  it('passes a credential-free delegation scope into the asset dock', async () => {
    const hash = 'a'.repeat(64)
    const id = '10000000-0000-4000-8000-000000000001'
    const path = `/bots/${id}/sessions/${id}/delegations/${id}/attachments/${hash}`
    const open = vi.fn()
    await renderAttachment(undefined, 'file', { content_hash: hash, url: path, metadata: { delegation_url: path } }, open)
    host.querySelector('button')!.click()
    expect(open).toHaveBeenCalledWith(expect.objectContaining({ key: path, delegationUrl: path, contentHash: hash, src: undefined }))
  })

  it.each(['history.txt', 'history.csv'])('does not fetch a large %s and preserves size and explicit download', async (name) => {
    await renderAttachment(201 * 1024 * 1024, name)
    expect(fetchMock).not.toHaveBeenCalled()
    expect(host.textContent).toContain(name)
    expect(host.textContent).toContain('201.0 MB')
    expect(host.textContent).not.toContain('chat.attachmentLines')
    const download = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      expect(this.href).toBe('https://example.invalid/synthetic')
      expect(this.download).toBe(name)
    })
    host.querySelector('button')!.click()
    expect(download).toHaveBeenCalledOnce()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it.each([undefined, -1, Number.NaN, Number.POSITIVE_INFINITY])('does not fetch an attachment with unknown or invalid size %s', async (size) => {
    await renderAttachment(size)
    expect(fetchMock).not.toHaveBeenCalled()
    expect(host.textContent).toContain('history.txt')
    expect(host.textContent).not.toContain('chat.attachmentLines')
  })

  it('still counts known small text files', async () => {
    fetchMock.mockResolvedValue({ ok: true, text: async () => 'one\ntwo' })
    await renderAttachment(7)
    await nextTick()
    expect(fetchMock).toHaveBeenCalledOnce()
    expect(host.textContent).toContain('chat.attachmentLines:2')
  })
})
