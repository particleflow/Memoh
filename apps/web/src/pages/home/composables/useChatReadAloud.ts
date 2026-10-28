import { computed, onBeforeUnmount, onDeactivated, shallowRef, watch, type Ref } from 'vue'
import { postBotsByBotIdTtsPlayback } from '@memohai/sdk'
import type { ChatMessage } from '@/store/chat/types'
import { ChatReadAloud, LiveSpeechReplies, speechKey, speechText, type SpeechState } from './chat-read-aloud'

export function useChatReadAloud(options: {
  botId: Ref<string | null | undefined>
  scope: Ref<string>
  enabled: Ref<boolean>
  loading: Ref<boolean>
  draft: Ref<string>
  messages: Ref<ChatMessage[]>
}) {
  const state = shallowRef<SpeechState>({ key: '', status: 'idle' })
  const replies = new LiveSpeechReplies()
  const controller = new ChatReadAloud({
    async synthesize(text, signal) {
      const { data } = await postBotsByBotIdTtsPlayback({
        path: { bot_id: options.botId.value! }, body: { text }, signal,
        parseAs: 'blob', throwOnError: true,
      })
      if (!(data instanceof Blob) || !data.size || !data.type.startsWith('audio/')) throw new Error('Invalid playback response')
      return data
    },
    createPlayer: () => new Audio(),
    createURL: blob => URL.createObjectURL(blob),
    revokeURL: url => URL.revokeObjectURL(url),
    onState: value => { state.value = value },
  })
  function stop() {
    replies.interrupt(options.messages.value)
    controller.stop()
  }
  watch(options.scope, () => { stop(); replies.reset() }, { flush: 'sync' })
  watch(options.enabled, enabled => { if (!enabled) stop() }, { flush: 'sync' })
  // flush:sync stops the player in the input event, before any pending media task.
  watch(options.draft, stop, { flush: 'sync' })
  watch(() => ({ messages: options.messages.value, enabled: options.enabled.value, loading: options.loading.value }), value => {
    for (const reply of replies.update(value.messages, value.enabled && !options.draft.value, value.loading)) controller.enqueue(reply)
  }, { deep: true, immediate: true })
  function canReplay(message: ChatMessage) { return options.enabled.value && !message.streaming && !!speechText(message) }
  function replay(message: ChatMessage) {
    if (!options.enabled.value || message.streaming) return
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
  return { state, status, stop, replay, canReplay, resume: () => controller.resume(), enabled: computed(() => options.enabled.value) }
}
