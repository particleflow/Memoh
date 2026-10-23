<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, Empty, EmptyDescription, InlineLoadingRow, Item, ItemContent, ItemTitle, ItemDescription } from '@felinic/ui'
import type { SessiontaskTask } from '@memohai/sdk'
import { apiErrorStatus, isApiErrorCode, parseMemohError, resolveApiErrorMessage } from '@/utils/api-error'

// Shared read-only content. The host owns dialog semantics, navigation and scope.
const props = defineProps<{
  items: SessiontaskTask[]
  detail?: SessiontaskTask
  selected?: Pick<SessiontaskTask, 'task_type' | 'task_id'>
  cursor?: string
  nextCursor?: string
  loading: boolean
  error?: unknown
}>()
const emit = defineEmits<{
  openTask: [task: SessiontaskTask]
  page: [cursor?: string]
  refresh: []
}>()
const { t } = useI18n()
const errorMessage = computed(() => {
  const code = parseMemohError(props.error)?.code
  // Legacy servers may return an untyped route 404. A current scoped denial
  // uses session.not_found and must not be reported as a missing feature.
  if (code === 'http.not_found' || (!code && apiErrorStatus(props.error) === 404)) return t('chat.tasks.unsupported')
  if (isApiErrorCode(props.error, 'session.not_found')) return t('chat.tasks.inaccessible')
  return resolveApiErrorMessage(props.error, t('chat.tasks.loadFailed'))
})
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
  <Empty v-if="error">
    <EmptyDescription>{{ errorMessage }}</EmptyDescription>
    <Button
      variant="outline"
      :disabled="loading"
      @click="emit('refresh')"
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
      @click="emit('openTask', item)"
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
        @click="emit('page')"
      >
        {{ t('chat.tasks.newest') }}
      </Button>
      <Button
        v-if="nextCursor"
        variant="outline"
        :disabled="loading"
        @click="emit('page', nextCursor)"
      >
        {{ t('chat.tasks.next') }}
      </Button>
    </div>
  </template>
</template>
