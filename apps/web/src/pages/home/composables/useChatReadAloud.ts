import { computed, inject, onBeforeUnmount, onDeactivated, onMounted, shallowRef, watch, type Ref } from 'vue'
import { postBotsByBotIdTtsPlayback } from '@memohai/sdk'
import type { ChatMessage } from '@/store/chat/types'
import { ChatReadAloud, LiveSpeechReplies, speechKey, speechText, type SpeechState } from './chat-read-aloud'
import { chatWorkspaceVisibleKey } from './chat-workspace-visibility'
import { renderedSpeechText } from './speech-rendered-text'
import { beginSpeechTiming, serverFirstAudioMS, type SpeechTiming } from './speech-timing'

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
  const timingOperations = new Map<string, SpeechTiming>()
  let observer: MutationObserver | undefined
  let observedRoot: Element | null = null
  const rendered = options.messageElement ? (message: ChatMessage) => renderedSpeechText(message, options.messageElement!(message.id)) : undefined
  const controller = new ChatReadAloud({
    async synthesize(text, signal, timing) {
      const { data, response } = await postBotsByBotIdTtsPlayback({
        path: { bot_id: options.botId.value! }, body: { text }, signal,
        parseAs: 'stream', throwOnError: true,
        ...(timing ? { headers: { 'X-Request-ID': `speech-timing-${timing.id}` } } : {}),
      })
      timing?.mark('response_headers', { serverFirstAudioMS: serverFirstAudioMS(response.headers.get('Server-Timing')) })
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
        replies.interrupt(options.messages.value)
      }
    },
  })
  function stop() {
    replies.interrupt(options.messages.value)
    controller.stop()
    timingOperations.clear()
  }
  watch(options.scope, () => { stop(); replies.reset() }, { flush: 'sync' })
  watch(enabled, value => { if (!value) stop() }, { flush: 'sync' })
  // flush:sync stops the player in the input event, before any pending media task.
  watch(options.draft, stop, { flush: 'sync' })
  watch(() => ({ messages: options.messages.value, enabled: enabled.value, loading: options.loading.value, draft: options.draft.value }), value => {
    replies.observe(value.messages, value.enabled && !value.draft, value.loading)
  }, { deep: true, immediate: true, flush: 'sync' })
  function collect(flush = false) {
    if (enabled.value && !options.loading.value && !options.draft.value) {
      for (const message of options.messages.value) {
        if (message.role !== 'assistant' || !message.streaming) continue
        const key = speechKey(message)
        let operation = timingOperations.get(key)
        if (!operation) { operation = beginSpeechTiming('automatic'); if (operation) { if (timingOperations.size >= 32) timingOperations.delete(timingOperations.keys().next().value!); timingOperations.set(key, operation) } }
        if (message.messages.some(block => block.type === 'text' && block.content.length)) operation?.mark('text_observed')
      }
    }
    for (const reply of replies.update(options.messages.value, enabled.value && !options.draft.value, options.loading.value, flush, rendered)) { const timing = timingOperations.get(reply.key); controller.enqueue(timing ? { ...reply, timing } : reply) }
    controller.setLive(enabled.value && !options.draft.value && !options.loading.value ? options.messages.value.filter(message => message.role === 'assistant' && message.streaming).map(speechKey) : [])
  }
  function observeBody() {
    const root = options.bodyRoot?.() ?? null
    if (observedRoot === root) return
    observer?.disconnect(); observedRoot = root
    if (!root) return
    observer ??= new MutationObserver(() => collect())
    observer.observe(root, { subtree: true, childList: true, characterData: true, attributes: true, attributeFilter: ['class', 'style', 'hidden', 'data-markstream-mode', 'data-markstream-pending'] })
  }
  watch(() => ({ messages: options.messages.value, enabled: enabled.value, loading: options.loading.value }), value => {
    if (value.messages.some(message => message.role === 'assistant' && speechKey(message) === state.value.key && message.messages.some(block => block.type === 'error'))) { stop(); return }
    observeBody(); collect()
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
    const timing = beginSpeechTiming('manual')
    const text = rendered ? rendered(message) : speechText(message)
    if (!text) return
    timing?.mark('text_observed')
    stop()
    controller.replay({ key: speechKey(message), text, ...(timing ? { timing } : {}) })
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
