<template>
  <!-- Progress ring: fills clockwise by measured progress, spins while the byte
       share is unknown (native transfers expose no byte events). The formatted
       speed follows the ring when a rate window exists. No status copy. -->
  <span class="inline-flex shrink-0 items-center gap-1.5">
    <svg viewBox="0 0 20 20" class="size-4" :class="fraction == null ? 'harmony-transfer-ring-spin' : ''">
      <circle cx="10" cy="10" r="8" fill="none" stroke="currentColor" stroke-width="2.5" opacity="0.25" />
      <circle
        v-if="fraction != null"
        cx="10" cy="10" r="8" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"
        :stroke-dasharray="CIRCUMFERENCE.toFixed(3)"
        :stroke-dashoffset="(CIRCUMFERENCE * (1 - progress)).toFixed(3)"
        transform="rotate(-90 10 10)"
      />
      <circle
        v-else
        cx="10" cy="10" r="8" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"
        :stroke-dasharray="`${(CIRCUMFERENCE * 0.25).toFixed(3)} ${CIRCUMFERENCE.toFixed(3)}`"
        transform="rotate(-90 10 10)"
      />
    </svg>
    <span v-if="speedLabel" class="text-caption tabular-nums">{{ speedLabel }}</span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { formatTransferSpeed } from './harmony-transfer-progress'

const props = defineProps<{ fraction?: number | null; rate?: number }>()

const CIRCUMFERENCE = 2 * Math.PI * 8
const progress = computed(() => {
  const value = Number(props.fraction)
  return Number.isFinite(value) ? Math.min(1, Math.max(0, value)) : 0
})
const speedLabel = computed(() => formatTransferSpeed(Number(props.rate)))
</script>

<style>
@keyframes harmony-transfer-ring-rotate {
  to { transform: rotate(360deg) }
}
.harmony-transfer-ring-spin {
  animation: harmony-transfer-ring-rotate 0.9s linear infinite;
  transform-origin: center;
}
</style>
