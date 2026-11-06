import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import type { ChatWebSocket, SessionSummary, UIStreamEvent, UITurn, WSClientMessage } from '@/composables/api/useChat'
import { createChatBootstrap, type ChatBootstrapDeps } from './bootstrap'
import { createChatRealtimeController, type ChatRealtimeTransport } from './realtime'
import { createChatViews } from './views'

const api = vi.hoisted(() => ({ fetchSessions: vi.fn(), resolvePrivateChat: vi.fn(), fetchMessagesUI: vi.fn(), locateMessageUI: vi.fn(),
  connectWebSocket: vi.fn(), streamBotSessionsActivityEvents: vi.fn() }))
vi.mock('@/composables/api/useChat', () => api)

const bot = 'history-bot', session = 'original-execution'
const summary = (id = session, botId = bot): SessionSummary => ({ id, bot_id: botId, type: 'chat', title: 'Original execution', created_at: '', updated_at: '' })
const history: UITurn[] = [
  { id: 'original-user', turn_id: 'original-turn', role: 'user', text: 'synthetic input', timestamp: '2026-10-30T00:00:00Z', attachments: [] },
  { id: 'original-assistant', turn_id: 'original-turn', role: 'assistant', timestamp: '2026-10-30T00:00:01Z', messages: [{ id: 1, type: 'text', content: 'synthetic result' }] },
]
const flush = async () => { for (let i = 0; i < 25; i++) await Promise.resolve() }

function harness(options: { holdHistory?: boolean, connectError?: Error } = {}) {
  const currentBotId = ref<string | null>(bot), sessionId = ref<string | null>(null)
  let resolveHistory!: (turns: UITurn[]) => void
  api.fetchMessagesUI.mockReset().mockImplementation(() => options.holdHistory ? new Promise<UITurn[]>(resolve => { resolveHistory = resolve }) : Promise.resolve(history))
  api.fetchSessions.mockReset().mockResolvedValue({ items: [summary()], nextCursor: null })
  api.resolvePrivateChat.mockReset()
  const views = createChatViews({ currentBotId, sessionId, rememberBackgroundTask: task => task,
    applyPendingBackgroundEventsToTool: () => {}, bumpFsChangedAtIfFsMutation: () => {} })
  const sockets: Array<{ botId: string, handler: (event: UIStreamEvent) => void, socket: ChatWebSocket, open: () => void, sent: WSClientMessage[], close: ReturnType<typeof vi.fn> }> = []
  const transport: ChatRealtimeTransport = {
    createRetryingStream: () => ({ start: () => {}, stop: () => {} }),
    streamBotSessionsActivityEvents: async () => {},
    connectWebSocket: (botId, handler) => {
      if (options.connectError) throw options.connectError
      let connected = false
      const sent: WSClientMessage[] = [], close = vi.fn()
      const socket: ChatWebSocket = { get connected() { return connected }, onOpen: null, onClose: null,
        send: message => { sent.push(message) }, close, abort: () => {}, forget: () => {}, bindSession: () => {} }
      sockets.push({ botId, handler, socket, sent, close, open: () => { connected = true; socket.onOpen?.() } })
      return socket
    },
  }
  const realtime = createChatRealtimeController({ prepareSessionRuntime: views.loadInitialMessages,
    onRuntimeProjection: (bid, sid, change) => { views.sessionTranscript(bid, sid).applyRuntimeTranscript(change.current.transcript) },
    onWebSocketEvent: () => {}, onBotSessionsActivityEvent: () => {} }, transport)
  views.configure({ runtimeProjection: realtime.runtimeProjection, startSessionRuntime: realtime.startSessionRuntime,
    stopSessionRuntime: realtime.stopSessionRuntime, discardDraft: () => {}, invalidateDraftCommand: () => {},
    saveDraftExternalAgent: () => {}, activateDraftExternalAgent: () => {}, refreshAppliedHook: () => {}, ensureVisibleSummary: () => {} })
  let selection = 0
  const deps: ChatBootstrapDeps = {
    hasHistoryEntry: () => true, currentBotId, sessionId, draftIntent: ref(false), explicitSessionSelection: ref(false),
    userScopeGeneration: () => 0, bumpSelectSessionRequest: () => ++selection, currentSelectSessionRequest: () => selection,
    prepareForInitialization: views.prepareForInitialization, resetRefreshCoordinator: () => {}, resetSessionActivity: () => {},
    stopStreams: realtime.stopStreams, stopWebSocket: realtime.stopWebSocket, ensureBot: async () => currentBotId.value,
    replaceSessions: items => items, sessionsCursor: ref(null), hasMoreSessions: ref(false), ensureSessionSummary: async (_bid, id) => summary(id),
    pendingExternalAgentSessionInput: ref(null), clearPendingExternalAgentSession: () => {}, clearHistoryView: views.clearHistoryView,
    markHistoryEmpty: views.markHistoryEmpty, knownSessionSummary: id => summary(id), startWebSocket: realtime.startWebSocket,
    ensureWebSocket: realtime.ensureWebSocket, startBotSessionsActivityStream: realtime.startBotSessionsActivityStream,
    startSessionRuntime: realtime.startSessionRuntime, releasePreviousSession: () => {}, abort: () => {}, abortAllAssistantStreams: () => {},
    clearFsForBotSwitch: () => {}, clearRememberedSessions: () => {}, resetToEmptyComposer: () => {}, stageDefaultExternalAgentFromSettings: async () => {},
  }
  return { currentBotId, views, realtime, sockets, deps, bootstrap: createChatBootstrap(deps),
    finishHistory: () => resolveHistory(history), snapshot: () => sockets[0]!.handler({
      type: 'runtime_snapshot', session_id: session, epoch: 'history-epoch', seq: 1,
      snapshot: { bot_id: bot, session_id: session, epoch: 'history-epoch', seq: 1, updated_at: '2026-10-30T00:00:02Z' },
    }) }
}

