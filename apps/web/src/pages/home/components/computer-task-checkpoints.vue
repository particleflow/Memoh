<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ComputertaskRecovery } from '@memohai/sdk'

// Projection only: the existing observation reader owns authorization and refresh.
const props = defineProps<{ data: ComputertaskRecovery; receivedAt?: string }>()
const { t, te } = useI18n()
const prefix = 'chat.tasks.observation'
const unknown = () => t(`${prefix}.states.unknown`)
function text(value?: string) { return value || unknown() }
function label(group: string, value?: string) {
  const key = `${prefix}.${group}.${value ?? 'unknown'}`
  return te(key) ? t(key) : te(`${prefix}.${group}.unknown`) ? t(`${prefix}.${group}.unknown`) : unknown()
}
function bytes(value?: number) {
  return value !== undefined && Number.isSafeInteger(value) && value >= 0 ? String(value) : unknown()
}
const groups = computed(() => {
  const { data } = props
  const checkpoint = data.reconciliation
  const outcomeKey = `chat.tasks.outcomes.${data.pi_result}`
  return [
    { name: 'receipt', rows: [
      ['revision', text(checkpoint?.revision)],
      ['attempt', text(data.attempt)],
      ['generation', text(checkpoint?.generation)],
      ['receipt_state', label('checkpoints.receiptStates', checkpoint?.receipt_state)],
      ['process_state', label('checkpoints.processStates', data.process_state)],
      ['pi_result', te(outcomeKey) ? t(outcomeKey) : unknown()],
      ['result_reason', label('checkpoints.resultReasons', data.result_reason)],
      // Pi completion is not a business outcome. No success is inferred here.
      ['business_outcome', unknown()],
    ] },
    { name: 'history', rows: [
      ['continuity', label('checkpoints.continuityStates', data.continuity?.status)],
      ['continuity_reason', label('checkpoints.continuityReasons', data.continuity?.reason)],
      ['before', label('checkpoints.historyStates', data.continuity?.last_invocation?.before)],
      ['after', label('checkpoints.historyStates', data.continuity?.last_invocation?.after)],
    ] },
  ]
})
</script>

<template>
  <div class="space-y-4">
    <section
      class="space-y-2"
      :aria-label="t(`${prefix}.checkpoints.connection`)"
    >
      <h4 class="text-label font-medium">
        {{ t(`${prefix}.checkpoints.connection`) }}
      </h4>
      <p class="text-body">
        {{ label('availability', data.native?.availability) }}
      </p>
      <p class="text-body">
        {{ t(`${prefix}.agentState`, { state: label('states', data.native?.agent_status) }) }}
      </p>
      <p class="text-body text-muted-foreground">
        {{ label('match', data.native?.reference_match) }}
      </p>
      <dl class="space-y-1 text-body">
        <div
          v-for="field in ['target_id', 'workspace_id', 'pane_id', 'pi_session_id'] as const"
          :key="field"
          class="grid gap-x-4 sm:grid-cols-2"
        >
          <dt class="text-muted-foreground">
            {{ t(`${prefix}.fields.${field}`) }}
          </dt>
          <dd class="min-w-0 break-all">
            {{ text(data[field]) }}
          </dd>
        </div>
      </dl>
      <p class="text-body text-muted-foreground">
        {{ t(`${prefix}.receivedAt`, { time: text(receivedAt) }) }}
      </p>
    </section>
    <section
      v-for="group in groups"
      :key="group.name"
      class="space-y-2"
      :aria-label="t(`${prefix}.checkpoints.${group.name}`)"
    >
      <h4 class="text-label font-medium">
        {{ t(`${prefix}.checkpoints.${group.name}`) }}
      </h4>
      <p class="text-body text-muted-foreground">
        {{ t(`${prefix}.checkpoints.${group.name}Notice`) }}
      </p>
      <dl class="space-y-1 text-body">
        <div
          v-for="[field, value] in group.rows"
          :key="field"
          class="grid gap-x-4 sm:grid-cols-2"
        >
          <dt class="text-muted-foreground">
            {{ t(`${prefix}.checkpoints.fields.${field}`) }}
          </dt>
          <dd class="min-w-0 break-all">
            {{ value }}
          </dd>
        </div>
      </dl>
    </section>
    <section
      class="space-y-2"
      :aria-label="t(`${prefix}.checkpoints.collection`)"
    >
      <h4 class="text-label font-medium">
        {{ t(`${prefix}.checkpoints.collection`) }}
      </h4>
      <p class="text-body text-muted-foreground">
        {{ t(`${prefix}.checkpoints.collectionNotice`) }}
      </p>
      <p
        v-if="!data.reconciliation?.files?.length"
        class="text-body"
      >
        {{ unknown() }}
      </p>
      <dl
        v-for="(file, index) in data.reconciliation?.files"
        :key="index"
        class="space-y-1 text-body"
      >
        <dt class="text-label font-medium break-all">
          {{ text(file.meta?.name) }}
        </dt>
        <dd>{{ label('checkpoints.collectionStates', file.state) }}</dd>
        <dt class="text-muted-foreground">
          {{ t(`${prefix}.checkpoints.fields.confirmed_offset`) }}
        </dt>
        <dd>{{ t(`${prefix}.checkpoints.bytes`, { offset: bytes(file.confirmed_offset), size: bytes(file.meta?.size) }) }}</dd>
        <dt class="text-muted-foreground">
          {{ t(`${prefix}.checkpoints.fields.current_availability`) }}
        </dt>
        <dd>{{ label('checkpoints.fileAvailability', file.current_availability) }}</dd>
        <dt class="text-muted-foreground">
          {{ t(`${prefix}.checkpoints.fields.recorded_content_hash`) }}
        </dt>
        <dd class="break-all">
          {{ text(file.recorded_content_hash) }}
        </dd>
      </dl>
    </section>
  </div>
</template>
