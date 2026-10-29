import { sha256 } from '@noble/hashes/sha2.js'
import { bytesToHex } from '@noble/hashes/utils.js'
import { client } from '@memohai/sdk/client'
import {
  getUsersMe, getBotsByBotIdMediaLimits,
  postBotsByBotIdMediaUploads, getBotsByBotIdMediaUploadsByUploadId,
  patchBotsByBotIdMediaUploadsByUploadId, postBotsByBotIdMediaUploadsByUploadIdComplete,
  getBotsByBotIdMediaByContentHash, headBotsByBotIdMediaByContentHash,
  getBotsByBotIdSessionsBySessionIdDelegationsByDelegationIdAttachmentsByContentHash,
  headBotsByBotIdSessionsBySessionIdDelegationsByDelegationIdAttachmentsByContentHash,
  type MediaUpload,
  type MediaTransferNetworkPolicy,
} from '@memohai/sdk'
import { sdkApiBaseUrl, sdkAuthQuery, usesPlatformFileRouting } from './api-client'
import { authSessionGeneration } from './auth-session'
import { mediaDispatch, mediaEndpoint } from './media-network'
import {
  listJournals, readJournal, writeJournal, removeJournal, localFile, writeLocalFile, mediaLock,
  type MediaJournal,
} from './media-transfer-storage'

// Bounded working buffers, not a per-file or total admission policy.
const BLOCK = 1024 * 1024
const HASH = /^[a-f0-9]{64}$/
const UUID = '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}'

export class MediaRecoveryError extends Error {
  readonly reason: 'unknown' | 'receiptMissing' | 'sourceMissing' | 'integrity' | 'storage' | 'unsupported'
  constructor(reason: MediaRecoveryError['reason']) {
    super(`Media recovery: ${reason}`)
    this.reason = reason
  }
}

export interface MediaIdentity {
  owner: string
  origin: string
  bot: string
  // Same NAS policy snapshot as native consumers. V1 leaves TCP selection with
  // the platform; V2 maps dispatch only, retaining this logical journal origin.
  networkPolicy?: Readonly<MediaTransferNetworkPolicy>
  // These are request-lifetime guards only and are never persisted.
  check: () => void
  fetch: typeof fetch
}

export async function mediaIdentity(bot: string, signal: AbortSignal): Promise<MediaIdentity> {
  const origin = sdkApiBaseUrl()
  const generation = authSessionGeneration()
  const token = sdkAuthQuery().token
  const route: { physicalOrigin?: string } = {}
  function check() {
    signal.throwIfAborted()
    if (!token || generation !== authSessionGeneration() || token !== sdkAuthQuery().token || origin !== sdkApiBaseUrl()) {
      throw new DOMException('Attachment identity changed', 'AbortError')
    }
  }
  const transport = client.getConfig().fetch ?? globalThis.fetch
  const guardedFetch: typeof fetch = async (input, init) => {
    // The shared interceptor runs before this final dispatch guard. A token
    // change while the SDK prepares a Request must not send the new account's
    // credential or bytes to the old journal's route.
    check()
    const request = input instanceof Request && !init ? input : new Request(input, init)
    const base = new URL(origin)
    const url = new URL(request.url)
    if (request.headers.get('Authorization') !== `Bearer ${token}` || url.origin !== base.origin
      || !url.pathname.startsWith(`${base.pathname.replace(/\/$/, '')}/`)) throw new DOMException('Attachment identity changed', 'AbortError')
    if (!route.physicalOrigin) return await transport(input, { ...init, redirect: 'error' })
    const dispatched = await mediaDispatch(request, route.physicalOrigin)
    check()
    return await transport(dispatched, { redirect: 'error' })
  }
  check()
  const { data } = await getUsersMe({ signal, fetch: guardedFetch, cache: 'no-store', throwOnError: true })
  check()
  if (!data.id) throw new MediaRecoveryError('unknown')
  const physicalOrigin = mediaEndpoint(origin, data.network_policy)
  if (!usesPlatformFileRouting()) route.physicalOrigin = physicalOrigin
  const networkPolicy = data.network_policy ? Object.freeze({ ...data.network_policy, lan_addresses: [...(data.network_policy.lan_addresses ?? [])], ...(data.network_policy.endpoint ? { endpoint: Object.freeze({ ...data.network_policy.endpoint }) } : {}) }) : undefined
  return { owner: data.id, origin, bot, check, fetch: guardedFetch, networkPolicy }
}

