import { shallowRef } from 'vue'
import type { MediaTransferNetworkPolicy } from '@memohai/sdk'
import { mediaEndpoint } from './media-network'

export interface FileConnectionIdentity {
  base: string
  token: string
  generation: number
}

// The existing authenticated profile is the sole source. This per-tab snapshot
// is not persisted and contains no queue, receipt or rewritten attachment data.
const route = shallowRef<(FileConnectionIdentity & { owner: string, physical?: string }) | undefined>()

export function clearFileNetwork() { route.value = undefined }

export function rememberFileNetwork(identity: FileConnectionIdentity, owner: string, policy?: MediaTransferNetworkPolicy) {
  if (!owner || !identity.token) return
  route.value = { ...identity, owner, physical: mediaEndpoint(identity.base, policy) }
}

export function fileEndpoint(identity: FileConnectionIdentity): string | undefined {
  const current = route.value
  if (!current) return undefined
  if (current.base !== identity.base || current.token !== identity.token || current.generation !== identity.generation) {
    clearFileNetwork()
    return undefined
  }
  return current.physical
}

function filePath(path: string, workspaceOnly: boolean) {
  if (/^\/bots\/[^/]+\/container\/fs(?:\/|$)/.test(path)) return true
  if (workspaceOnly) return false
  const id = '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}'
  return new RegExp('^/bots/[^/]+/media/[a-f0-9]{64}$').test(path)
    || new RegExp(`^/bots/(${id})/sessions/(${id})/delegations/(${id})/attachments/[a-f0-9]{64}$`).test(path)
}

export function fileTarget(target: string, identity: FileConnectionIdentity, workspaceOnly = false): string | undefined {
  const physicalOrigin = fileEndpoint(identity)
  if (!physicalOrigin) return undefined
  const base = new URL(identity.base)
  const url = new URL(target, `${base.origin}/`)
  const prefix = base.pathname.replace(/\/$/, '')
  if (!url.pathname.startsWith(prefix + '/') || !filePath(url.pathname.slice(prefix.length), workspaceOnly)) return undefined
  // Accept an already-resolved preview URL only for the same explicitly trusted
  // endpoint, path prefix and CURRENT query credential. Never follow a redirect.
  if (![base.origin, physicalOrigin].includes(url.origin)) throw new DOMException('File connection identity changed', 'AbortError')
  if (url.searchParams.has('token') && url.searchParams.get('token') !== identity.token) throw new DOMException('File connection identity changed', 'AbortError')
  const physical = new URL(physicalOrigin)
  url.protocol = physical.protocol
  url.host = physical.host
  return url.toString()
}

// Retain the original serialized BodyInit. Reading Request.body into a Blob
// would buffer whole workspace files; forwarding the stream would require H2
// in Chromium. FormData is re-encoded by the browser from the SAME File values.
export function fileRequest(request: Request, target: string, body?: BodyInit | null): Request {
  const headers = new Headers(request.headers)
  if (body instanceof FormData) headers.delete('Content-Type')
  if (!['GET', 'HEAD'].includes(request.method) && request.body && body === undefined) throw new TypeError('Original file request body unavailable')
  return new Request(target, {
    method: request.method, headers, body: ['GET', 'HEAD'].includes(request.method) ? undefined : body,
    signal: request.signal, mode: request.mode, cache: request.cache, integrity: request.integrity,
    keepalive: request.keepalive, referrerPolicy: request.referrerPolicy,
    credentials: 'omit', redirect: 'error',
  })
}
