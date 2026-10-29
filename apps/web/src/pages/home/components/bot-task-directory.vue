<script setup lang="ts">
import { computed, onScopeDispose, ref, watch } from 'vue'
import { useLocalStorage } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { ListTree } from 'lucide-vue-next'
import { Button, Dialog, DialogTrigger, DialogPanel, DialogViewHeader, DialogBody, Spinner, CalloutBanner, toast } from '@felinic/ui'
import { getBotsByBotIdTasksByTaskTypeByRecordId, getBotsByBotIdSessionsBySessionId, type SessiontaskDirectoryTask } from '@memohai/sdk'
import { onAuthSessionCleared } from '@/lib/auth-session'
import { resolveApiErrorMessage } from '@/utils/api-error'
import { taskHistoryQuery } from '@/utils/task-history-link'
import { useBotTaskDirectory } from '../composables/useBotTaskDirectory'

const props = withDefaults(defineProps<{ botId: string, visible?: boolean }>(), { visible: true })
const { t, te } = useI18n(), router = useRouter()
const token = useLocalStorage('token', ''), open = ref(false), navigating = ref(false)
const bot = computed(() => props.botId), visible = computed(() => open.value && props.visible)
const reader = useBotTaskDirectory(bot, token, visible)
const name = (row: SessiontaskDirectoryTask) => row.name || t(`chat.taskDirectory.types.${row.task_type}`)
const state = (row: SessiontaskDirectoryTask) => t(`chat.tasks.statuses.${te(`chat.tasks.statuses.${row.state}`) ? row.state : 'unknown'}`)
let epoch = 0, controller: AbortController | undefined
function invalidate() { epoch++; controller?.abort(); navigating.value = false }
watch([bot, token, visible], invalidate, { flush: 'sync' })
const off = onAuthSessionCleared(() => { open.value = false; invalidate() })
onScopeDispose(() => { off(); invalidate() })

async function navigate(row: SessiontaskDirectoryTask) {
  if (!row.destination || !visible.value || navigating.value) return
  const sourceBot = bot.value, identity = token.value, generation = epoch, control = new AbortController()
  controller = control; navigating.value = true
  const timeout = setTimeout(() => control.abort(), 15000)
  const current = () => generation === epoch && token.value === identity && reader.items.value.some(item => item.record_id === row.record_id && item.task_type === row.task_type && item.task_id === row.task_id)
  try {
    const { data: task } = await getBotsByBotIdTasksByTaskTypeByRecordId({ path: { bot_id: sourceBot, task_type: row.task_type, record_id: row.record_id }, cache: 'no-store', signal: control.signal, throwOnError: true })
    if (!current()) return
    const destination = task.destination
    if (task.task_id !== row.task_id || task.record_id !== row.record_id || task.task_type !== row.task_type || !destination
      || destination.bot_id !== row.destination.bot_id || destination.session_id !== row.destination.session_id
      || destination.run_id !== row.destination.run_id) throw new Error('Task destination changed')
    const { data: session } = await getBotsByBotIdSessionsBySessionId({ path: { bot_id: destination.bot_id, session_id: destination.session_id }, cache: 'no-store', signal: control.signal, throwOnError: true })
    if (!current()) return
    if (session.id !== destination.session_id || session.bot_id !== destination.bot_id) throw new Error('Execution context changed')
    // The explicit route owns selection and the pinned execution tab. Updating
    // global Bot selection first lets the old primary URL restore its contact.
    await router.push({ name: 'bot', params: { botName: destination.bot_id }, query: { session: session.id, ...taskHistoryQuery(sourceBot, task) } })
    open.value = false
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
        :disabled="!botId || !token"
      >
        <ListTree />{{ t('chat.tasks.title') }}
      </Button>
    </DialogTrigger>
    <DialogPanel :show-close-button="false">
      <DialogViewHeader>{{ t('chat.tasks.title') }}</DialogViewHeader>
      <DialogBody>
        <Spinner v-if="reader.loading.value && !reader.items.value.length" />
        <CalloutBanner
          v-if="reader.error.value"
          tone="destructive"
          size="sm"
          :description="t('chat.tasks.loadFailed')"
        />
        <ul v-if="reader.items.value.length">
          <li
            v-for="row in reader.items.value"
            :key="`${row.task_type}:${row.record_id}`"
          >
            <Button
              variant="ghost"
              size="sm"
              class="w-full justify-between"
              :disabled="navigating || !row.destination"
              @click="navigate(row)"
            >
              <span class="min-w-0 truncate">{{ name(row) }}</span>
              <span class="shrink-0 text-caption text-muted-foreground">{{ state(row) }}</span>
            </Button>
          </li>
        </ul>
        <p
          v-else-if="!reader.error.value && !reader.loading.value"
          class="text-label text-muted-foreground"
        >
          {{ t(reader.nextCursor.value ? 'chat.tasks.filteredPage' : 'chat.taskDirectory.empty') }}
        </p>
        <div class="flex justify-between gap-2">
          <Button
            v-if="reader.nextCursor.value"
            variant="ghost"
            size="sm"
            :disabled="reader.loading.value"
            @click="reader.next"
          >
            {{ t('chat.tasks.next') }}
          </Button>
        </div>
      </DialogBody>
    </DialogPanel>
  </Dialog>
</template>
