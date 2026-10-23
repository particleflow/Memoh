import { normalizeServiceUrl } from './harmony-service-url'

// Same Web Storage and stable WebView origin as existing login/drafts. Native
// holds only an in-memory copy for its network allowlist, never a second store.
export const SERVICE_CONFIG_KEY = 'memoh:harmony:service-config:v1'
export const LEGACY_STATE_KEY = 'memoh:harmony:unbound-state:v1'
export const EDIT_SERVICE_KEY = 'memoh:harmony:edit-service'
export interface ServiceConfig { version: 1; apiOrigin: string; hapDownloadUrl: string; resetPending: boolean }

export function readServiceConfig(storage: Storage = localStorage): ServiceConfig | null {
  const raw = storage.getItem(SERVICE_CONFIG_KEY)
  if (raw === null) return null
  const v = JSON.parse(raw) as ServiceConfig
  if (v.version !== 1 || typeof v.apiOrigin !== 'string' || typeof v.hapDownloadUrl !== 'string' || typeof v.resetPending !== 'boolean') throw new Error('SERVICE_CONFIG_INVALID')
  if (normalizeServiceUrl(v.apiOrigin) !== v.apiOrigin || (v.hapDownloadUrl && normalizeServiceUrl(v.hapDownloadUrl, false) !== v.hapDownloadUrl)) throw new Error('SERVICE_CONFIG_INVALID')
  return v
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
  // Delegate the user-scoped cleanup to the existing auth layer at bootstrap;
  // the complete storage snapshot below also protects legacy local-only data.
  for (const key of ['token', 'user']) { storage.removeItem(key); tab.removeItem(key) }
  return v
}

export function saveServiceConfig(api: string, download: string, storage: Storage = localStorage): ServiceConfig {
  const next: ServiceConfig = { version: 1, apiOrigin: normalizeServiceUrl(api), hapDownloadUrl: download ? normalizeServiceUrl(download, false) : '', resetPending: false }
  let old: ServiceConfig | null = null
  try { old = readServiceConfig(storage) } catch { /* An explicit save repairs invalid config, never an automatic fallback. */ }
  next.resetPending = old?.resetPending === true || old?.apiOrigin !== next.apiOrigin
  if (!old && !storage.getItem(LEGACY_STATE_KEY)) {
    // One local-only, unbound data backup, excluding the old token. It is never read as credentials,
    // exported, synchronized, or restored to a user-entered server automatically.
    const entries: Record<string, string> = {}
    for (let i = 0; i < storage.length; i++) {
      const key = storage.key(i)
      if (key && key !== 'token' && key !== SERVICE_CONFIG_KEY && key !== LEGACY_STATE_KEY) entries[key] = storage.getItem(key) ?? ''
    }
    if (Object.keys(entries).length) {
      const raw = JSON.stringify({ version: 1, entries })
      storage.setItem(LEGACY_STATE_KEY, raw)
      if (storage.getItem(LEGACY_STATE_KEY) !== raw) throw new Error('SERVICE_MIGRATION_WRITE')
    }
  }
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