function belongs(journal: MediaJournal, identity: MediaIdentity) {
  return journal.owner === identity.owner && journal.origin === identity.origin && journal.bot === identity.bot
}

export async function mediaHash(blob: Blob, check: () => void = () => {}) {
  const digest = sha256.create()
  for (let offset = 0; offset < blob.size; offset += BLOCK) {
    check()
    digest.update(new Uint8Array(await blob.slice(offset, offset + BLOCK).arrayBuffer()))
  }
  check()
  return bytesToHex(digest.digest())
}

function key(parts: unknown[]) {
  return bytesToHex(sha256(new TextEncoder().encode(JSON.stringify(parts))))
}

function checkReceipt(j: MediaJournal, receipt: MediaUpload) {
  if (!receipt.id || (j.receipt && j.receipt.id !== receipt.id)
    || typeof receipt.mime !== 'string' || !receipt.mime.trim()
    || !Number.isFinite(Date.parse(receipt.expires_at)) || receipt.size_bytes !== j.size
    || !Number.isSafeInteger(receipt.offset) || receipt.offset < j.offset || receipt.offset > j.size
    || !['uploading', 'ready'].includes(receipt.state)
    || (j.receipt && receipt.expires_at !== j.receipt.expires_at)
    || (receipt.state === 'ready' && (receipt.offset !== j.size || receipt.content_hash !== j.hash))) {
    throw new MediaRecoveryError('integrity')
  }
}

async function mutationResult<T>(request: () => Promise<T>): Promise<T> {
  try { return await request() } catch (error) {
    // Fetch cannot tell whether a failed request committed. Preserve the
    // checkpoint and present that uncertainty instead of suggesting a new send.
    if (error instanceof TypeError) throw new MediaRecoveryError('unknown')
    throw error
  }
}

// Only the existing two authorization routes are accepted. A delegation denial
// must never turn into an ordinary Bot/hash read, including after process restart.
function mediaPath(bot: string, hash: string, target?: string) {
  if (!HASH.test(hash) || !new RegExp(`^${UUID}$`).test(bot)) throw new MediaRecoveryError('integrity')
  const ordinary = `/bots/${bot}/media/${hash}`
  if (!target || target === ordinary) return { ordinary: { bot_id: bot, content_hash: hash }, target: ordinary }
  const match = new RegExp(`^/bots/(${UUID})/sessions/(${UUID})/delegations/(${UUID})/attachments/([a-f0-9]{64})$`).exec(target)
  if (!match || match[1] !== bot || match[4] !== hash) throw new MediaRecoveryError('integrity')
  return { delegated: { bot_id: bot, session_id: match[2]!, delegation_id: match[3]!, content_hash: hash }, target }
}

async function originalResponse(identity: MediaIdentity, hash: string, target: string | undefined, signal: AbortSignal, range?: [number, number]) {
  identity.check()
  const path = mediaPath(identity.bot, hash, target)
  const options = { signal, fetch: identity.fetch, cache: 'no-store' as const, throwOnError: true as const, parseAs: 'stream' as const }
  const headers = range ? { Range: `bytes=${range[0]}-${range[1]}`, 'If-Range': `"${hash}"` } : undefined
  const result = range
    ? path.delegated
      ? await getBotsByBotIdSessionsBySessionIdDelegationsByDelegationIdAttachmentsByContentHash({ ...options, path: path.delegated, headers })
      : await getBotsByBotIdMediaByContentHash({ ...options, path: path.ordinary!, headers })
    : path.delegated
      ? await headBotsByBotIdSessionsBySessionIdDelegationsByDelegationIdAttachmentsByContentHash({ ...options, path: path.delegated })
      : await headBotsByBotIdMediaByContentHash({ ...options, path: path.ordinary! })
  identity.check()
  if (result.response.headers.get('ETag') !== `"${hash}"`) throw new MediaRecoveryError('integrity')
  return result.response
}

async function originalChunk(identity: MediaIdentity, hash: string, target: string | undefined, size: number, offset: number, signal: AbortSignal) {
  const end = Math.min(offset + BLOCK, size) - 1
  const response = await originalResponse(identity, hash, target, signal, [offset, end])
  if (response.status !== 206 || response.headers.get('Content-Range') !== `bytes ${offset}-${end}/${size}`
    || Number(response.headers.get('Content-Length')) !== end - offset + 1) throw new MediaRecoveryError('integrity')
  const blob = await response.blob()
  identity.check()
  if (blob.size !== end - offset + 1) throw new MediaRecoveryError('integrity')
  return blob
}

