import { beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '@memohai/sdk/client'
import { getUsersMe, postBotsByBotIdContainerFsUpload } from '@memohai/sdk'
import { fileApiFetch, sdkFileApiUrl, setupApiClient } from './api-client'
import { mediaIdentity } from './media-transfer'
import { resolveUrl } from '@/pages/home/composables/useMediaGallery'

vi.mock('@/store/chat-list', () => ({ useChatStore: () => ({ currentBotId: '10000000-0000-4000-8000-000000000001' }) }))

const logical = 'https://nas.example.invalid:18443/api'
const physical = 'http://127.0.0.1:18068'
const bot = '10000000-0000-4000-8000-000000000001'
const hash = 'a'.repeat(64)
let token: string
let network: boolean
let requests: Request[]
let uploaded: { path: FormDataEntryValue | null, bytes: number[] } | undefined
const transport: typeof fetch = async input => {
  const request = input as Request
  requests.push(request)
  if (request.url === logical + '/users/me') return Response.json({ id: 'synthetic-user', ...(network ? { network_policy: { version: 2, endpoint: { logical_origin: new URL(logical).origin, physical_origin: physical } } } : {}) })
  if (request.url.endsWith('/container/fs/upload')) {
    const body = await request.formData()
    uploaded = { path: body.get('path'), bytes: [...new Uint8Array(await (body.get('file') as File).arrayBuffer())] }
  }
  return Response.json({ entries: [] })
}

beforeEach(() => {
  token = 'synthetic-no-authority'; network = true; requests = []; uploaded = undefined
  vi.stubGlobal('localStorage', { getItem: () => token, removeItem: () => { token = '' } })
  client.interceptors.request.clear()
  setupApiClient({ baseUrl: logical, fetch: transport })
})

describe('authenticated central routing for preview URLs and Files', () => {
  it('keeps old routes when policy is omitted and never changes external URLs', async () => {
    network = false
    await getUsersMe({ throwOnError: true })
    expect(resolveUrl({ type: 'image', bot_id: bot, content_hash: hash })).toContain(logical + '/bots/')
    expect(resolveUrl({ type: 'image', url: 'https://external.example.invalid/original.png' })).toBe('https://external.example.invalid/original.png')
  })

  it('maps preview/delegation and workspace URLs without rewriting original attachment identities', async () => {
    await getUsersMe({ throwOnError: true })
    const scoped = `/bots/${bot}/sessions/${bot}/delegations/${bot}/attachments/${hash}`
    const attachment = { type: 'image', bot_id: bot, content_hash: hash, metadata: { delegation_url: scoped } }
    const original = JSON.stringify(attachment)
    expect(resolveUrl(attachment)).toBe(`${physical}/api${scoped}?token=${token}`)
    expect(resolveUrl({ type: 'audio', bot_id: bot, content_hash: hash })).toBe(`${physical}/api/bots/${bot}/media/${hash}?token=${token}`)
    expect(sdkFileApiUrl({ url: '/bots/{bot}/container/fs/download', path: { bot }, query: { path: '/data/original.bin', token } })).toContain(`${physical}/api/bots/${bot}/container/fs/download?`)
    expect(JSON.stringify(attachment)).toBe(original)
    expect(requests).toHaveLength(1)
  })

  it('uses original SDK multipart File values without buffering the complete request', async () => {
    await getUsersMe({ throwOnError: true })
    const spy = vi.spyOn(Request.prototype, 'blob').mockRejectedValue(new Error('Whole request buffering forbidden'))
    try {
      const file = new File([new Uint8Array([0, 255, 128, 31])], 'original.bin')
      await postBotsByBotIdContainerFsUpload({ path: { bot_id: bot }, body: { path: '/data/original.bin', file } as never, throwOnError: true })
      expect(uploaded).toEqual({ path: '/data/original.bin', bytes: [0, 255, 128, 31] })
      expect(requests[1]!.url).toBe(`${physical}/api/bots/${bot}/container/fs/upload`)
      expect(requests[1]!.redirect).toBe('error')
      expect(requests[1]!.headers.get('Authorization')).toBe('Bearer ' + token)
      expect(spy).not.toHaveBeenCalled()
    } finally { spy.mockRestore() }
  })

  it('maps direct archive/extract bodies once and refuses foreign identity/query credentials', async () => {
    await getUsersMe({ throwOnError: true })
    await fileApiFetch(logical + `/bots/${bot}/container/fs/archive`, { method: 'POST', headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: JSON.stringify({ paths: ['/data/original.bin'] }) })
    expect(requests[1]!.url).toBe(`${physical}/api/bots/${bot}/container/fs/archive`)
    expect(await requests[1]!.text()).toBe(JSON.stringify({ paths: ['/data/original.bin'] }))
    expect(requests[1]!.redirect).toBe('error')
    expect(() => sdkFileApiUrl({ url: `https://foreign.example.invalid/api/bots/${bot}/media/${hash}`, query: { token } })).toThrow()
    expect(() => sdkFileApiUrl({ url: `/bots/${bot}/media/${hash}`, query: { token: 'other-account' } })).toThrow()
    expect(requests).toHaveLength(2)
  })

  it('refuses an account switch after SDK routing and before actual transport dispatch', async () => {
    await getUsersMe({ throwOnError: true })
    client.interceptors.request.use(request => { if (request.url.startsWith(physical)) token = 'other-account'; return request })
    await expect(postBotsByBotIdContainerFsUpload({ path: { bot_id: bot }, body: { path: '/data/original.bin', file: new File(['unchanged'], 'original.bin') } as never, throwOnError: true })).rejects.toMatchObject({ name: 'AbortError' })
    expect(requests).toHaveLength(1)
  })

  it('does not reuse a previous account endpoint or dispatch an old resolved URL after a token switch', async () => {
    await getUsersMe({ throwOnError: true })
    const old = resolveUrl({ type: 'image', bot_id: bot, content_hash: hash })
    token = 'different-current-account'
    await expect(fileApiFetch(old)).rejects.toMatchObject({ name: 'AbortError' })
    expect(sdkFileApiUrl({ url: `/bots/${bot}/media/${hash}`, query: { token } })).toContain(logical + '/bots/')
    expect(requests).toHaveLength(1)
  })

  it('refuses a late profile after the account changes and never adopts its endpoint', async () => {
    let release!: () => void
    let started!: () => void
    const ready = new Promise<void>(resolve => { started = resolve })
    const wait = new Promise<void>(resolve => { release = resolve })
    setupApiClient({ baseUrl: logical, fetch: async (input, init) => { started(); await wait; return await transport(input, init) } })
    const pending = getUsersMe({ throwOnError: true })
    await ready
    token = 'different-current-account'
    release()
    await expect(pending).rejects.toMatchObject({ name: 'AbortError' })
    expect(sdkFileApiUrl({ url: `/bots/${bot}/media/${hash}`, query: { token } })).toContain(logical + '/bots/')
  })

  it('keeps logical URLs for a native adapter that explicitly owns routing below its identity guard', async () => {
    expect(() => setupApiClient({ baseUrl: logical, fileRouting: 'platform' })).toThrow()
    setupApiClient({ baseUrl: logical, fileRouting: 'platform', fetch: transport })
    await getUsersMe({ throwOnError: true })
    expect(resolveUrl({ type: 'image', bot_id: bot, content_hash: hash })).toContain(logical + '/bots/')
    await postBotsByBotIdContainerFsUpload({ path: { bot_id: bot }, body: { path: '/data/original.bin', file: new File(['same'], 'original.bin') } as never, throwOnError: true })
    expect(requests[1]!.url).toBe(logical + `/bots/${bot}/container/fs/upload`)
    const identity = await mediaIdentity(bot, new AbortController().signal)
    await identity.fetch(new Request(logical + `/bots/${bot}/media/${hash}`, { headers: { Authorization: 'Bearer ' + token } }))
    expect(requests.at(-1)!.url).toBe(logical + `/bots/${bot}/media/${hash}`)
    expect(identity.origin).toBe(logical)
    expect(identity.networkPolicy?.endpoint?.physical_origin).toBe(physical)
    const count = requests.length
    client.interceptors.request.use(request => { if (request.url.includes('/container/fs/')) token = 'other-account'; return request })
    await expect(postBotsByBotIdContainerFsUpload({ path: { bot_id: bot }, body: { path: '/data/original.bin', file: new File(['same'], 'original.bin') } as never, throwOnError: true })).rejects.toMatchObject({ name: 'AbortError' })
    expect(requests).toHaveLength(count)
  })
})
