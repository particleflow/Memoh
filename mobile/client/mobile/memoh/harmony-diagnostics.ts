import { getBotsByBotIdContainerFsDownload, postBotsByBotIdContainerFsUpload } from '@memohai/sdk'
import { openDiagnosticLease, type DiagnosticLease } from '@/lib/api-client'
import { onAuthSessionCleared } from '@/lib/auth-session'

// Logical read phases: fetch Response availability, complete decoded body,
// successful verification, and final consumer outcome. No wire first-byte or
// per-chunk progress observation is implied by this event vocabulary.
type Kind = 'send' | 'headers' | 'load' | 'error' | 'abort' | 'loadend' | 'validation'
interface TraceEvent { seq: number, t: number, kind: Kind, g: number, request: number | null, status?: number, loaded?: number, requestTag?: string, check?: 'body', ok?: boolean }
// The only permitted response correlation field. Normal Fetch visibility/CORS
// applies; a missing, combined duplicate, invalid or unreadable value is unknown.
export function responseRequestTag(response: Response): string | undefined {
  try {
    const value = response.headers.get('X-NAS-Request-ID')
    return value && value.length === 32 && /^[a-f0-9]{32}$/.test(value) ? value : undefined
  } catch { return undefined }
}
interface Batch { id: string, document: string, through: number, body: string, writeAttempted?: boolean }
// Only our retained unknown-write state may defer one batch without stopping
// its siblings. Never classify arbitrary SDK/auth/integrity errors by message.
class DiagnosticWriteUnconfirmed extends Error {
  constructor() { super('DIAGNOSTIC_WRITE_UNCONFIRMED') }
}
function validateBatch(batch: Batch): void {
  const body = JSON.parse(batch.body) as { schema: number, document: string, timeUnit: string, events: Array<Record<string, unknown>> }
  const number = (value: unknown) => typeof value === 'number' && Number.isFinite(value) && value >= 0
  if (!body || JSON.stringify(body) !== batch.body || Object.keys(body).some(key => !['schema', 'document', 'timeUnit', 'events'].includes(key)) ||
    body.schema !== 1 || !/^[a-f0-9]{32}$/.test(body.document) || body.document !== batch.document || body.timeUnit !== 'relative_monotonic_ms' || !Array.isArray(body.events) || !body.events.length) throw new Error('DIAGNOSTIC_BODY')
  let previous = 0
  for (const event of body.events) {
    if (!event || Object.keys(event).some(key => !['seq', 't', 'kind', 'g', 'request', 'method', 'status', 'loaded', 'total', 'lengthComputable', 'check', 'ok', 'reason', 'requestTag'].includes(key)) ||
      typeof event.seq !== 'number' || !Number.isSafeInteger(event.seq) || event.seq <= previous || !number(event.t) ||
      typeof event.g !== 'number' || !Number.isSafeInteger(event.g) || event.g < 0 ||
      typeof event.kind !== 'string' || !['generation', 'stop_requested', 'run_failed', 'send', 'headers', 'progress', 'load', 'error', 'timeout', 'abort', 'loadend', 'validation'].includes(event.kind) ||
      event.request !== null && (typeof event.request !== 'number' || !Number.isSafeInteger(event.request) || event.request < 0) ||
      event.method !== undefined && !['HEAD', 'GET'].includes(String(event.method)) ||
      ['status', 'loaded', 'total'].some(key => event[key] !== undefined && !number(event[key])) ||
      event.check !== undefined && !['head', 'range_headers', 'body'].includes(String(event.check)) ||
      event.reason !== undefined && event.reason !== 'user_stop' ||
      event.requestTag !== undefined && (event.kind !== 'headers' || typeof event.requestTag !== 'string' || !/^[a-f0-9]{32}$/.test(event.requestTag)) ||
      ['ok', 'lengthComputable'].some(key => event[key] !== undefined && typeof event[key] !== 'boolean')) throw new Error('DIAGNOSTIC_BODY')
    previous = event.seq
  }
  if (previous !== batch.through) throw new Error('DIAGNOSTIC_CURSOR')
}
interface Bridge { request(raw: string): Promise<string> }
const bridge = () => (window as Window & { begoniaNative?: Bridge }).begoniaNative
const hash = async (bytes: Uint8Array) => Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256', new Uint8Array(bytes).buffer)), x => x.toString(16).padStart(2, '0')).join('')
let currentBot = '', serial = Promise.resolve(), flight: Promise<void> | undefined, again = false
const leases = new Set<DiagnosticLease>()

