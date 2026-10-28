import { ref, type Ref } from 'vue'
import { fetchSessions, resolvePrivateChat, inspectPrivateChat, bindPrivateChat, type SessionSummary, type PrivateChatEntry } from '@/composables/api/useChat'
import { apiErrorStatus, isApiErrorCode } from '@/utils/api-error'
import type { WSAbortReason } from '@/composables/api/useChat.ws'
import type { ChatViewTarget, ExternalAgentSessionInput } from './types'

export interface ChatBootstrapDeps {
  hasHistoryEntry?: () => boolean
  hasContactEntry?: () => boolean
  currentBotId: Ref<string | null>
  sessionId: Ref<string | null>
  draftIntent: Ref<boolean>
  explicitSessionSelection: Ref<boolean>
  userScopeGeneration: () => number
  bumpSelectSessionRequest: () => number
  currentSelectSessionRequest: () => number
  prepareForInitialization: () => void
  resetRefreshCoordinator: () => void
  resetSessionActivity: () => void
  stopStreams: () => void
  stopWebSocket: () => void
  ensureBot: () => Promise<string | null>
  replaceSessions: (sessions: SessionSummary[]) => SessionSummary[]
  sessionsCursor: Ref<string | null>
  hasMoreSessions: Ref<boolean>
  ensureSessionSummary: (
    botId: string,
    sessionId: string,
    requestId?: number,
  ) => Promise<SessionSummary | null>
  pendingExternalAgentSessionInput: Ref<ExternalAgentSessionInput | null>
  clearPendingExternalAgentSession: () => void
  clearHistoryView: () => void
  markHistoryEmpty: () => void
  knownSessionSummary: (sessionId: string) => SessionSummary | null | undefined
  startWebSocket: (botId: string) => void
  startBotSessionsActivityStream: (botId: string) => void
  startSessionRuntime: (botId: string, sessionId: string) => void
  releasePreviousSession: (botId: string, sessionId: string) => void
  abort: (target?: ChatViewTarget, reason?: WSAbortReason) => void
  abortAllAssistantStreams: (reason?: WSAbortReason) => void
  clearFsForBotSwitch: () => void
  clearRememberedSessions: () => void
  resetToEmptyComposer: (options: {
    clearPendingExternalAgent?: boolean
    explicitSelection?: boolean
    draftIntent?: boolean
  }) => void
  stageDefaultExternalAgentFromSettings: (requestId: number) => Promise<void>
}

