import { beforeEach, describe, expect, it, vi } from 'vitest'
import { sha256 } from '@noble/hashes/sha2.js'
import { bytesToHex } from '@noble/hashes/utils.js'
import { client } from '@memohai/sdk/client'
import type { MediaJournal } from './media-transfer-storage'
import { setupApiClient } from './api-client'
import { acknowledgeMedia, downloadMedia, forgetMediaFile, mediaIdentity, restoreMediaUploads, restoreMediaUploadScopes, uploadMedia } from './media-transfer'

const disk = vi.hoisted(() => ({ journals: new Map<string, MediaJournal>(), files: new Map<string, Blob>(), failSource: false, failJournal: false, generation: 0, locks: new Map<string, Promise<unknown>>() }))
vi.mock('./auth-session', () => ({ authSessionGeneration: () => disk.generation }))
vi.mock('./media-transfer-storage', () => ({
  listJournals: async () => [...disk.journals.values()].map(j => structuredClone(j)),
  readJournal: async (key: string) => structuredClone(disk.journals.get(key)),
  writeJournal: async (j: MediaJournal) => { if (disk.failJournal) throw new Error('IDB quota'); disk.journals.set(j.key, structuredClone(j)) },
  removeJournal: async (key: string) => { disk.journals.delete(key); disk.files.delete(key) },
  localFile: async (key: string) => { const b = disk.files.get(key); if (!b) throw new Error('not found'); return new File([b], key) },
  writeLocalFile: async (key: string, blob: Blob, offset = 0) => {
    if (disk.failSource) throw new Error('OPFS quota')
    disk.files.set(key, new Blob([...(offset ? [disk.files.get(key)!.slice(0, offset)] : []), blob]))
  },
  mediaLock: async (key: string, signal: AbortSignal, action: () => Promise<unknown>) => {
    const before = disk.locks.get(key) ?? Promise.resolve()
    const next = before.catch(() => {}).then(() => { signal.throwIfAborted(); return action() })
    disk.locks.set(key, next)
    return await next
  },
}))