async function verifyOriginal(identity: MediaIdentity, journal: MediaJournal, signal: AbortSignal) {
  const head = await originalResponse(identity, journal.hash, undefined, signal)
  if (Number(head.headers.get('Content-Length')) !== journal.size) throw new MediaRecoveryError('integrity')
  const digest = sha256.create()
  for (let offset = 0; offset < journal.size; offset += BLOCK) {
    const bytes = await originalChunk(identity, journal.hash, undefined, journal.size, offset, signal)
    digest.update(new Uint8Array(await bytes.arrayBuffer()))
  }
  identity.check()
  if (bytesToHex(digest.digest()) !== journal.hash) throw new MediaRecoveryError('integrity')
}

const fileJournals = new WeakMap<File, string>()
export function mediaFileJournalKey(file: File) { return fileJournals.get(file) }
export async function uploadMedia(identity: MediaIdentity, file: File, draft: string, chunkBytes: number, signal: AbortSignal) {
  const hash = await mediaHash(file, identity.check)
  const journalKey = key(['upload', identity.owner, identity.origin, identity.bot, draft, file.name, file.size, hash])
  const knownSource = fileJournals.get(file) === journalKey
  fileJournals.set(file, journalKey)
  return await mediaLock(journalKey, signal, async () => {
    identity.check()
    let journal = await readJournal(journalKey)
    if (journal && (!belongs(journal, identity) || journal.kind !== 'upload' || journal.hash !== hash || journal.size !== file.size)) {
      throw new MediaRecoveryError('integrity')
    }
    if (!journal) {
      journal = {
        key: journalKey, kind: 'upload', owner: identity.owner, origin: identity.origin, bot: identity.bot, draft,
        name: file.name, mime: file.type || 'application/octet-stream', hash, size: file.size,
        offset: 0, phase: 'create-unknown', source: false,
      }
      // Durable intent BEFORE Create. A crash even just before sending is
      // conservatively unknown: the protocol has no Create idempotency key.
      await writeJournal(journal)
      try {
        await writeLocalFile(journalKey, file)
        journal.source = true
        await writeJournal(journal)
      } catch {
        // Current File can continue, but recovery must request re-selection.
        journal.source = false
        await writeJournal(journal)
      }
      identity.check()
      const body = { sha256: hash, size_bytes: file.size, mime: journal.mime }
      const { data } = await mutationResult(() => postBotsByBotIdMediaUploads({
        path: { bot_id: identity.bot }, body, signal, fetch: identity.fetch, throwOnError: true,
      }))
      checkReceipt(journal, data)
      journal.receipt = { id: data.id, expires_at: data.expires_at }
      journal.mime = data.mime
      journal.offset = data.offset
      journal.phase = data.state === 'ready' ? 'ready' : 'uploading'
      // Save a late response under its ORIGINAL identity before honoring pause.
      await writeJournal(journal)
    } else {
      journal.dismissed = false
      await writeJournal(journal)
      if (!journal.receipt) throw new MediaRecoveryError('unknown')
      if (!journal.source || !knownSource) {
        try {
          await writeLocalFile(journalKey, file)
          journal.source = true
        } catch { /* Continue with the reselected File, preserving reselect state. */ }
      }
      await writeJournal(journal)
      identity.check()
      const result = await getBotsByBotIdMediaUploadsByUploadId({
        path: { bot_id: identity.bot, upload_id: journal.receipt.id }, signal, fetch: identity.fetch, cache: 'no-store',
      })
      identity.check()
      if (result.response?.status === 404) throw new MediaRecoveryError('receiptMissing')
      if (result.error || !result.data) throw result.error ?? new MediaRecoveryError('unknown')
      checkReceipt(journal, result.data)
      // Complete uncertainty is read-only until the original ID is ready.
      if (journal.phase === 'complete-unknown' && result.data.state !== 'ready') throw new MediaRecoveryError('unknown')
      journal.offset = result.data.offset
      journal.mime = result.data.mime
      journal.phase = result.data.state === 'ready' ? 'ready' : 'uploading'
      await writeJournal(journal)
    }
    identity.check()
    const path = { bot_id: identity.bot, upload_id: journal.receipt!.id }
    while (journal.phase !== 'ready' && journal.offset < journal.size) {
      const body = file.slice(journal.offset, journal.offset + chunkBytes)
      const chunkHash = await mediaHash(body, identity.check)
      journal.phase = 'patch-unknown'
      await writeJournal(journal)
      identity.check()
      const { data } = await mutationResult(() => patchBotsByBotIdMediaUploadsByUploadId({
        path, body, headers: { 'Upload-Offset': journal.offset, 'Upload-Length': body.size, 'Upload-SHA256': chunkHash }, signal, fetch: identity.fetch, throwOnError: true,
      }))
      checkReceipt(journal, data)
      if (data.offset !== journal.offset + body.size) throw new MediaRecoveryError('integrity')
      journal.offset = data.offset
      journal.phase = 'uploading'
      await writeJournal(journal)
      identity.check()
    }
    if (journal.phase !== 'ready') {
      journal.phase = 'complete-unknown'
      await writeJournal(journal)
      identity.check()
      const { data } = await mutationResult(() => postBotsByBotIdMediaUploadsByUploadIdComplete({ path, signal, fetch: identity.fetch, throwOnError: true }))
      checkReceipt(journal, data)
      if (data.state !== 'ready') throw new MediaRecoveryError('integrity')
      journal.mime = data.mime
      journal.phase = 'ready'
      await writeJournal(journal)
    }
    identity.check()
    await verifyOriginal(identity, journal, signal)
    return { key: journalKey, hash, mime: journal.mime, size: journal.size }
  })
}

