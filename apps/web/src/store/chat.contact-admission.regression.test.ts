import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import enMessages from '@/i18n/locales/en.json'
import { createPinia, disposePinia, setActivePinia, type Pinia } from 'pinia'
import type {
  BotSessionActivityEvent,
  RuntimeCurrentRunView,
  RuntimeDelta,
  RuntimeRunStatus,
  UIMessage,
  UIStreamEventHandler,
  UIUserTurn,
  WSAbortReason,
} from '@/composables/api/useChat'
import { useChatStore } from './chat-list'

const api = vi.hoisted(() => ({
  createSession: vi.fn(),
  deleteSession: vi.fn(),
  forkSessionFromTurn: vi.fn(),
  fetchSession: vi.fn(),
  fetchSessions: vi.fn(),
  resolvePrivateChat: vi.fn(),
  inspectPrivateChat: vi.fn(),
  bindPrivateChat: vi.fn(),
  fetchBots: vi.fn(),
  fetchMessagesUI: vi.fn(),
  executeQuickAction: vi.fn(),
  fetchSafeSkillCatalog: vi.fn(),
  updateSessionAgent: vi.fn(),
  ensureACPRuntime: vi.fn(),
  createACPRuntime: vi.fn(),
  fetchACPRuntimeByID: vi.fn(),
  setACPRuntimeMode: vi.fn(),
  setACPRuntimeModel: vi.fn(),
  setACPRuntimeModelByID: vi.fn(),
  setACPRuntimeReasoning: vi.fn(),
  setACPRuntimeReasoningByID: vi.fn(),
  closeACPRuntime: vi.fn(),
  streamBotSessionsActivityEvents: vi.fn(),
  connectWebSocket: vi.fn(),
  locateMessageUI: vi.fn(),
}))

const toast = vi.hoisted(() => ({
  error: vi.fn(),
}))

const sdk = vi.hoisted(() => ({
  getBotsByBotIdSettings: vi.fn(),
}))

vi.hoisted(() => {
  for (const name of ['localStorage', 'sessionStorage']) {
    Object.defineProperty(globalThis, name, {
      configurable: true,
      value: {
        getItem: () => null,
        setItem: () => {},
        removeItem: () => {},
        clear: () => {},
      },
    })
  }
})

vi.mock('@/composables/api/useChat', () => api)
vi.mock('@memohai/sdk', () => ({ getBotsByBotIdSettings: sdk.getBotsByBotIdSettings }))
vi.mock('vue-sonner', () => ({ toast }))
vi.mock('@felinic/ui', async (importOriginal) => {
  const original = await importOriginal<typeof import('@felinic/ui')>()
  return { ...original, toast }
})

function flushPromises() {
  return new Promise(resolve => setTimeout(resolve, 0))
}

type RuntimeTestUpdate =
  | { kind: 'run', status: RuntimeRunStatus, errorCode?: string }
  | { kind: 'message', message: UIMessage }
  | { kind: 'user_turn', turn: UIUserTurn }

const runtime = {
  started: { kind: 'run', status: 'running' } as RuntimeTestUpdate,
  completed: { kind: 'run', status: 'completed' } as RuntimeTestUpdate,
  // The run view has no error text; a failed run is described by its code.
  failed: (errorCode?: string): RuntimeTestUpdate => ({
    kind: 'run',
    status: 'errored',
    errorCode,
  }),
  message: (message: UIMessage): RuntimeTestUpdate => ({ kind: 'message', message }),
  userTurn: (turn: UIUserTurn): RuntimeTestUpdate => ({ kind: 'user_turn', turn }),
}


const h = {
  streamHandler: null as UIStreamEventHandler | null,
  sessionsActivityHandler: null as ((event: BotSessionActivityEvent) => void) | null,
  sendUpdates: [] as RuntimeTestUpdate[],
  // False makes the fake server answer nothing to a submission, so a test can
  // play a failure from before the server accepted it.
  acceptRuns: true,
  sentWSMessages: [] as Array<Record<string, unknown>>,
  runtimeUnsubscribes: [] as string[],
  wsRunIds: [] as string[],
  abortedWSRuns: [] as string[],
  abortedWSControls: [] as Array<{
    runId: string
    sessionId: string
    controlId: string
    reason?: WSAbortReason
  }>,
  lastRunId: '',
  lastSessionId: '',
  // Mirrors the server: a message without session_id creates a session and
  // announces it with session_created before run_accepted. Tests that drive
  // session_created themselves turn this off.
  inbandSessionId: 'session-1',
  manualSessionCreation: false,
  wsRunSeq: 0,
  runtimeBySession: new Map<string, {
    epoch: string
    seq: number
    run: RuntimeCurrentRunView | null
    publishedRunId: string
  }>(),
}

