<script setup lang="ts">
import { computed, inject, provide, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, Empty, EmptyDescription, InlineLoadingRow, Item, ItemContent, ItemTitle, ItemDescription } from '@felinic/ui'
import NativeTaskContent from './native-task-content.vue'
import type { NativeTaskView } from '../composables/useNativeTask'
import type { ComputerTaskObservationView } from '../composables/useComputerTaskObservation'
import type { ComputerTaskDeliveryView } from '../composables/useComputerTaskDeliveries'
import AttachmentBlock from './attachment-block.vue'
import MediaGalleryLightbox, { type MediaGalleryItem } from './media-gallery-lightbox.vue'
import { resolveUrl, isMediaType } from '../composables/useMediaGallery'
import { openAssetPreviewKey } from '../composables/useFileManagerProvider'
import type { AttachmentItem } from '@/store/chat-list'
import { formatFileSize } from '@/components/file-manager/utils'
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
  deliveries?: ComputerTaskDeliveryView
  observation?: ComputerTaskObservationView
  nativeTask?: NativeTaskView
}>()
const emit = defineEmits<{
  openTask: [task: SessiontaskTask]
  page: [cursor?: string]
  refresh: []
  assetOpened: []
}>()
const { t, te } = useI18n()
// Use the same attachment resolver, preview host and browser download as chat.
// No task credential or per-file preflight: native media rechecks Bot access.
const preview = inject(openAssetPreviewKey, undefined)
if (preview) provide(openAssetPreviewKey, args => { preview(args); emit('assetOpened') })
const attachments = computed(() => {
  const result = new Map<string, AttachmentItem>()
  if (!props.deliveries?.botId || props.deliveries.error) return result
  for (const file of props.deliveries.files) {
    if (file.state !== 'committed' || !file.reference) continue
    result.set(file.delivery_id!, {
      type: 'file', name: file.delivery_id, bot_id: props.deliveries.botId,
      content_hash: file.reference.content_hash, mime: file.reference.mime, size: file.reference.size_bytes,
    })
  }
  return result
})
const mediaItems = computed<MediaGalleryItem[]>(() => [...attachments.value.values()]
  .filter(isMediaType).map(att => ({ src: resolveUrl(att), type: att.mime?.startsWith('video/') ? 'video' : 'image', name: att.name })))
const mediaHost = ref<HTMLElement>()
// Keep the existing lightbox in the host's top layer, outside its scroll body.
const mediaTarget = computed(() => mediaHost.value?.closest<HTMLElement>('dialog, [role="dialog"]') ?? mediaHost.value)
const mediaIndex = ref<number | null>(null)
function openMedia(src: string) {
  const index = mediaItems.value.findIndex(item => item.src === src)
  mediaIndex.value = index < 0 ? null : index
}
watch([() => props.deliveries?.botId, () => props.detail?.task_id, () => props.deliveries?.error], () => { mediaIndex.value = null })
watch(mediaItems, (items, previous) => {
  const index = mediaIndex.value
  if (index !== null && items[index]?.src !== previous[index]?.src) mediaIndex.value = null
})

