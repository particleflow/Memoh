// @vitest-environment node
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { webcrypto } from 'node:crypto'
import { client } from '@memohai/sdk/client'
import { getBotsByBotIdContainerFsDownload } from '@memohai/sdk'
import { openDiagnosticLease, setupApiClient } from '@/lib/api-client'
import { notifyAuthSessionCleared } from '@/lib/auth-session'
import { diagnosticTransfer, reconcileDiagnosticBatch, resumeDiagnostics, suspendDiagnostics } from './harmony-diagnostics'

// Keep Request/Response/Blob/FormData/AbortSignal in one real platform realm.
// Only the DOM lifecycle/storage boundary is adapted; the generated SDK is real.
const { host, storage } = vi.hoisted(() => {
  const values = new Map<string, string>()
  const storage = { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => values.set(key, value), removeItem: (key: string) => values.delete(key), clear: () => values.clear(), key: (index: number) => [...values.keys()][index] ?? null, get length() { return values.size } }
  const host = Object.assign(new EventTarget(), { location: { origin: 'https://example.test', host: 'example.test', protocol: 'https:' } })
  vi.stubGlobal('window', host); vi.stubGlobal('localStorage', storage)
  return { host, storage }
})

const bot = '11111111-2222-4333-8444-555555555555', other = '66666666-7777-4888-8999-aaaaaaaaaaaa'
let principal = 'user-a', sends: Request[], objects: Map<string, ArrayBuffer>, writes: number, acks: number, loseReply: boolean
let blockRead: boolean, readReject: ((error: Error) => void) | undefined
const rows = new Map<string, Array<{ id: string, document: string, through: number, body: string }>>()
const cursors = new Map<string, number>()
const digest = async (body: string) => Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256', new TextEncoder().encode(body))), b => b.toString(16).padStart(2, '0')).join('')
const fetcher: typeof fetch = async (input, init) => {
  const request = new Request(input, init), url = new URL(request.url); sends.push(request)
  if (url.pathname.endsWith('/users/me')) return Response.json({ id: principal })
  if (url.pathname.endsWith('/download')) {
    if (blockRead) return new Promise((_resolve, reject) => { readReject = reject; request.signal.addEventListener('abort', () => reject(new Error('retired')), { once: true }) })
    const data = objects.get(url.searchParams.get('path')!)
    return data ? new Response(data) : new Response(null, { status: 404 })
  }
  if (url.pathname.endsWith('/upload')) {
    writes++
    const form = await request.formData(), file = form.get('file') as File
    objects.set(String(form.get('path')), await file.arrayBuffer())
    if (loseReply) { loseReply = false; throw new TypeError('controlled reply loss after store') }
    return Response.json({ size: file.size })
  }
  throw new Error('Unexpected diagnostic endpoint')
}
beforeEach(() => {
  vi.stubGlobal('window', host); vi.stubGlobal('localStorage', storage)
  vi.stubGlobal('crypto', webcrypto); sends = []; objects = new Map(); writes = 0; acks = 0; loseReply = false; blockRead = false; readReject = undefined; principal = 'user-a'; rows.clear(); cursors.clear()
  localStorage.clear(); localStorage.setItem('token', 'synthetic-a'); client.interceptors.request.clear()
  setupApiClient({ baseUrl: 'https://example.test/api', fetch: fetcher })
  Object.assign(window, { begoniaNative: { async request(raw: string) {
    const value = JSON.parse(raw), key = value.scope + ':' + value.document
    if (value.command === 'diagnostic.pending') return JSON.stringify({ ok: true, data: rows.get(value.scope) ?? [] })
    if (value.command === 'diagnostic.cursor') return JSON.stringify({ ok: true, data: cursors.get(key) ?? 0 })
    if (value.command === 'diagnostic.put') { rows.set(value.scope, [...(rows.get(value.scope) ?? []), value.batch]); cursors.set(value.scope + ':' + value.batch.document, value.batch.through) }
    if (value.command === 'diagnostic.ack') { acks++; rows.set(value.scope, (rows.get(value.scope) ?? []).filter(item => item.id !== value.hash)) }
    return JSON.stringify({ ok: true })
  } } })
})
afterEach(() => { suspendDiagnostics(); readReject?.(new Error('test cleanup')); delete (window as Window & { begoniaNative?: unknown }).begoniaNative; vi.unstubAllGlobals() })