let pinia: Pinia

function testRuntime(sessionId: string) {
  let runtime = h.runtimeBySession.get(sessionId)
  if (!runtime) {
    runtime = { epoch: `epoch-${sessionId}`, seq: 0, run: null, publishedRunId: '' }
    h.runtimeBySession.set(sessionId, runtime)
  }
  return runtime
}

function emitRuntimeSnapshot(onEvent: UIStreamEventHandler, sessionId: string) {
  const runtime = testRuntime(sessionId)
  onEvent({
    type: 'runtime_snapshot',
    session_id: sessionId,
    epoch: runtime.epoch,
    seq: runtime.seq,
    snapshot: {
      bot_id: 'bot-1',
      session_id: sessionId,
      epoch: runtime.epoch,
      seq: runtime.seq,
      current_run_view: runtime.run ?? undefined,
      updated_at: new Date().toISOString(),
    },
  })
}

function publishRuntimeUpdate(
  onEvent: UIStreamEventHandler,
  sessionId: string,
  runId: string,
  update: RuntimeTestUpdate,
) {
  if (!sessionId || !runId) return
  const runtime = testRuntime(sessionId)
  const now = new Date().toISOString()
  const knownRun = runtime.run?.run_id === runId
  const publishedRun = runtime.publishedRunId === runId
  if (!knownRun) {
    runtime.run = {
      run_id: runId,
      turn_id: `turn-${runId}`,
      generation: `generation-${runId}`,
      status: 'running',
      started_at: now,
      updated_at: now,
      messages: [],
    }
  }
  const run = runtime.run!
  let delta: RuntimeDelta
  if (update.kind === 'run') {
    run.status = update.status
    run.error_code = update.errorCode
    run.updated_at = now
    delta = publishedRun
      ? {
          run: {
            run_id: runId,
            status: update.status,
            error_code: update.errorCode,
            updated_at: now,
          },
        }
      : { current_run_view: run }
  }
  else if (update.kind === 'message') {
    const messages = run.messages
    const incoming = { ...update.message }
    const index = messages.findIndex(message => message.id === incoming.id)
    if (index < 0) messages.push(incoming)
    else messages[index] = incoming
    run.updated_at = now
    delta = publishedRun
      ? { message_upserts: [incoming] }
      : { current_run_view: run }
  }
  else {
    run.request_user_turn = update.turn
    run.updated_at = now
    delta = { current_run_view: run }
  }
  runtime.seq += 1
  runtime.publishedRunId = runId
  onEvent({
    type: 'runtime_delta',
    session_id: sessionId,
    epoch: runtime.epoch,
    seq: runtime.seq,
    delta,
  })
}

function emitRuntime(
  update: RuntimeTestUpdate,
  sessionId = h.lastSessionId,
  runId = h.lastRunId,
) {
  if (h.streamHandler) publishRuntimeUpdate(h.streamHandler, sessionId, runId, update)
}


// The invocation the client minted for an outbound message, and the run the
// fake server named in reply. Negative indexes count from the newest send.
function wsInvocationId(index = 0): string {
  const message = index < 0 ? h.sentWSMessages.at(index) : h.sentWSMessages[index]
  return (message?.invocation_id as string | undefined) ?? ''
}

function wsRunId(index = 0): string {
  return (index < 0 ? h.wsRunIds.at(index) : h.wsRunIds[index]) ?? ''
}

function recordAbort(runId: string, sessionId: string, controlId: string, reason?: WSAbortReason) {
  h.abortedWSRuns.push(runId)
  h.abortedWSControls.push({ runId, sessionId, controlId, reason })
}

