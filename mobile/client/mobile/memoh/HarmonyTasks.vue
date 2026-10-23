<template>
  <div
    v-if="harmonyChatOnly"
    ref="opener"
    class="flex shrink-0 justify-end px-3 py-1"
  >
    <Button
      type="button"
      variant="ghost"
      size="sm"
      :disabled="!botId || !parentSessionId || disabled"
      @click="open"
    >
      任务列表
    </Button>
    <dialog
      ref="dialog"
      aria-labelledby="harmony-tasks-title"
      :aria-describedby="group === 'delegations' ? 'harmony-delegations-description' : undefined"
      class="fixed inset-0 m-0 h-dvh max-h-none w-screen max-w-none border-0 bg-background p-4 text-foreground backdrop:bg-[var(--scrim)]"
      @cancel.prevent="back"
      @close="close"
    >
      <div class="mx-auto flex h-full max-w-2xl flex-col gap-4">
        <header class="flex shrink-0 items-center justify-between gap-3">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            @click="back"
          >
            {{ hasSelection ? '返回任务列表' : '返回父对话' }}
          </Button>
          <h2
            id="harmony-tasks-title"
            class="truncate font-medium"
          >
            {{ group === 'delegations' ? t('chat.delegations.title') : selected ? '任务概要' : '任务列表' }}
          </h2>
          <Button
            v-if="hasSelection"
            type="button"
            variant="ghost"
            size="sm"
            @click="close"
          >
            父对话
          </Button>
        </header>
        <Tabs
          v-model="group"
          class="flex min-h-0 flex-1 flex-col"
        >
          <TabsList v-if="!hasSelection">
            <TabsTrigger value="execution">
              {{ t('chat.delegations.executionGroup') }}
            </TabsTrigger>
            <TabsTrigger value="delegations">
              {{ t('chat.delegations.title') }}
            </TabsTrigger>
          </TabsList>
          <TabsContent
            value="delegations"
            class="min-h-0 flex-1 space-y-4 overflow-y-auto"
          >
            <p
              id="harmony-delegations-description"
              class="text-sm text-muted-foreground"
            >
              {{ t('chat.delegations.description') }}
            </p>
            <p
              v-if="scopePending"
              role="status"
              class="text-sm"
            >
              电脑选择尚未保存到服务端。请发送消息并确认会话记录更新，或恢复原选择，再读取任务。
            </p>
            <BotDelegationTasks
              v-else
              :items="delegations"
              :detail="delegationDetail"
              :selected="delegationSelected"
              :cursor="delegationCursor"
              :next-cursor="delegationNext"
              :loading="delegationLoading"
              :error="delegationError"
              :delivery-action="allowDelivery ? deliveryAction : undefined"
              @open-task="openDelegation"
              @page="delegationPage"
              @refresh="refreshDelegations"
            />
          </TabsContent>
          <TabsContent
            value="execution"
            class="flex min-h-0 flex-1 flex-col gap-4"
          >
            <p
              v-if="scopePending"
              role="status"
              class="text-sm"
            >
              电脑选择尚未保存到服务端。请发送消息并确认会话记录更新，或恢复原选择，再读取任务。
            </p>
            <p
              v-if="busy"
              role="status"
              class="text-sm"
            >
              正在读取…
            </p>
            <p
              v-if="error"
              role="alert"
              class="text-sm text-destructive"
            >
              {{ error }}
            </p>
            <section
              v-if="selected"
              class="min-h-0 flex-1 space-y-4 overflow-y-auto"
              aria-label="任务概要"
            >
              <h3 class="break-words font-medium">
                {{ taskTitle(selected) }}
              </h3>
              <p class="text-sm">
                {{ taskStatus(selected) }}
              </p>
              <p class="break-words text-sm">
                {{ selected.summary }}
              </p>
              <p
                v-if="selected.cancel_requested"
                class="text-sm"
              >
                已请求取消，尚不能确认进程已停止。
              </p>
              <template v-if="selected.task_type === 'computer_operation'">
                <p class="text-sm">
                  Pi 调用结果：{{ taskPiResult(selected) }}
                </p>
                <p class="text-sm">
                  业务结果未知；调用成功或进程退出不代表业务完成。
                </p>
                <p class="text-sm">
                  进程：{{ selected.process_state === 'exited' ? '已退出' : selected.process_state === 'not_started' ? '未启动' : '未知' }}
                </p>
              </template>
              <section
                v-if="selected.result_summary"
                class="space-y-2 rounded border p-3"
                aria-label="最终摘要"
              >
                <h4 class="font-medium">
                  最终助手摘要
                </h4>
                <span
                  v-if="selected.result_summary.truncated"
                  class="block text-xs text-muted-foreground"
                >摘要已截断，仅显示部分最终文本。</span>
                <p class="whitespace-pre-wrap text-sm [overflow-wrap:anywhere]">
                  {{ selected.result_summary.text }}
                </p>
              </section>
              <p
                v-else
                class="text-sm text-muted-foreground"
              >
                最终摘要暂不可用。
              </p>
              <dl class="space-y-2 break-all text-xs text-muted-foreground">
                <div><dt>任务 ID</dt><dd>{{ selected.task_id }}</dd></div>
                <div><dt>所属会话</dt><dd>{{ selected.session_id }}</dd></div>
                <div v-if="selected.target_id">
                  <dt>电脑挂载 ID</dt><dd>{{ selected.target_id }}</dd>
                </div>
                <div v-if="selected.revision">
                  <dt>记录版本</dt><dd>{{ selected.revision }}</dd>
                </div>
                <div><dt>创建于</dt><dd>{{ date(selected.created_at) }}</dd></div>
                <div><dt>记录更新于（不代表仍在运行）</dt><dd>{{ date(selected.updated_at) }}</dd></div>
              </dl>
              <p class="text-sm text-muted-foreground">
                执行消息、工具调用、日志正文和结果附件暂不可查看；当前没有取消或重试操作。
              </p>
              <Button
                type="button"
                variant="outline"
                size="sm"
                :disabled="busy"
                @click="detail(selected)"
              >
                刷新概要
              </Button>
            </section>
            <section
              v-show="!selected && !scopePending"
              ref="list"
              class="min-h-0 flex-1 space-y-3 overflow-y-auto"
              aria-label="当前会话任务"
            >
              <p class="text-sm text-muted-foreground">
                当前会话及其子会话的可访问任务。范围以服务端已保存的会话电脑选择和当前权限为准。
              </p>
              <Button
                type="button"
                variant="outline"
                size="sm"
                :disabled="busy"
                @click="load(false)"
              >
                刷新
              </Button>
              <p
                v-if="!busy && !error && !items.length"
                class="text-sm"
              >
                {{ cursor ? '本页没有可访问任务，可继续加载。' : '暂无可访问任务' }}
              </p>
              <Item
                v-for="task in items"
                :key="taskKey(task)"
                as="button"
                type="button"
                variant="outline"
                size="sm"
                class="w-full text-left disabled:opacity-50"
                :disabled="busy"
                @click="detail(task)"
              >
                <ItemContent>
                  <span class="block break-words">{{ taskTitle(task) }} · {{ taskStatus(task) }}</span>
                  <span class="block break-words text-xs text-muted-foreground">{{ task.summary }}</span>
                  <span
                    v-if="task.task_type === 'computer_operation'"
                    class="block text-xs"
                  >Pi 调用结果：{{ taskPiResult(task) }} · 业务结果未知</span>
                  <span
                    v-if="task.result_summary"
                    class="mt-2 block space-y-1"
                  >
                    <span class="line-clamp-3 whitespace-pre-wrap text-sm [overflow-wrap:anywhere]">{{ task.result_summary.text }}</span>
                    <span
                      v-if="task.result_summary.truncated"
                      class="block text-xs text-muted-foreground"
                    >摘要已截断</span>
                  </span>
                  <span
                    v-else
                    class="block text-xs text-muted-foreground"
                  >最终摘要暂不可用</span>
                  <span
                    v-if="task.cancel_requested"
                    class="block text-xs"
                  >已请求取消 · 停止状态未确认</span>
                  <span class="block text-xs text-muted-foreground">{{ date(task.created_at) }}</span>
                </ItemContent>
              </Item>
              <Button
                v-if="cursor"
                type="button"
                variant="outline"
                size="sm"
                :disabled="busy"
                @click="load(true)"
              >
                加载更多
              </Button>
              <p class="border-t pt-3 text-sm text-muted-foreground">
                这是任务记录列表。已提交、进程退出或取消请求都不代表业务完成。
              </p>
            </section>
          </TabsContent>
        </Tabs>
      </div>
    </dialog>
  </div>
