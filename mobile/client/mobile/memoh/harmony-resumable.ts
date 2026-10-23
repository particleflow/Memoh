import { createApiFetch, sdkApiUrl, sdkAuthQuery } from '@/lib/api-client'
import { authSessionGeneration, onAuthSessionCleared } from '@/lib/auth-session'
export interface NativeAttachment { handle: string; name: string; type: string; size: number; hash: string; resumable: boolean }
export const nativeAttachmentsAvailable = typeof window !== 'undefined' && !!(window as Window & { begoniaNative?: unknown }).begoniaNative
async function bridge(command: string, fields: object) {
  const host = window as Window & { begoniaNative?: { request: (raw: string) => Promise<string> } }
  if (!host.begoniaNative) throw new Error('当前设备不支持原生文件传输')
  return JSON.parse(await host.begoniaNative.request(JSON.stringify({ version: 1, command, ...fields })))
}
// All bytes stay in native-owned files. The existing auth response handler
// remains authoritative for 401; stale replies cannot clear a newer login.
export async function nativeAttachmentCommand<T>(command: string, botId: string, scope: string,
  fields: { handle?: string; photos?: boolean } = {}, signal?: AbortSignal): Promise<T> {
  const operation = crypto.randomUUID().replaceAll('-', ''), generation = authSessionGeneration(), token = sdkAuthQuery().token
  const abort = () => { void bridge('upload.abort', { operation }).catch(() => {}) }
  const off = onAuthSessionCleared(() => { void bridge('upload.close', {}).catch(() => {}) })
  const check = () => {
    signal?.throwIfAborted()
    if (!token || generation !== authSessionGeneration() || token !== sdkAuthQuery().token) throw new DOMException('账号已变化', 'AbortError')
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
        throw new Error(reply.error === 'UPLOAD_RESUME_UNAVAILABLE' ? '服务器暂不支持恢复；原件仍保留，请稍后重试'
          : reply.error === 'UPLOAD_LOCAL_QUOTA' ? '待传原件已达上限，请完成或移除已有附件'
          : reply.error === 'UPLOAD_LEGACY_UNCERTAIN' ? '旧服务不支持续传，写入结果未确认，请先核对原件'
          : reply.error === 'UPLOAD_BUSY' ? '正在处理另一份原件，请稍后重试'
          : reply.error === 'UPLOAD_HTTP_413' ? '附件为空或超过服务端限制'
          : '附件未完成：原件校验、授权或网络未通过；可从恢复入口重试')
      }
      return new Response(JSON.stringify(reply.data ?? null), { status: 200 })
    })(sdkApiUrl({ url: '/users/me' }), { headers: { authorization: 'Bearer ' + token } })
    check()
    if (response.status !== 200) throw new Error('附件访问未获授权')
    return await response.json() as T
  } finally { signal?.removeEventListener('abort', abort); off() }
}

if (nativeAttachmentsAvailable) onAuthSessionCleared(() => { void bridge('upload.close', {}).catch(() => {}) })
