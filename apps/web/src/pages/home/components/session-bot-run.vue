<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, Collapsible, CollapsibleContent, CollapsibleTrigger } from '@felinic/ui'
import { useChatStore } from '@/store/chat-list'
import { formatDateTimeSeconds } from '@/utils/date-time'
import { resolveApiErrorMessage } from '@/utils/api-error'
import { sessionBotRunContent, sessionBotRunStatus } from '../composables/session-bot-run'
import { provideNativeTaskResumeScope } from '../composables/native-task-resume-scope'
import ToolCallDetailGeneric from './tool-call-detail-generic.vue'

// Shared content, with no DialogRoot dependency. The chat host already owns the
// authorized runtime subscription and transcript; opening this view sends nothing.
const props = defineProps<{ botId: string, sessionId: string, visible: boolean }>()
provideNativeTaskResumeScope(props)
const { t } = useI18n()
const chat = useChatStore()
const expanded = ref(false)
const observation = computed(() => props.visible ? chat.runtimeObservationFor(props.botId, props.sessionId) : undefined)
const run = computed(() => observation.value?.synchronized ? observation.value.run : null)
const content = computed(() => run.value ? sessionBotRunContent(run.value,
  chat.chatView({ botId: props.botId, sessionId: props.sessionId }).transcript.visibleMessages.value) : undefined)
const state = computed(() => run.value ? sessionBotRunStatus(run.value.status) : 'unknown')
watch([() => props.botId, () => props.sessionId, () => props.visible, () => run.value?.run_id], () => { expanded.value = false })
</script>

<template>
  <section
    class="space-y-2"
    :aria-label="t('chat.botRun.title')"
  >
    <h3 class="text-label font-medium">
      {{ t('chat.botRun.title') }}
    </h3>
    <p
      v-if="!observation?.synchronized"
      class="text-body text-muted-foreground"
    >
      {{ t('chat.botRun.unavailable') }}
    </p>
    <p
      v-else-if="!run"
      class="text-body text-muted-foreground"
    >
      {{ t('chat.botRun.empty') }}
    </p>
    <p
      v-else-if="run.configuration_only"
      class="text-body text-muted-foreground"
    >
      {{ t('chat.botRun.configuration') }}
    </p>
    <template v-else-if="run && content">
      <p class="break-all text-body">
        {{ t('chat.delegations.runId') }} · {{ run.run_id }}
      </p>
      <p class="text-body">
        {{ state === 'aborting' ? t('chat.botRun.aborting') : t(`chat.tasks.statuses.${state}`) }}
      </p>
      <p class="text-body text-muted-foreground">
        {{ t('chat.delegations.runUpdated') }} · {{ formatDateTimeSeconds(run.updated_at) }}
      </p>
      <p
        v-if="run.error_code"
        class="text-body"
      >
        {{ resolveApiErrorMessage({ code: run.error_code }, t('chat.botRun.runError')) }}
      </p>
      <Collapsible v-model:open="expanded">
        <CollapsibleTrigger as-child>
          <Button variant="outline">
            {{ t('chat.botRun.content') }}
          </Button>
        </CollapsibleTrigger>
        <CollapsibleContent class="space-y-3 pt-3">
          <p
            v-if="content.prompt"
            class="whitespace-pre-wrap break-words text-body"
          >
            {{ content.prompt }}
          </p>
          <p
            v-if="content.text"
            class="whitespace-pre-wrap break-words text-body"
          >
            {{ content.text }}
          </p>
          <section
            v-for="tool in content.tools"
            :key="tool.block.toolCallId || tool.block.id"
            class="space-y-1"
          >
            <h4 class="break-all text-label font-medium">
              {{ tool.block.toolName }} · {{ tool.block.toolCallId }}
            </h4>
            <p class="text-body">
              {{ t(`chat.botRun.tools.${tool.state}`) }}
            </p>
            <ToolCallDetailGeneric :block="tool.block" />
          </section>
          <p
            v-if="!content.prompt && !content.text && !content.tools.length"
            class="text-body text-muted-foreground"
          >
            {{ t('chat.botRun.noContent') }}
          </p>
        </CollapsibleContent>
      </Collapsible>
    </template>
  </section>
</template>
