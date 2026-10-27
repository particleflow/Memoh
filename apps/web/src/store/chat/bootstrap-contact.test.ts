import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import type { SessionSummary } from '@/composables/api/useChat.types'
import { createChatBootstrap, type ChatBootstrapDeps } from './bootstrap'

const api = vi.hoisted(() => ({ fetchSessions: vi.fn(), resolvePrivateChat: vi.fn() }))
vi.mock('@/composables/api/useChat', () => api)

const conversation = (id: string): SessionSummary => ({ id, bot_id: 'bot', title: id, type: 'chat', created_at: '', updated_at: '' })
function fixture() {
  let generation = 0
  const remembered = new Map<string, SessionSummary>()
  const deps: ChatBootstrapDeps = {
    currentBotId: ref('bot'), sessionId: ref<string | null>(null), draftIntent: ref(false), explicitSessionSelection: ref(false),
    userScopeGeneration: () => generation, bumpSelectSessionRequest: vi.fn(() => 1), currentSelectSessionRequest: () => 1,
    prepareForInitialization: vi.fn(), resetRefreshCoordinator: vi.fn(), resetSessionActivity: vi.fn(), stopStreams: vi.fn(), stopWebSocket: vi.fn(),
    ensureBot: async () => deps.currentBotId.value,
    replaceSessions: vi.fn((items: SessionSummary[]) => { for (const item of items) remembered.set(item.id, item); return items }),
    sessionsCursor: ref<string | null>(null), hasMoreSessions: ref(false), defaultRuntimeIsExternalAgent: vi.fn(async () => false),
    ensureSessionSummary: vi.fn(async (_bot: string, id: string) => remembered.get(id) || conversation(id)),
    pendingExternalAgentSessionInput: ref(null), clearPendingExternalAgentSession: vi.fn(), clearHistoryView: vi.fn(), markHistoryEmpty: vi.fn(),
    knownSessionSummary: id => remembered.get(id), startWebSocket: vi.fn(), startBotSessionsActivityStream: vi.fn(), startSessionRuntime: vi.fn(),
    releasePreviousSession: vi.fn(), abort: vi.fn(), abortAllAssistantStreams: vi.fn(), clearFsForBotSwitch: vi.fn(), clearRememberedSessions: vi.fn(),
    resetToEmptyComposer: vi.fn(), stageDefaultExternalAgentFromSettings: vi.fn(async () => {}),
  }
  return { deps, bootstrap: createChatBootstrap(deps), invalidate: () => generation++ }
}
beforeEach(() => {
  vi.clearAllMocks()
  api.fetchSessions.mockResolvedValue({ items: [conversation('latest-historical-chat')], nextCursor: null })
  api.resolvePrivateChat.mockResolvedValue(conversation('server-owned-contact'))
})

describe('Bot contact entry', () => {
  it('opens the server contact instead of requiring a historical session choice', async () => {
    const { deps, bootstrap } = fixture(); await bootstrap.initialize()
    expect(api.resolvePrivateChat).toHaveBeenCalledExactlyOnceWith('bot')
    expect(deps.sessionId.value).toBe('server-owned-contact')
    expect(deps.startSessionRuntime).toHaveBeenCalledWith('bot', 'server-owned-contact')
    expect(deps.replaceSessions).toHaveBeenLastCalledWith([conversation('server-owned-contact'), conversation('latest-historical-chat')])
  })
  it('uses the same persisted contact in a second independent client', async () => {
    const a = fixture(), b = fixture(); await a.bootstrap.initialize(); await b.bootstrap.initialize()
    expect(a.deps.sessionId.value).toBe(b.deps.sessionId.value)
    expect(api.resolvePrivateChat).toHaveBeenCalledTimes(2)
  })
  it('keeps explicitly opened history without repointing it', async () => {
    const { deps, bootstrap } = fixture(); deps.sessionId.value = 'old-history'; deps.explicitSessionSelection.value = true
    await bootstrap.initialize()
    expect(api.resolvePrivateChat).not.toHaveBeenCalled(); expect(deps.sessionId.value).toBe('old-history')
  })
  it('keeps an explicit new-chat draft and configured external runtime', async () => {
    const draft = fixture(); draft.deps.draftIntent.value = true; draft.deps.explicitSessionSelection.value = true
    await draft.bootstrap.initialize(); expect(api.resolvePrivateChat).not.toHaveBeenCalled()
    const external = fixture(); vi.mocked(external.deps.defaultRuntimeIsExternalAgent).mockResolvedValue(true)
    await external.bootstrap.initialize(); expect(api.resolvePrivateChat).not.toHaveBeenCalled()
    expect(external.deps.stageDefaultExternalAgentFromSettings).toHaveBeenCalledOnce()
  })
  it('retains old-server navigation only for an absent contact route', async () => {
    api.resolvePrivateChat.mockRejectedValue({ status: 404, code: 'http.not_found' })
    const { deps, bootstrap } = fixture(); await bootstrap.initialize(); expect(deps.sessionId.value).toBe('latest-historical-chat')
  })
  it.each([401, 403, 404, 409, 500, 503])('does not switch to another conversation after HTTP %s', async status => {
    api.resolvePrivateChat.mockRejectedValue({ status })
    const { deps, bootstrap } = fixture(); await expect(bootstrap.initialize()).rejects.toEqual({ status })
    expect(deps.sessionId.value).toBeNull(); expect(deps.startSessionRuntime).not.toHaveBeenCalled()
  })
  it('ignores a late contact result after account scope changed', async () => {
    let resolve!: (value: SessionSummary) => void
    api.resolvePrivateChat.mockImplementationOnce(() => new Promise<SessionSummary>(done => { resolve = done }))
    const { deps, bootstrap, invalidate } = fixture(); const pending = bootstrap.initialize()
    await vi.waitFor(() => expect(api.resolvePrivateChat).toHaveBeenCalledOnce())
    invalidate(); resolve(conversation('stale-contact')); await pending
    expect(deps.sessionId.value).toBeNull(); expect(deps.startSessionRuntime).not.toHaveBeenCalled()
  })
})