export async function uploadPolicy(identity: MediaIdentity, files: File[], signal: AbortSignal) {
  const { data } = await getBotsByBotIdMediaLimits({ path: { bot_id: identity.bot }, signal, fetch: identity.fetch, cache: 'no-store', throwOnError: true })
  identity.check()
  if (!Number.isSafeInteger(data.max_upload_bytes) || data.max_upload_bytes <= 0) throw new MediaRecoveryError('integrity')
  if (files.some(f => f.size > data.max_upload_bytes)) return { limit: data.max_upload_bytes, chunk: 0 }
  if (!data.resumable_enabled) throw new MediaRecoveryError('unsupported')
  if (!Number.isSafeInteger(data.chunk_max_bytes) || data.chunk_max_bytes! <= 0) throw new MediaRecoveryError('integrity')
  return { limit: data.max_upload_bytes, chunk: Math.min(BLOCK, data.chunk_max_bytes!) }
}

export async function restoreMediaUploads(bot: string, draft: string, signal: AbortSignal) {
  const identity = await mediaIdentity(bot, signal)
  await getBotsByBotIdMediaLimits({ path: { bot_id: bot }, signal, fetch: identity.fetch, cache: 'no-store', throwOnError: true })
  identity.check()
  const files: File[] = []
  const missing: string[] = []
  let unknown = false
  for (const journal of await listJournals()) {
    identity.check()
    if (!belongs(journal, identity) || journal.kind !== 'upload' || journal.draft !== draft || journal.dismissed) continue
    unknown ||= !journal.receipt
    try {
      if (!journal.source) throw new MediaRecoveryError('sourceMissing')
      const source = await localFile(journal.key)
      if (source.size !== journal.size || await mediaHash(source, identity.check) !== journal.hash) throw new MediaRecoveryError('integrity')
      const file = new File([source], journal.name, { type: journal.mime })
      fileJournals.set(file, journal.key)
      files.push(file)
    } catch (error) {
      identity.check()
      missing.push(journal.name)
      if (error instanceof DOMException && error.name === 'AbortError') throw error
    }
  }
  identity.check()
  return { files, missing, unknown }
}

