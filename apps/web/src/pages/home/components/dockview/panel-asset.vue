<template>
  <DockPanelFrame editor-surface>
    <PanePlaceholder
      v-if="loading"
      loading
    >
      {{ t('common.loading') }}
    </PanePlaceholder>

    <MonacoEditor
      v-else-if="isText"
      v-model="content"
      :filename="name"
      readonly
      class="h-full"
    />

    <div
      v-else-if="isImage && renderUrl"
      class="flex h-full items-center justify-center overflow-auto bg-muted/30 p-4"
    >
      <img
        :src="renderUrl"
        :alt="name"
        class="max-h-full max-w-full rounded-md object-contain"
      >
    </div>

    <PanePlaceholder v-else>
      <template #icon>
        <File class="size-12 opacity-30" />
      </template>
      {{ t('bots.files.previewNotAvailable') }}
      <template
        v-if="renderUrl"
        #action
      >
        <Button
          variant="outline"
          size="sm"
          :loading="downloading"
          @click="handleDownload"
        >
          <Download class="mr-1.5 size-3" />
          {{ t('bots.files.download') }}
        </Button>
      </template>
    </PanePlaceholder>
  </DockPanelFrame>
</template>

<script setup lang="ts">
import { computed, onScopeDispose, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { File, Download } from 'lucide-vue-next'
import { Button, PanePlaceholder, toast } from '@felinic/ui'
import type { DockviewApi, DockviewPanelApi } from 'dockview-vue'
import MonacoEditor from '@/components/monaco-editor/index.vue'
import DockPanelFrame from './panel-frame.vue'
import { isTextFile, isImageFile } from '@/components/file-manager/utils'
import { sdkApiUrl, sdkAuthQuery } from '@/lib/api-client'
import { resolveApiErrorMessage } from '@/utils/api-error'
import { MediaRecoveryError, saveMediaDownload } from '@/lib/media-transfer'
import { resolveUrl } from '../../composables/useMediaGallery'
import { usePanelVisible } from './use-panel-visible'

// Renders a message attachment (a stored media asset) inside its own dock tab,
// instead of handing the source URL to the OS. The source is re-resolved from
// the content hash on every render so a reload picks up a fresh auth token; an
// attachment that has no hash yet (an optimistic, just-sent file) falls back to
// the direct source URL it was opened with.
const props = defineProps<{
  params: {
    params: { name?: string, botId?: string, contentHash?: string, size?: number, delegationUrl?: string, src?: string }
    api: DockviewPanelApi
    containerApi: DockviewApi
  }
}>()

const { t } = useI18n()

const visible = usePanelVisible(props.params.api)

const name = computed(() => props.params.params.name ?? '')
const isText = computed(() => isTextFile(name.value))
const isImage = computed(() => isImageFile(name.value))

const renderUrl = computed(() => {
  const { botId, contentHash, delegationUrl, src } = props.params.params
  const hash = (contentHash ?? '').trim()
  const owner = (botId ?? '').trim()
  if (delegationUrl !== undefined) {
    return resolveUrl({ type: 'file', content_hash: hash, metadata: { delegation_url: delegationUrl } })
  }
  if (hash && owner) {
    return sdkApiUrl({
      url: '/bots/{bot_id}/media/{content_hash}',
      path: { bot_id: owner, content_hash: hash },
      query: sdkAuthQuery(),
    })
  }
  return (src ?? '').trim()
})

const content = ref('')
const loading = ref(false)
let loadedUrl = ''

async function loadText() {
  const url = renderUrl.value
  if (!isText.value || !url || loadedUrl === url) return
  loading.value = true
  try {
    const res = await fetch(url)
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    content.value = await res.text()
    loadedUrl = url
  } catch (error) {
    toast.error(resolveApiErrorMessage(error, t('bots.files.readFailed')))
  } finally {
    loading.value = false
  }
}

const downloading = ref(false)
let downloadController: AbortController | undefined
function pauseDownload() { downloadController?.abort() }
onScopeDispose(pauseDownload)
watch([visible, renderUrl], pauseDownload)
async function handleDownload() {
  const url = renderUrl.value
  if (!url || downloading.value) return
  const { botId, contentHash, size, delegationUrl } = props.params.params
  if (!contentHash || !botId) {
    const a = document.createElement('a')
    a.href = url
    a.download = name.value || 'file'
    a.click()
    return
  }
  const controller = new AbortController()
  downloadController = controller
  downloading.value = true
  try {
    await saveMediaDownload(botId, contentHash, size, delegationUrl, name.value, controller.signal)
  } catch (error) {
    if (!controller.signal.aborted) toast.error(error instanceof MediaRecoveryError
      ? t(`chat.mediaRecovery.${error.reason}`)
      : resolveApiErrorMessage(error, t('bots.files.readFailed')))
  } finally {
    if (downloadController === controller) downloadController = undefined
    downloading.value = false
  }
}

// Text is fetched lazily the first time the tab is shown (an image renders
// straight from the URL, so it needs no fetch). Content is immutable per asset,
// so it is fetched once and kept.
watch([visible, renderUrl], ([isVisible]) => {
  if (isVisible) void loadText()
}, { immediate: true })
</script>
