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
  it('keeps an explicit draft but opens the ordinary contact despite an external runtime default', async () => {
    const draft = fixture(); draft.deps.draftIntent.value = true; draft.deps.explicitSessionSelection.value = true
    await draft.bootstrap.initialize(); expect(api.resolvePrivateChat).not.toHaveBeenCalled()
    const external = fixture()
    await external.bootstrap.initialize(); expect(api.resolvePrivateChat).toHaveBeenCalledOnce()
    expect(external.deps.stageDefaultExternalAgentFromSettings).not.toHaveBeenCalled()
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
  it('offers multiple eligible old chats once without choosing the latest or creating', async () => {
    api.inspectPrivateChat.mockResolvedValue({ candidates: [conversation('first'), conversation('second')], nextCursor: null })
    const { deps, bootstrap } = fixture(); await bootstrap.initialize()
    expect(deps.sessionId.value).toBeNull()
    expect(bootstrap.contactChoice.value?.candidates.map(s => s.id)).toEqual(['first', 'second'])
    expect(api.resolvePrivateChat).not.toHaveBeenCalled()
    api.bindPrivateChat.mockResolvedValue(conversation('second'))
    await bootstrap.confirmContact('second')
    expect(api.bindPrivateChat).toHaveBeenCalledExactlyOnceWith('bot', 'second')
    expect(bootstrap.contactOpenRequested.value?.sessionId).toBe('second')
  })
  it('never resolves or binds a contact for an explicit historical URL', async () => {
    const { deps, bootstrap } = fixture(); deps.hasHistoryEntry = () => true
    await bootstrap.initialize()
    expect(api.inspectPrivateChat).not.toHaveBeenCalled()
    expect(api.resolvePrivateChat).not.toHaveBeenCalled()
  })
  it('reconciles a concurrent device binding before confirming a stale candidate', async () => {
    api.inspectPrivateChat.mockResolvedValueOnce({ candidates: [conversation('old'), conversation('another')], nextCursor: null })
      .mockResolvedValue({ session: conversation('other-device'), candidates: [], nextCursor: null })
    const { bootstrap } = fixture(); await bootstrap.initialize(); await bootstrap.confirmContact('old')
    expect(api.bindPrivateChat).not.toHaveBeenCalled()
    expect(bootstrap.contactOpenRequested.value?.sessionId).toBe('other-device')
  })
  it('returns to a bound contact on a same-Bot click without resetting the composer', async () => {
    api.resolvePrivateChat.mockResolvedValue(conversation('bound'))
    const { deps, bootstrap } = fixture(); deps.sessionId.value = 'history'
    await bootstrap.selectBot('bot')
    expect(bootstrap.contactOpenRequested.value?.sessionId).toBe('bound')
    expect(deps.resetToEmptyComposer).not.toHaveBeenCalled()
    expect(deps.abortAllAssistantStreams).not.toHaveBeenCalled()
  })

  it('continues a single verified existing conversation without a permission or task-management step', async () => {
    api.inspectPrivateChat.mockResolvedValue({ candidates: [conversation('only-history')], nextCursor: null })
    api.bindPrivateChat.mockResolvedValue(conversation('only-history'))
    const { deps, bootstrap } = fixture(); await bootstrap.initialize()
    expect(bootstrap.contactChoice.value).toBeNull()
    expect(api.bindPrivateChat).toHaveBeenCalledExactlyOnceWith('bot', 'only-history')
    expect(api.resolvePrivateChat).not.toHaveBeenCalled()
    expect(deps.sessionId.value).toBe('only-history')
  })

  it('does not leave the next Bot busy after an outstanding contact read completes late', async () => {
    let complete!: (value: SessionSummary) => void
    api.resolvePrivateChat.mockImplementationOnce(() => new Promise(done => { complete = done }))
    const { deps, bootstrap } = fixture()
    const previous = bootstrap.openContact()
    expect(bootstrap.contactBusy.value).toBe(true)
    await bootstrap.selectBot('other-bot', { sessionId: 'explicit-history' })
    complete(conversation('stale')); await previous
    expect(bootstrap.contactBusy.value).toBe(false)
    expect(bootstrap.contactChoice.value).toBeNull()
    expect(deps.currentBotId.value).toBe('other-bot')
    expect(deps.sessionId.value).toBe('explicit-history')
  })

  it.each(['persisted-task', 'persisted-draft'])('gives a primary Bot entry priority over %s', async restored => {
    const { deps, bootstrap } = fixture(true)
    deps.sessionId.value = restored === 'persisted-task' ? restored : null
    deps.explicitSessionSelection.value = true
    deps.draftIntent.value = restored === 'persisted-draft'
    await bootstrap.initialize()
    expect(deps.sessionId.value).toBe('server-owned-contact')
    expect(api.inspectPrivateChat).not.toHaveBeenCalled()
    expect(api.bindPrivateChat).not.toHaveBeenCalled()
    expect(bootstrap.contactNavigationPending.value).toBe(true)
    bootstrap.completeContactOpen(bootstrap.contactOpenRequested.value!)
    expect(bootstrap.contactNavigationPending.value).toBe(false)
  })

  it('never adopts even a unique historical candidate on a primary Bot entry', async () => {
    api.inspectPrivateChat.mockResolvedValue({ candidates: [conversation('only-history')], nextCursor: null })
    const { deps, bootstrap } = fixture(true)
    await bootstrap.initialize()
    expect(deps.sessionId.value).toBe('server-owned-contact')
    expect(api.bindPrivateChat).not.toHaveBeenCalled()
    expect(bootstrap.contactChoice.value).toBeNull()
  })

  it('keeps a newer task selection when a pending contact resolves', async () => {
    let finish!: (value: SessionSummary) => void
    api.resolvePrivateChat.mockImplementationOnce(() => new Promise(done => { finish = done }))
    const { deps, bootstrap } = fixture()
    const pending = bootstrap.openContact()
    await bootstrap.selectSession('newer-task')
    finish(conversation('late-contact')); await pending
    expect(deps.sessionId.value).toBe('newer-task')
    expect(bootstrap.contactOpenRequested.value).toBeNull()
    expect(bootstrap.contactNavigationPending.value).toBe(false)
    expect(bootstrap.contactBusy.value).toBe(false)
  })

  it('does not subscribe to restored task history when contact authorization fails', async () => {
    api.resolvePrivateChat.mockRejectedValue({ status: 403 })
    const { deps, bootstrap } = fixture(true)
    deps.sessionId.value = 'persisted-task'
    deps.explicitSessionSelection.value = true
    await expect(bootstrap.initialize()).rejects.toEqual({ status: 403 })
    expect(deps.startSessionRuntime).not.toHaveBeenCalled()
    expect(bootstrap.contactOpenRequested.value).toBeNull()
    expect(bootstrap.contactNavigationPending.value).toBe(true)
  })
})