beforeEach(() => {
    pinia = createPinia()
    setActivePinia(pinia)
    // The runtime client coalesces delta projections to one per animation
    // frame; run frames synchronously so existing per-event assertions hold.
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
      callback(0)
      return 0
    })
    h.streamHandler = null
    h.sessionsActivityHandler = null
    h.lastRunId = ''
    h.lastSessionId = ''
    h.inbandSessionId = 'session-1'
    h.manualSessionCreation = false
    h.wsRunSeq = 0
    h.runtimeBySession = new Map()
    h.sentWSMessages = []
    h.runtimeUnsubscribes = []
    h.wsRunIds = []
    h.abortedWSRuns = []
    h.abortedWSControls = []
    h.sendUpdates = [runtime.started, runtime.failed('runtime_run_failed')]
    h.acceptRuns = true
    vi.resetAllMocks()

    api.fetchBots.mockResolvedValue([
      { id: 'bot-1', status: 'active', name: 'Bot' },
    ])
    api.fetchSessions.mockResolvedValue({ items: [], nextCursor: null })
    // Explicit fake server state: the declared owned chat is already bound.
    // Freeze that identity on first resolution, independently of later list
    // refreshes, pagination, UI selection, and user-entered target IDs.
    const contacts = new Map<string, Record<string, unknown>>()
    api.resolvePrivateChat.mockImplementation(async (botId: string) => {
      if (contacts.has(botId)) return contacts.get(botId)
      const firstList = api.fetchSessions.mock.results.find((_result, index) =>
        api.fetchSessions.mock.calls[index]?.[0] === botId)
      const page = firstList?.type === 'return' ? await firstList.value : undefined
      const declared = page?.items?.find((session: { bot_id: string, type?: string }) =>
        session.bot_id === botId && session.type !== 'schedule' && session.type !== 'subagent')
      const contact = declared ?? {
        id: 'session-1', bot_id: botId, title: 'Fixed contact', type: 'chat',
        channel_type: 'local',
      }
      contacts.set(botId, contact)
      return contact
    })
    api.inspectPrivateChat.mockImplementation(async (botId: string) => ({
      session: await api.resolvePrivateChat(botId), candidates: [], nextCursor: null,
    }))
    api.fetchSession.mockImplementation(async (botId: string, sessionId: string) => ({
      id: sessionId, bot_id: botId, title: 'Session', type: 'chat',
    }))
    api.createSession.mockResolvedValue({
      id: 'session-1',
      bot_id: 'bot-1',
      title: 'New session',
      type: 'chat',
    })
    api.updateSessionAgent.mockResolvedValue({
      id: 'session-1',
      bot_id: 'bot-1',
      title: '',
      type: 'acp_agent',
      metadata: {
        acp_agent_id: 'codex',
        project_path: '/data/app',
      },
    })
    api.ensureACPRuntime.mockResolvedValue({
      session_id: 'session-1',
      agent_id: 'codex',
      models: {
        current_model_id: 'gpt-5.1-codex',
        available_models: [{ id: 'gpt-5.1-codex', name: 'GPT-5.1 Codex' }],
      },
      reasoning: {
        current_effort: 'medium',
        available_efforts: [
          { id: 'medium', name: 'Medium' },
          { id: 'high', name: 'High' },
        ],
      },
    })
    api.createACPRuntime.mockResolvedValue({
      runtime_id: 'rt_warm',
      agent_id: 'codex',
      state: 'idle',
      default_model_id: 'gpt-5.1-codex',
      models: {
        current_model_id: 'gpt-5.1-codex',
        available_models: [
          { id: 'gpt-5.1-codex', name: 'GPT-5.1 Codex' },
          { id: 'gpt-5.1-codex-high', name: 'GPT-5.1 Codex High' },
        ],
      },
      reasoning: {
        current_effort: 'medium',
        available_efforts: [
          { id: 'medium', name: 'Medium' },
          { id: 'high', name: 'High' },
        ],
      },
    })
    api.fetchACPRuntimeByID.mockResolvedValue({
      runtime_id: 'rt_warm',
      agent_id: 'codex',
      state: 'idle',
      default_model_id: 'gpt-5.1-codex',
      models: {
        current_model_id: 'gpt-5.1-codex',
        available_models: [
          { id: 'gpt-5.1-codex', name: 'GPT-5.1 Codex' },
          { id: 'gpt-5.1-codex-high', name: 'GPT-5.1 Codex High' },
        ],
      },
      reasoning: {
        current_effort: 'medium',
        available_efforts: [
          { id: 'medium', name: 'Medium' },
          { id: 'high', name: 'High' },
        ],
      },
    })
    api.setACPRuntimeModel.mockResolvedValue({
      session_id: 'session-1',
      agent_id: 'codex',
      models: {
        current_model_id: 'gpt-5.1-codex-high',
        available_models: [{ id: 'gpt-5.1-codex-high', name: 'GPT-5.1 Codex High' }],
      },
    })
    api.setACPRuntimeModelByID.mockResolvedValue({
      runtime_id: 'rt_warm',
      agent_id: 'codex',
      state: 'idle',
      default_model_id: 'gpt-5.1-codex',
      models: {
        current_model_id: 'gpt-5.1-codex-high',
        available_models: [{ id: 'gpt-5.1-codex-high', name: 'GPT-5.1 Codex High' }],
      },
    })
    api.setACPRuntimeReasoning.mockResolvedValue({
      session_id: 'session-1',
      agent_id: 'codex',
      reasoning: {
        current_effort: 'low',
        available_efforts: [{ id: 'low', name: 'Low' }],
      },
    })
    api.setACPRuntimeReasoningByID.mockResolvedValue({
      runtime_id: 'rt_warm',
      agent_id: 'codex',
      state: 'idle',
      reasoning: {
        current_effort: 'high',
        available_efforts: [
          { id: 'medium', name: 'Medium' },
          { id: 'high', name: 'High' },
        ],
      },
    })
    api.closeACPRuntime.mockResolvedValue(undefined)
    api.fetchMessagesUI.mockResolvedValue([])
    api.executeQuickAction.mockResolvedValue(null)
    api.fetchSafeSkillCatalog.mockResolvedValue([])
    sdk.getBotsByBotIdSettings.mockResolvedValue({ data: { chat_runtime: 'model' } })
    api.streamBotSessionsActivityEvents.mockImplementation((_botId: string, signal: AbortSignal, onEvent: (event: BotSessionActivityEvent) => void) => new Promise<void>((resolve) => {
      h.sessionsActivityHandler = onEvent
      onEvent({ type: 'activity_ready', cache_invalidation: true })
      signal.addEventListener('abort', () => resolve(), { once: true })
    }))
    api.connectWebSocket.mockImplementation((_botId: string, onStreamEvent: UIStreamEventHandler) => {
      h.streamHandler = onStreamEvent
      return {
        get connected() {
          return true
        },
        send: vi.fn((message: {
          type?: string
          invocation_id?: string
          session_id?: string
          message_id?: string
          text?: string
          workdir_id?: string
        }) => {
          if (message.type === 'runtime_subscribe') {
            if (message.session_id) emitRuntimeSnapshot(onStreamEvent, message.session_id)
            return
          }
          if (message.type === 'runtime_unsubscribe') {
            if (message.session_id) h.runtimeUnsubscribes.push(message.session_id)
            return
          }
          h.sentWSMessages.push(message as Record<string, unknown>)
          h.lastSessionId = message.session_id ?? ''
          if (
            message.type === 'tool_approval_response'
            || message.type === 'user_input_response'
          ) {
            const control = message as typeof message & {
              control_id?: string
              decision_id?: string
              run_id?: string
              decision?: 'approve' | 'reject'
              canceled?: boolean
            }
            const runtime = testRuntime(h.lastSessionId)
            onStreamEvent({
              type: 'control_ack',
              session_id: h.lastSessionId,
              run_id: control.run_id ?? runtime.run?.run_id ?? '',
              control: message.type,
              control_id: control.control_id ?? '',
              applied: true,
            })
            if (runtime.run) {
              const decisionId = control.decision_id ?? ''
              const updated = runtime.run.messages.flatMap((candidate): UIMessage[] => {
                if (candidate.type !== 'tool') return []
                if (
                  message.type === 'tool_approval_response'
                  && candidate.approval?.approval_id === decisionId
                ) {
                  return [{
                    ...candidate,
                    approval: {
                      ...candidate.approval,
                      status: control.decision === 'reject' ? 'rejected' : 'approved',
                      can_approve: false,
                    },
                  }]
                }
                if (
                  message.type === 'user_input_response'
                  && candidate.user_input?.user_input_id === decisionId
                ) {
                  return [{
                    ...candidate,
                    user_input: {
                      ...candidate.user_input,
                      status: control.canceled ? 'canceled' : 'submitted',
                      can_respond: false,
                    },
                  }]
                }
                return []
              })
              for (const candidate of updated) {
                const index = runtime.run.messages.findIndex(item => item.id === candidate.id)
                if (index >= 0) runtime.run.messages[index] = candidate
              }
              if (updated.length) {
                runtime.seq += 1
                onStreamEvent({
                  type: 'runtime_delta',
                  session_id: h.lastSessionId,
                  epoch: runtime.epoch,
                  seq: runtime.seq,
                  delta: { message_upserts: updated },
                })
              }
            }
            return
          }
          // The server names the run and announces it before any turn output,
          // so every later event is addressed by run_id.
          const invocationId = message.invocation_id ?? ''
          if (message.type === 'message' && !h.lastSessionId && invocationId && !h.manualSessionCreation) {
            h.lastSessionId = h.inbandSessionId
            onStreamEvent({
              type: 'session_created',
              invocation_id: invocationId,
              session_id: h.lastSessionId,
              workdir_id: message.workdir_id,
            })
          }
          h.lastRunId = invocationId && h.acceptRuns ? `run-${++h.wsRunSeq}` : ''
          h.wsRunIds.push(h.lastRunId)
          if (h.lastRunId) {
            const runtime = testRuntime(h.lastSessionId)
            const now = new Date().toISOString()
            runtime.run = {
              run_id: h.lastRunId,
              turn_id: `turn-${h.lastRunId}`,
              invocation_id: invocationId,
              generation: `generation-${h.lastRunId}`,
              status: 'admitting',
              started_at: now,
              updated_at: now,
              messages: [],
              request_user_turn: message.type === 'message'
                ? {
                    turn_id: `turn-${h.lastRunId}`,
                    role: 'user',
                    text: message.text ?? '',
                    timestamp: now,
                  }
                : undefined,
              operation: message.type === 'retry_message'
                ? {
                    kind: 'retry',
                    replace_from_message_id: message.message_id ?? '',
                  }
                : message.type === 'edit_message'
                  ? {
                      kind: 'edit',
                      replace_from_message_id: message.message_id ?? '',
                      replacement_user_turn: {
                        turn_id: `turn-${h.lastRunId}`,
                        role: 'user',
                        text: message.text ?? '',
                        timestamp: now,
                      },
                    }
                  : undefined,
            }
            onStreamEvent({
              type: 'run_accepted',
              run_id: h.lastRunId,
              invocation_id: invocationId,
              session_id: h.lastSessionId,
              turn_id: runtime.run.turn_id,
              epoch: runtime.epoch,
              seq: runtime.seq + 1,
            })
          }
          for (const update of h.sendUpdates) {
            if (!h.lastSessionId && update.kind === 'run' && update.status === 'errored') {
              onStreamEvent({
                type: 'error',
                run_id: h.lastRunId,
                invocation_id: invocationId,
                // The frame carries a code and its copy, never the run's text.
                code: update.errorCode,
                message: '',
              })
              continue
            }
            publishRuntimeUpdate(onStreamEvent, h.lastSessionId, h.lastRunId, update)
          }
        }),
        abort: vi.fn(recordAbort),
        close: vi.fn(),
        forget: vi.fn(),
        bindSession: vi.fn(),
        onOpen: null,
        onClose: null,
      }
    })
})

