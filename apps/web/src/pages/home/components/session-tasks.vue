<script setup lang="ts">
import { computed, onScopeDispose, ref, shallowRef, watch } from 'vue'
import { useDocumentVisibility, useLocalStorage } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import { ListTree } from 'lucide-vue-next'
import { Button, Dialog, DialogTrigger, DialogPanel, DialogViewHeader, DialogBody, DialogDescription, Tabs, TabsList, TabsTrigger, TabsContent } from '@felinic/ui'
import { onAuthSessionCleared } from '@/lib/auth-session'
import { useSessionTasks } from '../composables/useSessionTasks'
import { useBotDelegations } from '../composables/useBotDelegations'
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
const { items, detail, selected, cursor, nextCursor, loading, error, openTask, back, page, refresh } = useSessionTasks(scope, computed(() => visible.value && group.value === 'execution'))
const delegationVisible = computed(() => visible.value && group.value === 'delegations')
const delegationReader = useBotDelegations(scope, delegationVisible)
const deliveryAction = useBotDelegationDelivery(scope, delegationVisible, delegationReader)
const { items: delegations, detail: delegationDetail, selected: delegationSelected, cursor: delegationCursor, nextCursor: delegationNext, loading: delegationLoading, error: delegationError, openTask: openDelegation, back: backDelegation, page: delegationPage, refresh: refreshDelegations } = delegationReader
const specialist = shallowRef<{ bot: string, session: string, delegation: string }>()
const specialistScope = computed(() => specialist.value && scope.value ? {
  bot: specialist.value.bot, session: specialist.value.session,
  selection: specialist.value.delegation, identity: scope.value.identity,
} : null)
const specialistVisible = computed(() => visible.value && !!specialist.value)
const specialistReader = useSessionTasks(specialistScope, specialistVisible)
const { items: specialistItems, detail: specialistDetail, selected: specialistSelected, cursor: specialistCursor, nextCursor: specialistNext, loading: specialistLoading, error: specialistError, openTask: openSpecialistTask, back: backSpecialistTask, page: specialistPage, refresh: refreshSpecialist } = specialistReader
const hasSelection = computed(() => !!specialist.value || (group.value === 'delegations' ? !!delegationSelected.value : !!selected.value))
function openSpecialistTasks() {
  if (specialist.value) return
  const task = delegationDetail.value
  if (!task?.target_session_id || task.delegation_id !== delegationSelected.value || delegationError.value) return
  specialist.value = { bot: task.target_bot_id, session: task.target_session_id, delegation: task.delegation_id }
}
watch([scope, visible, group], () => { specialist.value = undefined }, { flush: 'sync' })
// Keep the original delegation read active: losing that authorized relation must
// retire downstream content too, even if a target GET finishes later.
watch([delegationDetail, delegationError], () => {
  const current = specialist.value
  if (current && (delegationError.value || delegationDetail.value?.delegation_id !== current.delegation
    || delegationDetail.value?.target_bot_id !== current.bot || delegationDetail.value?.target_session_id !== current.session)) {
    specialist.value = undefined
  }
}, { flush: 'sync' })
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
          <SessionTaskResults
            :items="specialistItems"
            :detail="specialistDetail"
            :selected="specialistSelected"
            :cursor="specialistCursor"
            :next-cursor="specialistNext"
            :loading="specialistLoading"
            :error="specialistError"
            @open-task="openSpecialistTask"
            @page="specialistPage"
            @refresh="refreshSpecialist"
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
              :delivery-action="deliveryAction"
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
