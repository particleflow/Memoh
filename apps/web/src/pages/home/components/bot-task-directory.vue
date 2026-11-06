<script setup lang="ts">
import { computed, onScopeDispose, ref, watch } from 'vue'
import { useLocalStorage } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { ListTree, PauseCircle } from 'lucide-vue-next'
import { Button, Dialog, DialogTrigger, DialogPanel, DialogViewHeader, DialogBody, InlineLoadingRow, Spinner, CalloutBanner, toast } from '@felinic/ui'
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
// Live execution signal mirroring the sidebar activity row: a spinner for
// advancing work and a pause mark for a decision the Bot is waiting on.
const advancing = new Set(['accepted', 'submitted', 'queued', 'running', 'finishing', 'blocked'])
let epoch = 0, controller: AbortController | undefined
function invalidate() { epoch++; controller?.abort(); navigating.value = false }
watch([bot, token, visible], invalidate, { flush: 'sync' })
const off = onAuthSessionCleared(() => { open.value = false; invalidate() })
onScopeDispose(() => { off(); invalidate() })

// The window loads older rows when scrolled to its bottom and rebuilds the
// list when pulled down at the top; the 5 s live poll keeps statuses fresh.
const listEl = ref<HTMLElement>(), pulling = ref(false), pullDistance = ref(0), pullOrigin = ref(0)
const PULL_REFRESH_THRESHOLD = 64
function onScroll() {
  const el = listEl.value
  if (el && el.scrollTop + el.clientHeight >= el.scrollHeight - 48) void reader.loadOlder()
}
function onTouchStart(event: TouchEvent) {
  const el = listEl.value
  if (!el || el.scrollTop > 0 || reader.loading.value) return
  pulling.value = true; pullOrigin.value = event.touches[0]!.clientY; pullDistance.value = 0
}
function onTouchMove(event: TouchEvent) {
  if (!pulling.value) return
  pullDistance.value = Math.max(0, event.touches[0]!.clientY - pullOrigin.value)
}
function onTouchEnd() {
  if (!pulling.value) return
  pulling.value = false
  if (pullDistance.value >= PULL_REFRESH_THRESHOLD) void reader.restart()
  pullDistance.value = 0
}
const pullVisible = computed(() => pullDistance.value > 0 || (reader.loading.value && !reader.items.value.length))

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
        <div
          ref="listEl"
          class="max-h-[min(60vh,30rem)] overflow-y-auto overscroll-contain"
          @scroll="onScroll"
          @touchstart.passive="onTouchStart"
          @touchmove="onTouchMove"
          @touchend="onTouchEnd"
        >
          <div
            v-if="pullVisible"
            class="flex justify-center py-1"
            :style="{ opacity: Math.min(1, pullDistance / 64) }"
            aria-hidden="true"
          >
            <Spinner class="size-4" />
          </div>
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
                class="w-full justify-between gap-2"
                :disabled="navigating || !row.destination"
                :title="name(row)"
                @click="navigate(row)"
              >
                <span class="flex min-w-0 items-center gap-1.5">
                  <Spinner
                    v-if="advancing.has(row.state)"
                    class="size-3.5 shrink-0"
                    aria-hidden="true"
                  />
                  <PauseCircle
                    v-else-if="row.state === 'waiting_decision'"
                    class="size-3.5 shrink-0"
                    aria-hidden="true"
                  />
                  <span class="min-w-0 truncate">{{ name(row) }}</span>
                </span>
                <span class="flex shrink-0 items-center gap-1.5 text-caption text-muted-foreground">
                  <span v-if="row.progress">{{ row.progress }}</span>
                  <span>{{ state(row) }}</span>
                </span>
              </Button>
            </li>
          </ul>
          <p
            v-else-if="!reader.error.value && !reader.loading.value"
            class="text-label text-muted-foreground"
          >
            {{ t(reader.nextCursor.value ? 'chat.tasks.filteredPage' : 'chat.taskDirectory.empty') }}
          </p>
          <InlineLoadingRow v-if="reader.loadingOlder.value">
            {{ t('common.loading') }}
          </InlineLoadingRow>
        </div>
      </DialogBody>
    </DialogPanel>
  </Dialog>
</template>
