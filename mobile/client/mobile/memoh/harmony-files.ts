import {
  postBotsByBotIdMedia,
  getBotsByBotIdMediaLimits,
  getBotsByBotIdMediaByContentHash,
  getBotsByBotIdContainerFsDownload,
  getBotsByBotIdContainerFsRead,
} from '@memohai/sdk'
import type { ChatAttachment, UIAttachment } from '@/composables/api/useChat'
import type { VoiceSubmission } from './harmony-voice'
import { shallowReactive, toRaw } from 'vue'
import { client } from '@memohai/sdk/client'
import { imageTimingHandle, imageTimingFetch, imageTimingMark, imageTimingResource, imageTimingBlob } from './harmony-image-timing'
import { createApiFetch, sdkApiUrl, sdkAuthQuery } from '@/lib/api-client'
import { diagnosticTransfer } from './harmony-diagnostics'
import { normalizeDelegationPath } from './harmony-service-url'
import { saveNativeOriginal } from './harmony-download'
import { nativeAttachmentCommand, stageNativeFile, forgetNativeFile, type NativeAttachment, type NativeUploadReceipt } from './harmony-resumable'

const digest = async (bytes: ArrayBuffer) => Array.from(
  new Uint8Array(await crypto.subtle.digest('SHA-256', bytes)),
  byte => byte.toString(16).padStart(2, '0'),
).join('')

type UploadProgress = { phase: string; loaded?: number; total?: number }

export class UnknownWrite extends Error {
  constructor() {
    super('附件写入结果尚未确认；再次发送只会核验原件，不会自动重传')
    this.name = 'UnknownWrite'
  }
}

// This is foreground uncertainty tracking, not a durable background journal.
// Scope by Bot AND hash: a File object must never carry another Bot's state.
const uncertainWrites = new Set<string>()
const activeWrites = new Set<string>()
const sourceFiles = new WeakMap<object, File>()
export function selectedAttachmentFile(attachment: ChatAttachment): File | undefined {
  return sourceFiles.get(toRaw(attachment))
}
const mediaType = (mime: string) => mime.startsWith('image/') ? 'image'
  : mime.startsWith('audio/') ? 'audio' : mime.startsWith('video/') ? 'video' : 'file'

function referenceBot(attachment: UIAttachment): string {
  const direct = attachment.bot_id
  const meta = attachment.metadata?.bot_id
  if (direct && meta && direct !== meta) throw new Error('附件 Bot 身份不一致')
  const bot = direct ?? meta
  if (typeof bot !== 'string' || !bot.trim()) throw new Error('原件 Bot 引用不完整')
  return bot
}

export async function verifyAttachmentReferences(botId: string, attachments: ChatAttachment[], signal: AbortSignal) {
  for (const attachment of attachments) {
    const ui = attachment as UIAttachment
    if ((ui.bot_id || ui.metadata?.bot_id) && referenceBot(ui) !== botId) throw new Error('附件属于其他 Bot')
    await originalBlob({ ...ui, bot_id: botId }, signal)
  }
  signal.throwIfAborted()
}

// Existing phone send gate, kept with its byte/reference owner rather than growing the shared store.
export async function prepareAttachmentsForMessage(botId: string, attachments: ChatAttachment[] | undefined,
  prepare: ((signal: AbortSignal) => Promise<ChatAttachment[] | undefined>) | undefined,
  signal: AbortSignal): Promise<ChatAttachment[] | undefined> {
  try {
    if (prepare) attachments = await prepare(signal)
    else await verifyAttachmentReferences(botId, attachments ?? [], signal)
    signal.throwIfAborted()
    return attachments
  } catch (error) {
    if (error instanceof Error && error.name === 'AbortError') throw new Error('附件传输已取消', { cause: error })
    throw error
  }
}

// Only the current wire contract goes to the model. Local files, progress and
// native handles remain UI state and cannot be serialized into the WS payload.
export function attachmentForWire(attachment: ChatAttachment): ChatAttachment {
  return { type: attachment.type, content_hash: attachment.content_hash,
    mime: attachment.mime, name: attachment.name, size: attachment.size }
}

