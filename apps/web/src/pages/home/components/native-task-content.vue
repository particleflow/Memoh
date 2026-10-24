<script setup lang="ts">
import { computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useForm } from 'vee-validate'
import { toTypedSchema } from '@vee-validate/zod'
import * as z from 'zod'
import { Button, FieldStack, Textarea, Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from '@felinic/ui'
import type { NativeTaskView } from '../composables/useNativeTask'
import { apiErrorStatus, isApiErrorCode, resolveApiErrorMessage } from '@/utils/api-error'
const props = defineProps<{ view: NativeTaskView; operationId: string }>()
const { t } = useI18n()
const { defineField, handleSubmit, resetForm, errors } = useForm({
  validationSchema: toTypedSchema(z.object({ task: z.string().trim().min(1).refine(value => new TextEncoder().encode(value).length <= 32000).refine(value => !value.includes('\0')), mode: z.enum(['current_agent', 'bound_session']) })),
  initialValues: { task: '', mode: 'bound_session' as 'current_agent' | 'bound_session' },
})
const [task, taskAttrs] = defineField('task'), [mode, modeAttrs] = defineField('mode')
const submit = handleSubmit(async value => { await props.view.send(value.task, value.mode) })
watch(() => props.operationId, () => resetForm())
const failed = computed(() => {
  if (isApiErrorCode(props.view.error, 'session.not_found')) return t('chat.tasks.inaccessible')
  if (apiErrorStatus(props.view.error) === 404) return t('chat.tasks.native.unsupported')
  return resolveApiErrorMessage(props.view.error, t('chat.tasks.native.failed'))
})
const receiptState = computed(() => {
  const state = props.view.response?.state
  return ['accepted', 'rejected', 'unsupported', 'blocked', 'conflict'].includes(state ?? '') ? state : 'unknown'
})
</script>
<template>
  <section
    class="space-y-3"
    :aria-label="t('chat.tasks.native.title')"
  >
    <h3 class="text-label font-medium">
      {{ t('chat.tasks.native.title') }}
    </h3>
    <p class="text-body text-muted-foreground">
      {{ t('chat.tasks.native.notice') }}
    </p>
    <Button
      v-if="!view.opened"
      variant="outline"
      @click="view.open()"
    >
      {{ t('chat.tasks.native.open') }}
    </Button>
    <template v-else>
      <p
        v-if="view.error"
        class="text-body"
      >
        {{ failed }}
      </p>
      <p
        v-if="view.data && !view.data.supported"
        class="text-body"
      >
        {{ t('chat.tasks.native.batch') }}
      </p>
      <template v-else-if="view.data?.observation">
        <p class="text-body text-muted-foreground">
          terminal_snapshot_unverified · {{ view.data.observation.truncated ? t('chat.tasks.native.truncated') : t('chat.tasks.native.bounded') }}
        </p>
        <pre
          tabindex="0"
          class="max-h-64 overflow-auto text-body whitespace-pre-wrap break-all"
        >{{ view.data.observation.output || t('chat.tasks.native.empty') }}</pre>
        <p class="text-body text-muted-foreground">
          {{ t('chat.tasks.native.sequence', { sequence: view.data.observation.completion_seq || '—' }) }}
        </p>
      </template>
      <p
        v-else-if="view.data?.supported"
        class="text-body"
      >
        {{ t('chat.tasks.native.unavailable') }}
      </p>
      <Button
        variant="outline"
        :disabled="view.loading"
        @click="view.refresh()"
      >
        {{ t('chat.tasks.native.refresh') }}
      </Button>
      <p
        v-if="view.response"
        role="status"
        class="text-body"
      >
        {{ t(`chat.tasks.native.results.${receiptState}`) }}
      </p>
      <p
        v-if="view.requestId"
        class="text-body break-all"
      >
        {{ t('chat.tasks.native.pending', { id: view.requestId }) }}
      </p>
      <form
        v-if="view.data?.followup_available"
        class="space-y-3"
        @submit.prevent="submit"
      >
        <FieldStack :label="t('chat.tasks.native.mode')">
          <Select
            v-model="mode"
            v-bind="modeAttrs"
            :disabled="view.sending || !!view.requestId"
          >
            <SelectTrigger :aria-label="t('chat.tasks.native.mode')">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="bound_session">
                {{ t('chat.tasks.native.bound') }}
              </SelectItem>
              <SelectItem value="current_agent">
                {{ t('chat.tasks.native.current') }}
              </SelectItem>
            </SelectContent>
          </Select>
        </FieldStack>
        <p class="text-body text-muted-foreground">
          {{ t('chat.tasks.native.modeNotice') }}
        </p>
        <FieldStack :label="t('chat.tasks.native.instruction')">
          <Textarea
            v-model="task"
            v-bind="taskAttrs"
            :aria-label="t('chat.tasks.native.instruction')"
            :disabled="view.sending || !!view.requestId"
          />
          <p
            v-if="errors.task"
            class="text-body"
          >
            {{ t('chat.tasks.native.invalid') }}
          </p>
        </FieldStack>
        <Button
          type="submit"
          :disabled="view.sending || !!view.requestId"
        >
          {{ t('chat.tasks.native.send') }}
        </Button>
      </form>
    </template>
  </section>
</template>
