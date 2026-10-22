<script setup lang="ts">
import { computed, onScopeDispose, ref } from 'vue'
import { useDocumentVisibility, useLocalStorage } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import { ListTree } from 'lucide-vue-next'
import { Button, Dialog, DialogTrigger, DialogPanel, DialogViewHeader, DialogBody, DialogDescription, Empty, EmptyDescription, InlineLoadingRow, Item, ItemContent, ItemTitle, ItemDescription } from '@felinic/ui'
import type { SessiontaskTask } from '@memohai/sdk'
import { onAuthSessionCleared } from '@/lib/auth-session'
import { resolveApiErrorMessage } from '@/utils/api-error'
import { useSessionTasks } from '../composables/useSessionTasks'

const props = defineProps<{ botId: string, sessionId: string, selection: string, visible: boolean }>()
const { t } = useI18n()
const token = useLocalStorage('token', '')
const open = ref(false)
const documentVisibility = useDocumentVisibility()
const visible = computed(() => open.value && props.visible && documentVisibility.value === 'visible')
const scope = computed(() => props.botId && props.sessionId ? {
  bot: props.botId, session: props.sessionId, selection: props.selection, identity: token.value,
} : null)
const { items, detail, selected, cursor, nextCursor, loading, error, openTask, back, page, refresh } = useSessionTasks(scope, visible)
// Login notifies before replacing the token; a same-tab 401 can clear it
// without a storage event. Close now so pending reads cannot revive content
// while the parent view/router is still mounted.
onScopeDispose(onAuthSessionCleared(() => { open.value = false }))

function stateLabel(task: SessiontaskTask) {
  const outcome = task.pi_result
  const known = ['succeeded', 'failed', 'aborted', 'incomplete', 'not_started', 'invalid_protocol', 'output_limit']
  if (outcome && known.includes(outcome)) {
    const result = t(`chat.tasks.outcomes.${outcome}`)
    return task.process_state === 'exited' ? `${t('chat.tasks.statuses.process_exited')} · ${result}` : result
  }
  const status = ['submitted', 'process_exited', 'not_started', 'rejected'].includes(task.status) ? task.status : 'unknown'
  return t(`chat.tasks.statuses.${status}`)
}

function taskLabel(task: SessiontaskTask) {
  return `${t(`chat.tasks.types.${task.task_type}`)} · ${task.task_id.slice(0, 8)}`
}
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
        :centered="!!selected || items.length > 0"
        :show-back="!!selected"
        :back-label="t('common.back')"
        @back="back"
      >
        {{ selected ? t('chat.tasks.detail') : t('chat.tasks.title') }}
      </DialogViewHeader>
      <DialogBody class="space-y-4">
        <DialogDescription>{{ t('chat.tasks.description') }}</DialogDescription>
        <Empty v-if="error">
          <EmptyDescription>{{ resolveApiErrorMessage(error, t('chat.tasks.loadFailed')) }}</EmptyDescription>
          <Button
            variant="outline"
            @click="refresh"
          >
            {{ t('chat.tasks.reload') }}
          </Button>
        </Empty>
        <InlineLoadingRow v-else-if="loading && !detail && !items.length">
          {{ t('common.loading') }}
        </InlineLoadingRow>
        <template v-else-if="selected">
          <section
            v-if="detail"
            class="space-y-4"
          >
            <div class="space-y-1">
              <h3 class="text-label font-medium">
                {{ taskLabel(detail) }}
              </h3>
              <p class="text-body">
                {{ stateLabel(detail) }}
              </p>
              <p
                v-if="detail.cancel_requested"
                class="text-body text-muted-foreground"
              >
                {{ t('chat.tasks.cancelIntent') }}
              </p>
            </div>
            <div class="space-y-2">
              <h3 class="text-label font-medium">
                {{ t('chat.tasks.result') }}
              </h3>
              <!-- Plain text: never interpret links, HTML or embedded media. -->
              <p
                v-if="detail.content_available && detail.result_summary"
                class="whitespace-pre-wrap break-words text-body"
              >
                {{ detail.result_summary.text }}
              </p>
              <p
                v-else
                class="text-body text-muted-foreground"
              >
                {{ t('chat.tasks.unavailable') }}
              </p>
              <p
                v-if="detail.result_summary?.truncated"
                class="text-body text-muted-foreground"
              >
                {{ t('chat.tasks.truncated') }}
              </p>
            </div>
            <p class="text-body text-muted-foreground">
              {{ t('chat.tasks.businessUnknown') }}
            </p>
          </section>
        </template>
        <template v-else>
          <Empty v-if="!items.length">
            <EmptyDescription>{{ nextCursor ? t('chat.tasks.filteredPage') : t('chat.tasks.empty') }}</EmptyDescription>
          </Empty>
          <Item
            v-for="item in items"
            :key="`${item.task_type}:${item.task_id}`"
            as="button"
            type="button"
            size="sm"
            class="w-full text-left"
            @click="openTask(item)"
          >
            <ItemContent>
              <ItemTitle>{{ taskLabel(item) }}</ItemTitle>
              <ItemDescription>{{ stateLabel(item) }}</ItemDescription>
            </ItemContent>
          </Item>
          <div
            v-if="cursor || nextCursor"
            class="flex flex-wrap gap-2"
          >
            <Button
              v-if="cursor"
              variant="ghost"
              :disabled="loading"
              @click="page()"
            >
              {{ t('chat.tasks.newest') }}
            </Button>
            <Button
              v-if="nextCursor"
              variant="outline"
              :disabled="loading"
              @click="page(nextCursor)"
            >
              {{ t('chat.tasks.next') }}
            </Button>
          </div>
        </template>
      </DialogBody>
    </DialogPanel>
  </Dialog>
</template>