// The SDK still constructs the authenticated Request. XHR reports multipart
// wire bytes (including the form envelope), never an estimated file-byte count.
function uploadFetch(progress: ((value: UploadProgress) => void) | undefined, attempted: () => void): typeof fetch {
  return async (input, init) => {
    const request = new Request(input, init)
    request.signal.throwIfAborted()
    const body = await request.blob()
    request.signal.throwIfAborted()
    return new Promise<Response>((resolve, reject) => {
      const xhr = new XMLHttpRequest()
      const abort = () => xhr.abort()
      const finish = () => request.signal.removeEventListener('abort', abort)
      xhr.open(request.method, request.url)
      xhr.responseType = 'arraybuffer'
      xhr.withCredentials = request.credentials === 'include'
      request.headers.forEach((value, name) => xhr.setRequestHeader(name, value))
      xhr.upload.onprogress = event => progress?.({ phase: 'uploading', loaded: event.loaded,
        total: event.lengthComputable ? event.total : undefined })
      xhr.onload = () => {
        finish()
        const headers = new Headers()
        for (const line of xhr.getAllResponseHeaders().trim().split(/[\r\n]+/)) {
          const colon = line.indexOf(':')
          if (colon > 0) headers.append(line.slice(0, colon), line.slice(colon + 1).trim())
        }
        try {
          const response = new Response([204, 205, 304].includes(xhr.status) ? null : xhr.response,
            { status: xhr.status, statusText: xhr.statusText, headers })
          Object.defineProperties(response, { url: { value: xhr.responseURL },
            redirected: { value: xhr.responseURL !== request.url } })
          resolve(response)
        } catch (error) { reject(error) }
      }
      xhr.onerror = () => { finish(); reject(new TypeError('附件上传连接失败')) }
      xhr.onabort = () => { finish(); reject(new DOMException('附件传输已取消', 'AbortError')) }
      request.signal.addEventListener('abort', abort, { once: true })
      if (request.signal.aborted) { finish(); reject(request.signal.reason); return }
      attempted()
      xhr.send(body)
    })
  }
}

export async function originalBlob(attachment: UIAttachment, signal?: AbortSignal): Promise<Blob> {
  // Observability must never replace the original transfer/validation outcome.
  // Keep only fixed local failure phases, never the diagnostic Error or payload.
  const diagnosticFailure = (phase: 'INIT' | 'EVENT') => {
    try { console.warn(`GARNET_DIAGNOSTIC_${phase}_FAILED`) } catch { /* Logging is also optional. */ }
  }
  let trace: ReturnType<typeof diagnosticTransfer> = () => {}
  try {
    const observer = diagnosticTransfer(referenceBot(attachment))
    let disabled = false
    trace = (...args) => {
      if (disabled) return
      try { observer(...args) } catch { disabled = true; diagnosticFailure('EVENT') }
    }
  } catch { diagnosticFailure('INIT') }
  trace('send')
  try {
    const blob = await readOriginalBlob(attachment, signal, trace)
    return blob
  } catch (error) {
    trace(signal?.aborted ? 'abort' : 'error')
    throw error
  } finally { trace('loadend') }
}

async function readOriginalBlob(attachment: UIAttachment, signal: AbortSignal | undefined,
  trace: ReturnType<typeof diagnosticTransfer>): Promise<Blob> {
  signal?.throwIfAborted()
  const timing = imageTimingHandle(attachment)
  const hash = String(attachment.content_hash ?? '')
  const botId = String(attachment.bot_id ?? attachment.metadata?.bot_id ?? '')
  if (!/^[a-f0-9]{64}$/.test(hash) || referenceBot(attachment) !== botId) {
    throw new Error('原件引用不完整')
  }
  let bytes: unknown
  let response: Response
  const observeHeaders = (source: typeof fetch): typeof fetch => {
    const originalFetch = imageTimingFetch(timing, source)
    return async (input, init) => {
      const result = await originalFetch(input, init)
      // Response headers are visible at this approved fetch boundary, before
      // SDK body parsing. This is not a wire-header or first-body-byte clock.
      trace('headers', result.status)
      return result
    }
  }
  const scope = attachment.metadata?.delegation_url
  if (scope !== undefined) {
    const path = normalizeDelegationPath(scope as string, hash)
    const token = sdkAuthQuery().token
    if (!token) throw new Error('原件授权不可用')
    response = await observeHeaders(createApiFetch())(sdkApiUrl({ url: path }), {
      method: 'GET', signal, redirect: 'error', headers: { authorization: 'Bearer ' + token },
    })
    if (response.status !== 200) throw new Error('委派原件访问失败')
    bytes = await response.arrayBuffer()
  } else {
    const result = await getBotsByBotIdMediaByContentHash({
      path: { bot_id: botId, content_hash: hash }, signal, parseAs: 'arrayBuffer', throwOnError: true,
      fetch: observeHeaders(client.getConfig().fetch ?? globalThis.fetch) })
    bytes = result.data
    response = result.response
  }
  if (response.status !== 200) throw new Error('原件响应不完整')
  // Complete decoded body is now available to this reader; validation and Blob
  // work have not completed. This duration alone is not public-network throughput.
  if (bytes instanceof ArrayBuffer) trace('load', response.status, bytes.byteLength)
  imageTimingMark(timing, 'body', bytes instanceof ArrayBuffer ? bytes.byteLength : undefined)
  imageTimingResource(timing, response.url)
  if (!(bytes instanceof ArrayBuffer) || (typeof attachment.size === 'number' && bytes.byteLength !== attachment.size)) throw new Error('原件校验未通过')
  imageTimingMark(timing, 'hashStart')
  const verifiedHash = await digest(bytes)
  imageTimingMark(timing, 'hashEnd')
  if (verifiedHash !== hash) throw new Error('原件校验未通过')
  signal?.throwIfAborted()
  trace('validation', undefined, bytes.byteLength)
  const blob = new Blob([bytes], { type: attachment.mime || 'application/octet-stream' })
  imageTimingBlob(timing, blob)
  return blob
}

