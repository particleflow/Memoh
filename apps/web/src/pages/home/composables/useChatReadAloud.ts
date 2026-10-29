import { computed, inject, onBeforeUnmount, onDeactivated, onMounted, shallowRef, watch, type Ref } from 'vue'
import { postBotsByBotIdTtsPlayback } from '@memohai/sdk'
import type { ChatMessage } from '@/store/chat/types'
import { ChatReadAloud, LiveSpeechReplies, speechKey, speechText, type SpeechState } from './chat-read-aloud'
import { chatWorkspaceVisibleKey } from './chat-workspace-visibility'
import { renderedSpeechText } from './speech-rendered-text'

export function useChatReadAloud(options: {
  botId: Ref<string | null | undefined>
  scope: Ref<string>
  enabled: Ref<boolean>
  loading: Ref<boolean>
  draft: Ref<string>
  messages: Ref<ChatMessage[]>
  messageElement?: (id: string) => Element | null
  bodyRoot?: () => Element | null
}) {
  const workspaceVisible = inject(chatWorkspaceVisibleKey, shallowRef(true))
  const enabled = computed(() => options.enabled.value && workspaceVisible.value)
  const state = shallowRef<SpeechState>({ key: '', status: 'idle' })
  const replies = new LiveSpeechReplies()
  let flushTimer: ReturnType<typeof setTimeout> | undefined
  let observer: MutationObserver | undefined
  let observedRoot: Element | null = null
  const rendered = options.messageElement ? (message: ChatMessage) => renderedSpeechText(message, options.messageElement!(message.id)) : undefined
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
    for (const reply of replies.update(options.messages.value, enabled.value && !options.draft.value, options.loading.value, flush, rendered)) controller.enqueue(reply)
  }
  function scheduleFlush() {
    if (!flushTimer && enabled.value && !options.loading.value && !options.draft.value) {
      flushTimer = setTimeout(() => { flushTimer = undefined; collect(true) }, 80)
    }
  }
  function observeBody() {
    const root = options.bodyRoot?.() ?? null
    if (observedRoot === root) return
    observer?.disconnect(); observedRoot = root
    if (!root) return
    observer ??= new MutationObserver(() => { collect(); scheduleFlush() })
    observer.observe(root, { subtree: true, childList: true, characterData: true, attributes: true, attributeFilter: ['class', 'style', 'hidden', 'data-markstream-mode', 'data-markstream-pending'] })
  }
  watch(() => ({ messages: options.messages.value, enabled: enabled.value, loading: options.loading.value }), value => {
    if (value.messages.some(message => message.role === 'assistant' && speechKey(message) === state.value.key && message.messages.some(block => block.type === 'error'))) { stop(); return }
    observeBody(); collect()
    // A short opening phrase without punctuation must not wait for completion.
    // Do not reset on every delta: a continuous token stream cannot starve it.
    if (!flushTimer && value.enabled && !value.loading && !options.draft.value && value.messages.some(message => message.role === 'assistant' && message.streaming)) {
      scheduleFlush()
    }
  }, { deep: true, immediate: true, flush: 'post' })
  function canResume(message: ChatMessage) {
    if (message.role !== 'assistant' || speechKey(message) !== state.value.key) return false
    const playback = status(message)
    return playback === 'blocked' || playback === 'paused'
  }
  function canReplay(message: ChatMessage) { return enabled.value && (canResume(message) || (!message.streaming && !!speechText(message))) }
  function replay(message: ChatMessage) {
    if (!enabled.value) return
    // The existing menu selection is the supported user activation. Resume the
    // same loaded source synchronously, before stop(), DOM work or any request.
    if (canResume(message)) { controller.resume(); return }
    if (message.streaming) return
    const text = rendered ? rendered(message) : speechText(message)
    if (!text) return
    stop()
    controller.replay({ key: speechKey(message), text })
  }
  function status(message: ChatMessage) {
    const active = options.messages.value.find(item => item.role === 'assistant' && state.value.key === speechKey(item))
    return message.role === 'assistant' && message.id === active?.id ? state.value.status : 'idle'
  }
  onDeactivated(stop)
  onMounted(() => { observeBody(); collect() })
  onBeforeUnmount(() => { observer?.disconnect(); stop() })
  return { state, status, stop, replay, canReplay, resume: () => { if (enabled.value) controller.resume() }, enabled }
}
