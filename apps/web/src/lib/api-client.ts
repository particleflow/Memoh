import { client } from '@memohai/sdk/client'
import { authSessionGeneration, notifyAuthSessionCleared } from './auth-session'
import { clearFileNetwork, fileRequest, fileTarget, rememberFileNetwork, type FileConnectionIdentity } from './file-network'
import type { MediaTransferNetworkPolicy } from '@memohai/sdk'

export interface SetupApiClientOptions {
  baseUrl?: string
  fetch?: typeof fetch
  // An approved native host maps below its original logical identity guard.
  // Enable only when its existing fetch AND resource adapters own this route.
  fileRouting?: 'browser' | 'platform'
  // Called after the access token is cleared on a 401. Hosts decide what to
  // do — usually a router redirect to the login screen.
  onUnauthorized?: () => void
}

interface SdkUrlOptions {
  url: string
  path?: Record<string, unknown>
  query?: Record<string, unknown>
}

function fileIdentity(): FileConnectionIdentity {
  return { base: sdkApiBaseUrl(), token: authToken(), generation: authSessionGeneration() }
}

function checkFileIdentity(identity: FileConnectionIdentity) {
  const current = fileIdentity()
  if (!identity.token || identity.base !== current.base || identity.token !== current.token || identity.generation !== current.generation) throw new DOMException('File connection identity changed', 'AbortError')
}

const routedRequests = new WeakMap<Request, FileConnectionIdentity>()
let fileRouting: 'browser' | 'platform' = 'browser'
export function usesPlatformFileRouting() { return fileRouting === 'platform' }

export function sdkFileApiUrl(options: SdkUrlOptions): string {
  if (!options.url.startsWith('/') || options.url.startsWith('//')) throw new TypeError('File route must remain SDK-relative')
  if (options.query?.token !== undefined && options.query.token !== authToken()) throw new DOMException('File connection identity changed', 'AbortError')
  const logical = sdkApiUrl(options)
  if (usesPlatformFileRouting()) return logical
  return fileTarget(logical, fileIdentity()) ?? logical
}

// Direct Files fetches keep their original init/body instead of passing a whole
// file through the bounded media-chunk adapter. No new retries or queue exist.
export const fileApiFetch: typeof fetch = async (input, init) => {
  const identity = fileIdentity()
  const request = input instanceof Request && !init ? input : new Request(input, init)
  const url = new URL(request.url)
  if (/\/bots\/[^/]+\/(media\/|sessions\/|container\/fs(?:\/|$))/.test(url.pathname)) {
    const bearer = requestBearerToken(request)
    if (bearer && bearer !== identity.token || url.searchParams.has('token') && url.searchParams.get('token') !== identity.token) throw new DOMException('File connection identity changed', 'AbortError')
  }
  const target = usesPlatformFileRouting() ? undefined : fileTarget(request.url, identity)
  const transport = client.getConfig().fetch ?? globalThis.fetch
  if (!target) return await transport(input, init)
  checkFileIdentity(identity)
  if (request.headers.get('Authorization') !== `Bearer ${identity.token}` && new URL(request.url).searchParams.get('token') !== identity.token) throw new DOMException('File connection identity changed', 'AbortError')
  const dispatched = fileRequest(request, target, init?.body)
  routedRequests.set(dispatched, identity)
  return await transport(dispatched)
}

export function sdkAuthQuery(): { token?: string } {
  try {
    if (typeof localStorage === 'undefined') return {}
    const token = localStorage.getItem('token')?.trim()
    return token ? { token } : {}
  } catch {
    return {}
  }
}

export function sdkApiUrl(options: SdkUrlOptions): string {
  return client.buildUrl({
    ...client.getConfig(),
    ...options,
  })
}

function browserOrigin(): string {
  const { protocol, host, origin } = window.location
  return origin || `${protocol}//${host}`
}

/**
 * Return the configured HTTP API base as an absolute URL.
 *
 * The Remote Runtime CLI needs the same public base URL as the generated SDK:
 * hosted Web normally uses `/api`, while the desktop shell configures a direct
 * local-server URL. Keeping this derivation next to SDK setup prevents pages
 * from guessing which prefix the current host uses.
 */
export function sdkApiBaseUrl(): string {
  const configured = client.getConfig().baseUrl?.trim() || '/api'
  const normalized = configured.endsWith('/') ? configured : `${configured}/`
  let url: URL
  try {
    url = new URL(normalized)
  } catch {
    url = new URL(normalized, `${browserOrigin()}/`)
  }
  return url.toString().replace(/\/$/, '')
}

export function sdkWebSocketUrl(options: SdkUrlOptions): string {
  const url = new URL(sdkApiUrl(options), browserOrigin())
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
  return url.toString()
}

let onUnauthorizedHook: (() => void) | undefined
let unauthorizedNotified = false

interface ApiFetchContext {
  input: Parameters<typeof fetch>[0]
  init?: Parameters<typeof fetch>[1]
  requestToken: string
  sessionToken: string
  sessionGeneration: number
  base?: string
}

