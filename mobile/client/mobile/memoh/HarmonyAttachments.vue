<template>
  <div v-if="nativeAttachmentsAvailable" class="space-y-1 px-2 text-xs" aria-label="原件附件">
    <button type="button" class="underline" :disabled="disabled || busy" @click="restore">恢复本会话待传附件</button>
    <span v-if="busy" role="status"> 正在处理原件…</span>
    <button v-if="busy" type="button" @click="cancel">取消处理</button>
    <p v-if="error" role="alert">{{ error }}</p>
    <div v-for="file in modelValue" :key="file.handle" class="space-y-1">
      <img v-if="file.type.startsWith('image/')" :src="preview(file)" :alt="file.name" class="max-h-40 max-w-full object-contain">
      <audio v-else-if="file.type.startsWith('audio/')" :src="preview(file)" controls preload="none" class="max-w-full" />
      <video v-else-if="file.type.startsWith('video/')" :src="preview(file)" controls preload="none" class="max-h-48 max-w-full" />
      <span class="min-w-0 truncate">{{ file.name }} · {{ Math.ceil(file.size / 1024) }} KiB · {{ file.resumable ? '支持续传' : '旧服务：不支持续传' }}</span>
      <button type="button" :disabled="disabled || busy" @click="remove(file)">移除</button>
    </div>
  </div>
</template>
<script setup lang="ts">
import { ref, watch, onBeforeUnmount } from 'vue'
import { nativeAttachmentCommand, nativeAttachmentsAvailable, type NativeAttachment } from './harmony-resumable'
import { onAuthSessionCleared } from '@/lib/auth-session'
const props = defineProps<{ botId: string; scope: string; disabled: boolean; modelValue: NativeAttachment[] }>()
const emit = defineEmits<{ 'update:modelValue': [NativeAttachment[]] }>()
const busy = ref(false), error = ref('')
let controller: AbortController | undefined
function cancel() { controller?.abort(); controller = undefined; busy.value = false; error.value = '' }
function reset() { cancel(); emit('update:modelValue', []) }
watch(() => [props.botId, props.scope], reset)
const off = onAuthSessionCleared(reset)
onBeforeUnmount(() => { controller?.abort(); off() })
async function action(kind: string, fields: { handle?: string; photos?: boolean } = {}) {
  if (busy.value || props.disabled || !props.botId) return
  const mine = new AbortController(); controller = mine; busy.value = true; error.value = ''
  try {
    const result = await nativeAttachmentCommand<NativeAttachment[]>(kind, props.botId, props.scope, fields, mine.signal)
    if (mine.signal.aborted) return
    if (kind === 'upload.discard') emit('update:modelValue', props.modelValue.filter(file => file.handle !== fields.handle))
    else if (kind === 'upload.list') emit('update:modelValue', result)
    else emit('update:modelValue', [...props.modelValue, ...result])
  } catch (cause) { if (!mine.signal.aborted) error.value = cause instanceof Error ? cause.message : '原件操作未完成' }
  finally { if (controller === mine) { controller = undefined; busy.value = false } }
}
const preview = (file: NativeAttachment) => 'begonia-client://app/upload/' + file.handle
const select = (photos = false) => action('upload.select', { photos })
const restore = () => action('upload.list')
const remove = (file: NativeAttachment) => action('upload.discard', { handle: file.handle })
defineExpose({ select })
</script>
