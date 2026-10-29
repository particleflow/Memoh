<template>
  <div class="flex h-full min-h-0 min-w-0 flex-col overflow-hidden">
    <SidebarPanelHeader
      :label="historyOpen ? t('chat.contact.history') : t('chat.contact.contacts')"
      label-class="pl-[11px]"
      class="px-2 pb-0.5 pt-2"
    />
    <div
      v-if="!historyOpen"
      class="flex min-h-0 flex-1 flex-col"
    >
      <ScrollArea
        class="sidebar-scroll min-h-0 flex-1"
        :scroll-hide-delay="300"
      >
        <div class="px-2">
          <SidebarNavButton
            v-for="bot in bots"
            :key="bot.id"
            :active="bot.id === currentBotId"
            :disabled="bot.status === 'error' || chatStore.contactBusy || chatStore.loadingChats"
            @click="openBot(bot.id || '', bot.name)"
          >
            <Bot
              :stroke-width="1.75"
              class="size-[18px]"
            />
            <span class="truncate">{{ bot.display_name || bot.name }}</span>
          </SidebarNavButton>
        </div>
      </ScrollArea>
      <div class="shrink-0 px-2 pb-2">
        <SidebarNavButton
          :disabled="!currentBotId"
          @click="historyOpen = true"
        >
          <History
            :stroke-width="1.75"
            class="size-[18px]"
          />{{ t('chat.contact.history') }}
        </SidebarNavButton>
      </div>
    </div>
    <div
      v-show="historyOpen"
      class="flex min-h-0 flex-1 flex-col"
    >
      <div class="flex shrink-0 flex-col px-2 pb-0.5">
        <SidebarNavButton @click="historyOpen = false">
          <ArrowLeft
            :stroke-width="1.75"
            class="size-[18px]"
          />{{ t('chat.contact.contacts') }}
        </SidebarNavButton>

        <SidebarNavButton
          :disabled="!currentBotId"
          @click="handleBotSettings"
        >
          <Settings2
            :stroke-width="1.75"
            class="size-[18px]"
          />{{ t('chat.botSettings') }}
        </SidebarNavButton>
      </div>
      <SessionSelectionBar />
      <ScrollArea
        ref="scrollAreaRef"
        class="sidebar-scroll min-h-0 flex-1"
        :scroll-hide-delay="300"
      >
        <FoldersSection />
        <Recents :scroll-el="listScrollEl" />
      </ScrollArea>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, provide, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { Settings2, Bot, History, ArrowLeft } from 'lucide-vue-next'
import { ScrollArea, toast } from '@felinic/ui'
import { resolveApiErrorMessage } from '@/utils/api-error'
import { useChatStore } from '@/store/chat-list'
import { useWorkspaceTabsStore } from '@/store/workspace-tabs'
import SidebarPanelHeader from './panel-header.vue'
import SidebarNavButton from './nav-button.vue'
import FoldersSection from './folders-section.vue'
import Recents from './recents.vue'
import SessionSelectionBar from './session-selection-bar.vue'
import { createSessionSelection, SessionSelectionKey } from './session-selection'
import '@/styles/sidebar-scroll.css'

const { t } = useI18n()
const historyOpen = ref(false)

// The one scrollport for Folders + Recents. Recents needs the scrolling
// element itself for its load-more sentinel root, so it is passed down rather
// than provided. With ScrollArea the scroller is the inner viewport node, not
// the component root — grab it by its data-slot (the library's stable marker).
// Assigned in onMounted: Recents reads it reactively through toRef(props).
const scrollAreaRef = ref<InstanceType<typeof ScrollArea> | null>(null)
const listScrollEl = ref<HTMLElement | null>(null)
onMounted(() => {
  const rootEl = (scrollAreaRef.value as unknown as { $el?: HTMLElement } | null)?.$el
  listScrollEl.value = rootEl?.querySelector<HTMLElement>('[data-slot="scroll-area-viewport"]') ?? null
})
const router = useRouter()
const chatStore = useChatStore()
const workspaceTabs = useWorkspaceTabsStore()
const { currentBotId, bots } = storeToRefs(chatStore)

const currentBot = computed(() =>
  bots.value.find(bot => bot.id === currentBotId.value) ?? null,
)

// One selection for both Folders and Recents, so a batch can span them.
const selection = createSessionSelection()
provide(SessionSelectionKey, selection)
// Session ids are scoped to a bot; a selection never carries across.
watch(currentBotId, () => selection.clear())
// A session deleted any other way (its own menu, a failed send's cleanup)
// must not linger as a checked id with no row.
watch(() => chatStore.deletedSession, (deleted) => {
  if (deleted) selection.remove(deleted.id)
})

async function openBot(id: string, name?: string) {
  if (!id) return
  try {
    await chatStore.selectBot(id)
    await router.push({ name: 'bot', params: { botName: name || id } })
    workspaceTabs.closeMobileNav()
  } catch (error) { toast.error(resolveApiErrorMessage(error, t('chat.contact.failed'))) }
}

// Navigate to the current bot's settings overview.
function handleBotSettings() {
  const botId = currentBotId.value
  if (!botId) return
  void router.push({ name: 'bot-detail', params: { botName: currentBot.value?.name ?? botId } })
}
</script>