async function journal<T>(command: string, lease: DiagnosticLease, values: object = {}): Promise<T> {
  lease.assertCurrent()
  const native = bridge()
  if (!native) throw new Error('DIAGNOSTIC_BRIDGE_UNAVAILABLE')
  const result = JSON.parse(await native.request(JSON.stringify({ version: 1, command, scope: lease.scope, ...values }))) as { ok?: boolean, data?: T }
  lease.assertCurrent()
  if (!result.ok) throw new Error('DIAGNOSTIC_JOURNAL_FAILED')
  return result.data as T
}

export async function reconcileDiagnosticBatch(batch: Batch, lease: DiagnosticLease): Promise<void> {
  validateBatch(batch)
  const pending = await journal<Batch[]>('diagnostic.pending', lease)
  const retained = pending.find(item => item.id === batch.id && item.body === batch.body && item.document === batch.document && item.through === batch.through)
  if (!retained) throw new Error('DIAGNOSTIC_NOT_PENDING')
  if (retained.writeAttempted !== undefined && typeof retained.writeAttempted !== 'boolean') throw new Error('DIAGNOSTIC_JOURNAL_FAILED')
  const bytes = new TextEncoder().encode(batch.body)
  if (!/^[a-f0-9]{64}$/.test(batch.id) || await hash(bytes) !== batch.id) throw new Error('DIAGNOSTIC_DIGEST')
  lease.assertCurrent()
  const path = '/data/.memoh/media/' + batch.id.slice(0, 2) + '/' + batch.id + '.json'
  const read = () => getBotsByBotIdContainerFsDownload({ ...lease.sdkOptions(), path: { bot_id: lease.botId },
    query: { path }, parseAs: 'arrayBuffer', throwOnError: false })
  // A missing object permits only a first attempt. After any admitted attempt,
  // 404 cannot establish that a previous unknown write did not happen.
  let result = await read(); lease.assertCurrent()
  if (result.response?.status === 404) {
    if (retained.writeAttempted === true) throw new DiagnosticWriteUnconfirmed()
    // Reuse the original PUT row/command: persist the sticky attempt phase
    // before SDK dispatch. Its atomic claim also excludes concurrent POSTs.
    const claimed = await journal<boolean>('diagnostic.put', lease, { batch: { ...retained, writeAttempted: true } })
    if (claimed === false) throw new DiagnosticWriteUnconfirmed()
    if (claimed !== true) throw new Error('DIAGNOSTIC_JOURNAL_FAILED')
    lease.assertCurrent()
    await postBotsByBotIdContainerFsUpload({ ...lease.sdkOptions(), path: { bot_id: lease.botId },
      body: { path, file: new Blob([new Uint8Array(bytes).buffer], { type: 'application/json' }) }, throwOnError: true })
    lease.assertCurrent(); result = await read(); lease.assertCurrent()
  }
  if (result.response?.status !== 200 || !(result.data instanceof ArrayBuffer) || result.data.byteLength !== bytes.byteLength ||
    await hash(new Uint8Array(result.data)) !== batch.id) throw new Error('DIAGNOSTIC_UNVERIFIED')
  lease.assertCurrent()
  await journal('diagnostic.ack', lease, { hash: batch.id, verifiedHash: batch.id, verifiedSize: bytes.byteLength })
}

