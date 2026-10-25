import { normalizeServiceUrl } from './harmony-service-url'

// Same Web Storage and stable WebView origin as existing login/drafts. Native
// holds only an in-memory copy for its network allowlist, never a second store.
export const SERVICE_CONFIG_KEY = 'memoh:harmony:service-config:v1'
export const LEGACY_STATE_KEY = 'memoh:harmony:unbound-state:v1'
export const EDIT_SERVICE_KEY = 'memoh:harmony:edit-service'
export interface ServiceConfig { version: 1; apiOrigin: string; hapDownloadUrl: string; resetPending: boolean }

// Only an independently established binding of the existing installation may
// supply this value. Form input is not evidence. v1.0.6 used a compiled origin,
// not a stored key; deployment verification is not wired into this checkpoint.
export interface LegacyServiceBinding { apiOrigin: string }
export type ServiceSetup =
  | { kind: 'configured'; config: ServiceConfig }
  | { kind: 'legacy-bound'; config: ServiceConfig }
  | { kind: 'initial' }
  | { kind: 'unbound' }

export function readServiceConfig(storage: Storage = localStorage): ServiceConfig | null {
  const raw = storage.getItem(SERVICE_CONFIG_KEY)
  if (raw === null) return null
  const v = JSON.parse(raw) as ServiceConfig
  if (v.version !== 1 || typeof v.apiOrigin !== 'string' || typeof v.hapDownloadUrl !== 'string' || typeof v.resetPending !== 'boolean') throw new Error('SERVICE_CONFIG_INVALID')
  if (normalizeServiceUrl(v.apiOrigin) !== v.apiOrigin || (v.hapDownloadUrl && normalizeServiceUrl(v.hapDownloadUrl, false) !== v.hapDownloadUrl)) throw new Error('SERVICE_CONFIG_INVALID')
  return v
}

// Read-only classification: opening setup must not mutate legacy state.
export function inspectServiceSetup(storage: Storage = localStorage, legacy?: LegacyServiceBinding): ServiceSetup {
  const current = readServiceConfig(storage)
  if (current) return { kind: 'configured', config: current }
  if (legacy) return { kind: 'legacy-bound', config: { version: 1, apiOrigin: normalizeServiceUrl(legacy.apiOrigin), hapDownloadUrl: '', resetPending: false } }
  // Existing shared appearance/i18n preferences do not bind an account to a
  // server. Everything else, including recovery snapshots, stays unclassified.
  const preferences = new Set(['locale', 'language', 'theme', 'color-scheme', 'ui-font-family', 'code-font-family', 'ui-font-size-px', 'code-font-size-px', 'shiki-theme-light', 'shiki-theme-dark', 'mermaid-theme'])
  for (let i = 0; i < storage.length; i++) {
    const key = storage.key(i)
    // Retain previous recovery snapshots as well as active user data. Unknown
    // keys are not guessed to be harmless or attributed to the entered server.
    if (key && key !== SERVICE_CONFIG_KEY && !preferences.has(key) && storage.getItem(key)) return { kind: 'unbound' }
  }
  return { kind: 'initial' }
}

function writeConfig(v: ServiceConfig, storage: Storage) {
  const raw = JSON.stringify(v)
  storage.setItem(SERVICE_CONFIG_KEY, raw)
  if (storage.getItem(SERVICE_CONFIG_KEY) !== raw) throw new Error('SERVICE_CONFIG_WRITE')
}

// Crash-safe transition: pending is committed before clearing active identity.
// Boot must finish this step before importing any authenticated app code.
export function finishServiceTransition(v: ServiceConfig, storage: Storage = localStorage, tab: Storage = sessionStorage): ServiceConfig {
  if (!v.resetPending) return v
  // Delegate deliberate configured-service switches to the existing auth
  // cleanup. Unbound legacy state never enters this transition.
  for (const key of ['token', 'user']) { storage.removeItem(key); tab.removeItem(key) }
  return v
}

export function saveServiceConfig(api: string, download: string, storage: Storage = localStorage, legacy?: LegacyServiceBinding): ServiceConfig {
  const next: ServiceConfig = { version: 1, apiOrigin: normalizeServiceUrl(api), hapDownloadUrl: download ? normalizeServiceUrl(download, false) : '', resetPending: false }
  const setup = inspectServiceSetup(storage, legacy)
  if (setup.kind === 'unbound') throw new Error('SERVICE_BINDING_REQUIRED')
  if (setup.kind === 'legacy-bound' && setup.config.apiOrigin !== next.apiOrigin) throw new Error('SERVICE_LEGACY_ORIGIN_CHANGED')
  const old = setup.kind === 'configured' || setup.kind === 'legacy-bound' ? setup.config : null
  next.resetPending = !!old && (old.resetPending || old.apiOrigin !== next.apiOrigin)
  writeConfig(next, storage)
  return next
}

export function commitServiceTransition(v: ServiceConfig, storage: Storage = localStorage): ServiceConfig {
  const current = readServiceConfig(storage)
  if (!current || current.apiOrigin !== v.apiOrigin || current.hapDownloadUrl !== v.hapDownloadUrl) throw new Error('SERVICE_CONFIG_CHANGED')
  const next = { ...v, resetPending: false }
  writeConfig(next, storage)
  return next
}

export function activeServiceConfig(): ServiceConfig {
  const value = readServiceConfig()
  if (!value || value.resetPending) throw new Error('SERVICE_NOT_CONFIGURED')
  return value
}

export function editServiceConfig() {
  sessionStorage.setItem(EDIT_SERVICE_KEY, '1')
  window.location.reload()
}