const bot = '10000000-0000-4000-8000-000000000001'
const uploadId = '20000000-0000-4000-8000-000000000002'
const file = new File([new Uint8Array(3 * 1024 * 1024 + 19).map((_, i) => i * 31 % 251)], 'real-bytes.bin', { type: 'application/octet-stream' })
const bytes = new Uint8Array(await file.arrayBuffer())
const hash = bytesToHex(sha256(bytes))
const expires_at = '2026-10-27T00:00:00Z'
let token: string
let owner: string
let offset: number
let ready: boolean
let fault: string
let requests: { method: string, path: string, range: string | null, offset: string | null }[]
const signal = () => new AbortController().signal
const upload = async (draft = 'original-draft') => uploadMedia(await mediaIdentity(bot, signal()), file, draft, 1024 * 1024, signal())
const journal = () => [...disk.journals.values()].find(j => j.kind === 'upload')!
function receipt() { return { id: uploadId, offset, size_bytes: file.size, mime: file.type, expires_at, state: ready ? 'ready' : 'uploading', ...(ready ? { content_hash: hash } : {}) } }
function json(data: unknown, status = 200) { return new Response(JSON.stringify(data), { status, headers: { 'Content-Type': 'application/json' } }) }
async function server(input: RequestInfo | URL) {
  const req = input as Request
  const path = new URL(req.url).pathname.replace(/^\/api/, '')
  requests.push({ method: req.method, path, range: req.headers.get('Range'), offset: req.headers.get('Upload-Offset') })
  if (path === '/users/me') return json({ id: owner })
  if (path.endsWith('/media/limits')) return json({ max_upload_bytes: 200 * 1024 * 1024, resumable_enabled: true, chunk_max_bytes: 8 * 1024 * 1024 })
  if (path.endsWith('/uploads') && req.method === 'POST') {
    expect(journal().phase).toBe('create-unknown')
    if (fault === 'create') { fault = ''; throw new TypeError('lost response') }
    if (fault === 'server-mime') return json({ ...receipt(), mime: 'text/plain' }, 201)
    return json(receipt(), 201)
  }
  if (path.endsWith(`/uploads/${uploadId}`) && req.method === 'GET') {
    if (fault === '404') return json({}, 404)
    return json(receipt())
  }
  if (req.method === 'PATCH') {
    const body = new Uint8Array(await req.arrayBuffer())
    expect(req.headers.get('Upload-SHA256')).toBe(bytesToHex(sha256(body)))
    expect(Number(req.headers.get('Upload-Offset'))).toBe(offset)
    expect(bytesToHex(sha256(body))).toBe(bytesToHex(sha256(bytes.slice(offset, offset + body.length))))
    offset += body.length
    if (fault === 'patch') { fault = ''; throw new TypeError('lost PATCH') }
    if (fault === 'account') { token = 'other'; fault = '' }
    return json(receipt())
  }
  if (path.endsWith('/complete')) {
    if (fault === 'complete-pending') throw new TypeError('not observed')
    ready = true
    if (fault === 'server-mime') return json({ ...receipt(), mime: 'text/plain' })
    if (fault === 'complete') { fault = ''; throw new TypeError('lost Complete') }
    if (fault === 'ready-hash') return json({ ...receipt(), content_hash: 'a'.repeat(64) })
    return json(receipt())
  }
  if (path.includes('/attachments/') && fault === 'denied') return json({}, 403)
  if (path.endsWith(`/media/${hash}`) || path.endsWith(`/attachments/${hash}`)) {
    const headers: Record<string, string> = { ETag: `"${hash}"`, 'Content-Length': String(file.size), 'Content-Type': file.type }
    if (req.method === 'HEAD') return new Response(null, { headers })
    const [, a, b] = /^bytes=(\d+)-(\d+)$/.exec(req.headers.get('Range')!)!
    const start = Number(a), end = Number(b)
    if (fault === 'download' && start > 0) { fault = ''; throw new TypeError('download interrupted') }
    const body = bytes.slice(start, end + 1)
    if (fault === 'corrupt') body[0] = body[0]! ^ 255
    headers['Content-Range'] = `bytes ${start}-${end}/${file.size}`
    headers['Content-Length'] = String(body.length)
    return new Response(body, { status: 206, headers })
  }
  throw new Error(`Unexpected request ${req.method} ${path}`)
}
const count = (method: string, suffix: string) => requests.filter(r => r.method === method && r.path.endsWith(suffix)).length
beforeEach(() => {
  disk.journals.clear(); disk.files.clear(); disk.locks.clear(); disk.failSource = false; disk.failJournal = false; disk.generation = 0
  token = 'original-token'; owner = 'original-account'; offset = 0; ready = false; fault = ''; requests = []
  vi.stubGlobal('window', { location: { origin: 'https://nas.example.invalid' } })
  vi.stubGlobal('localStorage', { getItem: () => token, removeItem: () => { token = '' } })
  setupApiClient({ baseUrl: 'https://nas.example.invalid/api', fetch: server as typeof fetch })
})

