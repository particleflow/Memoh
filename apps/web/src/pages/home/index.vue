<template>
  <div class="relative flex h-full overflow-hidden">
    <!-- Preserve Dockview dimensions while a history link is being authorized. -->
    <ChatWorkspace
      v-if="currentBotId"
      :class="{ invisible: !workspaceVisible }"
    />
    <ContactChoiceDialog />
    <PanePlaceholder
      v-if="!workspaceVisible"
      class="absolute inset-0"
      :title="historyError || t('common.loading')"
    />
    <div
      v-else-if="!currentBotId"
      class="flex-1 bg-card"
    >
      <PanePlaceholder
        v-if="missingBotName"
        :title="t('bots.notFound')"
      >
        {{ t('bots.notFoundDescription') }}
      </PanePlaceholder>
      <PanePlaceholder
        v-else
        :title="t('chat.selectBot')"
      >
        {{ t('chat.selectBotHint') }}
      </PanePlaceholder>
    </div>
  </div>
</template>

<script setup lang="ts">
import { normalizeAgentID } from '@/utils/external-agent'
import { computed, nextTick, onScopeDispose, provide, ref, watch } from 'vue'
import { useTitle } from '@vueuse/core'
import { storeToRefs } from 'pinia'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { getBotsByBotIdAgents, getBotsById } from '@memohai/sdk'
import { fetchBot } from '@/composables/api/useBot'
import { PanePlaceholder, toast } from '@felinic/ui'
import { CHAT_ROUTE_NAMES } from '@/constants/chat-routes'
import { useChatStore } from '@/store/chat-list'
import { routeConversationLabel } from '@/store/chat-list.utils'
import { useComposerPrefillStore } from '@/store/composer-prefill'
import { useWorkspaceTabsStore } from '@/store/workspace-tabs'
import { ACP_NO_PROJECT_MODE, createACPNoProjectPath } from '@/utils/acp'
import { botAgentProvider } from '@/utils/bot-agent'
import ContactChoiceDialog from './components/contact-choice-dialog.vue'
import { fetchSession } from '@/composables/api/useChat'
import { resolveApiErrorMessage } from '@/utils/api-error'
import { hasTaskHistoryQuery, resolveTaskHistoryAnchor } from '@/utils/task-history-link'
import ChatWorkspace from './components/chat-workspace.vue'
import { useComposerPrefillResolver } from './composables/useComposerPrefillResolver'
import { chatWorkspaceVisibleKey } from './composables/chat-workspace-visibility'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const chatStore = useChatStore()
const workspaceTabs = useWorkspaceTabsStore()
const { currentBotId, bots, activeSession } = storeToRefs(chatStore)
const composerPrefill = useComposerPrefillStore()

useComposerPrefillResolver({
  pending: () => composerPrefill.pending,
  currentBotId: () => currentBotId.value,
  // Wait out a bot switch: until the new bot's chats load, the dock may still
  // show the previous bot's panels.
  dockReady: () => !!workspaceTabs.api && !chatStore.loadingChats,
  activeChatWritable: () => !!workspaceTabs.activeId && workspaceTabs.activePanelIsChat && !chatStore.activeChatReadOnly,
  openDraftChat: () => workspaceTabs.openDraftChat({ title: t('chat.newSession'), explicitSelection: false }),
})

// Resolve a bot UUID from a URL name slug. Prefers the already-loaded bot list,
// falling back to the API (which accepts both name and UUID identifiers).
// Returns null when the server reports no such bot; throws on other failures
// so a transient error isn't mistaken for a missing bot.
async function resolveBotIdFromName(nameOrId: string): Promise<string | null> {
  const value = nameOrId.trim()
  if (!value) return null
  const cached = bots.value.find((b) => b.name === value || b.id === value)
  if (cached?.id) return cached.id
  const bot = await fetchBot(value)
  return bot?.id ?? null
}

// Resolve a URL name slug from a bot UUID, preferring the loaded bot list.
async function resolveBotNameFromId(botId: string): Promise<string | null> {
  const value = botId.trim()
  if (!value) return null
  const cached = bots.value.find((b) => b.id === value)
  if (cached?.name) return cached.name
  try {
    const { data } = await getBotsById({ path: { id: value }, throwOnError: true })
    return data?.name ?? null
  } catch {
    return null
  }
}

let suppressUrlSync = false

// The URL's bot identifier when the server has no such bot. While set, the
// pane shows the not-found state and the URL is left as the user typed it.
const missingBotName = ref('')
const historyError = ref('')
const historyLoading = ref(false)
const workspaceVisible = computed(() => !historyError.value && !historyLoading.value && !chatStore.contactNavigationPending)
provide(chatWorkspaceVisibleKey, workspaceVisible)
let historyRequest = 0
let historyValidation: AbortController | undefined
function cancelHistoryValidation() {
  historyValidation?.abort()
  historyValidation = undefined
}
onScopeDispose(() => { historyRequest++; cancelHistoryValidation() })

