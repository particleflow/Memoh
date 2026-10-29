import { createApiFetch, sdkApiUrl, sdkAuthQuery } from '@/lib/api-client'
import { authSessionGeneration, onAuthSessionCleared } from '@/lib/auth-session'
export interface NativeAttachment { handle: string; name: string; type: string; size: number; hash: string; resumable: boolean; scope: string }
export interface NativeUploadReceipt { content_hash: string; size_bytes: number; mime: string }
export interface NativeUploadFailure { version: 1; phase: string; sdkCode: number; at: number; code: string }
const failurePhases = ['owner-read', 'limits-read', 'picker', 'source-metadata', 'source-open', 'snapshot-create',
  'snapshot-copy', 'snapshot-hash', 'source-verify', 'journal-publish', 'journal-dir-sync']
const failureCodes = ['MEDIA_SHA_MISMATCH', 'MEDIA_SIZE_MISMATCH', 'MEDIA_SHORT_READ', 'MEDIA_SHORT_WRITE',
  'UPLOAD_CANCELLED', 'UPLOAD_REQUEST', 'UPLOAD_IDENTITY', 'UPLOAD_LIMIT_INVALID', 'UPLOAD_JOURNAL_SIZE',
  'UPLOAD_JOURNAL_WRITE', 'UPLOAD_RECEIPT_SIZE', 'UPLOAD_HTTP_413', 'VOICE_UNAVAILABLE']
function normalizeUploadFailure(value: unknown, code: unknown): NativeUploadFailure | undefined {
  if (!value || typeof value !== 'object') return
  const failure = value as NativeUploadFailure
  if (failure.version !== 1 || !failurePhases.includes(failure.phase) || !Number.isSafeInteger(failure.sdkCode)
    || failure.sdkCode < 0 || failure.sdkCode > 2147483647 || !Number.isSafeInteger(failure.at) || failure.at <= 0) return
  return { version: 1, phase: failure.phase, sdkCode: failure.sdkCode, at: failure.at,
    code: typeof code === 'string' && failureCodes.includes(code) ? code : 'UNCLASSIFIED' }
}
// An existing error presenter can consume this without displaying raw SDK messages.
// No network, storage, event publication or user action is performed here.
export function nativeUploadFailure(error: unknown): NativeUploadFailure | undefined {
  if (!error || typeof error !== 'object') return
  const value = (error as { nativeFailure?: NativeUploadFailure }).nativeFailure
  return normalizeUploadFailure(value, value?.code)
}
export const nativeAttachmentsAvailable = typeof window !== 'undefined' && !!(window as Window & { begoniaNative?: unknown }).begoniaNative
async function bridge(command: string, fields: object) {
  const host = window as Window & { begoniaNative?: { request: (raw: string) => Promise<string> } }
  if (!host.begoniaNative) throw new Error('当前设备不支持原生文件传输')
  return JSON.parse(await host.begoniaNative.request(JSON.stringify({ version: 1, command, ...fields })))
}
export function closeNativeAttachments() { return bridge('upload.close', {}).then(() => undefined) }

export function validateNativeAttachments(value: unknown, scope: string, recoverDrafts = false): NativeAttachment[] {
  if (!Array.isArray(value) || value.length > 16) throw new Error('原件列表不完整')
  const handles = new Set<string>()
  for (const file of value as NativeAttachment[]) {
    if (!file || !/^[a-f0-9]{32}$/.test(file.handle) || handles.has(file.handle)
      || !/^[a-f0-9]{64}$/.test(file.hash) || !Number.isSafeInteger(file.size) || file.size <= 0
      || typeof file.name !== 'string' || !file.name || typeof file.type !== 'string' || !file.type
      || typeof file.resumable !== 'boolean' || typeof file.scope !== 'string'
      || !(file.scope === scope || (recoverDrafts && scope.startsWith('draft:') && file.scope.startsWith('draft:')))) {
      throw new Error('原件身份或范围不一致')
    }
    handles.add(file.handle)
  }
  return value as NativeAttachment[]
}
// Network uploads read native-owned files. Local File intake uses bounded chunks below.
// The existing auth response handler
// remains authoritative for 401; stale replies cannot clear a newer login.
export async function nativeAttachmentCommand<T>(command: string, botId: string, scope: string,
  fields: { handle?: string; photos?: boolean; recoverDrafts?: boolean; name?: string; type?: string; size?: number; offset?: number; data?: string } = {}, signal?: AbortSignal,
  operation = crypto.randomUUID().replaceAll('-', '')): Promise<T> {
  const generation = authSessionGeneration(), token = sdkAuthQuery().token
  const abort = () => { void bridge('upload.pause', { operation }).catch(() => {}) }
  const off = onAuthSessionCleared(() => { void bridge('upload.close', {}).catch(() => {}) })
  const check = () => {
    signal?.throwIfAborted()
    if (!token || generation !== authSessionGeneration() || token !== sdkAuthQuery().token) {
      void closeNativeAttachments().catch(() => {})
      throw new DOMException('账号已变化', 'AbortError')
    }
  }
  signal?.addEventListener('abort', abort, { once: true })
  try {
    check()
    const response = await createApiFetch(async () => {
      const reply = await bridge(command, { operation, botId, scope, authorization: 'Bearer ' + token, ...fields })
      check()
      if (!reply.ok) {
        const status = /^UPLOAD_HTTP_(401|403)$/.exec(reply.error ?? '')
        if (status) return new Response(null, { status: Number(status[1]) })
        const error = new Error(reply.error === 'UPLOAD_RESUME_UNAVAILABLE' ? '服务器暂不支持恢复；原件仍保留，请稍后重试'
          : reply.error === 'UPLOAD_LEGACY_UNCERTAIN' ? '旧服务不支持续传，写入结果未确认，请先核对原件'
          : reply.error === 'UPLOAD_CREATE_UNCERTAIN' ? '创建回执未确认；原件已保留，需先核对，不能重复创建'
          : reply.error === 'UPLOAD_RECEIPT_UNAVAILABLE' ? '原上传回执不可用；原件和原回执身份已保留，需先核对'
          : reply.error === 'UPLOAD_ORIGINAL_UNAVAILABLE' ? '服务器原件暂不可读取；本机原件已保留，请核对服务端'
          : reply.error === 'UPLOAD_COMPLETE_UNCERTAIN' ? '完成结果未确认；仅核对原回执，不会重复提交'
          : reply.error === 'UPLOAD_BUSY' ? '正在处理另一份原件，请稍后重试'
          : reply.error === 'UPLOAD_HTTP_413' ? '附件为空或超过服务端限制'
          : '附件未完成：原件校验、授权或网络未通过；可从恢复入口重试')
        const failure = normalizeUploadFailure(reply.failure, reply.error)
        if (failure) Object.assign(error, { nativeFailure: failure })
        throw error
      }
      return new Response(JSON.stringify(reply.data ?? null), { status: 200 })
    })(sdkApiUrl({ url: '/users/me' }), { headers: { authorization: 'Bearer ' + token } })
    check()
    if (response.status !== 200) throw new Error('附件访问未获授权')
    return await response.json() as T
  } finally { signal?.removeEventListener('abort', abort); off() }
}

