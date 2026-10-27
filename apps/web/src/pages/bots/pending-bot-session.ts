import type { BotsCreateBotRequest } from '@memohai/sdk'
import { safeSessionGet, safeSessionRemove, safeSessionSet } from '@/utils/safe-storage'
import type { StartBotCreateOptions } from '@/store/bot-create-progress'
import { directBotAgentMetadata, isDirectBotAgentRuntime } from '@/utils/bot-agent'

const KEY = 'memoh:new-bot:pending'
const ONBOARDING_KEY = 'memoh:onboarding:pending'
const storageKey = (scope: string, onboarding = false) => `${onboarding ? ONBOARDING_KEY : KEY}:${encodeURIComponent(scope)}`

export interface PendingBotSession {
  scope: string
  requestKey: string
  payload: BotsCreateBotRequest
  options: StartBotCreateOptions
}

// Persist an allowlist: request metadata and Agent metadata can hold secrets.
export function writePendingBotSession(scope: string, requestKey: string, payload: BotsCreateBotRequest, options: StartBotCreateOptions): boolean {
  if (!scope || !requestKey || (payload.metadata && Object.keys(payload.metadata).length)) return false
  const runtime = options.agent?.provider.trim().toLowerCase()
  if (runtime && !/^[a-z0-9_-]+$/.test(runtime)) return false
  if (options.agent?.metadata && Object.keys(options.agent.metadata).some(key => key !== 'provider' && key !== 'auth')) return false
  const value: PendingBotSession = {
    scope, requestKey,
    payload: { name: payload.name, display_name: payload.display_name, avatar_url: payload.avatar_url, timezone: payload.timezone, is_active: payload.is_active, acl_preset: payload.acl_preset, wait_for_ready: payload.wait_for_ready },
    options: {
      onboarding: options.onboarding,
      display: options.display && { name: options.display.name, display_name: options.display.display_name, avatar_url: options.display.avatar_url },
      settings: options.settings && { chat_model_id: options.settings.chat_model_id, memory_enabled: options.settings.memory_enabled, reasoning_effort: options.settings.reasoning_effort },
      grants: options.grants?.map(grant => ({ subject_type: grant.subject_type, user_id: grant.user_id, permissions: grant.permissions })),
      ...(runtime && { agent: { name: options.agent!.name, provider: runtime, authorizationId: options.agent!.authorizationId } }),
    },
  }
  return safeSessionSet(storageKey(scope, options.onboarding), JSON.stringify(value))
}

export function readPendingBotSession(scope: string, onboarding = false): PendingBotSession | null {
  if (!scope) return null
  try {
    const value = JSON.parse(safeSessionGet(storageKey(scope, onboarding)) || 'null') as PendingBotSession | null
    if (!value || value.scope !== scope || !value.requestKey || !value.payload || !value.options) return null
    if (value.options.agent) {
      if (!/^[a-z0-9_-]+$/.test(value.options.agent.provider)) return null
      if (isDirectBotAgentRuntime(value.options.agent.provider)) value.options.agent.metadata = directBotAgentMetadata(value.options.agent.provider)
    }
    return value
  } catch { return null }
}

export function clearPendingBotSession(scope: string, onboarding = false) {
  safeSessionRemove(storageKey(scope, onboarding))
}