it('blocks identity changes between the actual SDK auth interceptor and baseFetch', async () => {
  const lease = await openDiagnosticLease(bot)
  let entered!: () => void, release!: () => void
  const waiting = new Promise<void>(resolve => { entered = resolve }), gate = new Promise<void>(resolve => { release = resolve })
  client.interceptors.request.use(async request => { if (request.url.includes('/download')) { entered(); await gate } return request })
  const pending = getBotsByBotIdContainerFsDownload({ ...lease.sdkOptions(), path: { bot_id: bot }, query: { path: '/data/.memoh/media/test.json' }, throwOnError: true })
  const rejected = expect(pending).rejects.toThrow(); await waiting
  localStorage.setItem('token', 'synthetic-b'); notifyAuthSessionCleared('login'); release(); await rejected
  expect(sends.filter(request => request.url.includes('/download'))).toHaveLength(0)
  expect(localStorage.getItem('token')).toBe('synthetic-b')
})

it('rejects an assembled wrong bearer, a changed SDK base and another Bot before bytes dispatch', async () => {
  const lease = await openDiagnosticLease(bot)
  const mutate = (request: Request) => { request.headers.set('Authorization', 'Bearer synthetic-other'); return request }
  client.interceptors.request.use(mutate)
  await expect(getBotsByBotIdContainerFsDownload({ ...lease.sdkOptions(), path: { bot_id: bot }, query: { path: 'x' }, throwOnError: true })).rejects.toThrow()
  expect(sends).toHaveLength(1)
  client.interceptors.request.eject(mutate)
  const next = await openDiagnosticLease(bot); client.setConfig({ baseUrl: 'https://other.test/api' })
  expect(next.assertCurrent).toThrow()
  setupApiClient({ baseUrl: 'https://example.test/api', fetch: fetcher }); const final = await openDiagnosticLease(bot)
  await expect(getBotsByBotIdContainerFsDownload({ ...final.sdkOptions(), path: { bot_id: other }, query: { path: 'x' }, throwOnError: true })).rejects.toThrow()
  expect(sends.filter(request => request.url.includes('/download'))).toHaveLength(0)
})

it('retiring a lease aborts in-flight work and prevents a stale response ACK', async () => {
  const lease = await openDiagnosticLease(bot); blockRead = true
  const pending = getBotsByBotIdContainerFsDownload({ ...lease.sdkOptions(), path: { bot_id: bot }, query: { path: 'x' }, throwOnError: true })
  const rejected = expect(pending).rejects.toThrow(); await vi.waitFor(() => expect(readReject).toBeTypeOf('function'))
  notifyAuthSessionCleared('logout'); await rejected
  expect(sends[sends.length - 1]!.signal.aborted).toBe(true); expect(acks).toBe(0)
})

it('reconciles a stored diagnostic after reply loss without a second write, then ACKs exact bytes', async () => {
  const lease = await openDiagnosticLease(bot), body = JSON.stringify({ schema: 1, document: 'c'.repeat(32), timeUnit: 'relative_monotonic_ms', events: [{ seq: 1, t: 2, kind: 'load', g: 1, request: 1 }] })
  const item = { id: await digest(body), body, document: 'c'.repeat(32), through: 1 }; loseReply = true
  rows.set(lease.scope, [item])
  await expect(reconcileDiagnosticBatch(item, lease)).rejects.toThrow(); expect(writes).toBe(1); expect(acks).toBe(0)
  lease.retire(); const restored = await openDiagnosticLease(bot); await reconcileDiagnosticBatch(item, restored)
  expect(writes).toBe(1); expect(acks).toBe(1)
  const different = await openDiagnosticLease(other); expect(different.scope).not.toBe(restored.scope)
  await expect(reconcileDiagnosticBatch(item, different)).rejects.toThrow('DIAGNOSTIC_NOT_PENDING')
  expect(writes).toBe(1)
  principal = 'user-b'; const newOwner = await openDiagnosticLease(bot); expect(newOwner.scope).not.toBe(restored.scope)
})