if (nativeAttachmentsAvailable) onAuthSessionCleared(() => { void bridge('upload.close', {}).catch(() => {}) })

// Clipboard, drop and recorded Files enter the same native snapshot/receipt
// path as the picker. Only bounded local-copy chunks cross this existing bridge;
// no file bytes enter the chat or a new network endpoint.
const stagedFiles = new WeakMap<File, Map<string, Promise<NativeAttachment>>>()
const stageKey = (botId: string, scope: string) => JSON.stringify([botId, scope, authSessionGeneration(), sdkApiUrl({ url: '/users/me' })])
export async function forgetNativeFile(botId: string, scope: string, file: File, handle: string): Promise<void> {
  const slots = stagedFiles.get(file), key = stageKey(botId, scope), saved = slots?.get(key)
  if (saved && (await saved).handle === handle && slots?.get(key) === saved) slots.delete(key)
}
export function stageNativeFile(botId: string, scope: string, file: File, signal?: AbortSignal): Promise<NativeAttachment> {
  if (signal?.aborted) return Promise.reject(signal.reason)
  if (!scope || !file.size || !Number.isSafeInteger(file.size)) return Promise.reject(new Error('原件会话或大小不完整'))
  const key = stageKey(botId, scope)
  let slots = stagedFiles.get(file)
  if (!slots) { slots = new Map(); stagedFiles.set(file, slots) }
  const saved = slots.get(key)
  if (saved) return saved
  const operation = crypto.randomUUID().replaceAll('-', '')
  const token = sdkAuthQuery().token, generation = authSessionGeneration()
  const check = () => {
    signal?.throwIfAborted()
    if (!token || token !== sdkAuthQuery().token || generation !== authSessionGeneration()) throw new DOMException('账号已变化', 'AbortError')
  }
  const pending = (async () => {
    let finished = false
    try {
      check()
      const start = await nativeAttachmentCommand<{ handle: string }>('upload.stage.begin', botId, scope,
        { name: file.name, type: file.type || 'application/octet-stream', size: file.size }, signal, operation)
      if (!start || !/^[a-f0-9]{32}$/.test(start.handle)) throw new Error('原件暂存身份不完整')
      for (let offset = 0; offset < file.size;) {
        check()
        const bytes = new Uint8Array(await file.slice(offset, offset + 65536).arrayBuffer())
        check()
        if (bytes.byteLength !== Math.min(65536, file.size - offset)) throw new Error('原件读取不完整')
        let binary = ''
        for (let at = 0; at < bytes.length; at += 8192) binary += String.fromCharCode(...bytes.subarray(at, at + 8192))
        const reply = await nativeAttachmentCommand<{ offset: number }>('upload.stage.append', botId, scope,
          { offset, data: btoa(binary) }, signal, operation)
        if (!reply || reply.offset !== offset + bytes.length) throw new Error('原件暂存位置不一致')
        offset = reply.offset
      }
      const ready = await nativeAttachmentCommand<NativeAttachment>('upload.stage.finish', botId, scope, {}, signal, operation)
      check(); validateNativeAttachments([ready], scope)
      if (ready.handle !== start.handle || ready.size !== file.size) throw new Error('原件暂存身份不一致')
      finished = true; return ready
    } finally {
      if (!finished) await bridge('upload.pause', { operation }).catch(() => {})
    }
  })()
  slots.set(key, pending)
  void pending.catch(() => { if (slots?.get(key) === pending) slots.delete(key) })
  return pending
}