function saveBlob(blob: Blob, name: string): void {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = name || 'file'
  link.click()
  setTimeout(() => URL.revokeObjectURL(url), 60_000)
}

export async function saveOriginal(attachment: UIAttachment): Promise<void> {
  if ((window as Window & { begoniaNative?: unknown }).begoniaNative) {
    const botId = referenceBot(attachment)
    const hash = String(attachment.content_hash ?? '')
    const scope = attachment.metadata?.delegation_url
    if (scope !== undefined) normalizeDelegationPath(scope as string, hash)
    if (!Number.isSafeInteger(attachment.size) || (attachment.size ?? 0) <= 0) throw new Error('原件大小不完整')
    await saveNativeOriginal(botId, hash, attachment.size!, attachment.name || 'file', undefined, scope as string | undefined)
    return
  }
  saveBlob(await originalBlob(attachment), attachment.name || 'file')
}

// Keep the official file-tree action and Bot-scoped SDK authorization.
export async function saveWorkspaceFile(botId: string, path: string, name: string, size?: number): Promise<void> {
  if (!botId || !path.startsWith('/') || /[\\\x00-\x1f]/.test(path)
    || path.slice(1).split('/').some(part => !part || part === '.' || part === '..')) {
    throw new Error('原件路径不完整')
  }
  const match = /^\/data\/\.memoh\/media\/([a-f0-9]{2}\/([a-f0-9]{64})\.[a-z0-9]{1,12})$/.exec(path)
  if (match) {
    await saveOriginal({ type: 'file', name, size, content_hash: match[2],
      metadata: { bot_id: botId, storage_key: match[1] } })
    return
  }
  // FSRead computes revision from the raw server bytes; its text is not used.
  const { data: original } = await getBotsByBotIdContainerFsRead({ path: { bot_id: botId },
    query: { path }, throwOnError: true })
  const revision = /^sha256:([a-f0-9]{64})$/.exec(original.revision ?? '')
  if (original.path !== path || !revision || typeof original.size !== 'number'
    || !Number.isSafeInteger(original.size) || original.size < 0) {
    throw new Error('原件引用不完整')
  }
  const { data: bytes } = await getBotsByBotIdContainerFsDownload({ path: { bot_id: botId },
    query: { path }, parseAs: 'arrayBuffer', throwOnError: true })
  if (!(bytes instanceof ArrayBuffer) || bytes.byteLength !== original.size || await digest(bytes) !== revision[1]) {
    throw new Error('原件校验未通过')
  }
  saveBlob(new Blob([bytes], { type: 'application/octet-stream' }), name)
}

// The current media endpoint persists originals independently of Bot workspaces.
async function uploadLimit(botId: string, files: File[], signal?: AbortSignal): Promise<void> {
  if (!botId.trim()) throw new Error('原件 Bot 引用不完整')
  const { data } = await getBotsByBotIdMediaLimits({ path: { bot_id: botId }, signal, throwOnError: true })
  signal?.throwIfAborted()
  const limit = data.max_upload_bytes
  if (!Number.isSafeInteger(limit) || limit <= 0) throw new Error('无法确认服务端上传限制')
  for (const file of files) {
    if (!file.size || file.size > limit) throw new Error('附件为空或超过服务端上传限制')
  }
}

export async function prepareAttachment(botId: string, file: File, signal?: AbortSignal,
  progress?: (value: UploadProgress) => void, scope?: string): Promise<ChatAttachment & UIAttachment> {
  if (!(window as Window & { begoniaNative?: unknown }).begoniaNative) await uploadLimit(botId, [file], signal)
  return prepareOriginal(botId, file, signal, progress, scope)
}

