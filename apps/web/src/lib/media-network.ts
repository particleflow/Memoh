import type { MediaTransferNetworkPolicy } from '@memohai/sdk'

// This mapping is read only from authenticated NAS configuration. It changes
// dispatch, never the logical journal origin, API prefix or request protocol.
export function mediaEndpoint(base: string, policy?: MediaTransferNetworkPolicy): string | undefined {
  if (!policy || policy.version === 1 && !policy.endpoint) return undefined
  const endpoint = policy.endpoint
  if (policy.version !== 2 || !endpoint) throw new TypeError('Invalid file endpoint policy')
  const logical = new URL(base)
  for (const value of [endpoint.logical_origin, endpoint.physical_origin]) {
    if (!value || value.length > 2048 || /[?#%\\\s]/.test(value)) throw new TypeError('Invalid file endpoint policy')
    const url = new URL(value)
    if (url.origin !== value || url.username || url.password || url.pathname !== '/' || (url.port && Number(url.port) < 1)
      || !['https:', 'http:'].includes(url.protocol)) throw new TypeError('Invalid file endpoint policy')
  }
  if (endpoint.logical_origin !== logical.origin) throw new TypeError('File endpoint identity mismatch')
  return endpoint.physical_origin
}

export async function mediaDispatch(request: Request, physicalOrigin?: string): Promise<Request> {
  if (!physicalOrigin) return request
  const logical = new URL(request.url)
  const physical = new URL(physicalOrigin)
  logical.protocol = physical.protocol
  logical.host = physical.host
  // Reusing Request.body turns a Blob-backed chunk into a ReadableStream;
  // Chromium then requires HTTP/2. Retain the original bounded request bytes
  // as a Blob so the explicitly selected HTTP/1 path remains usable.
  return new Request(logical, {
    method: request.method, headers: request.headers, signal: request.signal,
    body: ['GET', 'HEAD'].includes(request.method) ? undefined : await request.blob(),
    mode: request.mode, cache: request.cache, integrity: request.integrity,
    keepalive: request.keepalive, referrerPolicy: request.referrerPolicy,
    credentials: 'omit', redirect: 'error',
  })
}
