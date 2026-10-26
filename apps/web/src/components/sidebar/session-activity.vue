<script setup lang="ts">
import { computed, nextTick, ref, shallowRef } from 'vue'
import { useDocumentVisibility, useLocalStorage, until } from '@vueuse/core'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Clock3, PauseCircle } from 'lucide-vue-next'
import { Spinner, toast } from '@felinic/ui'
import type { BotdelegationView } from '@memohai/sdk'
import { useChatStore } from '@/store/chat-list'
import { useWorkspaceTabsStore } from '@/store/workspace-tabs'
import { isRuntimeRunActive } from '@/store/chat/runtime-projection'
import { sessionBotRunStatus } from '@/pages/home/composables/session-bot-run'
import { useSessionActivity } from '@/pages/home/composables/useSessionActivity'
import { useBotDelegationHistory } from '@/pages/home/composables/useBotDelegationHistory'
import { apiErrorStatus, resolveApiErrorMessage } from '@/utils/api-error'
import SidebarPanelHeader from './panel-header.vue'
import SidebarNavButton from './nav-button.vue'

const chat = useChatStore(), tabs = useWorkspaceTabsStore(), router = useRouter()
const props = withDefaults(defineProps<{ visible?: boolean }>(), { visible: true })
const { t } = useI18n()
const token = useLocalStorage('token', ''), documentVisibility = useDocumentVisibility()
const visible = computed(() => props.visible && documentVisibility.value === 'visible')
const scope = computed(() => chat.currentBotId && chat.sessionId && token.value ? {
  bot: chat.currentBotId, session: chat.sessionId, selection: chat.workspaceTargetSelectionFor().targetId, identity: token.value,
} : null)
const reader = useSessionActivity(scope, visible)
const selectedDelegation = shallowRef<BotdelegationView>()
const navigationReader = { detail: selectedDelegation, selected: computed(() => selectedDelegation.value?.delegation_id), error: reader.delegationError, loading: ref(false) }
const history = useBotDelegationHistory(scope, visible, navigationReader, async target => {
  await chat.selectBot(target.bot)
  if (token.value !== target.identity || chat.currentBotId !== target.bot) return
  await router.push({ name: 'bot', params: { botName: target.bot } })
  await until(() => tabs.api).toBeTruthy({ timeout: 5000, throwOnTimeout: true })
  if (token.value !== target.identity || chat.currentBotId !== target.bot) return
  tabs.openSessionChat({ sessionId: target.session, title: target.title }); tabs.closeMobileNav()
})
interface Activity { key: string; title: string; state: string; session?: string; delegation?: BotdelegationView }
const activeStates = new Set(['accepted', 'submitted', 'running', 'waiting_decision', 'finishing'])
const rows = computed(() => {
  if (!visible.value || !token.value || [reader.taskError.value, reader.delegationError.value].some(error => [401, 403].includes(apiErrorStatus(error) ?? 0))) return []
  const result = new Map<string, Activity>(), runs = new Set<string>()
  for (const session of chat.knownSessions) {
    if (session.bot_id !== chat.currentBotId) continue
    const observation = chat.runtimeObservationFor(session.bot_id, session.id), run = observation?.synchronized ? observation.run : null
    if (!run || run.configuration_only || !isRuntimeRunActive(run.status)) continue
    runs.add(run.run_id)
    const title = session.title || run.request_user_turn?.text?.split('\n')[0] || t('chat.tasks.title')
    result.set('run:' + run.run_id, { key: 'run:' + run.run_id, title, state: sessionBotRunStatus(run.status), session: session.id })
  }
  for (const task of reader.tasks.value) {
    if (!activeStates.has(task.status) || (task.task_type === 'subagent' && task.run_id && runs.has(task.run_id))) continue
    if (task.pi_result && !['unknown', 'not_applicable'].includes(task.pi_result)) continue
    const title = chat.knownSessionSummary(task.session_id)?.title || t(`chat.tasks.types.${task.task_type}`)
    result.set(task.task_type + ':' + task.task_id, { key: task.task_type + ':' + task.task_id, title, state: task.status, session: task.session_id || undefined })
  }
  for (const task of reader.delegations.value) {
    if (!['accepted', 'running'].includes(task.state) || (task.target_run_state && !activeStates.has(task.target_run_state))) continue
    const bot = chat.bots.find(bot => bot.id === task.target_bot_id)
    const title = bot?.display_name || bot?.name || t('chat.delegations.title')
    result.set('delegation:' + task.delegation_id, { key: 'delegation:' + task.delegation_id, title, state: task.target_run_state || task.state,
      session: task.target_session_id, delegation: task })
  }
  return [...result.values()]
})
async function open(row: Activity) {
  if (!row.session) return
  if (row.delegation) {
    if (!scope.value) return
    selectedDelegation.value = row.delegation
    await nextTick()
    await history.value.open()
    if (history.value.error) toast.error(resolveApiErrorMessage(history.value.error, t('chat.delegations.historyUnavailable')))
  } else {
    tabs.openSessionChat({ sessionId: row.session, title: row.title }); tabs.closeMobileNav()
  }
}
</script>

<template>
  <section
    v-if="rows.length"
    class="px-2 pb-2"
    :aria-label="t('chat.tasks.title')"
  >
    <SidebarPanelHeader
      :label="t('chat.tasks.title')"
      label-class="pl-3"
      class="py-1"
    />
    <SidebarNavButton
      v-for="row in rows"
      :key="row.key"
      :disabled="!row.session || !!(row.delegation && !row.delegation.target_run_id) || history.pending"
      @click="open(row)"
    >
      <PauseCircle
        v-if="row.state === 'waiting_decision'"
        class="size-4 shrink-0"
        aria-hidden="true"
      />
      <Spinner
        v-else-if="['running', 'finishing', 'aborting'].includes(row.state)"
        class="size-4 shrink-0"
        aria-hidden="true"
      />
      <Clock3
        v-else
        class="size-4 shrink-0"
        aria-hidden="true"
      />
      <span class="min-w-0 flex-1 truncate text-left">{{ row.title }}</span>
      <span class="shrink-0 text-caption text-muted-foreground">{{ row.state === 'aborting' ? t('chat.botRun.aborting') : t(`chat.tasks.statuses.${row.state}`) }}</span>
    </SidebarNavButton>
  </section>
</template>