</template>
<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onDeactivated, ref, watch } from 'vue'
import { useDocumentVisibility, useEventListener, useLocalStorage } from '@vueuse/core'
import { Button, Item, ItemContent, Tabs, TabsList, TabsTrigger, TabsContent } from '@felinic/ui'
import { sessionTaskDetail, sessionTasks, taskKey, taskTitle, taskStatus, taskPiResult, taskError, type Task } from './harmony-tasks'
import { useI18n } from 'vue-i18n'
import { useBotDelegations } from '@/pages/home/composables/useBotDelegations'
import { useBotDelegationDelivery } from '@/pages/home/composables/useBotDelegationDelivery'
import BotDelegationTasks from '@/pages/home/components/bot-delegation-tasks.vue'
import { harmonyChatOnly } from './harmony-chat-scope'
import { authSessionGeneration, onAuthSessionCleared } from '@/lib/auth-session'
import { sdkAuthQuery } from '@/lib/api-client'
const props = defineProps<{ botId: string; parentSessionId: string; disabled?: boolean; scopePending?: boolean; scopeKey?: string; allowDelivery?: boolean }>()
const dialog = ref<HTMLDialogElement>(), opener = ref<HTMLDivElement>(), list = ref<HTMLElement>()
const { t } = useI18n()
const group = ref('execution'), opened = ref(false)
const identity = useLocalStorage('token', '')
const documentVisibility = useDocumentVisibility()
const delegationScope = computed(() => opened.value && !props.disabled && !props.scopePending && props.botId && props.parentSessionId && identity.value ? {
  bot: props.botId, session: props.parentSessionId, selection: props.scopeKey ?? '', identity: identity.value,
} : null)
const delegationVisible = computed(() => opened.value && group.value === 'delegations' && documentVisibility.value === 'visible')
const delegationReader = useBotDelegations(delegationScope, delegationVisible)
const deliveryScope = computed(() => props.allowDelivery ? delegationScope.value : null)
const deliveryAction = useBotDelegationDelivery(deliveryScope, delegationVisible, delegationReader)
const { items: delegations, detail: delegationDetail, selected: delegationSelected, cursor: delegationCursor, nextCursor: delegationNext, loading: delegationLoading, error: delegationError, openTask: openDelegation, back: backDelegation, page: delegationPage, refresh: refreshDelegations } = delegationReader
const hasSelection = computed(() => group.value === 'delegations' ? !!delegationSelected.value : !!selected.value)
const items = ref<Task[]>([]), cursor = ref(''), selected = ref<Task>(), busy = ref(false), error = ref('')
let scroll = 0, epoch = 0, controller: AbortController | undefined
function cancel() { epoch++; controller?.abort(); controller = undefined; busy.value = false; error.value = '' }
function clear() { selected.value = undefined; items.value = []; cursor.value = ''; scroll = 0 }
function close() { opened.value = false; cancel(); clear(); if (dialog.value?.open) dialog.value.close(); opener.value?.querySelector('button')?.focus() }
// Returning from the background requires a fresh authorized read. Do not keep
// task summaries in a hidden document or let its delayed response revive them.
useEventListener(document, 'visibilitychange', () => { if (document.visibilityState === 'hidden') close() })
useEventListener(window, 'pagehide', close)
watch(() => [props.botId, props.parentSessionId, props.disabled, props.scopePending, props.scopeKey, props.allowDelivery], close, { flush: 'sync' })
watch(identity, close, { flush: 'sync' })
watch(group, () => { cancel(); clear(); if (opened.value && group.value === 'execution') void load() }, { flush: 'sync' })
const offAuth = onAuthSessionCleared(close)
onBeforeUnmount(() => { close(); offAuth() }); onDeactivated(close)
async function request<T>(run: (signal: AbortSignal) => Promise<T>, accept: (data: T) => void) {
  cancel(); const current = epoch, auth = authSessionGeneration(), token = sdkAuthQuery().token, mine = new AbortController()
  controller = mine; busy.value = true
  const valid = () => current === epoch && !mine.signal.aborted && auth === authSessionGeneration() && token === sdkAuthQuery().token && dialog.value?.open && group.value === 'execution' && !props.scopePending
  try { const result = await run(mine.signal); if (valid()) accept(result) }
  catch (cause) { if (valid()) { clear(); error.value = taskError(cause) } }
  finally { if (current === epoch) { controller = undefined; busy.value = false } }
}
async function load(more = false) {
  if (busy.value || props.scopePending || group.value !== 'execution') return
  const next = more ? cursor.value : ''
  if (!more) clear()
  await request(signal => sessionTasks(props.botId, props.parentSessionId, next, signal), page => {
    const combined = new Map((more ? items.value : []).map(item => [taskKey(item), item]))
    page.items.forEach(item => combined.set(taskKey(item), item))
    items.value = [...combined.values()]; cursor.value = page.cursor
  })
}
async function open() {
  if (!props.botId || !props.parentSessionId || props.disabled || dialog.value?.open || document.visibilityState === 'hidden') return
  group.value = 'execution'; clear(); dialog.value?.showModal(); opened.value = true; if (!props.scopePending) await load()
}
async function detail(task: Task) {
  if (busy.value || props.scopePending) return
  if (!selected.value) scroll = list.value?.scrollTop || 0
  selected.value = undefined
  await request(signal => sessionTaskDetail(props.botId, props.parentSessionId, task, signal), data => { selected.value = data })
}
async function back() {
  if (group.value === 'delegations') {
    if (delegationSelected.value) backDelegation()
    else close()
    return
  }
  cancel()
  if (!selected.value) { close(); return }
  selected.value = undefined
  await nextTick(); if (list.value) list.value.scrollTop = scroll
}
function date(value: string) { return new Date(value).toLocaleString() }
</script>
