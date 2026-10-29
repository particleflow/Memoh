import { computed, inject, onBeforeUnmount, onDeactivated, shallowRef, watch, type Ref } from 'vue'
import { postBotsByBotIdTtsPlayback } from '@memohai/sdk'
import type { ChatMessage } from '@/store/chat/types'
import { ChatReadAloud, LiveSpeechReplies, speechKey, speechText, type SpeechState } from './chat-read-aloud'
import { chatWorkspaceVisibleKey } from './chat-workspace-visibility'

export function useChatReadAloud(options: {
  botId: Ref<string | null | undefined>
  scope: Ref<string>
  enabled: Ref<boolean>
  loading: Ref<boolean>
  draft: Ref<string>
  messages: Ref<ChatMessage[]>
}) {
  const workspaceVisible = inject(chatWorkspaceVisibleKey, shallowRef(true))
  const enabled = computed(() => options.enabled.value && workspaceVisible.value)
  const state = shallowRef<SpeechState>({ key: '', status: 'idle' })
  const replies = new LiveSpeechReplies()
  let flushTimer: ReturnType<typeof setTimeout> | undefined
  const controller = new ChatReadAloud({
    async synthesize(text, signal) {
      const { data, response } = await postBotsByBotIdTtsPlayback({
        path: { bot_id: options.botId.value! }, body: { text }, signal,
        parseAs: 'stream', throwOnError: true,
      })
      const contentType = response.headers.get('Content-Type') ?? ''
      if (!(data instanceof ReadableStream) || !contentType.startsWith('audio/')) {
        if (data instanceof ReadableStream) await data.cancel()
        throw new Error('Invalid playback stream')
      }
      return { stream: data as ReadableStream<Uint8Array>, contentType }
    },
    createPlayer: () => new Audio(),
    createURL: blob => URL.createObjectURL(blob),
    revokeURL: url => URL.revokeObjectURL(url),
    onState: value => {
      state.value = value
      if (value.status === 'error') {
        clearTimeout(flushTimer); flushTimer = undefined
        replies.interrupt(options.messages.value)
      }
    },
  })
  function stop() {
    clearTimeout(flushTimer); flushTimer = undefined
    replies.interrupt(options.messages.value)
    controller.stop()
  }
  watch(options.scope, () => { stop(); replies.reset() }, { flush: 'sync' })
  watch(enabled, value => { if (!value) stop() }, { flush: 'sync' })
  // flush:sync stops the player in the input event, before any pending media task.
  watch(options.draft, stop, { flush: 'sync' })
  function collect(flush = false) {
    for (const reply of replies.update(options.messages.value, enabled.value && !options.draft.value, options.loading.value, flush)) controller.enqueue(reply)
  }
  watch(() => ({ messages: options.messages.value, enabled: enabled.value, loading: options.loading.value }), value => {
    if (value.messages.some(message => message.role === 'assistant' && speechKey(message) === state.value.key && message.messages.some(block => block.type === 'error'))) { stop(); return }
    collect()
    // A short opening phrase without punctuation must not wait for completion.
    // Do not reset on every delta: a continuous token stream cannot starve it.
    if (!flushTimer && value.enabled && !value.loading && !options.draft.value && value.messages.some(message => message.role === 'assistant' && message.streaming)) {
      flushTimer = setTimeout(() => { flushTimer = undefined; collect(true) }, 80)
    }
  }, { deep: true, immediate: true })
  function canReplay(message: ChatMessage) { return enabled.value && !message.streaming && !!speechText(message) }
  function replay(message: ChatMessage) {
    if (!enabled.value || message.streaming) return
    const text = speechText(message)
    if (!text) return
    stop()
    controller.replay({ key: speechKey(message), text })
  }
  function status(message: ChatMessage) {
    const active = options.messages.value.find(item => item.role === 'assistant' && state.value.key === speechKey(item))
    return message.role === 'assistant' && message.id === active?.id ? state.value.status : 'idle'
  }
  onDeactivated(stop)
  onBeforeUnmount(stop)
  return { state, status, stop, replay, canReplay, resume: () => { if (enabled.value) controller.resume() }, enabled }
}
