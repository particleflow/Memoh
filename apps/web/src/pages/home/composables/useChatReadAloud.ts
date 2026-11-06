import { computed, inject, nextTick, onActivated, onBeforeUnmount, onDeactivated, onMounted, shallowRef, watch, type Ref } from 'vue'
import { getBotsByBotIdTtsPlaybackConfig, postBotsByBotIdTtsPlaybackInput, putBotsByBotIdTtsPlaybackInput, postBotsByBotIdTtsPlayback } from '@memohai/sdk'
import { useLocalStorage } from '@vueuse/core'
import { randomUUID } from '@/utils/uuid'
import type { ChatMessage } from '@/store/chat/types'
import { ChatReadAloud, LiveSpeechReplies, speechKey, speechText, type SpeechState, type SpeechEntry } from './chat-read-aloud'
import { chatWorkspaceVisibleKey } from './chat-workspace-visibility'
import { speechRenderComplete } from './speech-render-completion'
import { renderedSpeechText } from './speech-rendered-text'
import { beginSpeechTiming, serverFirstAudioMS, type SpeechTiming } from './speech-timing'

export function useChatReadAloud(options: {
  botId: Ref<string | null | undefined>
  scope: Ref<string>
  enabled: Ref<boolean>
  modelId?: Ref<string | undefined>
  nativeInput?: boolean
  runIdentity?: (message: ChatMessage) => string | undefined
  loading: Ref<boolean>
  draft: Ref<string>
  messages: Ref<ChatMessage[]>
  messageElement?: (id: string) => Element | null
  bodyRoot?: () => Element | null
}) {
  const workspaceVisible = inject(chatWorkspaceVisibleKey, shallowRef(true))
  const enabled = computed(() => options.enabled.value && workspaceVisible.value)
  const state = shallowRef<SpeechState>({ key: '', status: 'idle' })
  const notice = shallowRef('')
  const generatedRuns = new Map<string, string>()
  const authToken = useLocalStorage('token', '')
  const activePrincipal = shallowRef(authToken.value)
  let replayRequest: AbortController | undefined
  let replayGeneration = 0
  const replies = new LiveSpeechReplies()
  const timingOperations = new Map<string, SpeechTiming>()
  let observer: MutationObserver | undefined
  let observedRoot: Element | null = null
  const rendered = options.messageElement ? (message: ChatMessage) => renderedSpeechText(message, options.messageElement!(message.id)) : undefined
  const controller = new ChatReadAloud({
    ...(options.nativeInput ? { async openInput(entry: SpeechEntry, signal: AbortSignal) {
      const owner = entry.cacheKey ? JSON.parse(entry.cacheKey) as unknown[] : undefined
      const principal = localStorage.getItem('token') ?? authToken.value
      if (owner && (owner[0] !== principal || owner[1] !== options.botId.value || owner[2] !== options.scope.value)) throw new Error('Playback owner changed')
      const bot = options.botId.value!
      const { data: capability, response: capabilityResponse } = await getBotsByBotIdTtsPlaybackConfig({ path: { bot_id: bot }, signal, throwOnError: false })
      // A legacy backend's missing read-only capability route has produced no
      // audio. Preserve its original fresh-reply path exactly once. Auth and
      // network failures never become a synthesis fallback; replay is separate.
      if (capabilityResponse?.status === 404) return
      if (!capabilityResponse?.ok || !capability?.identity) throw new Error('Playback capability is unavailable')
      const mime = capability?.input_content_type
      if (!mime || !/^audio\/(?:mpeg|mp3)(?:;|$)/u.test(mime) || typeof MediaSource === 'undefined' || !MediaSource.isTypeSupported(mime)) return
      if (signal.aborted) throw signal.reason
      if ((localStorage.getItem('token') ?? authToken.value) !== principal || bot !== options.botId.value || (owner && owner[2] !== options.scope.value)) throw new Error('Playback owner changed')
      const id = randomUUID()
      const { data, response } = await postBotsByBotIdTtsPlaybackInput({ path: { bot_id: bot }, body: { id, sequence: 0, offset: 0, text: entry.text, final: false }, signal, parseAs: 'stream', throwOnError: true })
      const contentType = response.headers.get('Content-Type') ?? ''
      if (!(data instanceof ReadableStream) || contentType !== mime) {
        if (data instanceof ReadableStream) await data.cancel()
        throw new Error('Playback input format changed')
      }
      activePrincipal.value = principal
      return {
        audio: { stream: data as ReadableStream<Uint8Array>, contentType, identity: response.headers.get('X-Speech-Playback-Identity') ?? '' },
        async append(frame: import('./chat-read-aloud').SpeechInputFrame) {
          if (signal.aborted) throw signal.reason
          const current = localStorage.getItem('token') ?? authToken.value
          if (current !== principal || (owner && (owner[1] !== options.botId.value || owner[2] !== options.scope.value))) throw new Error('Playback owner changed')
          await putBotsByBotIdTtsPlaybackInput({ path: { bot_id: bot }, body: { id, ...frame }, signal, throwOnError: true })
        },
      }
    } } : {}),
    async synthesize(text, signal, timing, entry) {
      const principal = localStorage.getItem('token') ?? authToken.value
      if (entry?.cacheKey) {
        const owner = JSON.parse(entry.cacheKey) as unknown[]
        if (owner[0] !== principal || owner[1] !== options.botId.value || owner[2] !== options.scope.value) throw new Error('Playback owner changed')
      }
      activePrincipal.value = principal
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
      return { stream: data as ReadableStream<Uint8Array>, contentType, identity: response.headers.get('X-Speech-Playback-Identity') ?? '' }
    },
    createPlayer: () => new Audio(),
    createURL: blob => URL.createObjectURL(blob),
    revokeURL: url => URL.revokeObjectURL(url),
    onState: value => {
      state.value = value
      notice.value = value.status === 'error' ? 'chat.readAloud.failed' : ''
      if (value.status === 'error') {
        replies.interrupt(options.messages.value)
      }
    },
  })
  function stop() {
    ++replayGeneration
    replayRequest?.abort(); replayRequest = undefined
    replies.interrupt(options.messages.value)
    controller.stop()
    timingOperations.clear()
  }
  watch(options.scope, () => { stop(); replies.reset() }, { flush: 'sync' })
  if (options.modelId) watch(options.modelId, stop, { flush: 'sync' })
  watch(authToken, () => { stop(); controller.clearCache(); generatedRuns.clear() }, { flush: 'sync' })
  watch(enabled, value => { if (!value) stop() }, { flush: 'sync' })
  // flush:sync stops the player in the input event, before any pending media task.
  watch(options.draft, stop, { flush: 'sync' })
  watch(() => ({ messages: options.messages.value, enabled: enabled.value, loading: options.loading.value, draft: options.draft.value }), value => {
    for (const message of value.messages) if (message.role === 'assistant' && (message.runtimeRunId || options.runIdentity?.(message))) cacheKey(message, true)
    replies.observe(value.messages, value.enabled && !value.draft, value.loading)
  }, { deep: true, immediate: true, flush: 'sync' })
  function collect(flush = false) {
    if (enabled.value && !options.loading.value && !options.draft.value) {
      for (const message of options.messages.value) {
        if (message.role !== 'assistant' || !message.streaming) continue
        const key = message.id
        let operation = timingOperations.get(key)
        if (!operation) { operation = beginSpeechTiming('automatic'); if (operation) { if (timingOperations.size >= 32) timingOperations.delete(timingOperations.keys().next().value!); timingOperations.set(key, operation) } }
        if (message.messages.some(block => block.type === 'text' && block.content.length)) operation?.mark('text_observed')
      }
    }
    for (const reply of replies.update(options.messages.value, enabled.value && !options.draft.value, options.loading.value, flush, rendered)) {
      const message = options.messages.value.find(message => message.role === 'assistant' && (reply.messageId ? message.id === reply.messageId : speechKey(message) === reply.key))
      if (!message) continue
      const timing = timingOperations.get(reply.messageId ?? reply.key)
      controller.enqueue({ ...reply, cacheKey: cacheKey(message, true), ...(timing ? { timing } : {}) })
    }
    controller.setLive(enabled.value && !options.draft.value && !options.loading.value ? options.messages.value.filter(message => message.role === 'assistant' && (message.streaming || (replies.isObserved(message) && !renderComplete(message)))).map(message => message.id) : [])
  }
  function renderComplete(message: ChatMessage) {
    if (!options.nativeInput || !options.messageElement || message.role !== 'assistant') return true
    return speechRenderComplete(message, options.messageElement(message.id))
  }
  function observeBody() {
    const root = options.bodyRoot?.() ?? null
    if (observedRoot === root) return
    observer?.disconnect(); observedRoot = root
    if (!root) return
    observer ??= new MutationObserver(() => collect())
    observer.observe(root, { subtree: true, childList: true, characterData: true, attributes: true, attributeFilter: ['class', 'style', 'hidden', 'data-markstream-mode', 'data-markstream-pending', 'data-speech-final'] })
  }
  watch(() => ({ messages: options.messages.value, enabled: enabled.value, loading: options.loading.value, scope: options.scope.value }), value => {
    if (value.messages.some(message => message.role === 'assistant' && (state.value.messageId ? message.id === state.value.messageId : speechKey(message) === state.value.key) && message.messages.some(block => block.type === 'error'))) { stop(); return }
    observeBody(); collect()
  }, { deep: true, immediate: true, flush: 'post' })
  function canResume(message: ChatMessage) {
    if ((localStorage.getItem('token') ?? authToken.value) !== activePrincipal.value) return false
    if (message.role !== 'assistant' || speechKey(message) !== state.value.key) return false
    const playback = status(message)
    return playback === 'blocked' || playback === 'paused'
  }
  function cacheKey(message: ChatMessage, generated = false) {
    // Read at use time as well as watching the shared auth ref: native hosts
    // can replace localStorage directly without a same-window storage event.
    const principal = localStorage.getItem('token') ?? authToken.value
    const slot = [principal, options.botId.value, options.scope.value, speechKey(message), message.id]
    const run = message.role === 'assistant' ? message.runtimeRunId || options.runIdentity?.(message) : undefined
    // Transcript reconciliation preserves the render id but omits runtimeRunId.
    // Bind the locally observed run to that exact row; sibling rows and known
    // later runs cannot inherit its audio. Text and server config also match.
    if (generated && run) generatedRuns.set(JSON.stringify(slot), run)
    return JSON.stringify([principal, options.botId.value, options.scope.value, options.modelId?.value, speechKey(message), message.id, run || generatedRuns.get(JSON.stringify(slot)) || ''])
  }
  function entry(message: ChatMessage) { return { key: speechKey(message), messageId: message.id, cacheKey: cacheKey(message), text: rendered ? rendered(message) : speechText(message) } }
  function menuAvailable(message: ChatMessage) { return enabled.value && message.role === 'assistant' && (canResume(message) || !!speechText(message)) }
  function canReplay(message: ChatMessage) { return enabled.value && (canResume(message) || (!message.streaming && !!speechText(message))) }
  function replayLabel(message: ChatMessage) { return canResume(message) ? 'chat.readAloud.continue' : 'chat.readAloud.replay' }
  async function replay(message: ChatMessage) {
    if (!enabled.value) return
    // The existing menu selection is the supported user activation. Resume the
    // same loaded source synchronously, before stop(), DOM work or any request.
    if (canResume(message)) { controller.resume(); return }
    if (message.streaming) return
    const timing = beginSpeechTiming('manual')
    const cachedEntry = entry(message)
    if (!cachedEntry.text) return
    timing?.mark('text_observed')
    // Cached audio is never a precondition for reading aloud. Ready bytes replay
    // only after the read-only identity check; every other case synthesizes
    // through the same live path as an automatic reply.
    if (controller.replayAvailability(cachedEntry) !== 'ready') {
      stop()
      controller.enqueue({ ...cachedEntry, ...(timing ? { timing } : {}) })
      return
    }
    stop()
    const token = replayGeneration, scope = options.scope.value
    replayRequest = new AbortController()
    let replayed = false
    try {
      // Read-only current permission/config validation, never a TTS request.
      const { data } = await getBotsByBotIdTtsPlaybackConfig({ path: { bot_id: options.botId.value! }, signal: replayRequest.signal, throwOnError: true })
      if (token !== replayGeneration || scope !== options.scope.value || cachedEntry.cacheKey !== cacheKey(message) || cachedEntry.text !== entry(message).text || !enabled.value || options.draft.value) return
      activePrincipal.value = localStorage.getItem('token') ?? authToken.value
      replayed = !!data?.identity && controller.replay({ ...cachedEntry, ...(timing ? { timing } : {}) }, data.identity)
    } catch {
      if (token !== replayGeneration) return
    } finally { if (token === replayGeneration) replayRequest = undefined }
    // Voice settings or text changed, or validation failed: re-synthesize now.
    if (token === replayGeneration && scope === options.scope.value && !replayed && enabled.value && !options.draft.value) controller.enqueue({ ...cachedEntry, ...(timing ? { timing } : {}) })
  }
  function status(message: ChatMessage) {
    const active = options.messages.value.find(item => item.role === 'assistant' && (state.value.messageId ? state.value.messageId === item.id : state.value.key === speechKey(item)))
    return message.role === 'assistant' && message.id === active?.id ? state.value.status : 'idle'
  }
  onDeactivated(stop)
  onMounted(() => { observeBody(); collect() })
  onActivated(() => { void nextTick(() => { observeBody() }) })
  onBeforeUnmount(() => { observer?.disconnect(); stop(); controller.clearCache() })
  return { state, notice, status, stop, replay, canReplay, menuAvailable, replayLabel, resume: () => { if (enabled.value) controller.resume() }, enabled }
}
