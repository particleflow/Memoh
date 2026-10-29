import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import type { SessionSummary } from '@/composables/api/useChat.types'
import { createChatBootstrap, type ChatBootstrapDeps } from './bootstrap'

const api = vi.hoisted(() => ({ fetchSessions: vi.fn(), resolvePrivateChat: vi.fn(), inspectPrivateChat: vi.fn(), bindPrivateChat: vi.fn() }))
vi.mock('@/composables/api/useChat', () => api)

const conversation = (id: string): SessionSummary => ({ id, bot_id: 'bot', title: id, type: 'chat', created_at: '', updated_at: '' })
function fixture(contactEntry = false) {
  let generation = 0
  const remembered = new Map<string, SessionSummary>()
  const deps: ChatBootstrapDeps = {
    hasContactEntry: () => contactEntry,
    currentBotId: ref('bot'), sessionId: ref<string | null>(null), draftIntent: ref(false), explicitSessionSelection: ref(false),
    userScopeGeneration: () => generation, bumpSelectSessionRequest: vi.fn(() => 1), currentSelectSessionRequest: () => 1,
    prepareForInitialization: vi.fn(), resetRefreshCoordinator: vi.fn(), resetSessionActivity: vi.fn(), stopStreams: vi.fn(), stopWebSocket: vi.fn(),
    ensureBot: async () => deps.currentBotId.value,
    replaceSessions: vi.fn((items: SessionSummary[]) => { for (const item of items) remembered.set(item.id, item); return items }),
    sessionsCursor: ref<string | null>(null), hasMoreSessions: ref(false),
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
  api.inspectPrivateChat.mockResolvedValue({ candidates: [], nextCursor: null })
})

describe('Fixed Bot contact', () => {
  it.each([false, true])('uses only system resolution on primary entry (seeded=%s)', async seeded => {
    const { deps, bootstrap } = fixture(true)
    if (seeded) await bootstrap.selectBot('bot'); else await bootstrap.initialize()
    expect(bootstrap.contactOpenRequested.value?.sessionId).toBe('server-owned-contact')
    expect(api.inspectPrivateChat).not.toHaveBeenCalled()
    expect(api.bindPrivateChat).not.toHaveBeenCalled()
    expect(bootstrap.contactChoice.value).toBeNull()
    expect(bootstrap.contactSessionIds.value.bot).toBe('server-owned-contact')
    expect(deps.resetToEmptyComposer).not.toHaveBeenCalled()
  })
  it.each(['createNewSession', 'selectDraft'] as const)('legacy %s reopens the contact without a new draft', async action => {
    const { deps, bootstrap } = fixture(true)
    deps.sessionId.value = 'server-owned-contact'
    await bootstrap[action]({ explicitSelection: true })
    await vi.waitFor(() => expect(bootstrap.contactOpenRequested.value?.sessionId).toBe('server-owned-contact'))
    expect(deps.resetToEmptyComposer).not.toHaveBeenCalled()
    expect(deps.stageDefaultExternalAgentFromSettings).not.toHaveBeenCalled()
    expect(deps.sessionId.value).toBe('server-owned-contact')
  })
  it('does not expose candidate choice or bind from a stale dialog', async () => {
    const { bootstrap } = fixture(true)
    await bootstrap.confirmContact(null); await bootstrap.confirmContact('old'); await bootstrap.loadContactCandidates()
    expect(api.resolvePrivateChat).not.toHaveBeenCalled()
    expect(api.bindPrivateChat).not.toHaveBeenCalled()
  })
  it.each([401,403,404,409,503])('does not fall back to history or an empty draft after HTTP %s', async status => {
    api.resolvePrivateChat.mockRejectedValue({status,code:'http.not_found'})
    const { deps, bootstrap } = fixture(true)
    await expect(bootstrap.initialize()).rejects.toMatchObject({status})
    expect(deps.startSessionRuntime).not.toHaveBeenCalled()
    expect(deps.sessionId.value).toBeNull()
  })
  it('primary entry overrides a restored draft without discarding its composer state', async () => {
    const { deps, bootstrap } = fixture(true)
    deps.draftIntent.value=true; deps.explicitSessionSelection.value=true
    await bootstrap.initialize()
    expect(deps.sessionId.value).toBe('server-owned-contact')
    expect(deps.resetToEmptyComposer).not.toHaveBeenCalled()
  })
  it('retains explicit history as history without repointing the contact', async () => {
    const { deps, bootstrap } = fixture(true)
    deps.hasHistoryEntry=()=>true; deps.sessionId.value='old-75';deps.explicitSessionSelection.value=true
    await bootstrap.initialize()
    expect(deps.sessionId.value).toBe('old-75');expect(api.resolvePrivateChat).not.toHaveBeenCalled()
  })
  it('drops late identity after logout and clears the resolved contact cache', async () => {
    let finish!: (v:SessionSummary)=>void
    api.resolvePrivateChat.mockImplementationOnce(()=>new Promise(done=>{finish=done}))
    const { bootstrap, invalidate }=fixture(true)
    const opening=bootstrap.openContact();invalidate();bootstrap.reset();finish(conversation('stale'));await opening
    expect(bootstrap.contactOpenRequested.value).toBeNull();expect(bootstrap.contactSessionIds.value).toEqual({})
  })
  it('same contact selection retries history without releasing its runtime', async () => {
    const {deps,bootstrap}=fixture(true);deps.sessionId.value='bound'
    await bootstrap.selectSession('bound',{explicitSelection:false})
    expect(deps.startSessionRuntime).toHaveBeenCalledExactlyOnceWith('bot','bound');expect(deps.releasePreviousSession).not.toHaveBeenCalled()
  })
})
