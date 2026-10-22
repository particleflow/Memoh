<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, DialogDescription, Empty, EmptyDescription, InlineLoadingRow, Item, ItemContent, ItemTitle, ItemDescription } from '@felinic/ui'
import type { BotdelegationView } from '@memohai/sdk'
import { isApiErrorCode, resolveApiErrorMessage } from '@/utils/api-error'
import { delegationAPIUnsupported } from '../composables/useBotDelegations'

const props = defineProps<{
  items: BotdelegationView[]
  detail?: BotdelegationView
  selected?: string
  cursor?: string
  nextCursor?: string
  loading: boolean
  error?: unknown
}>()
const emit = defineEmits<{
  openTask: [task: BotdelegationView]
  page: [cursor?: string]
  refresh: []
}>()
const { t } = useI18n()
const errorMessage = computed(() => {
  if (delegationAPIUnsupported(props.error)) return t('chat.delegations.unsupported')
  if (isApiErrorCode(props.error, 'session.not_found')) return t('chat.delegations.inaccessible')
  return resolveApiErrorMessage(props.error, t('chat.delegations.loadFailed'))
})
function stateLabel(value: string) {
  const state = ['accepted', 'running', 'unknown', 'completed', 'failed'].includes(value) ? value : 'unknown'
  return t(`chat.delegations.states.${state}`)
}
function deliveryLabel(value: string) {
  const state = ['pending', 'accepted', 'unknown', 'unavailable'].includes(value) ? value : 'unknown'
  return t(`chat.delegations.delivery.${state}`)
}
function taskLabel(task: BotdelegationView) {
  return `${t('chat.delegations.targetBot')} ${task.target_bot_id.slice(0, 8)} · ${task.delegation_id.slice(0, 8)}`
}
</script>

<template>
  <DialogDescription>{{ t('chat.delegations.description') }}</DialogDescription>
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
  <section
    v-else-if="selected && detail"
    class="space-y-4"
  >
    <div class="space-y-1">
      <h3 class="text-label font-medium">
        {{ taskLabel(detail) }}
      </h3>
      <p class="text-body">
        {{ t('chat.delegations.execution') }} · {{ stateLabel(detail.state) }}
      </p>
      <p class="text-body">
        {{ t('chat.delegations.returnDelivery') }} · {{ deliveryLabel(detail.delivery_state) }}
      </p>
      <p
        v-if="detail.delivery_state === 'accepted'"
        class="text-body text-muted-foreground"
      >
        {{ t('chat.delegations.deliveryAccepted') }}
      </p>
    </div>
    <div class="space-y-2">
      <h3 class="text-label font-medium">
        {{ t('chat.delegations.reply') }}
      </h3>
      <!-- The service exposes final visible text, never a trusted HTML document. -->
      <p
        v-if="detail.state === 'completed' && detail.summary?.text"
        class="whitespace-pre-wrap break-words text-body"
      >
        {{ detail.summary.text }}
      </p>
      <p
        v-else
        class="text-body text-muted-foreground"
      >
        {{ t('chat.delegations.contentUnavailable') }}
      </p>
      <p
        v-if="detail.state === 'completed' && detail.summary?.truncated"
        class="text-body text-muted-foreground"
      >
        {{ t('chat.tasks.truncated') }}
      </p>
      <p
        v-if="detail.state === 'accepted' || detail.state === 'running'"
        class="text-body text-muted-foreground"
      >
        {{ t('chat.delegations.noLiveContent') }}
      </p>
      <!-- Attachment delivery is being verified separately. Never construct a
           Bot/media fallback URL or imply that a reported reference is downloadable. -->
      <p
        v-if="detail.state === 'completed' && detail.attachments.length"
        class="text-body text-muted-foreground"
      >
        {{ t('chat.delegations.attachmentsUnavailable', { count: detail.attachments.length }) }}
      </p>
    </div>
  </section>
  <template v-else-if="!selected">
    <Empty v-if="!items.length">
      <EmptyDescription>{{ nextCursor ? t('chat.tasks.filteredPage') : t('chat.delegations.empty') }}</EmptyDescription>
    </Empty>
    <Item
      v-for="item in items"
      :key="item.delegation_id"
      as="button"
      type="button"
      size="sm"
      class="w-full text-left"
      @click="emit('openTask', item)"
    >
      <ItemContent>
        <ItemTitle>{{ taskLabel(item) }}</ItemTitle>
        <ItemDescription>{{ stateLabel(item.state) }} · {{ t('chat.delegations.returnDelivery') }}: {{ deliveryLabel(item.delivery_state) }}</ItemDescription>
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
