<script setup lang="ts">
import { computed, onScopeDispose, ref, shallowRef, watch } from 'vue'
import { until, useDocumentVisibility, useLocalStorage } from '@vueuse/core'
import { useRouter } from 'vue-router'
import { useChatStore } from '@/store/chat-list'
import { useWorkspaceTabsStore } from '@/store/workspace-tabs'
import { useBotDelegationHistory } from '../composables/useBotDelegationHistory'
import BotDelegationRun from './bot-delegation-run.vue'
import { useI18n } from 'vue-i18n'
import { ListTree } from 'lucide-vue-next'
import { Button, Dialog, DialogTrigger, DialogPanel, DialogViewHeader, DialogBody, DialogDescription, Tabs, TabsList, TabsTrigger, TabsContent } from '@felinic/ui'
import { onAuthSessionCleared } from '@/lib/auth-session'
import { apiErrorStatus } from '@/utils/api-error'
import { useSessionTasks } from '../composables/useSessionTasks'
import { delegationAPIUnsupported, useBotDelegations } from '../composables/useBotDelegations'
import { useBotDelegationDownload } from '../composables/useBotDelegationDownload'
import { useBotDelegationDelivery } from '../composables/useBotDelegationDelivery'
import BotDelegationTasks from './bot-delegation-tasks.vue'
import SessionTaskResults from './session-task-results.vue'

const props = defineProps<{ botId: string, sessionId: string, selection: string, visible: boolean }>()
const { t } = useI18n()
const token = useLocalStorage('token', '')
const open = ref(false)
const group = ref('execution')
const documentVisibility = useDocumentVisibility()
const visible = computed(() => open.value && props.visible && documentVisibility.value === 'visible')
const scope = computed(() => props.botId && props.sessionId ? {
  bot: props.botId, session: props.sessionId, selection: props.selection, identity: token.value,
} : null)
const { items, detail, selected, cursor, nextCursor, loading, error, openTask, back, page, refresh, deliveries, observation, nativeTask } = useSessionTasks(scope, computed(() => visible.value && group.value === 'execution'))
const delegationVisible = computed(() => visible.value && group.value === 'delegations')
const delegationReader = useBotDelegations(scope, delegationVisible)
const deliveryAction = useBotDelegationDelivery(scope, delegationVisible, delegationReader)
const chatStore = useChatStore()
const workspaceTabs = useWorkspaceTabsStore()
const router = useRouter()
const historyAction = useBotDelegationHistory(scope, delegationVisible, delegationReader, async target => {
  await chatStore.selectBot(target.bot)
  if (token.value !== target.identity || chatStore.currentBotId !== target.bot) return
  await router.push({ name: 'bot', params: { botName: target.bot } })
  await until(() => workspaceTabs.api).toBeTruthy({ timeout: 5000, throwOnTimeout: true })
  if (token.value !== target.identity || chatStore.currentBotId !== target.bot) return
  workspaceTabs.openSessionChat({ sessionId: target.session, title: target.title })
  open.value = false
})
const downloadAction = useBotDelegationDownload(scope, delegationVisible, delegationReader)
const { items: delegations, detail: delegationDetail, selected: delegationSelected, cursor: delegationCursor, nextCursor: delegationNext, loading: delegationLoading, error: delegationError, openTask: openDelegation, back: backDelegation, page: delegationPage, refresh: refreshDelegations } = delegationReader
const specialist = shallowRef<{ bot: string, session: string, delegation: string }>()
const specialistScope = computed(() => specialist.value && scope.value ? {
  bot: specialist.value.bot, session: specialist.value.session,
  selection: specialist.value.delegation, identity: scope.value.identity,
} : null)
const specialistVisible = computed(() => visible.value && !!specialist.value)
const specialistPaused = computed(() => !!specialist.value && (!!delegationError.value
  || delegationDetail.value?.delegation_id !== specialist.value.delegation
  || delegationDetail.value?.target_bot_id !== specialist.value.bot
  || delegationDetail.value?.target_session_id !== specialist.value.session))
