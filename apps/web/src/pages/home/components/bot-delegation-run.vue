<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, CalloutBanner } from '@felinic/ui'
import type { BotdelegationView } from '@memohai/sdk'
import { formatDateTimeSeconds } from '@/utils/date-time'
import { resolveApiErrorMessage } from '@/utils/api-error'
import type { BotDelegationHistoryAction } from '../composables/useBotDelegationHistory'

const props = defineProps<{ task: BotdelegationView, loading?: boolean, historyAction?: BotDelegationHistoryAction }>()
const { t } = useI18n()
const state = computed(() => ['accepted', 'running', 'waiting_decision', 'finishing', 'completed', 'aborted', 'failed', 'lost'].includes(props.task.target_run_state ?? '') ? props.task.target_run_state : 'unknown')
</script>

<template>
  <section class="space-y-2">
    <h3 class="text-label font-medium">
      {{ t('chat.delegations.nativeRun') }}
    </h3>
    <template v-if="task.target_run_id">
      <p class="break-all text-body">
        {{ t('chat.delegations.runId') }} · {{ task.target_run_id }}
      </p>
      <p class="text-body">
        {{ t(`chat.tasks.statuses.${state}`) }}
      </p>
      <p
        v-if="task.target_run_created_at"
        class="text-body text-muted-foreground"
      >
        {{ t('chat.delegations.runCreated') }} · {{ formatDateTimeSeconds(task.target_run_created_at) }}
      </p>
      <p
        v-if="task.target_run_updated_at"
        class="text-body text-muted-foreground"
      >
        {{ t('chat.delegations.runUpdated') }} · {{ formatDateTimeSeconds(task.target_run_updated_at) }}
      </p>
      <Button
        v-if="historyAction"
        variant="outline"
        :disabled="loading || historyAction.pending"
        @click="historyAction.open()"
      >
        {{ t('chat.delegations.openHistory') }}
      </Button>
      <CalloutBanner
        v-if="historyAction?.error"
        tone="warning"
        size="sm"
        :description="resolveApiErrorMessage(historyAction.error, t('chat.delegations.historyUnavailable'))"
      />
    </template>
    <p
      v-else
      class="text-body text-muted-foreground"
    >
      {{ t('chat.delegations.runUnavailable') }}
    </p>
  </section>
</template>
