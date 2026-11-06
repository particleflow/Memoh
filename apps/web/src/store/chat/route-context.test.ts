import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import type { SessionSummary } from '@/composables/api/useChat'
import { createChatBootstrap, type ChatBootstrapDeps } from './bootstrap'
import { createChatRouteContext } from './route-context'

const api = vi.hoisted(() => ({ fetchSessions: vi.fn(), resolvePrivateChat: vi.fn() }))
vi.mock('@/composables/api/useChat', () => api)
const row = (id: string): SessionSummary => ({ id, bot_id: 'bot', type: 'chat', title: id, created_at: '', updated_at: '' })

function fixture() {
  // Match main.ts: the store exists before app.use(router).
  const route = createChatRouteContext(null)
  const startSessionRuntime = vi.fn(), sessionId = ref<string | null>('history')
  const deps: ChatBootstrapDeps = {
    ...route, currentBotId: ref('bot'), sessionId, explicitSessionSelection: ref(true), draftIntent: ref(false),
    userScopeGeneration: () => 0, bumpSelectSessionRequest: () => 1, currentSelectSessionRequest: () => 1,
    prepareForInitialization: () => {}, resetRefreshCoordinator: () => {}, resetSessionActivity: () => {},
    stopStreams: () => {}, stopWebSocket: () => {}, ensureBot: async () => 'bot', replaceSessions: items => items,
    sessionsCursor: ref(null), hasMoreSessions: ref(false), ensureSessionSummary: async (_bot, id) => row(id),
    pendingExternalAgentSessionInput: ref(null), clearPendingExternalAgentSession: () => {}, clearHistoryView: () => {},
    markHistoryEmpty: () => {}, knownSessionSummary: id => row(id), startWebSocket: () => {}, ensureWebSocket: () => true,
    startBotSessionsActivityStream: () => {}, startSessionRuntime, releasePreviousSession: () => {},
    abort: () => {}, abortAllAssistantStreams: () => {}, clearFsForBotSwitch: () => {}, clearRememberedSessions: () => {},
    resetToEmptyComposer: () => {}, stageDefaultExternalAgentFromSettings: async () => {},
  }
  api.fetchSessions.mockReset().mockResolvedValue({ items: [row('history'), row('contact')], nextCursor: null })
  api.resolvePrivateChat.mockReset().mockResolvedValue(row('contact'))
  return { route, sessionId, startSessionRuntime, bootstrap: createChatBootstrap(deps) }
}

describe('late Router binding during native initialize recovery', () => {
  it('keeps an already selected authorized history instead of resolving and subscribing the contact on later initialize', async () => {
    const h = fixture()
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/bot/:botName', name: 'bot', component: { render: () => null } }] })
    await router.push('/bot/bot?session=history')
    h.route.bindRouteContext(router)
    await h.bootstrap.initialize()
    expect(h.sessionId.value).toBe('history')
    expect(api.resolvePrivateChat).not.toHaveBeenCalled()
    expect(h.startSessionRuntime).toHaveBeenCalledExactlyOnceWith('bot', 'history')
  })

  it('uses the live primary route on the next initialize, preserving fixed-contact entry over persisted old history', async () => {
    const h = fixture()
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/bot/:botName', name: 'bot', component: { render: () => null } }] })
    await router.push('/bot/bot?session=history'); h.route.bindRouteContext(router)
    await router.push('/bot/bot')
    await h.bootstrap.initialize()
    expect(api.resolvePrivateChat).toHaveBeenCalledExactlyOnceWith('bot')
    expect(h.sessionId.value).toBe('contact')
    expect(h.startSessionRuntime).toHaveBeenCalledExactlyOnceWith('bot', 'contact')
  })
})
