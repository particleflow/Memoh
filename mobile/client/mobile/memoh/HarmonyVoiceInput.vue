<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onDeactivated, onMounted, ref } from 'vue'
import { Button, Spinner } from '@felinic/ui'
import { Mic } from 'lucide-vue-next'
import HarmonyVoiceFan from './HarmonyVoiceFan.vue'
import uiTheme from './harmony-theme.json'
import { voiceFanChoice, voiceFanLayout, type VoiceChoice, type VoiceOrigin, type VoiceTarget } from './harmony-voice-geometry'
import { finishRecording, nativeVoice, type Capture, type FinishedCapture, type VoiceSubmission } from './harmony-voice'

const props = defineProps<{ disabled: boolean; editorTarget: () => VoiceTarget | undefined; send: (submission: VoiceSubmission) => Promise<unknown> }>()
const emit = defineEmits<{ transcribe: [string]; error: [unknown] }>()
const capture = ref<Capture>()
const origin = ref<VoiceOrigin>({ x: 0, y: 0, radius: 44, hand: 'right' })
const finger = ref({ x: 0, y: 0 })
const choice = ref<VoiceChoice>('send')
const closing = ref(false)
const converting = ref(false)
const textTarget = ref<VoiceTarget>()
const sentTarget = ref<VoiceTarget>()
const overlayHost = ref<HTMLElement>()
const feedback = ref<InstanceType<typeof HarmonyVoiceFan>>()
let held = false
let pointer = -1
let button: HTMLElement | undefined
let active = true
let pollTimer: ReturnType<typeof setTimeout> | undefined
let exitTimer: ReturnType<typeof setTimeout> | undefined
let targetFrame: number | undefined
let targetObserver: MutationObserver | undefined
const label = computed(() => converting.value ? '正在转字' : capture.value && !closing.value
  ? choice.value === 'text' ? '松开转字' : choice.value === 'cancel' ? '松开取消' : '松开发送' : '按住录音')