afterEach(() => {
  // A timed-out async case may never reach its own finally block.
  vi.useRealTimers()
  disposePinia(pinia)
  vi.unstubAllGlobals()
})


describe('Fixed contact pending-admission source regressions', () => {
  it('blocks a second deferred draft send while the first stream is still unbound', async () => {
      h.manualSessionCreation = true
      h.sendUpdates = []
      const store = useChatStore()

      await store.selectBot('bot-1')
      const firstSend = store.sendMessage('first activation', undefined, {
        requestedSkills: [{ name: 'alpha' }],
        composerScope: 'bot-1:draft-a',
      })
      await flushPromises()
      const runId = wsRunId(0)

      expect(runId).toBe('run-1')
      const secondSend = store.sendMessage('second activation', undefined, {
        requestedSkills: [{ name: 'beta' }],
        composerScope: 'bot-1:draft-a',
      })
      await flushPromises()
      expect(h.sentWSMessages).toHaveLength(1)
      const secondResult = await secondSend
      expect(secondResult).toMatchObject({ ok: false, stage: 'startup' })

      const invocationId = wsInvocationId(0)
      h.streamHandler?.({
        type: 'session_created',
        invocation_id: invocationId,
        session_id: 'session-1',
      })
      emitRuntime(runtime.completed, 'session-1', runId)
      await expect(firstSend).resolves.toMatchObject({ ok: true })
      expect(store.streaming).toBe(false)
    })

  it('bounds an unacknowledged contact invocation without cancelling or resending it', async () => {
    h.sendUpdates = []
    h.acceptRuns = false
    const store = useChatStore()
    await store.selectBot('bot-1')
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    let result: Awaited<ReturnType<typeof store.sendMessage>> | undefined
    try {
      void store.sendMessage('hello').then(value => { result = value })
      await vi.advanceTimersByTimeAsync(0)
      const sent = h.sentWSMessages.filter(message => message.type === 'message')
      expect(sent).toHaveLength(1)
      expect(sent[0]).toMatchObject({ session_id: 'session-1', invocation_id: expect.any(String) })
      const socket = api.connectWebSocket.mock.results.at(-1)!.value
      socket.onClose?.()
      await vi.advanceTimersByTimeAsync(30_000)
      expect(h.abortedWSRuns).toEqual([])
      expect(api.deleteSession).not.toHaveBeenCalled()
      expect(h.sentWSMessages.filter(message => message.type === 'message')).toEqual(sent)
      expect(store.sessionId).toBe('session-1')
      expect(result).toMatchObject({ ok: false, error: enMessages.chat.sendOutcomeUnknown })
    } finally {
      store.abort()
      vi.useRealTimers()
    }
  })

})