// home is now mounted persistently (via main-container, which App.vue keeps
// alive across chat↔settings) rather than torn down when leaving chat. So these
// route watchers keep running while the user is in settings. The settings route
// shares the :botName param, so without this guard the route.params.botName
// watcher would "canonicalize" that UUID and router.replace back to /bot/<name>,
// yanking the user out of settings. URL sync must only run while a chat route
// (home/bot) is current. route.name is the reliable signal (no mount/unmount
// timing to race).
const isChatRoute = () => CHAT_ROUTE_NAMES.has(route.name as string)

// Home remains mounted behind settings, so the route gates the browser title.
// A first send's session is titled like the draft it came from until the
// server confirms the send (see isSessionTentative).
useTitle(() => {
  if (isChatRoute() && currentBotId.value && workspaceTabs.activeContactTitle) return `Memoh · ${workspaceTabs.activeContactTitle}`
  const session = activeSession.value
  if (!isChatRoute() || !currentBotId.value || !session || chatStore.isSessionTentative(session.id)) return 'Memoh'
  const title = (session.title ?? '').trim() || routeConversationLabel(session) || t('chat.untitledSession')
  return `Memoh · ${title}`
}, { restoreOnUnmount: () => 'Memoh' })

// One-shot guard so concurrent syncStoreFromUrl() calls can't both start a
// session for the same redirect. Set synchronously before the first await.
let agentStartConsumed = false

function stripAgentQuery() {
  if (route.query.agent === undefined && route.query.acp === undefined) return
  const query = { ...route.query }
  delete query.agent
  delete query.acp
  void router.replace({ query })
}

// When onboarding redirects here with ?agent=<id>, open an External Agent
// session so the user lands inside it. The old ?acp= spelling remains a
// read-only compatibility alias for links created before this rename.
async function maybeStartExternalAgentSession() {
  if (agentStartConsumed) return
  const raw = route.query.agent ?? route.query.acp
  if (typeof raw !== 'string' || raw === '') {
    stripAgentQuery()
    return
  }
  agentStartConsumed = true
  const agentId = normalizeAgentID(raw)
  try {
    const botId = currentBotId.value?.trim() ?? ''
    if (agentId && botId) {
      const { data } = await getBotsByBotIdAgents({ path: { bot_id: botId }, throwOnError: true })
      const botAgent = data.items?.find(agent => agent.enabled !== false && botAgentProvider(agent) === agentId)
      if (!botAgent?.id) return
      const startedOnPanel = workspaceTabs.activeId
      const { session } = await chatStore.createExternalAgentSession({
        botAgentId: botAgent.id,
        agentId,
        projectMode: ACP_NO_PROJECT_MODE,
        projectPath: createACPNoProjectPath(),
      })
      // Creation is async. If the user moved to another tab meanwhile, leave the
      // dock alone: opening a tab now would replace the group's preview tab,
      // which may be the one they are reading. A draft that was focused when
      // creation started already shows the session, and Recents lists it.
      if (workspaceTabs.activeId === startedOnPanel) {
        workspaceTabs.openSessionChat({ sessionId: session.id })
      }
    }
  } catch {
    // Bot may not have the agent enabled; user can still pick it from the composer.
  } finally {
    // Always strip the one-shot query param, even for malformed/empty values.
    stripAgentQuery()
  }
}

