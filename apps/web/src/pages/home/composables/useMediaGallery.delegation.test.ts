import { describe, expect, it, vi } from 'vitest'

vi.mock('@/lib/api-client', () => ({
  sdkApiUrl: ({ url, path }: { url: string, path?: Record<string, string> }) => path
    ? url.replace('{bot_id}', path.bot_id).replace('{content_hash}', path.content_hash)
    : url,
  sdkAuthQuery: () => ({ token: 'synthetic' }),
}))
vi.mock('@/store/chat-list', () => ({ useChatStore: () => ({ currentBotId: 'current' }) }))

import { resolveUrl } from './useMediaGallery'

const hash = 'a'.repeat(64)
const id = '10000000-0000-4000-8000-000000000001'
const url = `/bots/${id}/sessions/${id}/delegations/${id}/attachments/${hash}`

describe('delegated attachment routes', () => {
  it('keeps the scoped route after metadata is restored from history', () => {
    expect(resolveUrl({ content_hash: hash, metadata: { delegation_url: url } })).toBe(url)
    expect(resolveUrl({ content_hash: hash, bot_id: id })).toBe(`/bots/${id}/media/${hash}`)
  })

  it.each([`https://example.com${url}`, `//example.com${url}`, `${url}?x=1`, url.replace(hash, 'b'.repeat(64)), '/bots/other/media/hash', null])('rejects invalid scoped metadata without falling back: %s', (value) => {
    expect(resolveUrl({ content_hash: hash, url: 'https://example.com', metadata: { delegation_url: value } })).toBe('')
  })
})