async function prepareOriginal(botId: string, file: File, signal?: AbortSignal,
  progress?: (value: UploadProgress) => void, scope?: string,
  onStaged?: (file: NativeAttachment) => void): Promise<ChatAttachment & UIAttachment> {
  signal?.throwIfAborted()
  if ((window as Window & { begoniaNative?: unknown }).begoniaNative) {
    if (!scope) throw new Error('原件会话范围不完整')
    progress?.({ phase: 'checking' })
    const staged = await stageNativeFile(botId, scope, file, signal)
    onStaged?.(staged)
    progress?.({ phase: 'uploading' })
    const receipt = await nativeAttachmentCommand<NativeUploadReceipt>('upload.run', botId, staged.scope, { handle: staged.handle }, signal)
    signal?.throwIfAborted()
    if (!receipt || receipt.content_hash !== staged.hash || receipt.size_bytes !== staged.size
      || typeof receipt.mime !== 'string' || !receipt.mime.trim()) throw new Error('服务端未确认完整附件')
    const attachment: ChatAttachment & UIAttachment = { type: mediaType(receipt.mime), name: file.name,
      mime: receipt.mime, size: receipt.size_bytes, content_hash: receipt.content_hash, bot_id: botId, metadata: { bot_id: botId } }
    sourceFiles.set(attachment, file)
    return attachment
  }
  const hash = await digest(await file.arrayBuffer())
  signal?.throwIfAborted()
  const identity = JSON.stringify([botId, hash])
  const attachment: ChatAttachment & UIAttachment = {
    type: mediaType(file.type), name: file.name, mime: file.type || 'application/octet-stream',
    size: file.size, content_hash: hash, bot_id: botId, metadata: { bot_id: botId },
  }
  sourceFiles.set(attachment, file)
  progress?.({ phase: 'checking' })
  const existing = await getBotsByBotIdMediaByContentHash({
    path: { bot_id: botId, content_hash: hash }, signal, parseAs: 'arrayBuffer', throwOnError: false })
  signal?.throwIfAborted()
  if (existing.response?.status === 200) {
    if (!(existing.data instanceof ArrayBuffer) || existing.data.byteLength !== file.size
      || await digest(existing.data) !== hash) throw new Error('原件完整性校验未通过')
    signal?.throwIfAborted()
    uncertainWrites.delete(identity)
    // Reuse by hash, even when the selected display filename has changed.
    return attachment
  }
  if (existing.response?.status !== 404) throw new Error('原件状态无法确认')
  if (activeWrites.has(identity)) throw new Error('此 Bot 的同一原件正在上传')
  if (uncertainWrites.has(identity)) { progress?.({ phase: 'unknown' }); throw new UnknownWrite() }
  activeWrites.add(identity)
  progress?.({ phase: 'uploading' })
  let writeAttempted = false
  try {
    const { data: written, response } = await postBotsByBotIdMedia({
      path: { bot_id: botId }, body: { file }, signal,
      fetch: createApiFetch(uploadFetch(progress, () => { writeAttempted = true; uncertainWrites.add(identity) })), throwOnError: true,
    })
    signal?.throwIfAborted()
    if (response.status !== 201 || written.content_hash !== hash || written.size_bytes !== file.size
      || typeof written.mime !== 'string' || !written.mime.trim()) throw new Error('服务端未确认完整附件')
    attachment.mime = written.mime
    attachment.type = mediaType(written.mime)
    progress?.({ phase: 'verifying' })
    await originalBlob(attachment, signal)
    signal?.throwIfAborted()
    uncertainWrites.delete(identity)
    return attachment
  } catch (error) {
    if (!writeAttempted) throw error
    progress?.({ phase: 'unknown' })
    throw new UnknownWrite()
  } finally {
    activeWrites.delete(identity)
  }
}