it('persists later capture while the diagnostic network is stalled, then resumes the original queue automatically', async () => {
  blockRead = true
  const trace = diagnosticTransfer(bot); trace('send')
  await vi.waitFor(() => expect([...rows.values()].flat()).toHaveLength(1))
  trace('headers', 200); trace('loadend')
  await vi.waitFor(() => expect([...rows.values()].flat()).toHaveLength(3))
  expect(writes).toBe(0)
  blockRead = false; readReject?.(new Error('controlled disconnect'))
  await vi.waitFor(() => expect(sends.filter(request => request.signal.aborted).length).toBeGreaterThan(0))
  window.dispatchEvent(new Event('garnet:diagnostic-resume'))
  await vi.waitFor(() => expect([...rows.values()].flat()).toHaveLength(0))
  expect(acks).toBe(3)
  const uploaded = [...objects.values()].map(bytes => new TextDecoder().decode(bytes)).join('')
  for (const forbidden of ['synthetic-a', 'example.test', 'user-a', bot, 'filename', 'authorization']) expect(uploaded).not.toContain(forbidden)
})

it('does not start anything on an uncredentialed public page without the native bridge', async () => {
  delete (window as Window & { begoniaNative?: unknown }).begoniaNative
  diagnosticTransfer(bot)('send'); await resumeDiagnostics(bot)
  expect(sends).toHaveLength(0)
})

it('keeps a corrupt existing diagnostic object pending without overwrite or ACK', async () => {
  const lease = await openDiagnosticLease(bot)
  const body = JSON.stringify({ schema: 1, document: 'd'.repeat(32), timeUnit: 'relative_monotonic_ms', events: [{ seq: 1, t: 3, kind: 'loadend', g: 1, request: 1 }] })
  const item = { id: await digest(body), body, document: 'd'.repeat(32), through: 1 }
  rows.set(lease.scope, [item])
  objects.set('/data/.memoh/media/' + item.id.slice(0, 2) + '/' + item.id + '.json', new TextEncoder().encode('corrupt').buffer)
  await expect(reconcileDiagnosticBatch(item, lease)).rejects.toThrow('DIAGNOSTIC_UNVERIFIED')
  expect(writes).toBe(0); expect(acks).toBe(0); expect(rows.get(lease.scope)).toEqual([item])
})

it('reopens only the same principal and Bot queue after a fresh login generation', async () => {
  const lease = await openDiagnosticLease(bot)
  const body = JSON.stringify({ schema: 1, document: 'e'.repeat(32), timeUnit: 'relative_monotonic_ms', events: [{ seq: 1, t: 4, kind: 'load', g: 1, request: 1 }] })
  const item = { id: await digest(body), body, document: 'e'.repeat(32), through: 1 }
  rows.set(lease.scope, [item]); lease.retire()
  principal = 'user-b'; localStorage.setItem('token', 'synthetic-b'); notifyAuthSessionCleared('login')
  await resumeDiagnostics(bot); expect(acks).toBe(0); expect(rows.get(lease.scope)).toEqual([item])
  principal = 'user-a'; localStorage.setItem('token', 'synthetic-a-new'); notifyAuthSessionCleared('login')
  await resumeDiagnostics(other); expect(acks).toBe(0)
  await resumeDiagnostics(bot); expect(acks).toBe(1); expect(rows.get(lease.scope)).toEqual([])
})

it('checks identity at dispatch even when no lifecycle notification was emitted', async () => {
  const lease = await openDiagnosticLease(bot)
  client.interceptors.request.use(async request => {
    if (request.url.includes('/download')) localStorage.setItem('token', 'synthetic-new-without-event')
    return request
  })
  await expect(getBotsByBotIdContainerFsDownload({ ...lease.sdkOptions(), path: { bot_id: bot }, query: { path: 'x' }, throwOnError: true })).rejects.toThrow('DIAGNOSTIC_IDENTITY_CHANGED')
  expect(sends.filter(request => request.url.includes('/download'))).toHaveLength(0)
})
