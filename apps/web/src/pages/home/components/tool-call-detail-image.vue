<template>
  <div class="space-y-1.5">
    <img
      v-if="src"
      :src="src"
      :alt="prompt"
      class="rounded-md border border-border max-w-xs max-h-64 object-contain"
    >
    <p
      v-if="path"
      class="text-xs text-muted-foreground font-mono truncate"
      :title="path"
    >
      {{ path }}
    </p>
    <EmptyRow v-if="contentHash || (!src && !path)">
      {{ statusText }}
    </EmptyRow>
    <p
      v-if="contentHash"
      class="text-xs text-muted-foreground font-mono break-all"
    >
      {{ t('chat.tools.detail.imageReference') }}: {{ contentHash }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ToolCallBlock } from '@/store/chat-list'
import { asRecord, pickString, structuredToolResult } from '@/store/chat-list.normalize'
import EmptyRow from './tool-detail/empty-row.vue'

const props = defineProps<{ block: ToolCallBlock }>()
const { t } = useI18n()

const prompt = computed(() => {
  const input = props.block.input as Record<string, unknown> | undefined
  return (input?.prompt as string) ?? ''
})

const result = computed(() => structuredToolResult(props.block.result))
const state = computed(() => pickString(result.value, 'state'))
const finished = computed(() => props.block.done && !props.block.running)
const confirmed = computed(() => finished.value && props.block.result != null
  && (!state.value || state.value === 'completed'))

const contentHash = computed(() => {
  if (!confirmed.value) return ''
  const hash = pickString(result.value, 'content_hash')
  return /^[0-9a-f]{64}$/.test(hash) ? hash : ''
})

// The result has no Bot/session authorization scope. Keep previews in the
// existing attachment flow; do not invent an asset URL from the current Bot.
const statusText = computed(() => {
  if (state.value === 'generated' && result.value.storage_state === 'unconfirmed') {
    return t('chat.tools.detail.imageStorageUnconfirmed')
  }
  if (state.value && state.value !== 'completed') return t('chat.tools.detail.imageUnconfirmed')
  if (!finished.value) return t('chat.tools.detail.waitingOutput')
  if (!confirmed.value) return t('chat.tools.detail.imageUnconfirmed')
  if (contentHash.value) {
    return t(result.value.delivered === 'current_conversation'
      ? 'chat.tools.detail.imageReturned'
      : 'chat.tools.detail.imageSaved')
  }
  return t('chat.tools.detail.noImage')
})

const path = computed(() => confirmed.value ? pickString(result.value, 'path') : '')

const src = computed(() => {
  if (!confirmed.value) return ''
  const content = asRecord(props.block.result).content
  if (!Array.isArray(content)) return ''
  const img = content.map(asRecord).find(c => c.type === 'image')
  if (!img) return ''
  const data = pickString(img, 'data')
  const mime = pickString(img, 'mimeType') || 'image/png'
  return data ? `data:${mime};base64,${data}` : ''
})
</script>
