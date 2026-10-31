<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button } from '@felinic/ui'
import { relationHasDestination, type ConversationRelation } from '../composables/useConversationRelations'
import { sessionBotRunStatus } from '../composables/session-bot-run'

const props = defineProps<{ row: ConversationRelation, items: ConversationRelation[], disabled: boolean }>()
const emit = defineEmits<{ open: [row: ConversationRelation] }>()
const { t } = useI18n()
const children = computed(() => props.items.filter(item => item.parent === props.row.key && item.key !== props.row.key))
const fallback = computed(() => props.row.kind === 'bot' ? t('bots.title') : t(`chat.tasks.types.${props.row.kind === 'agent' ? 'subagent' : 'computer_operation'}`))
const state = computed(() => props.row.state === 'errored' || props.row.state === 'admitting'
  ? sessionBotRunStatus(props.row.state) : props.row.state || 'unknown')
</script>

<template>
  <li>
    <Button
      variant="ghost"
      size="sm"
      class="w-full justify-between"
      :disabled="disabled || !relationHasDestination(row)"
      @click="emit('open', row)"
    >
      <span class="min-w-0 truncate">{{ row.name || fallback }}</span>
      <span class="shrink-0 text-caption text-muted-foreground">{{ state === 'aborting' ? t('chat.botRun.aborting') : t(`chat.tasks.statuses.${state}`) }}</span>
    </Button>
    <ul
      v-if="children.length"
      class="pl-4"
    >
      <ConversationRelationRow
        v-for="child in children"
        :key="child.key"
        :row="child"
        :items="items"
        :disabled="disabled"
        @open="emit('open', $event)"
      />
    </ul>
  </li>
</template>