export async function restoreMediaUploadScopes(bot: string, signal: AbortSignal) {
  const identity = await mediaIdentity(bot, signal)
  await getBotsByBotIdMediaLimits({ path: { bot_id: bot }, signal, fetch: identity.fetch, cache: 'no-store', throwOnError: true })
  const scopes = new Map<string, { tab: string, session: string | null }>()
  for (const journal of await listJournals()) {
    identity.check()
    if (!belongs(journal, identity) || journal.kind !== 'upload' || journal.dismissed) continue
    try {
      const scope: unknown = JSON.parse(journal.draft)
      if (!Array.isArray(scope) || scope.length !== 2 || typeof scope[0] !== 'string' || typeof scope[1] !== 'string') continue
      if (!scope[0].startsWith(`${bot}:`) || (scope[1] && !new RegExp(`^${UUID}$`).test(scope[1]))) continue
      const tab = scope[0].slice(bot.length + 1)
      if (!/^chat(?::[a-zA-Z0-9_-]+)*$/.test(tab)) continue
      scopes.set(journal.draft, { tab, session: scope[1] || null })
    } catch { /* Unknown historical scope formats stay preserved, never rebound. */ }
  }
  identity.check()
  return { scopes: [...scopes.values()], check: identity.check }
}

export async function forgetMediaFile(file: File) {
  const journalKey = fileJournals.get(file)
  if (!journalKey) return
  await mediaLock(journalKey, new AbortController().signal, async () => {
    const journal = await readJournal(journalKey)
    // Removing a tray card is not permission to recreate an uncertain upload.
    if (journal) await writeJournal({ ...journal, dismissed: true })
  })
  fileJournals.delete(file)
}

export async function acknowledgeMedia(keys: string[]) {
  for (const journalKey of keys) {
    await mediaLock(journalKey, new AbortController().signal, () => removeJournal(journalKey))
  }
}

export async function downloadMedia(bot: string, hash: string, expectedSize: number | undefined, target: string | undefined, signal: AbortSignal) {
  const identity = await mediaIdentity(bot, signal)
  const exact = mediaPath(bot, hash, target).target
  // Recheck current authorization, even for a locally complete download.
  const head = await originalResponse(identity, hash, exact, signal)
  const size = Number(head.headers.get('Content-Length'))
  if (!head.headers.has('Content-Length') || !Number.isSafeInteger(size) || size < 0
    || (expectedSize !== undefined && size !== expectedSize)) throw new MediaRecoveryError('integrity')
  const journalKey = key(['download', identity.owner, identity.origin, bot, exact, hash, size])
  return await mediaLock(journalKey, signal, async () => {
    identity.check()
    let journal = await readJournal(journalKey)
    if (!journal) {
      journal = { key: journalKey, kind: 'download', owner: identity.owner, origin: identity.origin, bot, draft: '', name: hash,
        mime: head.headers.get('Content-Type') || 'application/octet-stream', hash, size, offset: 0, phase: 'downloading', target: exact }
      await writeLocalFile(journalKey, new Blob())
      await writeJournal(journal)
    }
    if (!belongs(journal, identity) || journal.kind !== 'download' || journal.target !== exact || journal.size !== size
      || !Number.isSafeInteger(journal.offset) || journal.offset < 0 || journal.offset > size) throw new MediaRecoveryError('integrity')
    const saved = await localFile(journalKey)
    if (saved.size < journal.offset) throw new MediaRecoveryError('sourceMissing')
    while (journal.offset < size) {
      const blob = await originalChunk(identity, hash, exact, size, journal.offset, signal)
      await writeLocalFile(journalKey, blob, journal.offset)
      identity.check()
      journal.offset += blob.size
      await writeJournal(journal)
    }
    const result = await localFile(journalKey)
    if (result.size !== size || await mediaHash(result, identity.check) !== hash) {
      journal.offset = 0
      journal.phase = 'downloading'
      await writeJournal(journal)
      throw new MediaRecoveryError('integrity')
    }
    journal.phase = 'ready'
    await writeJournal(journal)
    identity.check()
    return result
  })
}

export async function saveMediaDownload(bot: string, hash: string, size: number | undefined, target: string | undefined, name: string, signal: AbortSignal) {
  const generation = authSessionGeneration()
  const token = sdkAuthQuery().token
  const origin = sdkApiBaseUrl()
  const file = await downloadMedia(bot, hash, size, target, signal)
  signal.throwIfAborted()
  if (generation !== authSessionGeneration() || token !== sdkAuthQuery().token || origin !== sdkApiBaseUrl()) throw new DOMException('Attachment identity changed', 'AbortError')
  const url = URL.createObjectURL(file)
  try {
    const link = document.createElement('a')
    link.href = url
    link.download = name || hash
    link.click()
  } finally { setTimeout(() => URL.revokeObjectURL(url), 60_000) }
}