async function syncStoreFromUrl(rawName: string) {
  const urlName = rawName.trim()
  const request = ++historyRequest
  cancelHistoryValidation()
  const validation = new AbortController()
  historyValidation = validation
  const requestedRoute = route.fullPath
  historyError.value = ''
  historyLoading.value = !!route.query.session
  if (hasTaskHistoryQuery(route.query) && !route.query.session) {
    historyLoading.value = false
    historyError.value = t('chat.contact.historyFailed')
    return
  }
  if (route.query.session !== undefined && (typeof route.query.session !== 'string' || !route.query.session.trim())) {
    historyLoading.value = false
    historyError.value = t('chat.contact.historyFailed')
    return
  }
  if (!urlName) {
    if (route.query.session) {
      historyLoading.value = false
      historyError.value = t('chat.contact.historyFailed')
      return
    }
    missingBotName.value = ''
    if (!currentBotId.value) {
      await chatStore.initializeWithRecovery()
    } else {
      await chatStore.selectBot(currentBotId.value)
    }
    await maybeStartExternalAgentSession()
    return
  }
  let resolvedId: string | null
  try {
    resolvedId = await resolveBotIdFromName(urlName)
  } catch (error) {
    if (request === historyRequest) {
      historyLoading.value = false
      historyError.value = resolveApiErrorMessage(error, t('chat.contact.historyFailed'))
    }
    return
  }
  // A previous Bot lookup must never consume the next route's session query.
  if (request !== historyRequest || route.fullPath !== requestedRoute) return
  if (!resolvedId) {
    if (route.query.session) {
      if (request === historyRequest) {
        historyLoading.value = false
        historyError.value = t('bots.notFoundDescription')
      }
      return
    }
    // The URL may have moved on while the lookup was in flight; only the
    // current URL may clear the selection.
    if (((route.params.botName as string) ?? '').trim() !== urlName) return
    missingBotName.value = urlName
    // Drop the previously selected bot rather than showing its chat under
    // this URL. Clearing the selection resets the chat store, bot list
    // included, so reload the list for the bot switcher afterwards.
    if (currentBotId.value) {
      currentBotId.value = null
      await nextTick()
    }
    void chatStore.refreshBots()
    return
  }
  missingBotName.value = ''
  const historyId = typeof route.query.session === 'string' ? route.query.session.trim() : ''
  if (historyId) {
    try {
      const taskAnchor = await resolveTaskHistoryAnchor(route.query, resolvedId, historyId, validation.signal)
      if (request !== historyRequest || route.fullPath !== requestedRoute) return
      const session = await fetchSession(resolvedId, historyId)
      if (request !== historyRequest || route.query.session !== historyId || route.params.botName !== rawName) return
      suppressUrlSync = true
      try { await chatStore.selectBot(resolvedId, { sessionId: session.id }) }
      finally { suppressUrlSync = false }
      await nextTick()
      if (request === historyRequest && route.fullPath === requestedRoute && route.query.session === historyId && currentBotId.value === resolvedId) {
        chatStore.cancelContactChoice()
        workspaceTabs.openSessionChatPinned({ sessionId: session.id, title: session.title, taskAnchor })
      }
    } catch (error) {
      if (request === historyRequest) historyError.value = resolveApiErrorMessage(error, t('chat.contact.historyFailed'))
    } finally {
      if (request === historyRequest) historyLoading.value = false
    }
  } else {
    suppressUrlSync = true
    try {
      await chatStore.selectBot(resolvedId)
    } finally {
      suppressUrlSync = false
    }
  }
  if (!historyId) await maybeStartExternalAgentSession()
  // Canonicalize the URL to the bot's name slug. This covers entry points that
  // navigate with a UUID (e.g. returning from settings), where currentBotId is
  // unchanged so the watcher below never fires.
  const canonicalName = await resolveBotNameFromId(resolvedId)
  if (request !== historyRequest) return
  if (canonicalName && urlName !== canonicalName) {
    void router.replace({ name: 'bot', params: { botName: canonicalName }, query: route.query })
  }
}

watch(
  [
    () => route.name, () => route.params.botName, () => route.query.session,
    () => route.query.task_bot, () => route.query.task_type, () => route.query.task_id,
    () => route.query.task_record, () => route.query.run,
  ],
  ([routeName, paramBotName], prev) => {
    if (!CHAT_ROUTE_NAMES.has(routeName as string)) {
      historyRequest++
      cancelHistoryValidation()
      return
    }
    // Returning to chat from a non-chat route (settings overlay): per-bot config
    // edited there — enabled agents, model, name — won't have reached the store,
    // which loads bots once and isn't wired to the settings query cache. Re-pull
    // so the composer's agent menu reflects the change. Fire-and-forget; the bot
    // list swaps in place and currentBot recomputes. Skips the initial run (no
    // prev) since initialize() already loads a fresh list. Desktop is a separate
    // window whose route never enters settings — it refreshes via the
    // cross-window invalidate listener in the renderer bootstrap instead.
    const prevName = prev?.[0] as string | undefined
    if (prevName !== undefined && !CHAT_ROUTE_NAMES.has(prevName)) {
      void chatStore.refreshBots()
    }
    const requestedRoute = route.fullPath
    void syncStoreFromUrl((paramBotName as string) ?? '').catch(error => {
      if (route.fullPath !== requestedRoute) return
      historyLoading.value = false
      historyError.value = resolveApiErrorMessage(error, t('chat.contact.failed'))
      toast.error(historyError.value)
    })
  },
  { immediate: true },
)

watch(currentBotId, async (newBotId) => {
  if (suppressUrlSync) return
  // Don't touch the URL while a non-chat route (e.g. settings) is current; home
  // stays mounted during /settings route changes and must not redirect away from it.
  if (!isChatRoute()) return
  const storeBot = (newBotId ?? '').trim()
  if (!storeBot) {
    if (missingBotName.value) return
    if (route.name !== 'home') {
      void router.replace({ name: 'home' })
    }
    return
  }
  const requestedRoute = route.fullPath
  const botName = await resolveBotNameFromId(storeBot)
  if (!botName || currentBotId.value !== storeBot || !isChatRoute() || route.fullPath !== requestedRoute) return
  if (((route.params.botName as string) ?? '').trim() === botName) return
  void router.replace({
    name: 'bot',
    params: { botName },
  })
})

let openedContactRequest: unknown = null
watch(
  () => [chatStore.contactOpenRequested, workspaceTabs.api, chatStore.loadingChats, route.query.session] as const,
  async ([request, api, loading]) => {
    if (!request || !api || loading || request === openedContactRequest || route.query.session || currentBotId.value !== request.botId) return
    await nextTick()
    if (chatStore.contactOpenRequested !== request || currentBotId.value !== request.botId || route.query.session) return
    openedContactRequest = request
    workspaceTabs.openSessionChatPinned({ sessionId: request.sessionId, explicitSelection: false, contactView: true })
    chatStore.completeContactOpen(request)
  },
  { immediate: true },
)
</script>
