<template>
  <DockPanelFrame>
    <KeepAlive>
      <ChatPane
        v-if="visible && currentBotId === panelBotId"
        :key="`chat-pane:${panelBotId}:${panelId}`"
        :tab-id="panelId"
        :session-id="paramsSessionId"
        :contact-view="params.params.contactView === true"
        :task-anchor="params.params.taskAnchor ?? undefined"
        :visible="visible"
        :active="focused"
      />
    </KeepAlive>
  </DockPanelFrame>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, watch } from 'vue'
import { storeToRefs } from 'pinia'
import type { DockviewApi, DockviewPanelApi } from 'dockview-vue'
import { useChatStore } from '@/store/chat-list'
import type { TaskHistoryAnchor } from '@/utils/task-history-link'
import ChatPane from '../chat-pane.vue'
import { usePanelFocused, usePanelVisible } from './use-panel-visible'
import DockPanelFrame from './panel-frame.vue'

// A per-session chat tab. The panel id (chat:<n>) is stable for the tab's whole
// life; the session it renders lives in params.sessionId (null = unsaved draft)
// and the workspace store mutates it (draft→real promotion) via updateParameters
// WITHOUT remounting ChatPane — the keep-alive key is the panel id, not the
// session. Session data is keyed independently from the focused selection, so
// every visible split can stay live at the same time.
// No breadcrumb: the tab already carries the session title.
const props = defineProps<{
  params: {
    params: { sessionId?: string | null, contactView?: boolean, taskAnchor?: TaskHistoryAnchor | null }
    api: DockviewPanelApi
    containerApi: DockviewApi
  }
}>()

const chatStore = useChatStore()
const { currentBotId } = storeToRefs(chatStore)
// Dock cleanup follows the Bot change; this panel still belongs to its creator.
const panelBotId = currentBotId.value?.trim() ?? ''

const visible = usePanelVisible(props.params.api)
const focused = usePanelFocused(props.params.api)
const panelId = props.params.api.id
const paramsSessionId = computed(() => props.params.params.sessionId ?? null)

watch([currentBotId, paramsSessionId, visible], ([botId, sessionId, isVisible]) => {
  const bid = botId?.trim() ?? ''
  if (!bid || bid !== panelBotId) return
  chatStore.bindChatView(panelId, {
    botId: bid,
    sessionId,
    viewId: panelId,
  }, isVisible)
}, { immediate: true })

watch(focused, (isFocused) => {
  if (isFocused) chatStore.focusChatView(panelId)
}, { immediate: true })

onBeforeUnmount(() => chatStore.unbindChatView(panelId))
</script>