async function flush(lease: DiagnosticLease): Promise<void> {
  if (flight) { again = true; await flight; lease.assertCurrent(); return flush(lease) }
  const work = (async () => {
    do {
      again = false
      for (const batch of await journal<Batch[]>('diagnostic.pending', lease)) {
        lease.assertCurrent()
        try { await reconcileDiagnosticBatch(batch, lease) } catch (error) {
          if (!(error instanceof DiagnosticWriteUnconfirmed)) throw error
          lease.assertCurrent()
          // Retain this exact object without POST/ACK; other valid batches can
          // progress. A later natural resume can verify and ACK the same object.
        }
      }
    } while (again)
  })()
  flight = work
  try { await work } finally { if (flight === work) flight = undefined }
}

export function suspendDiagnostics(): void { currentBot = ''; for (const lease of leases) lease.retire(); leases.clear() }
export async function resumeDiagnostics(botId = currentBot): Promise<void> {
  if (!bridge() || !/^[a-f0-9-]{36}$/i.test(botId)) return
  currentBot = botId
  const lease = await openDiagnosticLease(botId); leases.add(lease)
  try { await flush(lease) } finally { lease.retire(); leases.delete(lease) }
}

// Only natural formal-client transfers call this sink; it never replays a file,
// HAP, message, model or task. No caller-provided text, names, URLs or IDs enter.
export function diagnosticTransfer(botId: string): (kind: Kind, status?: number, loaded?: number, requestTag?: string) => void {
  if (!bridge()) return () => {}
  currentBot = botId
  const document = crypto.randomUUID().replace(/-/g, ''), start = performance.now(), events: TraceEvent[] = []
  const bound = openDiagnosticLease(botId).then(lease => { leases.add(lease); return lease })
  // Attach immediately: authentication may fail before the next natural event.
  void bound.catch(() => {})
  return (kind, status, loaded, requestTag) => {
    const event: TraceEvent = { seq: events.length + 1, t: Math.max(0, performance.now() - start), kind, g: 1, request: 1 }
    if (typeof status === 'number' && Number.isFinite(status) && status >= 0) event.status = status
    if (typeof loaded === 'number' && Number.isSafeInteger(loaded) && loaded >= 0) event.loaded = loaded
    if (kind === 'headers' && typeof requestTag === 'string' && /^[a-f0-9]{32}$/.test(requestTag)) event.requestTag = requestTag
    if (kind === 'validation') { event.check = 'body'; event.ok = true }
    events.push(event)
    const captured = events.slice()
    const work = serial.then(async () => {
      const lease = await bound
      lease.assertCurrent()
      const cursor = await journal<number>('diagnostic.cursor', lease, { document })
      const remaining = captured.filter(item => item.seq > cursor)
      if (remaining.length) {
        const body = JSON.stringify({ schema: 1, document, timeUnit: 'relative_monotonic_ms', events: remaining })
        const id = await hash(new TextEncoder().encode(body)); lease.assertCurrent()
        await journal('diagnostic.put', lease, { batch: { id, document, through: remaining[remaining.length - 1]!.seq, body } })
      }
    })
    serial = work.catch(() => {})
    void work.then(async () => {
      const lease = await bound
      try { await flush(lease) } finally {
        if (kind === 'loadend') { lease.retire(); leases.delete(lease) }
      }
    }).catch(async () => {
      if (kind === 'loadend') { const lease = await bound.catch(() => undefined); lease?.retire(); if (lease) leases.delete(lease) }
    })
    // A disconnected upload must not block later captures. Persist first, then
    // retain the same original batch for the next ready/online/foreground event.
  }
}

if (typeof window !== 'undefined') {
  onAuthSessionCleared(suspendDiagnostics)
  window.addEventListener('online', () => { void resumeDiagnostics().catch(() => {}) })
  window.addEventListener('garnet:diagnostic-resume', () => { void resumeDiagnostics().catch(() => {}) })
  window.addEventListener('pagehide', suspendDiagnostics)
}
