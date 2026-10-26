import { normalizeServiceUrl, normalizeTransferNetworkPolicy, type TransferNetworkPolicy } from './harmony-service-url'

// Same Web Storage and stable WebView origin as existing login/drafts. Native
// holds only an in-memory copy for its network allowlist, never a second store.
export const SERVICE_CONFIG_KEY = 'memoh:harmony:service-config:v1'
export const LEGACY_STATE_KEY = 'memoh:harmony:unbound-state:v1'
export const EDIT_SERVICE_KEY = 'memoh:harmony:edit-service'
export interface ServiceConfig { version: 1; apiOrigin: string; hapDownloadUrl: string; resetPending: boolean }

// Only an independently established binding of the existing installation may
// supply this value. Form input is not evidence. v1.0.6 used a compiled origin,
// not a stored key; the locally injected deployment config establishes a match.
export interface LegacyServiceBinding { apiOrigin: string }
export type ServiceSetup =
  | { kind: 'configured'; config: ServiceConfig }
  | { kind: 'legacy-bound'; config: ServiceConfig }
  | { kind: 'initial' }
  | { kind: 'unbound' }

interface DeploymentConfig {
  apiOrigin: string
  hapDownloadUrl: string
  productName: string
  networkPolicy?: TransferNetworkPolicy
  legacyBinding?: { schemaVersion: 1; bundleName: string; versionCode: number; sourceHapSHA256: string; apiBaseSHA256: string; apiOrigin?: string } | null
}

// Address hints come only from the existing packaged private resource. They
// are not another persisted setting and never follow a user to a different NAS.
export function deploymentNetworkPolicy(raw: unknown, apiOrigin: string): TransferNetworkPolicy | undefined {
  if (!raw || typeof raw !== 'object' || Array.isArray(raw) || !('networkPolicy' in raw)) return undefined
  const seed = raw as DeploymentConfig
  if (typeof seed.apiOrigin !== 'string') throw new Error('DEPLOYMENT_CONFIG_INVALID')
  const bound = normalizeServiceUrl(seed.apiOrigin)
  if (bound !== normalizeServiceUrl(apiOrigin)) return undefined
  if (!bound.startsWith('https://')) throw new Error('NETWORK_POLICY_HTTPS')
  return normalizeTransferNetworkPolicy(seed.networkPolicy as TransferNetworkPolicy)
}

// Read only from the signed application's packaged resource. Never fetch a
// deployment config from an entered endpoint, download URL or external service.
export async function readDeploymentConfig(): Promise<unknown> {
  const resource = new URL('./memoh_client.json', window.location.href)
  const response = await fetch(resource, { credentials: 'omit', cache: 'no-store' })
  if (!response.ok) throw new Error('DEPLOYMENT_CONFIG_UNAVAILABLE')
  return response.json()
}

export async function applyDeploymentConfig(raw: unknown, storage: Storage = localStorage): Promise<ServiceConfig | null> {
  const current = readServiceConfig(storage)
  // Only a locally signed, explicit old-origin binding may upgrade persisted
  // HTTP. Ordinary seeds must never replace user configuration.
  const upgradeRequested = !!raw && typeof raw === 'object' && 'legacyBinding' in raw &&
    !!raw.legacyBinding && typeof raw.legacyBinding === 'object' && 'apiOrigin' in raw.legacyBinding
  if (current && !upgradeRequested) return current
  if (!raw || typeof raw !== 'object' || Array.isArray(raw) ||
    Object.keys(raw).some(key => !['apiOrigin', 'hapDownloadUrl', 'productName', 'legacyBinding', 'networkPolicy'].includes(key))) throw new Error('DEPLOYMENT_CONFIG_INVALID')
  const value = raw as DeploymentConfig
  if (typeof value.apiOrigin !== 'string' || typeof value.hapDownloadUrl !== 'string' || typeof value.productName !== 'string' ||
    !value.productName || value.productName.length > 128) throw new Error('DEPLOYMENT_CONFIG_INVALID')
  if (!value.apiOrigin) {
    if (value.hapDownloadUrl || value.legacyBinding || value.networkPolicy !== undefined) throw new Error('DEPLOYMENT_CONFIG_INVALID')
    return null // Public unsigned template; Windows must fill it before signing.
  }
  const apiOrigin = normalizeServiceUrl(value.apiOrigin)
  deploymentNetworkPolicy(raw, apiOrigin)
  const download = value.hapDownloadUrl ? normalizeServiceUrl(value.hapDownloadUrl, false) : ''
  let legacy: LegacyServiceBinding | undefined
  if (value.legacyBinding != null) {
    const binding = value.legacyBinding
    const keys = ['schemaVersion', 'bundleName', 'versionCode', 'sourceHapSHA256', 'apiBaseSHA256']
    if (typeof binding !== 'object' || Array.isArray(binding) || Object.keys(binding).length !== keys.length + (upgradeRequested ? 1 : 0) ||
      Object.keys(binding).some(key => !keys.includes(key) && !(upgradeRequested && key === 'apiOrigin')) || binding.schemaVersion !== 1 ||
      binding.bundleName !== 'cloud.begonia.workassistant' || binding.versionCode !== 1000006 ||
      typeof binding.sourceHapSHA256 !== 'string' || !/^[a-f0-9]{64}$/.test(binding.sourceHapSHA256) ||
      typeof binding.apiBaseSHA256 !== 'string' || !/^[a-f0-9]{64}$/.test(binding.apiBaseSHA256)) throw new Error('DEPLOYMENT_LEGACY_INVALID')
    const boundOrigin = upgradeRequested ? normalizeServiceUrl(binding.apiOrigin as string) : apiOrigin
    if (upgradeRequested) {
      const old = new URL(boundOrigin), next = new URL(apiOrigin)
      if (boundOrigin !== binding.apiOrigin || old.protocol !== 'http:' || next.protocol !== 'https:' ||
        old.hostname !== next.hostname || old.pathname !== next.pathname) throw new Error('DEPLOYMENT_LEGACY_ORIGIN_MISMATCH')
    }
    const digest = new Uint8Array(await crypto.subtle.digest('SHA-256', new TextEncoder().encode(boundOrigin)))
    const actual = Array.from(digest, byte => byte.toString(16).padStart(2, '0')).join('')
    if (actual !== binding.apiBaseSHA256) throw new Error('DEPLOYMENT_LEGACY_ORIGIN_MISMATCH')
    legacy = { apiOrigin: boundOrigin }
  }
  // A concurrent configuration write also wins over the package seed.
  const latest = readServiceConfig(storage)
  if (upgradeRequested) {
    if (JSON.stringify(latest) !== JSON.stringify(current)) throw new Error('SERVICE_CONFIG_CHANGED')
    if (!legacy) throw new Error('DEPLOYMENT_LEGACY_INVALID')
    if (latest?.apiOrigin === apiOrigin && !latest.resetPending) return latest
    if (latest && (latest.apiOrigin !== legacy.apiOrigin || latest.resetPending)) throw new Error('SERVICE_LEGACY_ORIGIN_CHANGED')
    const upgraded: ServiceConfig = { version: 1, apiOrigin, hapDownloadUrl: latest?.hapDownloadUrl || download, resetPending: false }
    writeConfig(upgraded, storage)
    return upgraded
  }
  return latest ?? saveServiceConfig(apiOrigin, download, storage, legacy)
}

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
