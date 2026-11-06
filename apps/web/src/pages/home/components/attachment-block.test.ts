// @vitest-environment jsdom
import { createApp, h, nextTick, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { AttachmentItem } from '@/store/chat-list'
import AttachmentBlock from './attachment-block.vue'
import { openAssetPreviewKey, type OpenAssetPreview } from '../composables/useFileManagerProvider'

const transfer = vi.hoisted(() => ({ save: vi.fn().mockResolvedValue(undefined) }))
vi.mock('@/lib/media-transfer', () => ({ saveMediaDownload: transfer.save, MediaRecoveryError: class extends Error {} }))
vi.mock('@/store/chat-list', () => ({ useChatStore: () => ({ currentBotId: 'bot-a' }) }))
vi.mock('../composables/useMediaGallery', () => ({
  resolveUrl: (att: AttachmentItem) => att.url || '',
}))
vi.mock('@felinic/ui', () => ({ Skeleton: 'span', toast: { error: vi.fn() } }))
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

async function renderAttachment(size: number | undefined, name = 'history.txt', extra: Partial<AttachmentItem> = {}, open?: OpenAssetPreview, variant: 'attachment' | 'content' = 'attachment', onOpenMedia = vi.fn()) {
  host = document.createElement('div')
  document.body.append(host)
  app = createApp({
    render: () => h(AttachmentBlock, {
      variant,
      onOpenMedia,
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
  it.each(['attachment', 'content'] as const)('retires a failed scoped image in the %s variant without changing its reference or retrying', async (variant) => {
    const hash = 'a'.repeat(64)
    const id = '10000000-0000-4000-8000-000000000001'
    const url = `/bots/${id}/sessions/${id}/delegations/${id}/attachments/${hash}`
    const attachment = { type: 'image', mime: 'image/png', content_hash: hash, url, metadata: { delegation_url: url } }
    const original = JSON.stringify(attachment)
    const open = vi.fn()
    await renderAttachment(0, 'original.png', attachment, undefined, variant, open)
    expect(host.querySelector('img')?.getAttribute('src')).toBe(url)
    host.querySelector('img')!.dispatchEvent(new Event('error'))
    await nextTick()
    expect(host.textContent).toContain('chat.attachmentUnavailable')
    expect(host.textContent).toContain('original.png')
    expect(host.querySelector('img')).toBeNull()
    expect(host.querySelector('button')).toBeNull()
    host.click()
    await nextTick()
    expect(open).not.toHaveBeenCalled()
    expect(fetchMock).not.toHaveBeenCalled()
    expect(JSON.stringify(attachment)).toBe(original)
  })

  it('keeps a loaded image preview available', async () => {
    const open = vi.fn()
    await renderAttachment(74, 'original.png', { type: 'image', mime: 'image/png' }, undefined, 'content', open)
    host.querySelector('img')!.dispatchEvent(new Event('load'))
    await nextTick()
    host.querySelector('button')!.click()
    expect(open).toHaveBeenCalledWith('https://example.invalid/synthetic')
    expect(host.textContent).not.toContain('chat.attachmentUnavailable')
  })

  it('uses durable scope-bound download outside a dock for stored originals', async () => {
    const hash = 'b'.repeat(64)
    await renderAttachment(3145747, 'original.bin', { content_hash: hash })
    host.querySelector('button')!.click()
    await nextTick()
    expect(transfer.save).toHaveBeenCalledWith('bot-a', hash, 3145747, undefined, 'original.bin', expect.any(AbortSignal))
  })

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
