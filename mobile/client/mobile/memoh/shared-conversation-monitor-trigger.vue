<script setup lang="ts">
import { computed, onScopeDispose, ref, watch } from 'vue'
import { useLocalStorage, until } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { Bot, ListTree } from 'lucide-vue-next'
import { Button, Dialog, DialogTrigger, DialogPanel, DialogViewHeader, DialogBody, Spinner, CalloutBanner, toast } from '@felinic/ui'
import { getBotsByBotIdSessionsBySessionId } from '@memohai/sdk'
import { useChatStore } from '@/store/chat-list'
import { useWorkspaceTabsStore } from '@/store/workspace-tabs'
import { useChatViewTarget } from '@/pages/home/composables/useChatViewContext'
import { useConversationRelations, type ConversationRelation } from '@/pages/home/composables/useConversationRelations'
import ConversationRelationRow from '@/pages/home/components/conversation-relation-row.vue'
import { resolveApiErrorMessage } from '@/utils/api-error'
import { onAuthSessionCleared } from '@/lib/auth-session'

const props = withDefaults(defineProps<{ kind?: 'tasks' | 'bots', botId?: string, sessionId?: string, visible?: boolean }>(), { kind: 'tasks', visible: true })
const { t } = useI18n(), chat = useChatStore(), tabs = useWorkspaceTabsStore(), router = useRouter()
const target = useChatViewTarget(), token = useLocalStorage('token', '')
const open = ref(false), navigating = ref(false)
const scope = computed(() => {
  const bot = props.botId || target.value.botId, session = props.sessionId || target.value.sessionId
  return bot && session && token.value ? { bot, session, selection: '', identity: token.value } : null
})
const visible = computed(() => open.value && props.visible)
const title = computed(() => t(props.kind === 'bots' ? 'bots.title' : 'chat.tasks.title'))
const reader = useConversationRelations(scope, visible, id => {
  const bot = chat.bots.find(bot => bot.id === id)
  return bot?.display_name || bot?.name || ''
}, () => {
  const current = scope.value
  const observation = current ? chat.runtimeObservationFor(current.bot, current.session) : undefined
  return observation?.synchronized ? observation.run?.status || '' : ''
})
const rows = computed(() => props.kind === 'bots' ? reader.bots.value : reader.items.value)
const roots = computed(() => {
  const keys = new Set(rows.value.map(row => row.key))
  return rows.value.filter(row => !row.parent || !keys.has(row.parent))
})
let epoch = 0, controller: AbortController | undefined
function invalidate() { epoch++; controller?.abort(); navigating.value = false }
watch([scope, visible], invalidate, { flush: 'sync' })
const off = onAuthSessionCleared(() => { open.value = false; invalidate() })
onScopeDispose(() => { off(); invalidate() })
async function navigate(row: ConversationRelation) {
  if (!scope.value || !visible.value || navigating.value || !rows.value.some(item => item.key === row.key)) return
  const generation = epoch, identity = token.value, control = new AbortController()
  controller = control; navigating.value = true
  const timeout = setTimeout(() => control.abort(), 15000)
  try {
    const { data: session } = await getBotsByBotIdSessionsBySessionId({ path: { bot_id: row.bot, session_id: row.session }, signal: control.signal, cache: 'no-store', throwOnError: true })
    if (generation !== epoch || token.value !== identity) return
    if (session.id !== row.session || session.bot_id !== row.bot) throw new Error('Session identity changed')
    await chat.selectBot(row.bot, { sessionId: session.id })
    if (token.value !== identity || chat.currentBotId !== row.bot) return
    await router.push({ name: 'bot', params: { botName: row.bot }, query: { session: session.id } })
    await until(() => tabs.api).toBeTruthy({ timeout: 5000, throwOnTimeout: true })
    if (token.value !== identity || chat.currentBotId !== row.bot) return
    tabs.openSessionChat({ sessionId: session.id, title: session.title }); tabs.closeMobileNav(); open.value = false
  } catch (cause) {
    if (generation === epoch && !control.signal.aborted) toast.error(resolveApiErrorMessage(cause, t('chat.delegations.historyUnavailable')))
  } finally {
    clearTimeout(timeout)
    if (generation === epoch) navigating.value = false
    if (controller === control) controller = undefined
  }
}
</script>

<template>
  <Dialog v-model:open="open">
    <DialogTrigger as-child>
      <Button
        variant="ghost"
        size="sm"
        :disabled="!scope"
      >
        <Bot v-if="kind === 'bots'" />
        <ListTree v-else />
        {{ title }}
      </Button>
    </DialogTrigger>
    <DialogPanel :show-close-button="false">
      <DialogViewHeader>{{ title }}</DialogViewHeader>
      <DialogBody>
        <Spinner v-if="reader.loading.value && !rows.length" />
        <CalloutBanner
          v-if="reader.error.value"
          tone="destructive"
          size="sm"
          :description="t('chat.tasks.loadFailed')"
        />
        <ul v-if="rows.length">
          <ConversationRelationRow
            v-for="row in roots"
            :key="row.key"
            :row="row"
            :items="rows"
            :disabled="navigating"
            @open="navigate"
          />
        </ul>
        <p
          v-else-if="!reader.error.value && !reader.loading.value"
          class="text-label text-muted-foreground"
        >
          {{ t('chat.tasks.empty') }}
        </p>
      </DialogBody>
    </DialogPanel>
  </Dialog>
</template>