describe('persistent browser media safety (SDK HTTP seam, synthetic storage)', () => {
  it('checks real chunk bytes and complete original hash before returning a reference; ACK cleans local only', async () => {
    const result = await upload()
    expect(result).toMatchObject({ hash, size: 3 * 1024 * 1024 + 19 })
    expect(count('POST', '/uploads')).toBe(1)
    expect(count('PATCH', `/uploads/${uploadId}`)).toBe(4)
    expect(count('POST', '/complete')).toBe(1)
    await acknowledgeMedia([result.key])
    expect(disk.journals.size).toBe(0)
    expect(requests.some(r => r.method === 'DELETE')).toBe(false)
  })
  it('retains the server MIME on the verified attachment', async () => {
    fault = 'server-mime'; expect((await upload()).mime).toBe('text/plain')
  })
  it('never POSTs again after a lost Create, including remove and re-selection', async () => {
    fault = 'create'
    await expect(upload()).rejects.toThrow()
    await forgetMediaFile(file)
    expect((await restoreMediaUploads(bot, 'original-draft', signal())).files).toHaveLength(0)
    await expect(upload()).rejects.toMatchObject({ reason: 'unknown' })
    expect(count('POST', '/uploads')).toBe(1)
  })
  it('resumes a lost PATCH from original GET offset without retransmitting acknowledged bytes', async () => {
    fault = 'patch'; await expect(upload()).rejects.toThrow()
    expect(journal().offset).toBe(0)
    expect(journal().phase).toBe('patch-unknown')
    await upload()
    expect(count('POST', '/uploads')).toBe(1)
    expect(count('GET', `/uploads/${uploadId}`)).toBe(1)
    expect(requests.filter(r => r.method === 'PATCH').map(r => Number(r.offset))).toEqual([0, 1048576, 2097152, 3145728])
  })
  it('keeps receipt, expiry and confirmed offset after original GET404', async () => {
    fault = 'patch'; await expect(upload()).rejects.toThrow()
    const before = structuredClone(journal())
    fault = '404'; await expect(upload()).rejects.toMatchObject({ reason: 'receiptMissing' })
    expect(journal().receipt).toEqual(before.receipt)
    expect(journal().offset).toBe(before.offset)
    expect(count('POST', '/uploads')).toBe(1)
  })
  it('reconciles lost Complete with GET ready and never re-POSTs Complete', async () => {
    fault = 'complete'; await expect(upload()).rejects.toThrow()
    expect(journal().phase).toBe('complete-unknown')
    await upload()
    expect(count('POST', '/complete')).toBe(1)
    expect(count('GET', `/uploads/${uploadId}`)).toBe(1)
    expect(journal().receipt).toEqual({ id: uploadId, expires_at })
  })
  it('keeps Complete uncertainty when original GET is not ready', async () => {
    fault = 'complete-pending'; await expect(upload()).rejects.toThrow()
    fault = ''; await expect(upload()).rejects.toMatchObject({ reason: 'unknown' })
    expect(count('POST', '/complete')).toBe(1)
  })
  it('rejects a ready receipt with a false hash', async () => {
    fault = 'ready-hash'; await expect(upload()).rejects.toMatchObject({ reason: 'integrity' })
  })
  it('does not send Create when the intent cannot commit', async () => {
    disk.failJournal = true; await expect(upload()).rejects.toThrow('IDB quota')
    expect(count('POST', '/uploads')).toBe(0)
  })
  it('reports lost local source and resumes the same receipt after re-selection', async () => {
    disk.failSource = true; fault = 'patch'; await expect(upload()).rejects.toThrow()
    const restored = await restoreMediaUploads(bot, 'original-draft', signal())
    expect(restored.files).toHaveLength(0); expect(restored.missing).toEqual([file.name])
    disk.failSource = false; await upload()
    expect(count('POST', '/uploads')).toBe(1)
    expect(journal().source).toBe(true)
  })
  it('restores File bytes only in the original account/Bot/API/draft scope', async () => {
    fault = 'patch'; await expect(upload()).rejects.toThrow()
    expect((await restoreMediaUploads(bot, 'original-draft', signal())).files[0]!.size).toBe(file.size)
    expect((await restoreMediaUploads(bot, 'other-draft', signal())).files).toHaveLength(0)
    owner = 'other-account'; expect((await restoreMediaUploads(bot, 'original-draft', signal())).files).toHaveLength(0)
    owner = 'original-account'; setupApiClient({ baseUrl: 'https://other.example.invalid/api', fetch: server as typeof fetch })
    expect((await restoreMediaUploads(bot, 'original-draft', signal())).files).toHaveLength(0)
  })
  it('restores only valid original tab/session scopes for the authenticated owner', async () => {
    fault = 'patch'
    await expect(upload(JSON.stringify([`${bot}:chat:original`, '']))).rejects.toThrow()
    expect((await restoreMediaUploadScopes(bot, signal())).scopes).toEqual([{ tab: 'chat:original', session: null }])
    owner = 'another-account'
    expect((await restoreMediaUploadScopes(bot, signal())).scopes).toEqual([])
    owner = 'original-account'
    disk.journals.set('malformed', { ...journal(), key: 'malformed', draft: JSON.stringify([`${bot}:file:wrong`, '']) })
    expect((await restoreMediaUploadScopes(bot, signal())).scopes).toEqual([{ tab: 'chat:original', session: null }])
  })
  it('preserves a late PATCH checkpoint but prevents follow-up after account change', async () => {
    fault = 'account'; await expect(upload()).rejects.toMatchObject({ name: 'AbortError' })
    expect(journal().offset).toBe(1048576)
    expect(count('POST', '/complete')).toBe(0)
  })
  it('prevents token-switch dispatch after SDK request preparation', async () => {
    const switchAccount = (request: Request) => {
      if (request.method === 'PATCH') { token = 'different-account'; request.headers.set('Authorization', `Bearer ${token}`) }
      return request
    }
    client.interceptors.request.use(switchAccount)
    try {
      await expect(upload()).rejects.toMatchObject({ name: 'AbortError' })
      expect(requests.some(r => r.method === 'PATCH')).toBe(false)
      expect(journal().phase).toBe('patch-unknown')
      expect(offset).toBe(0)
    } finally { client.interceptors.request.eject(switchAccount) }
  })
  it('serializes competing callers into one Create and one completion', async () => {
    await Promise.all([upload(), upload()])
    expect(count('POST', '/uploads')).toBe(1); expect(count('POST', '/complete')).toBe(1)
  })
  it('resumes download at committed local offset, validates Range/size/hash', async () => {
    fault = 'download'; await expect(downloadMedia(bot, hash, file.size, undefined, signal())).rejects.toThrow()
    expect([...disk.journals.values()][0]!.offset).toBe(1048576)
    const result = await downloadMedia(bot, hash, file.size, undefined, signal())
    expect(bytesToHex(sha256(new Uint8Array(await result.arrayBuffer())))).toBe(hash)
    expect(requests.filter(r => r.method === 'GET' && r.path.endsWith(`/media/${hash}`)).map(r => r.range))
      .toEqual(['bytes=0-1048575', 'bytes=1048576-2097151', 'bytes=1048576-2097151', 'bytes=2097152-3145727', 'bytes=3145728-3145746'])
  })
  it('rejects corrupt downloaded bytes and permits only a fresh read of the original target', async () => {
    fault = 'corrupt'; await expect(downloadMedia(bot, hash, file.size, undefined, signal())).rejects.toMatchObject({ reason: 'integrity' })
    expect([...disk.journals.values()][0]!.offset).toBe(0)
    fault = ''; await expect(downloadMedia(bot, hash, file.size, undefined, signal())).resolves.toBeInstanceOf(File)
  })
  it('never broadens a denied delegation into a Bot media request', async () => {
    fault = 'denied'
    const target = `/bots/${bot}/sessions/${bot}/delegations/${bot}/attachments/${hash}`
    await expect(downloadMedia(bot, hash, file.size, target, signal())).rejects.toBeDefined()
    expect(requests.some(r => r.path.includes('/media/'))).toBe(false)
  })
  it('rechecks authorization before returning already verified local bytes', async () => {
    const target = `/bots/${bot}/sessions/${bot}/delegations/${bot}/attachments/${hash}`
    await downloadMedia(bot, hash, file.size, target, signal())
    fault = 'denied'
    await expect(downloadMedia(bot, hash, file.size, target, signal())).rejects.toBeDefined()
  })
})
