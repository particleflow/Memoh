<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Button } from '@felinic/ui'
import { Check, Download, Pause, Play, X } from 'lucide-vue-next'
import WaveSurfer from 'wavesurfer.js'
import uiTheme from './harmony-theme.json'

const props = defineProps<{
  src?: string; file?: Blob; peaks?: number[]; durationMs?: number
  handle?: string; transfer?: string; removable?: boolean; saveOriginal?: () => Promise<void>
  wireLoaded?: number; wireTotal?: number
}>()
const emit = defineEmits<{ remove: [] }>()
const root = ref<HTMLElement>()
const waveform = ref<HTMLElement>()
const playing = ref(false)
const ready = ref(false)
const current = ref(0)
const duration = ref(0)
const error = ref('')
const saving = ref(false)
const saveError = ref('')
let player: WaveSurfer | undefined
let observer: MutationObserver | undefined
let epoch = 0
let playableHandle: string | undefined
const dark = ref(document.documentElement.classList.contains('dark'))
const palette = computed(() => uiTheme.themes[(dark.value ? uiTheme.defaultDark : uiTheme.defaultLight) as keyof typeof uiTheme.themes])
const clock = (seconds: number) => `${Math.floor(seconds / 60)}:${String(Math.floor(seconds % 60)).padStart(2, '0')}`
const transferLabel = computed(() => props.transfer === 'ready' ? '原件已校验'
  : props.transfer === 'uploading' && props.wireLoaded != null
    ? `上传请求 ${props.wireLoaded}${props.wireTotal ? '/' + props.wireTotal : ''} B（含 multipart 封装）`
    : props.transfer === 'verifying' ? '正在完整校验 NAS 原件' : '正在准备录音')

async function load() {
  const generation = ++epoch
  if (!player) return
  // Ready is the same verified recording, not a different playback source.
  if (props.handle && props.handle === playableHandle && ready.value) return
  ready.value = false
  error.value = ''
  try {
    const seconds = (props.durationMs ?? 0) / 1000
    const peaks = props.peaks?.length ? [props.peaks] : undefined
    if (props.file) await player.loadBlob(props.file, peaks, seconds || undefined)
    else if (props.src) await player.load(props.src, peaks, seconds || undefined)
    else if (peaks && seconds) await player.load('', peaks, seconds)
    if (generation === epoch) {
      ready.value = !!(props.file || props.src)
      if (ready.value) playableHandle = props.handle
    }
  } catch (failure) {
    if (generation === epoch && !(failure instanceof DOMException && failure.name === 'AbortError')) {
      error.value = '录音暂时无法播放'
    }
  }
}
function colors() {
  if (!root.value) return
  const style = getComputedStyle(root.value)
  player?.setOptions({ waveColor: style.getPropertyValue('--color-muted-foreground').trim() || style.color,
    progressColor: style.getPropertyValue('--color-primary').trim() || style.color })
}
function seek(event: KeyboardEvent) {
  if (!ready.value || !player || !['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  const next = event.key === 'Home' ? 0 : event.key === 'End' ? duration.value
    : current.value + (event.key === 'ArrowLeft' ? -5 : 5)
  player.setTime(Math.max(0, Math.min(duration.value, next)))
}
async function toggle() {
  try { await player?.playPause() } catch { error.value = '录音暂时无法播放' }
}
async function save() {
  if (!props.saveOriginal || saving.value) return
  saving.value = true
  saveError.value = ''
  try { await props.saveOriginal() } catch { saveError.value = '录音原件未保存，请重试' }
  finally { saving.value = false }
}
onMounted(() => {
  if (!waveform.value) return
  player = WaveSurfer.create({ container: waveform.value, height: 32, barWidth: 2, barGap: 2,
    barRadius: 2, normalize: false, dragToSeek: true, cursorWidth: 1 })
  player.on('play', () => { playing.value = true })
  player.on('pause', () => { playing.value = false })
  player.on('timeupdate', time => { current.value = time })
  player.on('ready', seconds => { duration.value = seconds })
  colors()
  observer = new MutationObserver(() => { dark.value = document.documentElement.classList.contains('dark'); colors() })
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class', 'style'] })
  void load()
})
watch(() => [props.src, props.file, props.durationMs, props.peaks], () => { void load() })
onBeforeUnmount(() => { ++epoch; observer?.disconnect(); player?.destroy() })
</script>

<template>
  <div ref="root" class="flex items-center gap-2 rounded-lg border border-border bg-muted-soft px-3 py-2 w-72 max-w-full" :data-local-voice="handle" :data-transfer="transfer" @click.stop>
    <Button type="button" variant="ghost" shape="circle" size="icon-sm" :disabled="!ready || !!error" :aria-label="error || (playing ? '暂停录音' : '播放录音')" :title="error || undefined" @click="toggle">
      <Pause v-if="playing" class="size-4" /><Play v-else class="size-4" />
    </Button>
    <div ref="waveform" class="min-w-0 flex-1 touch-none select-none" role="slider" tabindex="0" aria-label="录音播放位置" :aria-valuemin="0" :aria-valuemax="duration" :aria-valuenow="current" :aria-disabled="!ready" @keydown="seek" />
    <span class="text-caption tabular-nums text-muted-foreground">{{ clock(playing || current ? current : (duration || (durationMs ?? 0) / 1000)) }}</span>
    <span v-if="transfer" class="inline-flex min-w-4 shrink-0 items-center justify-center text-caption tabular-nums" :aria-label="transferLabel" :title="transferLabel">
      <Check v-if="transfer === 'ready'" class="size-4" :style="{ color: palette.colors.Success.Main }" />
      <span v-else-if="transfer === 'uploading' && wireTotal">{{ Math.min(100, Math.floor((wireLoaded || 0) * 100 / wireTotal)) }}%</span>
      <span v-else class="size-1.5 rounded-full" :style="{ backgroundColor: palette.transferPending }" />
    </span>
    <Button v-if="saveOriginal" type="button" variant="ghost" shape="circle" size="icon-sm" :disabled="saving" :aria-busy="saving || undefined" :aria-label="saveError || '保存录音原件'" :title="saveError || undefined" @click="save"><Download class="size-4" /></Button>
    <Button v-if="removable" type="button" variant="ghost" shape="circle" size="icon-sm" aria-label="移除录音" @click="emit('remove')"><X class="size-3" /></Button>
  </div>
</template>