function releaseGuard() {
  held = false
  clearTimeout(pollTimer)
  if (button?.hasPointerCapture(pointer)) button.releasePointerCapture(pointer)
}
function report(error: unknown) { if (active) emit('error', error) }
// The SDK proxy returns a Promise, but entering it can still block the Web thread.
// Paint the released gesture and the official local turn before native cleanup.
function afterFeedback<T>(work: () => Promise<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    requestAnimationFrame(() => { setTimeout(() => { void work().then(resolve, reject) }, 0) })
  })
}
function hide() {
  if (targetFrame !== undefined) cancelAnimationFrame(targetFrame)
  targetFrame = undefined
  targetObserver?.disconnect()
  targetObserver = undefined
  if (active) { capture.value = undefined; closing.value = false }
}
function abandon() {
  if (!held) return
  const handle = capture.value?.handle
  releaseGuard()
  hide()
  if (handle) void nativeVoice('voice.cancel', handle).catch(() => {})
}
function visibilityChanged() { if (document.hidden) abandon() }
async function poll(handle: string) {
  try {
    const next = await nativeVoice<Capture>('voice.status', handle)
    if (!held || capture.value?.handle !== handle) return
    capture.value = next
    pollTimer = setTimeout(() => { void poll(handle) }, uiTheme.shared.voiceInput.meterPollMs)
  } catch (error) {
    if (held && capture.value?.handle === handle) { abandon(); report(error) }
  }
}
function finish(cancelled = false) {
  if (!held || !capture.value) return
  const snapshot = { ...capture.value, peaks: [...capture.value.peaks] }
  const action = cancelled || snapshot.state !== 'recording' || !snapshot.peaks.some(peak => peak > 0) ? 'cancel' : choice.value
  // A native permission dialog or an unfinished start cannot inherit a Send selection.
  releaseGuard()
  choice.value = action
  if (action === 'text') textTarget.value = props.editorTarget()
  closing.value = true
  if (action === 'cancel') exitTimer = setTimeout(hide, uiTheme.shared.voiceMotion.exitMs)
  if (action === 'cancel') { void afterFeedback(() => nativeVoice('voice.cancel', snapshot.handle)).catch(report); return }
  if (action === 'send') {
    const recording = afterFeedback(() => finishRecording(snapshot.handle))
    void recording.catch(report)
    const selector = '[data-local-voice="' + snapshot.handle + '"]'
    const resolve = () => {
      const node = document.querySelector<HTMLElement>(selector)
      if (!node?.isConnected) return
      const rect = node.getBoundingClientRect()
      if (!rect.width || !rect.height) return
      const viewport = node.closest('[data-slot="scroll-area-viewport"]')?.getBoundingClientRect()
      // A body-level effect must respect the actual message viewport's clip.
      if (viewport && (rect.top < viewport.top || rect.bottom > viewport.bottom
        || rect.left < viewport.left || rect.right > viewport.right)) return
      return { x: rect.x, y: rect.y, width: rect.width, height: rect.height,
        radius: Number.parseFloat(getComputedStyle(node).borderRadius) || 0 }
    }
    const locate = () => {
      targetFrame = undefined
      if (!active || !closing.value || capture.value?.handle !== snapshot.handle) return
      const bounds = resolve()
      // Attachment preparation and send confirmation precede the actual row.
      // Follow this capture's DOM lifecycle, never a timer or another message.
      if (!bounds) {
        if (document.querySelector(selector)) hide()
        return
      }
      targetObserver?.disconnect()
      targetObserver = undefined
      sentTarget.value = { ...bounds, resolve }
      void nextTick().then(() => feedback.value?.feedbackFinished()).then(hide, hide)
    }
    const scheduleTarget = () => {
      if (targetFrame === undefined) targetFrame = requestAnimationFrame(locate)
    }
    targetObserver = new MutationObserver(scheduleTarget)
    targetObserver.observe(document.body, { childList: true, subtree: true, attributes: true,
      attributeFilter: ['style', 'class', 'data-local-voice'] })
    scheduleTarget()
    const settled = () => {
      if (capture.value?.handle === snapshot.handle && !sentTarget.value) hide()
    }
    void props.send({ capture: snapshot, recording }).then(settled, error => { settled(); report(error) })
    return
  }
  converting.value = true
  const recognition = afterFeedback(() => nativeVoice<FinishedCapture>('voice.transcribe', snapshot.handle))
  const motion = nextTick().then(() => {
    if (!active || !feedback.value) throw new DOMException('转字已取消', 'AbortError')
    return feedback.value.feedbackFinished()
  })
  // Hide only after this actual feedback finishes (or is cancelled), not a
  // duration estimate. A rejected animation remains a failed insertion gate.
  void motion.then(hide, hide)
  void Promise.all([recognition, motion])
    .then(([result]) => { if (active && result.text.trim()) emit('transcribe', result.text) })
    .catch(report).finally(async () => {
      // A cancelled animation must not unlock another capture while the
      // original native recognizer is still releasing its recording.
      await recognition.catch(() => {})
      if (active) converting.value = false
    })
}
function down(event: PointerEvent) {
  if (props.disabled || converting.value || held || closing.value || !event.isPrimary || event.button !== 0) return
  event.preventDefault()
  event.stopPropagation()
  clearTimeout(exitTimer)
  held = true
  pointer = event.pointerId
  button = event.currentTarget as HTMLElement
  overlayHost.value = button.closest<HTMLElement>('.chat-composer-edge') ?? undefined
  overlayHost.value?.style.setProperty('--harmony-recording-composer', String(uiTheme.shared.layers.recordingComposer))
  button.setPointerCapture(pointer)
  const rect = button.getBoundingClientRect()
  const point = { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2, hand: 'right' as const }
  textTarget.value = props.editorTarget()
  origin.value = voiceFanLayout(point, innerWidth, visualViewport?.offsetTop ?? 0, textTarget.value)
  finger.value = point
  sentTarget.value = undefined
  choice.value = 'send'
  closing.value = false
  const initial: Capture = { handle: crypto.randomUUID().replaceAll('-', ''), state: 'starting', durationMs: 0, peaks: [], text: '', speechError: '' }
  capture.value = initial
  void nativeVoice<Capture>('voice.start', initial.handle).then(value => {
    if (held && capture.value?.handle === value.handle) { capture.value = value; void poll(value.handle) }
  }).catch(error => {
    if (held && capture.value?.handle === initial.handle) { releaseGuard(); hide(); report(error) }
  })
}
function move(event: PointerEvent) {
  if (!held || event.pointerId !== pointer) return
  event.preventDefault()
  choice.value = voiceFanChoice(origin.value, event.clientX, event.clientY)
  finger.value = { x: event.clientX, y: event.clientY }
}
function up(event: PointerEvent) {
  if (!held || event.pointerId !== pointer) return
  event.preventDefault()
  event.stopPropagation()
  // Release coordinates, not the last move event, choose the final action.
  choice.value = voiceFanChoice(origin.value, event.clientX, event.clientY)
  finger.value = { x: event.clientX, y: event.clientY }
  finish()
}
onDeactivated(() => { abandon(); hide() })
onMounted(() => { window.addEventListener('blur', abandon); document.addEventListener('visibilitychange', visibilityChanged) })
onBeforeUnmount(() => {
  abandon()
  active = false
  clearTimeout(exitTimer)
  clearTimeout(pollTimer)
  if (targetFrame !== undefined) cancelAnimationFrame(targetFrame)
  targetObserver?.disconnect()
  window.removeEventListener('blur', abandon)
  document.removeEventListener('visibilitychange', visibilityChanged)
})
</script>

<template>
  <Teleport v-if="capture && overlayHost" :to="overlayHost">
    <HarmonyVoiceFan ref="feedback" :origin="origin" :choice="choice" :finger="finger" :elapsed="capture.durationMs" :peaks="capture.peaks.map(value => value * 1024)" :closing="closing" :text-target="textTarget" :sent-target="sentTarget" />
  </Teleport>
  <Button type="button" variant="primary" shape="circle" size="icon-sm" class="harmony-voice-mic max-md:size-11 shrink-0" :disabled="disabled || converting || closing" :aria-label="label" :aria-busy="converting || undefined" :data-recording="!!capture && !closing" @pointerdown="down" @pointermove="move" @pointerup="up" @pointercancel="finish(true)" @lostpointercapture="finish(true)" @contextmenu.prevent @click.stop.prevent>
    <Spinner v-if="converting" aria-hidden="true" class="size-4 max-md:size-5 motion-reduce:animate-none" />
    <Mic v-else class="size-4 max-md:size-5" />
  </Button>
</template>

<style>
.harmony-voice-mic { touch-action: none; user-select: none; -webkit-user-select: none; }
.chat-composer-edge:has(.harmony-voice-overlay) { z-index: 1; }
.chat-composer-edge:has(.harmony-voice-overlay) > :not(.voice-overlay) { position: relative; z-index: var(--harmony-recording-composer); }
.harmony-voice-mic[data-recording="true"] { background: transparent; }
</style>
