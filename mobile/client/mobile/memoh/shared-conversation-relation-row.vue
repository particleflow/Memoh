<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button } from '@felinic/ui'
import type { ConversationRelation } from '../composables/useConversationRelations'

const props = defineProps<{ row: ConversationRelation, items: ConversationRelation[], disabled: boolean }>()
const emit = defineEmits<{ open: [row: ConversationRelation] }>()
const { t } = useI18n()
const children = computed(() => props.items.filter(item => item.parent === props.row.key && item.key !== props.row.key))
const fallback = computed(() => props.row.kind === 'bot' ? t('bots.title') : t(`chat.tasks.types.${props.row.kind === 'agent' ? 'subagent' : 'computer_operation'}`))
</script>

<template>
  <li>
    <Button
      variant="ghost"
      size="sm"
      class="w-full justify-between"
      :disabled="disabled"
      @click="emit('open', row)"
    >
      <span class="min-w-0 truncate">{{ row.name || fallback }}</span>
      <span class="shrink-0 text-caption text-muted-foreground">{{ row.state === 'aborting' ? t('chat.botRun.aborting') : t(`chat.tasks.statuses.${row.state || 'unknown'}`) }}</span>
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