type ApiFetchResponseHandler = (
  response: Response,
  context: ApiFetchContext,
) => void | Promise<void>

function handleUnauthorized(requestToken?: string) {
  const currentToken = authToken()
  if (currentToken && requestToken && requestToken !== currentToken) return

  try {
    if (currentToken) {
      localStorage.removeItem('token')
    }
  } catch {
  }
  if (!currentToken && unauthorizedNotified) return
  unauthorizedNotified = true
  notifyAuthSessionCleared('unauthorized')
  onUnauthorizedHook?.()
}

function createApiFetch(
  baseFetch: typeof fetch = globalThis.fetch,
  responseHandlers: ApiFetchResponseHandler[] = [handleAuthResponse, handleFileNetworkProfile],
): typeof fetch {
  return async (input, init) => {
    if (input instanceof Request) {
      const identity = routedRequests.get(input)
      if (identity) checkFileIdentity(identity)
    }
    const context: ApiFetchContext = {
      input,
      init,
      requestToken: requestBearerToken(input, init),
      sessionToken: authToken(),
      sessionGeneration: authSessionGeneration(),
    }
    const url = input instanceof Request ? input.url : String(input)
    if (/\/users\/me$/.test(url)) context.base = sdkApiBaseUrl()
    const response = await baseFetch(input, init)
    for (const handler of responseHandlers) {
      await handler(response, context)
    }
    return response
  }
}

async function handleFileNetworkProfile(response: Response, context: ApiFetchContext) {
  if (response.status !== 200 || !context.base || !context.requestToken || context.requestToken !== context.sessionToken) return
  const identity = { base: context.base, token: context.requestToken, generation: context.sessionGeneration }
  const request = context.input instanceof Request ? context.input : new Request(context.input, context.init)
  if (request.method !== 'GET' || request.url !== `${identity.base}/users/me`) return
  const profile = await response.clone().json() as { id?: string, network_policy?: MediaTransferNetworkPolicy }
  checkFileIdentity(identity)
  if (profile.id) rememberFileNetwork(identity, profile.id, profile.network_policy)
}

function handleAuthResponse(response: Response, context: ApiFetchContext) {
  if (response.status === 401) {
    // A late response belongs to the identity that started the request.
    if (context.sessionGeneration !== authSessionGeneration() || context.sessionToken !== authToken()) return
    handleUnauthorized(context.requestToken)
  }
}

function authToken(): string {
  try {
    return localStorage.getItem('token')?.trim() ?? ''
  } catch {
    return ''
  }
}

function requestBearerToken(input: Parameters<typeof fetch>[0], init?: Parameters<typeof fetch>[1]): string {
  const headers = input instanceof Request ? new Headers(input.headers) : new Headers()
  if (init?.headers) {
    new Headers(init.headers).forEach((value, key) => {
      headers.set(key, value)
    })
  }
  const authorization = headers.get('Authorization')?.trim() ?? ''
  const match = /^Bearer\s+(.+)$/i.exec(authorization)
  return match?.[1]?.trim() ?? ''
}

function addAuthorizationHeader(request: Request, options?: { body?: unknown, serializedBody?: unknown }): Request {
  const token = authToken()
  if (token) {
    unauthorizedNotified = false
    request.headers.set('Authorization', `Bearer ${token}`)
  }
  const identity = fileIdentity()
  if (usesPlatformFileRouting() && /\/bots\/[^/]+\/container\/fs(?:\/|$)/.test(new URL(request.url).pathname)) {
    routedRequests.set(request, identity)
  }
  const target = usesPlatformFileRouting() ? undefined : fileTarget(request.url, identity, true)
  if (target) {
    checkFileIdentity(identity)
    const body = options?.serializedBody !== undefined ? options.serializedBody : options?.body
    const dispatched = fileRequest(request, target, body as BodyInit | undefined)
    routedRequests.set(dispatched, identity)
    return dispatched
  }
  return request
}

function installAuthRequestInterceptor() {
  if (client.interceptors.request.exists(addAuthorizationHeader)) return
  client.interceptors.request.use(addAuthorizationHeader)
}

/**
 * Configure the SDK client with base URL, auth interceptor, and 401 handling.
 * Call this once at app startup (main.ts).
 */
export function setupApiClient(options: SetupApiClientOptions = {}) {
  if (options.fileRouting === 'platform' && !options.fetch) throw new TypeError('Platform file routing requires its approved transport')
  fileRouting = options.fileRouting ?? 'browser'
  clearFileNetwork()
  const apiBaseUrl = options.baseUrl?.trim() || import.meta.env.VITE_API_URL?.trim() || '/api'
  const agentBaseUrl = import.meta.env.VITE_AGENT_URL?.trim() || '/agent'
  void agentBaseUrl

  onUnauthorizedHook = options.onUnauthorized

  client.setConfig({
    baseUrl: apiBaseUrl,
    fetch: createApiFetch(options.fetch),
  })

  installAuthRequestInterceptor()
}