// The official transcript owns the local message. Its existing send path waits
// here for the exact originals before its one native WebSocket submission.
export function prepareSubmission(botId: string, selectedFiles: File[], voice?: VoiceSubmission,
  native?: { scope: string; files: NativeAttachment[] }) {
  const files = [...selectedFiles]
  const nativeFiles = native?.files.map(file => ({ ...file })) ?? []
  const consumableFiles = new Map(nativeFiles.map(file => [file.handle, file]))
  const stagedOriginals = new Map<string, File>()
  let preparation: Promise<ChatAttachment[]> | undefined
  const attachments = files.map<ChatAttachment & UIAttachment>(file => ({
    type: mediaType(file.type),
    name: file.name, mime: file.type, size: file.size,
    metadata: shallowReactive<Record<string, unknown>>({
      harmony_transfer: 'uploading', bot_id: botId,
      ...(file.type.startsWith('audio/') ? { harmony_voice_file: file } : {}) }),
  }))
  if (voice) attachments.push({
    type: 'audio', name: '录音-' + voice.capture.handle + '.wav', mime: 'audio/wav',
    metadata: shallowReactive<Record<string, unknown>>({ harmony_transfer: 'finalizing', voice_handle: voice.capture.handle,
      voice_duration_ms: voice.capture.durationMs, voice_peaks: voice.capture.peaks }),
  })
  for (const file of nativeFiles) attachments.push({
    type: mediaType(file.type), name: file.name, mime: file.type, size: file.size,
    metadata: shallowReactive<Record<string, unknown>>({ harmony_transfer: 'uploading', bot_id: botId }),
  })
  let completed: ChatAttachment[] | undefined
  return {
    files,
    nativeFiles,
    // Called only after the existing chat store confirms its one send. A ready
    // NAS receipt alone must never consume the recoverable composer original.
    consumeNative: async () => {
      if (!completed) return
      for (const file of consumableFiles.values()) {
        try {
          await nativeAttachmentCommand('upload.consume', botId, file.scope, { handle: file.handle })
          const original = stagedOriginals.get(file.handle)
          if (original) await forgetNativeFile(botId, file.scope, original, file.handle)
        }
        catch { /* Keep the journal if cleanup/authentication fails. */ }
      }
    },
    attachments: attachments.length ? attachments : undefined,
    prepareAttachments: attachments.length ? (signal: AbortSignal) => preparation ??= (async () => {
      signal.throwIfAborted()
      if (voice) {
        const recording = await voice.recording
        signal.throwIfAborted()
        if (!recording) throw new Error('未检测到录音内容')
        voice.capture.durationMs = recording.durationMs
        voice.capture.peaks = recording.peaks
        const local = attachments.find(attachment => attachment.metadata?.voice_handle === voice.capture.handle)
        if (local?.metadata) Object.assign(local.metadata, { harmony_voice_file: recording.file,
          voice_duration_ms: recording.durationMs, voice_peaks: recording.peaks, harmony_transfer: 'uploading' })
        files.push(recording.file)
      }
      if (files.length && !(window as Window & { begoniaNative?: unknown }).begoniaNative) await uploadLimit(botId, files, signal)
      const ready: (ChatAttachment & UIAttachment)[] = []
      // One submission prepares in order; no global/background queue.
      for (const [index, file] of files.entries()) {
        ready.push(await prepareOriginal(botId, file, signal, value => {
          const local = attachments[index]
          if (local?.metadata) Object.assign(local.metadata, { harmony_transfer: value.phase,
            ...(value.loaded == null ? {} : { harmony_upload_wire_loaded: value.loaded }),
            ...(value.total == null ? {} : { harmony_upload_wire_total: value.total }) })
        }, native?.scope, staged => { consumableFiles.set(staged.handle, staged); stagedOriginals.set(staged.handle, file) }))
      }
      for (const file of nativeFiles) {
        if (!native?.scope || !(file.scope === native.scope
          || (native.scope.startsWith('draft:') && file.scope.startsWith('draft:')))) throw new Error('原件会话范围不一致')
        const receipt = await nativeAttachmentCommand<NativeUploadReceipt>('upload.run', botId, file.scope, { handle: file.handle }, signal)
        signal.throwIfAborted()
        if (!receipt || receipt.content_hash !== file.hash || receipt.size_bytes !== file.size
          || typeof receipt.mime !== 'string' || !receipt.mime.trim()) throw new Error('服务端未确认完整附件')
        ready.push({ type: mediaType(receipt.mime), name: file.name, mime: receipt.mime,
          size: receipt.size_bytes, content_hash: receipt.content_hash, bot_id: botId, metadata: { bot_id: botId } })
      }
      for (const attachment of ready) {
        attachment.metadata = { ...attachment.metadata, harmony_transfer: 'ready' }
        if (voice && attachment.name === '录音-' + voice.capture.handle + '.wav') {
          attachment.metadata = { ...attachment.metadata, voice_handle: voice.capture.handle,
            voice_duration_ms: voice.capture.durationMs, voice_peaks: voice.capture.peaks }
        }
      }
      signal.throwIfAborted()
      completed = ready
      return ready
    })().catch(error => {
      if (error instanceof UnknownWrite) throw error
      if (signal.aborted) throw new Error('附件传输已取消')
      throw error
    }) : undefined,
  }
}