const specialistReader = useSessionTasks(specialistScope, specialistVisible, specialistPaused)
const { items: specialistItems, detail: specialistDetail, selected: specialistSelected, cursor: specialistCursor, nextCursor: specialistNext, loading: specialistLoading, error: specialistError, openTask: openSpecialistTask, back: backSpecialistTask, page: specialistPage, refresh: refreshSpecialist, deliveries: specialistDeliveries, observation: specialistObservation, nativeTask: specialistNativeTask } = specialistReader
const hasSelection = computed(() => !!specialist.value || (group.value === 'delegations' ? !!delegationSelected.value : !!selected.value))
function openSpecialistTasks() {
  if (specialist.value) return
  const task = delegationDetail.value
  if (!task?.target_session_id || task.delegation_id !== delegationSelected.value || delegationError.value) return
  specialist.value = { bot: task.target_bot_id, session: task.target_session_id, delegation: task.delegation_id }
}
watch([scope, visible, group], () => { specialist.value = undefined }, { flush: 'sync' })
// Revalidate the original relation throughout observation. Transient failures
// pause and clear downstream content; a denial or changed relation retires the
// navigation too. Neither path can revive a pending response from the old read.
watch([delegationDetail, delegationError], () => {
  const current = specialist.value
  if (!current) return
  const denied = delegationAPIUnsupported(delegationError.value)
    || [401, 403, 404].includes(apiErrorStatus(delegationError.value) ?? 0)
  const relation = delegationDetail.value
  if (denied || (relation && (relation.delegation_id !== current.delegation
    || relation.target_bot_id !== current.bot || relation.target_session_id !== current.session))) {
    specialist.value = undefined
  }
}, { flush: 'sync' })
function reloadSpecialist() {
  if (specialistPaused.value) void refreshDelegations()
  else void refreshSpecialist()
}
function goBack() {
  if (specialist.value) {
    if (specialistSelected.value) backSpecialistTask()
    else specialist.value = undefined
  } else if (group.value === 'delegations') backDelegation()
  else back()
}
// Login notifies before replacing the token; a same-tab 401 can clear it
// without a storage event. Close now so pending reads cannot revive content
// while the parent view/router is still mounted.
onScopeDispose(onAuthSessionCleared(() => { open.value = false }))
</script>

<template>
  <Dialog v-model:open="open">
    <DialogTrigger as-child>
      <Button
        variant="ghost"
        size="sm"
      >
        <ListTree />
        {{ t('chat.tasks.title') }}
      </Button>
    </DialogTrigger>
    <DialogPanel view-swap>
      <DialogViewHeader
        :centered="hasSelection || items.length > 0 || delegations.length > 0"
        :show-back="hasSelection"
        :back-label="t('common.back')"
        @back="goBack"
      >
        {{ specialist ? t('chat.delegations.downstreamTitle') : hasSelection ? t('chat.tasks.detail') : t('chat.tasks.title') }}
      </DialogViewHeader>
      <DialogBody class="space-y-4">
        <template v-if="specialist">
          <DialogDescription>{{ t('chat.delegations.downstreamDescription') }}</DialogDescription>
          <BotDelegationRun
            v-if="!specialistSelected && !specialistPaused && delegationDetail"
            :task="delegationDetail"
            :loading="delegationLoading"
            :history-action="historyAction"
          />
          <h3
            v-if="!specialistSelected && !specialistPaused"
            class="text-label font-medium"
          >
            {{ t('chat.delegations.childTasks') }}
          </h3>
          <SessionTaskResults
            :items="specialistItems"
            :detail="specialistDetail"
            :selected="specialistSelected"
            :cursor="specialistCursor"
            :next-cursor="specialistNext"
            :loading="specialistLoading || (specialistPaused && delegationLoading)"
            :error="delegationError || specialistError"
            :deliveries="specialistDeliveries"
            :observation="specialistObservation"
            :native-task="specialistNativeTask"
            @asset-opened="open = false"
            @open-task="openSpecialistTask"
            @page="specialistPage"
            @refresh="reloadSpecialist"
          />
        </template>
        <Tabs
          v-else
          v-model="group"
        >
          <TabsList v-if="!hasSelection">
            <TabsTrigger value="execution">
              {{ t('chat.delegations.executionGroup') }}
            </TabsTrigger>
            <TabsTrigger value="delegations">
              {{ t('chat.delegations.title') }}
            </TabsTrigger>
          </TabsList>
          <TabsContent value="delegations">
            <DialogDescription>{{ t('chat.delegations.description') }}</DialogDescription>
            <BotDelegationTasks
              :items="delegations"
              :detail="delegationDetail"
              :selected="delegationSelected"
              :cursor="delegationCursor"
              :next-cursor="delegationNext"
              :loading="delegationLoading"
              :error="delegationError"
              :history-action="historyAction"
              :delivery-action="deliveryAction"
              :download-action="downloadAction"
              :tasks-action="delegationDetail?.target_session_id ? openSpecialistTasks : undefined"
              @open-task="openDelegation"
              @page="delegationPage"
              @refresh="refreshDelegations"
            />
          </TabsContent>
          <TabsContent value="execution">
            <DialogDescription>{{ t('chat.tasks.description') }}</DialogDescription>
            <SessionTaskResults
              :items="items"
              :detail="detail"
              :selected="selected"
              :cursor="cursor"
              :next-cursor="nextCursor"
              :loading="loading"
              :error="error"
              :deliveries="deliveries"
              :observation="observation"
              :native-task="nativeTask"
              @asset-opened="open = false"
              @open-task="openTask"
              @page="page"
              @refresh="refresh"
            />
          </TabsContent>
        </Tabs>
      </DialogBody>
    </DialogPanel>
  </Dialog>
</template>