describe('cold same-Bot historical runtime admission', () => {
  it.each(['rest-first', 'snapshot-first'] as const)('opens the target transport before history hydration and commits atomically (%s)', async order => {
    const h = harness({ holdHistory: order === 'snapshot-first' })
    const transcript = h.views.sessionTranscript(bot, session)
    await h.bootstrap.selectBot(bot, { sessionId: session }); await flush()
    expect(api.fetchMessagesUI).toHaveBeenCalledExactlyOnceWith(bot, session, { limit: 30 })
    expect(transcript.loadingMessages.value).toBe(true)
    expect(transcript.visibleMessages.value).toEqual([])
    expect(h.sockets.map(socket => socket.botId)).toEqual([bot])
    h.sockets[0]!.open()
    expect(h.sockets[0]!.sent).toEqual([{ type: 'runtime_subscribe', session_id: session }])
    h.snapshot(); await flush()
    if (order === 'snapshot-first') {
      expect(transcript.loadingMessages.value).toBe(true)
      expect(transcript.visibleMessages.value).toEqual([])
      h.finishHistory(); await flush()
    }
    expect(transcript.loadingMessages.value).toBe(false)
    expect(transcript.visibleMessages.value.map(row => row.id)).toEqual(['original-user', 'original-assistant'])
    expect(api.resolvePrivateChat).not.toHaveBeenCalled()
    h.realtime.stopStreams(); h.realtime.stopWebSocket()
  })

  it('reuses an existing handshaking target socket instead of resetting its connection or history', async () => {
    const h = harness()
    h.realtime.startWebSocket(bot)
    await h.bootstrap.selectBot(bot, { sessionId: session }); await flush()
    expect(h.sockets).toHaveLength(1)
    expect(h.sockets[0]!.close).not.toHaveBeenCalled()
    h.sockets[0]!.open(); h.snapshot(); await flush()
    expect(h.views.sessionTranscript(bot, session).visibleMessages.value).toHaveLength(2)
    expect(h.sockets[0]!.sent).toEqual([{ type: 'runtime_subscribe', session_id: session }])
    h.realtime.stopStreams(); h.realtime.stopWebSocket()
  })

  it('releases the same pending history when the real pane-view binding started runtime before Bot selection', async () => {
    const h = harness()
    const view = h.views.bindChatView('history-panel', { botId: bot, sessionId: session, viewId: 'history-panel' }, true)
    await flush()
    expect(view.transcript.loadingMessages.value).toBe(true)
    expect(h.sockets).toHaveLength(0)
    await h.bootstrap.selectBot(bot, { sessionId: session }); await flush()
    expect(h.sockets.map(socket => socket.botId)).toEqual([bot])
    h.sockets[0]!.open(); h.snapshot(); await flush()
    expect(view.transcript.visibleMessages.value.map(row => row.id)).toEqual(['original-user', 'original-assistant'])
    expect(api.fetchMessagesUI).toHaveBeenCalledTimes(1)
    h.realtime.stopStreams(); h.realtime.stopWebSocket()
  })

  it('propagates a transport setup error before starting a permanently masked history load', async () => {
    const error = new Error('synthetic transport setup failed'), h = harness({ connectError: error })
    await expect(h.bootstrap.selectBot(bot, { sessionId: session })).rejects.toBe(error)
    expect(api.fetchMessagesUI).not.toHaveBeenCalled()
    expect(h.views.sessionTranscript(bot, session).loadingMessages.value).toBe(false)
    expect(api.resolvePrivateChat).not.toHaveBeenCalled()
  })

  it('does not establish the old Bot transport after navigation supersedes a pending initialization', async () => {
    const h = harness()
    let finishList!: (result: { items: SessionSummary[], nextCursor: null }) => void
    api.fetchSessions.mockImplementationOnce(() => new Promise(resolve => { finishList = resolve }))
    const initializing = h.bootstrap.initialize(); await flush()
    const selecting = h.bootstrap.selectBot(bot, { sessionId: session })
    h.currentBotId.value = 'next-bot'; h.bootstrap.cancelContactChoice()
    finishList({ items: [summary()], nextCursor: null })
    await initializing; await selecting; await flush()
    expect(h.sockets.map(socket => socket.botId)).toEqual(['next-bot'])
    expect(api.fetchMessagesUI).not.toHaveBeenCalled()
    expect(api.resolvePrivateChat).not.toHaveBeenCalled()
    h.realtime.stopStreams(); h.realtime.stopWebSocket()
  })
})