export function createChatBootstrap(deps: ChatBootstrapDeps) {
  const loadingChats = ref(false)
  const initializing = ref(false)
  let rerunRequested = false
  let initializingBotId: string | null = null
  let initializePromise: Promise<void> | null = null

  const contactChoice = ref<(PrivateChatEntry & { botId: string }) | null>(null)
  const contactBusy = ref(false)
  const contactOpenRequested = ref<{ botId: string, sessionId: string } | null>(null)
  // Route intent is separate from persisted tab focus. A restored task tab is
  // never evidence that the user entered that task instead of the Bot contact.
  const contactNavigationPending = ref(deps.hasContactEntry?.() === true)
  let contactRequest = 0

  async function resolveContact(botId: string, valid: () => boolean): Promise<SessionSummary | undefined> {
    const entry = await inspectPrivateChat(botId, undefined, deps.explicitSessionSelection.value ? deps.sessionId.value ?? undefined : undefined)
    if (!valid()) return undefined
    if (entry.session) return entry.session
    if (entry.candidates.length === 1 && !entry.nextCursor) {
      // A unique, server-verified ordinary history needs no extra user gate.
      // The bind still checks ownership and first-writer semantics atomically.
      return bindPrivateChat(botId, entry.candidates[0]!.id)
    }
    if (entry.candidates.length) {
      contactChoice.value = { ...entry, botId }
      return undefined
    }
    return resolvePrivateChat(botId)
  }

  async function openContact() {
    const botId = deps.currentBotId.value
    if (!botId || contactBusy.value) return
    const request = ++contactRequest
    const selectionRequest = deps.currentSelectSessionRequest()
    const generation = deps.userScopeGeneration()
    contactOpenRequested.value = null
    contactBusy.value = true
    contactNavigationPending.value = true
    try {
      // The primary entrance resolves the server-owned identity directly. It
      // must not adopt a historical session because it happened to be focused.
      const session = await resolvePrivateChat(botId)
      if (request !== contactRequest || selectionRequest !== deps.currentSelectSessionRequest() || generation !== deps.userScopeGeneration() || deps.currentBotId.value !== botId) return
      if (session) {
        contactChoice.value = null
        contactOpenRequested.value = { botId, sessionId: session.id }
      }
    } finally {
      if (request === contactRequest) {
        contactBusy.value = false
        if (!contactOpenRequested.value) contactNavigationPending.value = false
      }
    }
  }

  function completeContactOpen(request: { botId: string, sessionId: string }) {
    if (contactOpenRequested.value !== request) return
    contactOpenRequested.value = null
    contactNavigationPending.value = false
  }

  function cancelContactNavigation() {
    contactRequest++
    contactBusy.value = false
    contactNavigationPending.value = false
    contactOpenRequested.value = null
  }

  async function loadContactCandidates() {
    const choice = contactChoice.value
    if (!choice?.nextCursor || contactBusy.value) return
    const generation = deps.userScopeGeneration()
    const request = ++contactRequest
    contactBusy.value = true
    try {
      const page = await inspectPrivateChat(choice.botId, choice.nextCursor)
      if (request !== contactRequest || contactChoice.value !== choice || generation !== deps.userScopeGeneration()) return
      if (page.session) {
        contactChoice.value = null
        contactOpenRequested.value = { botId: choice.botId, sessionId: page.session.id }
      } else contactChoice.value = { ...choice, candidates: [...new Map([...choice.candidates, ...page.candidates].map(s => [s.id, s])).values()], nextCursor: page.nextCursor }
    } finally { if (request === contactRequest && generation === deps.userScopeGeneration()) contactBusy.value = false }
  }

  async function confirmContact(sessionId: string | null) {
    const choice = contactChoice.value
    if (!choice || contactBusy.value) return
    const generation = deps.userScopeGeneration()
    const request = ++contactRequest
    contactBusy.value = true
    try {
      // Inspect first after failures too. This reconciles unknown commits and
      // another device's first binding without replaying a different choice.
      const current = await inspectPrivateChat(choice.botId)
      if (request !== contactRequest || contactChoice.value !== choice || generation !== deps.userScopeGeneration() || deps.currentBotId.value !== choice.botId) return
      const session = current.session ?? (sessionId
        ? await bindPrivateChat(choice.botId, sessionId)
        : await resolvePrivateChat(choice.botId))
      if (request !== contactRequest || contactChoice.value !== choice || generation !== deps.userScopeGeneration() || deps.currentBotId.value !== choice.botId) return
      contactChoice.value = null
      contactOpenRequested.value = { botId: choice.botId, sessionId: session.id }
    } finally { if (request === contactRequest && generation === deps.userScopeGeneration()) contactBusy.value = false }
  }

  function cancelContactChoice() {
    if (!contactBusy.value) contactChoice.value = null
  }

  async function initialize() {
    if (initializing.value) {
      const requestedBotId = (deps.currentBotId.value ?? '').trim() || null
      if (initializingBotId && requestedBotId !== initializingBotId) rerunRequested = true
      if (initializePromise) await initializePromise
      return
    }

    const generation = deps.userScopeGeneration()
    // The promise clears its own slot in finally, so it must be assigned after
    // construction while remaining visible to that closure.
    let run!: Promise<void>
    // eslint-disable-next-line prefer-const
    run = (async () => {
      initializing.value = true
      loadingChats.value = true
      try {
        do {
          if (generation !== deps.userScopeGeneration()) return
          rerunRequested = false
          initializingBotId = (deps.currentBotId.value ?? '').trim() || null
          deps.prepareForInitialization()
          deps.resetRefreshCoordinator()
          deps.resetSessionActivity()
          deps.stopStreams()
          deps.stopWebSocket()

          const botId = await deps.ensureBot()
          if (generation !== deps.userScopeGeneration()) return
          if (!botId) {
            deps.replaceSessions([])
            deps.sessionsCursor.value = null
            deps.hasMoreSessions.value = false
            deps.sessionId.value = null
            deps.clearPendingExternalAgentSession()
            deps.clearHistoryView()
            continue
          }
          initializingBotId = botId

          let response: Awaited<ReturnType<typeof fetchSessions>>
          try {
            response = await fetchSessions(botId)
          } catch (error) {
            if (generation !== deps.userScopeGeneration()) return
            if ((deps.currentBotId.value ?? '').trim() !== botId) {
              rerunRequested = true
              continue
            }
            throw error
          }
          if (generation !== deps.userScopeGeneration()) return
          if ((deps.currentBotId.value ?? '').trim() !== botId) {
            rerunRequested = true
            continue
          }

          const visibleSessions = deps.replaceSessions(response.items)
          deps.sessionsCursor.value = response.nextCursor
          deps.hasMoreSessions.value = response.nextCursor !== null

          // Schedule changes and background-created sessions use the bot-wide
          // activity stream. Start it after applying the initial list snapshot,
          // but before restoring a specific view or opening the WebSocket, so a
          // failure in either path cannot disable every live list update.
          deps.startBotSessionsActivityStream(botId)

          const restoredSessionId = (deps.sessionId.value ?? '').trim()
          const preferContact = contactNavigationPending.value && !deps.hasHistoryEntry?.()
          const restoredExplicitSession = !preferContact && restoredSessionId
            && deps.explicitSessionSelection.value
            ? await deps.ensureSessionSummary(botId, restoredSessionId)
            : null
          if (generation !== deps.userScopeGeneration()) return
          if ((deps.currentBotId.value ?? '').trim() !== botId) {
            rerunRequested = true
            continue
          }
          const preservePendingExternalAgentStage = !preferContact && !!deps.pendingExternalAgentSessionInput.value
            && !deps.sessionId.value
          const preserveExplicitEmptyComposer = !preferContact && deps.explicitSessionSelection.value
            && !deps.sessionId.value
          let contact: SessionSummary | undefined
          if (!preservePendingExternalAgentStage && !preserveExplicitEmptyComposer && !restoredExplicitSession && (preferContact || !deps.draftIntent.value) && !deps.hasHistoryEntry?.()) {
            const request = deps.currentSelectSessionRequest()
            try {
              contact = preferContact
                ? await resolvePrivateChat(botId)
                : await resolveContact(botId, () => generation === deps.userScopeGeneration() && request === deps.currentSelectSessionRequest() && deps.currentBotId.value === botId && !deps.hasHistoryEntry?.())
            } catch (error) {
              // Old servers retain their original navigation. Auth, storage and
              // uncertain outcomes must never fall back to a different chat.
              const status = apiErrorStatus(error)
              const unsupported = (status === 404 && isApiErrorCode(error, 'http.not_found'))
                || (status === 405 && isApiErrorCode(error, 'http.method_not_allowed'))
              if (!unsupported) throw error
              contactNavigationPending.value = false
            }
            if (generation !== deps.userScopeGeneration()) return
            if (request !== deps.currentSelectSessionRequest()) return
            if ((!preferContact && deps.draftIntent.value) || deps.hasHistoryEntry?.()) contact = undefined
            if ((deps.currentBotId.value ?? '').trim() !== botId) {
              rerunRequested = true
              continue
            }
          }

          if (preservePendingExternalAgentStage) {
            deps.sessionId.value = null
            deps.markHistoryEmpty()
          } else if (preserveExplicitEmptyComposer) {
            deps.sessionId.value = null
            deps.clearHistoryView()
          } else if (restoredExplicitSession) {
            deps.draftIntent.value = false
          } else if (contact) {
            deps.replaceSessions([contact, ...visibleSessions.filter(session => session.id !== contact.id)])
            deps.sessionId.value = contact.id
            deps.explicitSessionSelection.value = false
            deps.draftIntent.value = false
            contactOpenRequested.value = { botId, sessionId: contact.id }
          } else if (contactChoice.value || deps.hasHistoryEntry?.()) {
            // Choice/deep-link navigation owns selection. Never guess a recent chat.
          } else if (!visibleSessions.length) {
            deps.sessionId.value = null
            deps.explicitSessionSelection.value = false
            deps.clearHistoryView()
          } else if (
            deps.sessionId.value
            && deps.knownSessionSummary(deps.sessionId.value)
          ) {
            deps.draftIntent.value = false
          } else if (deps.draftIntent.value) {
            deps.sessionId.value = null
            deps.clearHistoryView()
          } else {
            const firstConversation = visibleSessions.find(
              session => (session.type ?? 'chat') !== 'schedule',
            )
            deps.sessionId.value = (firstConversation ?? visibleSessions[0]!).id
            deps.explicitSessionSelection.value = false
          }

          deps.startWebSocket(botId)
          if (deps.sessionId.value) {
            deps.startSessionRuntime(botId, deps.sessionId.value)
          }
        } while (rerunRequested)
      } finally {
        if (generation === deps.userScopeGeneration()) {
          loadingChats.value = false
          initializing.value = false
          initializingBotId = null
          rerunRequested = false
        }
        if (initializePromise === run) initializePromise = null
      }
    })()
    initializePromise = run
    await run
  }

  function switchActiveSession(sessionId: string, previousSessionId = '') {
    const botId = (deps.currentBotId.value ?? '').trim()
    if (!botId || !sessionId) return
    const previous = previousSessionId.trim()
    if (previous && previous !== sessionId) {
      deps.releasePreviousSession(botId, previous)
    }
    deps.startSessionRuntime(botId, sessionId)
  }

  async function selectBot(targetBotId: string, options: { sessionId?: string } = {}) {
    cancelContactNavigation()
    contactNavigationPending.value = !options.sessionId
    const navigation = contactRequest
    if (deps.currentBotId.value === targetBotId) {
      // A cold URL can arrive before bootstrap opens the socket. Wait for
      // that transport boundary before selecting the historical runtime.
      if (initializePromise) await initializePromise
      if (deps.currentBotId.value !== targetBotId || navigation !== contactRequest) return
      if (options.sessionId) await selectSession(options.sessionId)
      else await openContact()
      return
    }
    contactRequest++
    contactBusy.value = false
    contactChoice.value = null
    contactOpenRequested.value = null
    deps.bumpSelectSessionRequest()
    deps.abort(undefined, 'bot_switch')
    deps.abortAllAssistantStreams('bot_switch')
    deps.clearPendingExternalAgentSession()
    deps.clearFsForBotSwitch()
    deps.resetSessionActivity()
    deps.replaceSessions([])
    deps.sessionsCursor.value = null
    deps.hasMoreSessions.value = false
    deps.currentBotId.value = targetBotId
    deps.sessionId.value = options.sessionId ?? null
    deps.clearRememberedSessions()
    deps.explicitSessionSelection.value = !!options.sessionId
    deps.draftIntent.value = false
    await initialize()
  }

  async function selectSession(
    targetSessionId: string,
    options: { explicitSelection?: boolean } = {},
  ) {
    const sessionId = targetSessionId.trim()
    if (!sessionId) return
    cancelContactNavigation()
    const previousSessionId = (deps.sessionId.value ?? '').trim()
    const sameSession = sessionId === previousSessionId
    const requestId = deps.bumpSelectSessionRequest()
    const botId = (deps.currentBotId.value ?? '').trim()
    deps.clearPendingExternalAgentSession()
    deps.sessionId.value = sessionId
    deps.draftIntent.value = false
    deps.explicitSessionSelection.value = options.explicitSelection !== false
    if (!sameSession) switchActiveSession(sessionId, previousSessionId)
    await deps.ensureSessionSummary(botId, sessionId, requestId)
  }

  async function createNewSession(options: { explicitSelection?: boolean } = {}) {
    cancelContactNavigation()
    const botId = await deps.ensureBot()
    if (!botId) return
    deps.resetToEmptyComposer({
      explicitSelection: options.explicitSelection === true,
      draftIntent: true,
    })
  }

  function selectDraft(options: { explicitSelection?: boolean } = {}) {
    cancelContactNavigation()
    const explicitSelection = options.explicitSelection === true
    deps.resetToEmptyComposer({
      clearPendingExternalAgent: false,
      explicitSelection,
      draftIntent: true,
    })
    if (!explicitSelection) {
      void deps.stageDefaultExternalAgentFromSettings(deps.currentSelectSessionRequest())
    }
  }

  function reset() {
    contactRequest++
    contactChoice.value = null
    contactBusy.value = false
    contactOpenRequested.value = null
    contactNavigationPending.value = deps.hasContactEntry?.() === true
    loadingChats.value = false
    initializing.value = false
    rerunRequested = false
    initializingBotId = null
    initializePromise = null
  }

  return {
    loadingChats, contactChoice, contactBusy, contactOpenRequested, contactNavigationPending, completeContactOpen,
    openContact, confirmContact, loadContactCandidates, cancelContactChoice,
    initialize,
    selectBot,
    selectSession,
    createNewSession,
    selectDraft,
    reset,
  }
}
