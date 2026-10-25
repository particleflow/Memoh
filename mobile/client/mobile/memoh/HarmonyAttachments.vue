<template>
  <div v-if="nativeAttachmentsAvailable" class="space-y-1 px-2 text-body" aria-label="原件附件">
    <Button variant="ghost" :disabled="disabled || busy" @click="restore">{{ scope.startsWith('draft:') ? '恢复未发送草稿附件' : '恢复本会话待传附件' }}</Button>
    <p class="text-muted-foreground">每次恢复最多 16 项；发送或移除后可继续恢复。</p>
    <span v-if="busy" role="status"> 正在处理原件…</span>
    <Button v-if="busy" variant="ghost" @click="cancel">暂停处理</Button>
    <p v-if="error" role="alert">{{ error }}</p>
    <div v-for="file in modelValue" :key="file.handle" class="space-y-1">
      <img v-if="showPreviews && file.type.startsWith('image/')" :src="preview(file)" :alt="file.name" class="max-h-40 max-w-full object-contain">
      <audio v-else-if="showPreviews && file.type.startsWith('audio/')" :src="preview(file)" controls preload="none" class="max-w-full" />
      <video v-else-if="showPreviews && file.type.startsWith('video/')" :src="preview(file)" controls preload="none" class="max-h-48 max-w-full" />
      <span class="min-w-0 truncate">{{ file.name }} · {{ Math.ceil(file.size / 1024) }} KiB · {{ file.resumable ? '支持续传' : '旧服务：不支持续传' }}</span>
      <Button variant="ghost" :disabled="disabled || busy" @click="remove(file)">移除</Button>
    </div>
  </div>
</template>
<script setup lang="ts">
import { ref, watch, onBeforeUnmount } from 'vue'
import { Button } from '@felinic/ui'
import { nativeAttachmentCommand, nativeAttachmentsAvailable, closeNativeAttachments, validateNativeAttachments, type NativeAttachment } from './harmony-resumable'
import { onAuthSessionCleared } from '@/lib/auth-session'
const props = defineProps<{ botId: string; scope: string; disabled: boolean; active?: boolean; modelValue: NativeAttachment[] }>()
const emit = defineEmits<{ 'update:modelValue': [NativeAttachment[]] }>()
const busy = ref(false), error = ref('')
const showPreviews = ref(true)
let controller: AbortController | undefined
function cancel() { controller?.abort(); controller = undefined; busy.value = false; error.value = '' }
function suspend() { cancel(); showPreviews.value = false; void closeNativeAttachments().catch(() => {}) }
function reset() { suspend(); emit('update:modelValue', []) }
watch(() => [props.botId, props.scope], reset, { flush: 'sync' })
watch(() => props.active, active => { if (active === false) suspend(); else void reopenPreviews() }, { flush: 'sync' })
const off = onAuthSessionCleared(reset)
onBeforeUnmount(() => { controller?.abort(); void closeNativeAttachments().catch(() => {}); off() })
async function action(kind: string, fields: { handle?: string; photos?: boolean; recoverDrafts?: boolean } = {}, scope = props.scope) {
  if (busy.value || props.disabled || !props.botId) return
  const mine = new AbortController(); controller = mine; busy.value = true; error.value = ''
  try {
    const result = await nativeAttachmentCommand<unknown>(kind, props.botId, scope, fields, mine.signal)
    if (mine.signal.aborted) return
    if (kind === 'upload.discard') emit('update:modelValue', props.modelValue.filter(file => file.handle !== fields.handle))
    else {
      const files = validateNativeAttachments(result, scope, fields.recoverDrafts)
      const merged = new Map(props.modelValue.map(file => [file.handle, file]))
      for (const file of files) merged.set(file.handle, file)
      emit('update:modelValue', [...merged.values()])
      showPreviews.value = true
    }
  } catch (cause) { if (!mine.signal.aborted) error.value = cause instanceof Error ? cause.message : '原件操作未完成' }
  finally { if (controller === mine) { controller = undefined; busy.value = false } }
}
async function reopenPreviews() {
  if (busy.value || !props.modelValue.length || !props.botId) return
  const mine = new AbortController(); controller = mine; busy.value = true; error.value = ''
  try {
    for (const file of props.modelValue) {
      const value = await nativeAttachmentCommand<unknown>('upload.list', props.botId, file.scope, { handle: file.handle }, mine.signal)
      const found = validateNativeAttachments(value, file.scope)
      if (found.length !== 1 || found[0]?.handle !== file.handle || found[0]?.hash !== file.hash || found[0]?.size !== file.size) throw new Error('原件预览需重新恢复')
    }
    if (!mine.signal.aborted) showPreviews.value = true
  } catch (cause) { if (!mine.signal.aborted) error.value = cause instanceof Error ? cause.message : '原件预览未获授权' }
  finally { if (controller === mine) { controller = undefined; busy.value = false } }
}
const preview = (file: NativeAttachment) => 'begonia-client://app/upload/' + file.handle
const select = (photos = false) => action('upload.select', { photos })
const restore = () => action('upload.list', { recoverDrafts: props.scope.startsWith('draft:') })
const remove = (file: NativeAttachment) => action('upload.discard', { handle: file.handle }, file.scope)
defineExpose({ select, busy })
</script>