// Additional protocol guards on the same fixed-contact fixture; the two
// integration-owned reproducer bodies above are intentionally unchanged.
describe('Fixed contact admission guards', () => {
  it.each([
    ['error', { type: 'error', code: 'workspace_target.unavailable', message: 'offline' }],
    ['rejected', { type: 'run_rejected', code: 'session_runtime.ledger_unavailable', message: 'unavailable' }],
    ['command', { type: 'command_error', terminal: true, code: 'slash.unknown', message: 'unknown' }],
    ['busy', { type: 'command_error', terminal: true, code: 'session_runtime.session_busy', message: 'busy' }],
  ] as const)('keeps disconnected admission unknown after %s refusal', async (_, refusal) => {
    h.sendUpdates = []
    h.acceptRuns = false
    const store = useChatStore()
    await store.selectBot('bot-1')
    const sending = store.sendMessage('original request')
    api.connectWebSocket.mock.results.at(-1)!.value.onClose?.()
    h.streamHandler?.({ ...refusal, invocation_id: wsInvocationId(), session_id: 'session-1' })
    expect(await sending).toMatchObject({ ok: false, error: enMessages.chat.sendOutcomeUnknown })
    expect(store.sessionId).toBe('session-1')
    expect(api.deleteSession).not.toHaveBeenCalled()
    expect(h.abortedWSRuns).toEqual([])
    expect(h.sentWSMessages).toHaveLength(1)
  })

  it('reserves a fixed contact before a second send in the same frame', async () => {
    h.sendUpdates = []
    const store = useChatStore()
    await store.selectBot('bot-1')
    const first = store.sendMessage('first', undefined, { requestedSkills: [{ name: 'alpha' }] })
    expect(await store.sendMessage('second')).toMatchObject({ ok: false, stage: 'startup' })
    expect(h.sentWSMessages).toHaveLength(1)
    expect(store.streaming).toBe(true)
    emitRuntime(runtime.completed)
    expect(await first).toMatchObject({ ok: true })
  })

  it.each([true, false])('does not cancel late acceptance after unknown (disconnected: %s)', async (disconnected) => {
    h.sendUpdates = []
    h.acceptRuns = false
    const store = useChatStore()
    await store.selectBot('bot-1')
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    const sending = store.sendMessage('original request')
    const socket = api.connectWebSocket.mock.results.at(-1)!.value
    if (disconnected) socket.onClose?.()
    await vi.advanceTimersByTimeAsync(30_000)
    expect(await sending).toMatchObject({ ok: false, error: enMessages.chat.sendOutcomeUnknown })
    expect(socket.forget).toHaveBeenCalledWith(wsInvocationId())
    h.streamHandler?.({ type: 'run_accepted', invocation_id: wsInvocationId(), session_id: 'session-1',
      run_id: 'late-run', turn_id: 'late-turn', epoch: 'epoch-session-1', seq: 1 })
    expect(h.abortedWSRuns).toEqual([])
    expect(api.deleteSession).not.toHaveBeenCalled()
    expect(h.sentWSMessages).toHaveLength(1)
    expect(store.sessionId).toBe('session-1')
    expect(store.streaming).toBe(false)
  })

  it('preserves an explicit stop before acceptance and addresses the late run', async () => {
    h.sendUpdates = []
    h.acceptRuns = false
    const store = useChatStore()
    await store.selectBot('bot-1')
    const sending = store.sendMessage('stop me')
    store.abort()
    expect(await sending).toMatchObject({ ok: false })
    h.streamHandler?.({ type: 'run_accepted', invocation_id: wsInvocationId(), session_id: 'session-1',
      run_id: 'late-run', turn_id: 'late-turn', epoch: 'epoch-session-1', seq: 1 })
    expect(h.abortedWSRuns).toEqual(['late-run'])
    expect(h.abortedWSControls[0]).toMatchObject({ sessionId: 'session-1', reason: 'user_stop' })
  })

  it('does not time out accepted work or block the contact after its main run completes', async () => {
    h.sendUpdates = [runtime.started]
    const store = useChatStore()
    await store.selectBot('bot-1')
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    let settled = false
    const first = store.sendMessage('delegate work').then(value => { settled = true; return value })
    await vi.advanceTimersByTimeAsync(30_000)
    expect(settled).toBe(false)
    expect(h.abortedWSRuns).toEqual([])
    // An independent task session remains active while the main run returns.
    emitRuntime(runtime.started, 'task-session', 'task-run')
    emitRuntime(runtime.completed, 'session-1', wsRunId())
    await vi.advanceTimersByTimeAsync(0)
    expect(await first).toMatchObject({ ok: true })
    expect(store.streaming).toBe(false)
    h.sendUpdates = [runtime.started, runtime.completed]
    expect(await store.sendMessage('continue chatting')).toMatchObject({ ok: true })
    expect(h.sentWSMessages).toHaveLength(2)
    expect(testRuntime('task-session').run?.status).toBe('running')
  })
})

it('uses confirmed runtime failure after a disconnected submission is accepted', async () => {
  h.sendUpdates = []
  h.acceptRuns = false
  const store = useChatStore()
  await store.selectBot('bot-1')
  const sending = store.sendMessage('confirmed after reconnect')
  api.connectWebSocket.mock.results.at(-1)!.value.onClose?.()
  h.streamHandler?.({ type: 'run_accepted', invocation_id: wsInvocationId(), session_id: 'session-1',
    run_id: 'confirmed-run', turn_id: 'confirmed-turn', epoch: 'epoch-session-1', seq: 1 })
  h.streamHandler?.({ type: 'error', invocation_id: wsInvocationId(), session_id: 'session-1',
    run_id: 'confirmed-run', code: 'runtime_run_failed', message: '' })
  const result = await sending
  expect(result).toMatchObject({ ok: false, stage: 'stream' })
  expect(result.error).not.toBe(enMessages.chat.sendOutcomeUnknown)
  expect(h.abortedWSRuns).toEqual([])
})
