import { getBotsByBotIdMediaByContentHash } from '@memohai/sdk'
import { createApiFetch, sdkAuthQuery } from '@/lib/api-client'
import { authSessionGeneration, onAuthSessionCleared } from '@/lib/auth-session'
import { toast } from '@felinic/ui'

interface DownloadFields { operation?: string; botId?: string; hash?: string; size?: number; name?: string; url?: string; authorization?: string }
class DownloadHTTPError extends Error {
  readonly status: number
  constructor(status: number) { super('原件访问未获授权'); this.status = status }
}
async function command<T>(name: string, fields: DownloadFields): Promise<T> {
  const host = window as Window & { begoniaNative?: { request: (raw: string) => Promise<string> } }
  if (!host.begoniaNative) throw new Error('当前设备无法使用原生文件保存')
  const reply = JSON.parse(await host.begoniaNative.request(JSON.stringify({ version: 1, command: name, ...fields })))
  if (!reply.ok && ['MEDIA_DOWNLOAD_HTTP_401', 'MEDIA_DOWNLOAD_HTTP_403'].includes(reply.error)) {
    throw new DownloadHTTPError(Number(reply.error.slice(-3)))
  }
  if (!reply.ok) throw new Error(reply.error === 'MEDIA_DOWNLOAD_BUSY'
    ? '已有原件正在下载或保存，请先完成或取消'
    : '原件未保存：下载、授权或完整性校验未完成')
  return reply.data as T
}

// The SDK constructs the same authenticated hash request as the ordinary Web
// client. Its fetch seam passes only the envelope, never file bytes, to native.
export async function saveNativeOriginal(botId: string, hash: string, size: number, name: string, signal?: AbortSignal): Promise<void> {
  signal?.throwIfAborted()
  const controller = new AbortController(), operation = crypto.randomUUID().replaceAll('-', '')
  const fields = { operation, botId }, generation = authSessionGeneration(), token = sdkAuthQuery().token
  const abort = () => { controller.abort(); void command('download.abort', fields).catch(() => {}) }
  const offAuth = onAuthSessionCleared(abort)
  const check = () => {
    controller.signal.throwIfAborted(); signal?.throwIfAborted()
    if (!token || generation !== authSessionGeneration() || token !== sdkAuthQuery().token) {
      abort(); throw new DOMException('账号已变化，原件未保存', 'AbortError')
    }
  }
  signal?.addEventListener('abort', abort, { once: true })
  const notice = toast.info('正在下载并校验原件…', { action: { label: '取消', onClick: abort }, duration: Infinity })
  try {
    check()
    const nativeFetch: typeof fetch = async (input, init) => {
      const request = new Request(input, init); check(); request.signal.throwIfAborted()
      if (request.method !== 'GET' || request.headers.get('authorization') !== 'Bearer ' + token) throw new Error('原件授权已变化')
      let ready: { operation: string; hash: string; size: number }
      try {
        ready = await command<typeof ready>('download.fetch', {
          ...fields, hash, size, name, url: request.url, authorization: request.headers.get('authorization') ?? '',
        })
      } catch (error) {
        check()
        // Let the existing SDK response handler invalidate a revoked token.
        if (error instanceof DownloadHTTPError) return new Response(null, { status: error.status })
        throw error
      }
      check()
      if (ready.operation !== operation || ready.hash !== hash || ready.size !== size) throw new Error('原件下载身份不一致')
      // No response body is exposed to WebView. The native cache was verified.
      return new Response(null, { status: 200 })
    }
    await getBotsByBotIdMediaByContentHash({ path: { bot_id: botId, content_hash: hash },
      signal: controller.signal, parseAs: 'stream', fetch: createApiFetch(nativeFetch), throwOnError: true })
    check()
    toast.info('原件已校验，请选择保存位置', { id: notice, action: { label: '取消', onClick: abort }, duration: Infinity })
    await command<boolean>('download.save', fields)
    check()
  } finally {
    offAuth(); signal?.removeEventListener('abort', abort); toast.dismiss(notice)
    // Covers failures after native readiness, late replies, picker dismissal.
    await command('download.abort', fields).catch(() => {})
  }
}