const errorMessage = computed(() => readError(props.error, 'chat.tasks.unsupported', 'chat.tasks.loadFailed'))
const observationError = computed(() => readError(props.observation?.error, 'chat.tasks.observation.unsupported', 'chat.tasks.observation.loadFailed'))
function nativeLabel(group: string, value?: string) {
  const key = `chat.tasks.observation.${group}.${value ?? 'unknown'}`
  return te(key) ? t(key) : t(`chat.tasks.observation.${group}.unknown`)
}
const deliveriesError = computed(() => readError(props.deliveries?.error, 'chat.tasks.files.unsupported', 'chat.tasks.files.loadFailed'))
function readError(error: unknown, unsupported: string, fallback: string) {
  const code = parseMemohError(error)?.code
  // Legacy servers may return an untyped route 404. A current scoped denial
  // uses session.not_found and must not be reported as a missing feature.
  if (code === 'http.not_found' || (!code && apiErrorStatus(error) === 404)) return t(unsupported)
  if (isApiErrorCode(error, 'session.not_found')) return t('chat.tasks.inaccessible')
  return resolveApiErrorMessage(error, t(fallback))
}
function stateLabel(task: SessiontaskTask) {
  const outcome = task.pi_result
  const known = ['succeeded', 'failed', 'aborted', 'incomplete', 'not_started', 'invalid_protocol', 'output_limit']
  if (outcome && known.includes(outcome)) {
    const result = t(`chat.tasks.outcomes.${outcome}`)
    return task.process_state === 'exited' ? `${t('chat.tasks.statuses.process_exited')} · ${result}` : result
  }
  const status = ['submitted', 'process_exited', 'not_started', 'rejected', 'accepted', 'running', 'waiting_decision', 'finishing', 'completed', 'failed', 'aborted', 'lost'].includes(task.status) ? task.status : 'unknown'
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
      <NativeTaskContent
        v-if="detail.task_type === 'computer_operation' && nativeTask"
        :view="nativeTask"
        :operation-id="detail.task_id"
      />
      <section
        v-if="detail.task_type === 'computer_operation' && observation"
        class="space-y-2"
        :aria-label="t('chat.tasks.observation.title')"
      >
        <h3 class="text-label font-medium">
          {{ t('chat.tasks.observation.title') }}
        </h3>
        <p class="text-body text-muted-foreground">
          {{ t('chat.tasks.observation.description') }}
        </p>
        <p
          v-if="observation.error"
          class="text-body"
        >
          {{ observationError }}
        </p>
        <InlineLoadingRow v-else-if="observation.loading && !observation.data">
          {{ t('common.loading') }}
        </InlineLoadingRow>
        <template v-else-if="observation.data">
          <p class="text-body">
            {{ nativeLabel('availability', observation.data.native?.availability) }}
          </p>
          <p class="text-body">
            {{ t('chat.tasks.observation.agentState', { state: nativeLabel('states', observation.data.native?.agent_status) }) }}
          </p>
          <p class="text-body text-muted-foreground">
            {{ nativeLabel('match', observation.data.native?.reference_match) }}
          </p>
          <dl class="space-y-1 text-body break-all">
            <div
              v-for="field in ['target_id', 'workspace_id', 'pane_id', 'pi_session_id'] as const"
              :key="field"
            >
              <template v-if="observation.data[field]">
                <dt class="text-muted-foreground">
                  {{ t(`chat.tasks.observation.fields.${field}`) }}
                </dt>
                <dd>{{ observation.data[field] }}</dd>
              </template>
            </div>
          </dl>
          <p class="text-body text-muted-foreground">
            {{ t('chat.tasks.observation.receivedAt', { time: observation.receivedAt }) }}
          </p>
        </template>
        <Button
          variant="outline"
          :disabled="observation.loading"
          @click="observation.refresh()"
        >
          {{ t('chat.tasks.observation.refresh') }}
        </Button>
      </section>
      <p
        v-if="detail.task_type === 'subagent'"
        class="text-body text-muted-foreground"
      >
        {{ t('chat.tasks.pollingOutput') }}
      </p>
      <div class="space-y-2">
        <h3 class="text-label font-medium">
          {{ t(detail.task_type === 'subagent' ? 'chat.tasks.visibleOutput' : 'chat.tasks.result') }}
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
      <section
        v-if="detail.task_type === 'computer_operation' && deliveries"
        class="space-y-2"
        :aria-label="t('chat.tasks.files.title')"
      >
        <h3 class="text-label font-medium">
          {{ t('chat.tasks.files.title') }}
        </h3>
        <Empty v-if="deliveries.error">
          <EmptyDescription>{{ deliveriesError }}</EmptyDescription>
          <Button
            variant="outline"
            :disabled="deliveries.loading"
            @click="deliveries.refresh()"
          >
            {{ t('chat.tasks.reload') }}
          </Button>
        </Empty>
        <template v-else>
          <InlineLoadingRow v-if="deliveries.loading">
            {{ t('common.loading') }}
          </InlineLoadingRow>
          <p
            v-else-if="deliveries.loaded && !deliveries.files.length"
            class="text-body text-muted-foreground"
          >
            {{ t('chat.tasks.files.empty') }}
          </p>
          <Item
            v-for="file in deliveries.files"
            :key="file.delivery_id"
            size="sm"
          >
            <ItemContent>
              <ItemTitle class="break-all">
                {{ file.delivery_id }}
              </ItemTitle>
              <ItemDescription>
                {{ t(`chat.tasks.files.states.${['pending', 'reserved', 'committed', 'canceled', 'failed'].includes(file.state ?? '') ? file.state : 'unknown'}`) }}
              </ItemDescription>
              <template v-if="file.state === 'committed' && file.reference">
                <ItemDescription>{{ t(`chat.tasks.files.${file.reference.direction}`) }} · {{ file.reference.mime }} · {{ formatFileSize(file.reference.size_bytes ?? 0) }}</ItemDescription>
                <ItemDescription class="break-all">
                  {{ file.reference.content_hash }}
                </ItemDescription>
                <template v-if="attachments.has(file.delivery_id!)">
                  <AttachmentBlock
                    :block="{ id: 0, type: 'attachments', attachments: [attachments.get(file.delivery_id!)!] }"
                    :on-open-media="openMedia"
                  />
                  <Button
                    variant="outline"
                    size="sm"
                    as-child
                  >
                    <a
                      :href="resolveUrl(attachments.get(file.delivery_id!)!)"
                      :download="file.delivery_id"
                    >{{ t('bots.files.download') }}</a>
                  </Button>
                </template>
              </template>
            </ItemContent>
          </Item>
        </template>
      </section>
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
  <div ref="mediaHost" />
  <MediaGalleryLightbox
    v-if="mediaTarget"
    :teleport-to="mediaTarget"
    :items="mediaItems"
    :open-index="mediaIndex"
    @update:open-index="mediaIndex = $event"
  />
</template>
