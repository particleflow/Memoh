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
import { createApiFetch, sdkApiUrl, sdkAuthQuery } from '@/lib/api-client'
import { normalizeDelegationPath } from './harmony-service-url'
import { saveNativeOriginal } from './harmony-download'

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
  signal?.throwIfAborted()
  const hash = String(attachment.content_hash ?? '')
  const botId = String(attachment.bot_id ?? attachment.metadata?.bot_id ?? '')
  if (!/^[a-f0-9]{64}$/.test(hash) || referenceBot(attachment) !== botId) {
    throw new Error('原件引用不完整')
  }
  let bytes: unknown
  let response: Response
  const scope = attachment.metadata?.delegation_url
  if (scope !== undefined) {
    const path = normalizeDelegationPath(scope as string, hash)
    const token = sdkAuthQuery().token
    if (!token) throw new Error('原件授权不可用')
    response = await createApiFetch()(sdkApiUrl({ url: path }), {
      method: 'GET', signal, redirect: 'error', headers: { authorization: 'Bearer ' + token },
    })
    if (response.status !== 200) throw new Error('委派原件访问失败')
    bytes = await response.arrayBuffer()
  } else {
    const result = await getBotsByBotIdMediaByContentHash({
      path: { bot_id: botId, content_hash: hash }, signal, parseAs: 'arrayBuffer', throwOnError: true })
    bytes = result.data
    response = result.response
  }
  if (response.status !== 200) throw new Error('原件响应不完整')
  if (!(bytes instanceof ArrayBuffer) || (typeof attachment.size === 'number' && bytes.byteLength !== attachment.size)
    || await digest(bytes) !== hash) throw new Error('原件校验未通过')
  signal?.throwIfAborted()
  return new Blob([bytes], { type: attachment.mime || 'application/octet-stream' })
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
  progress?: (value: UploadProgress) => void): Promise<ChatAttachment & UIAttachment> {
  await uploadLimit(botId, [file], signal)
  return prepareOriginal(botId, file, signal, progress)
}

async function prepareOriginal(botId: string, file: File, signal?: AbortSignal,
  progress?: (value: UploadProgress) => void): Promise<ChatAttachment & UIAttachment> {
  signal?.throwIfAborted()
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
export function prepareSubmission(botId: string, selectedFiles: File[], voice?: VoiceSubmission) {
  const files = [...selectedFiles]
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
  return {
    files,
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
      await uploadLimit(botId, files, signal)
      const ready: (ChatAttachment & UIAttachment)[] = []
      // One submission prepares in order; no global/background queue.
      for (const [index, file] of files.entries()) {
        ready.push(await prepareOriginal(botId, file, signal, value => {
          const local = attachments[index]
          if (local?.metadata) Object.assign(local.metadata, { harmony_transfer: value.phase,
            ...(value.loaded == null ? {} : { harmony_upload_wire_loaded: value.loaded }),
            ...(value.total == null ? {} : { harmony_upload_wire_total: value.total }) })
        }))
      }
      for (const attachment of ready) {
        attachment.metadata = { ...attachment.metadata, harmony_transfer: 'ready' }
        if (voice && attachment.name === '录音-' + voice.capture.handle + '.wav') {
          attachment.metadata = { ...attachment.metadata, voice_handle: voice.capture.handle,
            voice_duration_ms: voice.capture.durationMs, voice_peaks: voice.capture.peaks }
        }
      }
      signal.throwIfAborted()
      return ready
    })().catch(error => {
      if (error instanceof UnknownWrite) throw error
      if (signal.aborted) throw new Error('附件传输已取消')
      throw error
    }) : undefined,
  }
}
