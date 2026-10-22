import { describe, expect, it, vi } from 'vitest'
import type { UIAttachment } from '@/composables/api/useChat.types'

vi.mock('@/lib/api-client', () => ({
  sdkApiUrl: ({ url, path }: { url: string, path?: Record<string, string> }) => path
    ? Object.entries(path).reduce((result, [key, value]) => result.replace(`{${key}}`, value), url)
    : url,
  sdkAuthQuery: () => ({ token: 'synthetic' }),
}))
vi.mock('@/store/chat-list', () => ({ useChatStore: () => ({ currentBotId: 'current' }) }))

import { resolveUrl } from './useMediaGallery'

const hash = 'a'.repeat(64)
const attachment: UIAttachment = { type: 'image', content_hash: hash }
const id = '10000000-0000-4000-8000-000000000001'
const url = `/bots/${id}/sessions/${id}/delegations/${id}/attachments/${hash}`

describe('delegated attachment routes', () => {
  it('keeps the scoped route after metadata is restored from history', () => {
    expect(resolveUrl({ ...attachment, metadata: { delegation_url: url } })).toBe(url)
    expect(resolveUrl({ ...attachment, bot_id: id })).toBe(`/bots/${id}/media/${hash}`)
  })

  it.each([`https://example.com${url}`, `//example.com${url}`, `${url}?x=1`, url.replace(hash, 'b'.repeat(64)), '/bots/other/media/hash', null])('rejects invalid scoped metadata without falling back: %s', (value) => {
    expect(resolveUrl({ ...attachment, url: 'https://example.com', metadata: { delegation_url: value } })).toBe('')
  })
})
