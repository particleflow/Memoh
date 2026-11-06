import type { ChatMessage } from '@/store/chat/types'
import { visibleSpeechText } from './speech-visible-text'
import { prepareSpeechSource, type SpeechAudioStream } from './speech-audio-stream'
import { beginSpeechTiming, type SpeechTiming } from './speech-timing'
import { completedSpeechPrefix } from './speech-sentence-boundary'
import { SpeechReplayCache } from './speech-replay-cache'

export type SpeechStatus = 'idle' | 'loading' | 'playing' | 'paused' | 'blocked' | 'error'
export interface SpeechState { key: string, messageId?: string, status: SpeechStatus, error?: unknown }
export interface SpeechEntry { key: string, messageId?: string, text: string, timing?: SpeechTiming, cacheKey?: string }
interface SpeechJob extends SpeechEntry { sourceText?: string, audio?: Blob | SpeechAudioStream }
export interface SpeechPlayer {
  src: string
  readonly currentTime?: number
  readonly buffered?: TimeRanges
  play(): Promise<void>
  pause(): void
  load(): void
  addEventListener(type: string, listener: EventListener): void
  removeEventListener(type: string, listener: EventListener): void
}
export interface SpeechInputFrame { sequence: number, offset: number, text: string, final: boolean }
export interface SpeechInputSession {
  audio: SpeechAudioStream
  append(frame: SpeechInputFrame): Promise<void>
}
export interface SpeechDependencies {
  openInput?(entry: SpeechEntry, signal: AbortSignal): Promise<SpeechInputSession | undefined>
  synthesize(text: string, signal: AbortSignal, timing?: SpeechTiming, entry?: SpeechEntry): Promise<Blob | SpeechAudioStream>
  createPlayer(): SpeechPlayer
  createURL(blob: Blob | MediaSource): string
  revokeURL(url: string): void
  onState(state: SpeechState): void
  onCacheChange?(): void
}

// Match the server's rune limit, including emoji, without splitting a surrogate pair.
export function speechChunks(text: string): string[] {
  const points = Array.from(text.trim())
  const chunks: string[] = []
  while (points.length) {
    let end = Math.min(500, points.length)
    if (end < points.length) {
      for (let i = end - 1; i >= end / 2; i--) {
        if (/[。！？.!?\n\s]/u.test(points[i]!)) { end = i + 1; break }
      }
    }
    const chunk = points.splice(0, end).join('').trim()
    if (chunk) chunks.push(chunk)
  }
  return chunks
}

export function speechKey(message: ChatMessage): string {
  return message.turnId || message.serverId || message.id
}

export function speechText(message: ChatMessage): string {
  if (message.role !== 'assistant') return ''
  // Explicit audio/voice output remains an attachment; do not read its accompanying
  // caption a second time. Never synthesize tool output, errors or reasoning.
  if (message.messages.some(block => block.type === 'attachments' && block.attachments.some(att =>
    att.type === 'audio' || att.type === 'voice' || att.mime?.startsWith('audio/'),
  ))) return ''
  return message.messages.flatMap(block => block.type === 'text' ? [visibleSpeechText(block.content, !!message.streaming)] : []).join('\n\n').trim()
}

/** Only replies observed live are eligible. History loads and returning to a pane
 * establish a baseline. Interruption consumes the live reply as well as the queue. */
export class LiveSpeechReplies {
  private live = new Map<string, string>()
  private spoken = new Map<string, string>()
  private consumed = new Set<string>()
  private interruptedRuns = new Set<string>()
  private interruptedInvocations = new Set<string>()
  reset() { this.live.clear(); this.spoken.clear(); this.consumed.clear(); this.interruptedRuns.clear(); this.interruptedInvocations.clear() }
  isObserved(message: ChatMessage) { return this.live.has(message.id) }
  // Capture eligibility in the reactive update, before Vue batches a fast turn
  // into its final render. Reading visible text still waits for the DOM watcher.
  observe(messages: ChatMessage[], enabled: boolean, loading: boolean) {
    if (!enabled || loading) return
    for (const message of messages) {
      if (message.role !== 'assistant' || !message.streaming) continue
      const key = message.id, revision = message.runtimeRunId ? `${key}:${message.runtimeRunId}` : key
      if (!this.consumed.has(revision) && !this.interruptedRuns.has(message.runtimeRunId ?? '') && !this.interruptedInvocations.has(message.invocationId ?? '')) this.live.set(key, revision)
    }
  }
  interrupt(messages: ChatMessage[]) {
    for (const message of messages) {
      const key = message.id
      this.consumed.add(message.role === 'assistant' && message.runtimeRunId ? `${key}:${message.runtimeRunId}` : key)
      if (message.role === 'assistant' && message.streaming) {
        if (message.runtimeRunId) this.interruptedRuns.add(message.runtimeRunId)
        if (message.invocationId) this.interruptedInvocations.add(message.invocationId)
      }
    }
    this.live.clear()
    this.spoken.clear()
  }
  update(messages: ChatMessage[], enabled: boolean, loading: boolean, flush = false, rendered?: (message: ChatMessage) => string): SpeechEntry[] {
    if (!enabled || loading) { this.interrupt(messages); return [] }
    const result: SpeechEntry[] = []
    for (const message of messages) {
      if (message.role !== 'assistant') continue
      const key = message.id
      const revision = message.runtimeRunId ? `${key}:${message.runtimeRunId}` : key
      if (this.consumed.has(revision)
        || (message.runtimeRunId && this.interruptedRuns.has(message.runtimeRunId))
        || (message.invocationId && this.interruptedInvocations.has(message.invocationId))) continue
      const observed = this.live.get(key)
      // Baseline history must not be parsed/rendered again on every live delta.
      if (!message.streaming && (!observed || (message.runtimeRunId && observed !== revision))) {
        this.live.delete(key)
        this.consumed.add(revision)
        if (observed) { this.consumed.add(observed); this.spoken.delete(observed) }
        continue
      }
      if (message.streaming) this.live.set(key, revision)
      const text = rendered ? rendered(message) : speechText(message)
      if (message.messages.some(block => block.type === 'error')) { this.consumed.add(revision); continue }
      if (message.streaming || (observed && (!message.runtimeRunId || observed === revision))) {
        // Settled history omits runtimeRunId; retain the observed run for both
        // cursor reads and commits. An explicit new run keeps its own revision.
        const cursorRevision = observed && !message.runtimeRunId ? observed : revision
        const prior = this.spoken.get(cursorRevision) ?? ''
        // A replacement is not an append. Never replay previously spoken text.
        if (text.startsWith(prior)) {
          const tail = text.slice(prior.length)
          let end = tail.length
          if (message.streaming && !flush) {
            end = completedSpeechPrefix(tail)
            // The API's 500-rune limit is the only forced split; it is not a
            // latency timer or a first-phrase character threshold.
            if (!end && Array.from(tail).length >= 500) end = speechChunks(tail)[0]!.length
          }
          const delta = tail.slice(0, end)
          if (delta.trim()) { result.push({ key: speechKey(message), messageId: message.id, text: delta }); this.spoken.set(cursorRevision, prior + delta) }
        }
      }
      // A final message can precede typewriter, math and diagram DOM updates.
      // Retain only already-observed live replies so those later visible tails
      // can be collected. Stop/scope/permission changes still consume the run.
      if (message.streaming || rendered) continue
      this.live.delete(key)
      this.consumed.add(revision)
      if (observed) this.consumed.add(observed)
      if (!observed || (message.runtimeRunId && observed !== revision)) continue
      this.spoken.delete(revision)
      if (observed) this.spoken.delete(observed)
    }
    return result
  }
}

/** One player, one in-flight synthesis request, one generation. No retry of a
 * failed/uncertain synthesis. Late promises and media events cannot restart it. */
export class ChatReadAloud {
  private generation = 0
  private queue: SpeechJob[] = []
  private cache: SpeechReplayCache
  private abort?: AbortController
  private player?: SpeechPlayer
  private url = ''
  private detach?: () => void
  private disposeStream?: () => void
  private timing?: SpeechTiming
  private liveKeys = new Set<string>()
  private wake?: () => void
  private currentMessageId?: string
  private state: SpeechState = { key: '', status: 'idle' }
  private deps: SpeechDependencies
  constructor(deps: SpeechDependencies) { this.deps = deps; this.cache = new SpeechReplayCache(deps.onCacheChange) }

  private publish(key: string, status: SpeechStatus, error?: unknown) {
    this.state = { key, status, error, ...(this.currentMessageId ? { messageId: this.currentMessageId } : {}) }
    this.deps.onState(this.state)
  }
  private release() {
    this.disposeStream?.(); this.disposeStream = undefined
    this.detach?.(); this.detach = undefined
    if (this.player) { this.player.pause(); this.player.src = ''; this.player.load() }
    if (this.url) { this.deps.revokeURL(this.url); this.url = '' }
  }
  stop() {
    this.timing?.mark('interrupted'); this.timing = undefined
    ++this.generation
    this.queue = []
    this.abort?.abort(); this.abort = undefined
    this.liveKeys.clear(); this.wake?.(); this.wake = undefined
    this.release()
    this.currentMessageId = undefined
    this.publish('', 'idle')
  }
  enqueue(entry: SpeechEntry) {
    const operation = entry.timing ?? beginSpeechTiming('direct')
    let cursor = 0
    const chunks = speechChunks(entry.text)
    this.queue.push(...chunks.map((text, index) => {
      const end = index === chunks.length - 1 ? entry.text.length : entry.text.indexOf(text, cursor) + text.length
      const sourceText = entry.text.slice(cursor, end); cursor = end
      const timing = operation?.fork(); timing?.mark('text_chunk_ready')
      return { key: entry.key, messageId: entry.messageId, cacheKey: entry.cacheKey, text, sourceText, timing }
    }))
    this.wake?.()
    if (this.state.status === 'idle' || this.state.status === 'error') void this.next()
  }
  setLive(keys: Iterable<string>) { this.liveKeys = new Set(keys); this.wake?.() }
  clearCache() { this.cache.clear() }
  replayAvailability(entry: SpeechEntry) { return this.cache.status(entry.cacheKey ?? entry.key, entry.text) }
  replay(entry: SpeechEntry, identity: string): boolean {
    const parts = this.cache.read(entry.cacheKey ?? entry.key, entry.text, identity)
    if (!parts) return false
    this.stop()
    // MSE can replay the same encoded MP3 bytes on its one native clock. Other
    // formats retain separate, already-generated native files; no re-encoding.
    if (/^audio\/(?:mpeg|mp3)(?:;|$)/u.test(parts[0]!.type) && typeof MediaSource !== 'undefined' && MediaSource.isTypeSupported(parts[0]!.type)) {
      const buffers = parts.map(part => part.stream())
      let reader = buffers.shift()!.getReader()
      let canceled = false
      const stream = new ReadableStream<Uint8Array>({ async pull(output) {
        for (;;) {
          const next = await reader.read()
          if (canceled) return
          if (!next.done) { output.enqueue(next.value); return }
          reader.releaseLock(); const nextStream = buffers.shift()
          if (!nextStream) { output.close(); return }
          reader = nextStream.getReader()
        }
      }, cancel(reason) { canceled = true; buffers.length = 0; void reader.cancel(reason).catch(() => {}); reader.releaseLock() } }, { highWaterMark: 0 })
      this.queue.push({ ...entry, audio: { stream, contentType: parts[0]!.type, sequential: true } })
    } else this.queue.push(...parts.map(audio => ({ ...entry, audio })))
    void this.next(); return true
  }
  resume() {
    // Called directly by the user's button gesture. Reuse the already loaded
    // audio; do not await a network request before calling play().
    if (this.player && (this.state.status === 'blocked' || this.state.status === 'paused')) {
      this.tryPlay(this.generation)
    }
  }
  private fail(token: number, error: unknown) {
    if (token !== this.generation) return
    this.timing?.mark('error')
    ++this.generation
    this.queue = []
    this.abort?.abort(); this.abort = undefined
    this.wake?.(); this.wake = undefined
    this.release()
    this.publish(this.state.key, 'error', error)
  }
  private tryPlay(token: number) {
    try {
      this.timing?.mark('native_play_call')
      void this.player!.play().catch(error => {
        if (token !== this.generation) return
        if (error && typeof error === 'object' && error.name === 'NotAllowedError') {
          this.timing?.mark('blocked')
          this.publish(this.state.key, 'blocked')
        } else this.fail(token, error)
      })
    } catch (error) { this.fail(token, error) }
  }
  /** One native decoder/clock for consecutive MP3 responses of the same reply.
   * Fetch while prior audio plays; keep at most one complete response ahead.
   * Other MIME types retain the established native compatibility path. */
  private waitInput(signal: AbortSignal, ready: () => boolean) {
    return new Promise<void>((resolve, reject) => {
      const check = () => {
        if (!signal.aborted && !ready()) return
        signal.removeEventListener('abort', check); this.wake = undefined
        if (signal.aborted) reject(signal.reason); else resolve()
      }
      this.wake = check; signal.addEventListener('abort', check, { once: true }); check()
    })
  }
  private async feedInput(session: SpeechInputSession, entry: SpeechJob, signal: AbortSignal) {
    let sequence = 1, offset = Array.from(entry.sourceText ?? entry.text).length
    for (;;) {
      await this.waitInput(signal, () => this.queue.length > 0 || !this.liveKeys.has(entry.messageId ?? entry.key))
      const next = this.queue[0]
      if (!next || next.key !== entry.key || next.messageId !== entry.messageId || next.cacheKey !== entry.cacheKey) {
        await session.append({ sequence, offset, text: '', final: true }); return
      }
      this.queue.shift()
      const text = next.sourceText ?? next.text
      await session.append({ sequence, offset, text, final: false })
      entry.sourceText = (entry.sourceText ?? entry.text) + text
      offset += Array.from(text).length; sequence++
    }
  }
  private async request(entry: SpeechJob, signal: AbortSignal, allowInput = false) {
    const token = this.generation
    const session = allowInput && this.deps.openInput ? await this.deps.openInput({ ...entry, text: entry.sourceText ?? entry.text }, signal) : undefined
    if (signal.aborted) {
      if (session) void session.audio.stream.cancel().catch(() => {})
      throw signal.reason
    }
    if (session) {
      void this.feedInput(session, entry, signal).catch(error => this.fail(token, error))
      return this.cache.capture(entry.cacheKey ?? entry.key, () => entry.sourceText ?? entry.text, { ...session.audio, incrementalInput: true }, signal)
    }
    const audio = await this.deps.synthesize(entry.text, signal, entry.timing, entry)
    if (signal.aborted) { if (!(audio instanceof Blob)) void audio.stream.cancel().catch(() => {}); return audio }
    return audio instanceof Blob ? audio : this.cache.capture(entry.cacheKey ?? entry.key, entry.sourceText ?? entry.text, audio, signal)
  }
  private continuous(first: SpeechAudioStream, entry: SpeechJob, signal: AbortSignal): SpeechAudioStream {
    let reader = first.stream.getReader(), timing = entry.timing, previousStart = 0, closed = false
    const mime = (type: string) => type.trim().replace(/^audio\/mp3(?=;|$)/u, 'audio/mpeg')
    const cancel = () => { void reader.cancel(signal.reason).catch(() => {}); this.wake?.() }
    signal.addEventListener('abort', cancel, { once: true })
    const finish = () => { closed = true; signal.removeEventListener('abort', cancel); reader.releaseLock() }
    const wait = (ready: () => boolean) => new Promise<void>((resolve, reject) => {
      const check = () => {
        if (!signal.aborted && !ready()) return
        this.player?.removeEventListener('timeupdate', check)
        signal.removeEventListener('abort', check)
        this.wake = undefined
        if (signal.aborted) reject(signal.reason); else resolve()
      }
      this.wake = check
      this.player?.addEventListener('timeupdate', check)
      signal.addEventListener('abort', check, { once: true }); check()
    })
    const stream = new ReadableStream<Uint8Array>({
      pull: async output => {
        try {
          for (;;) {
            if (signal.aborted) throw signal.reason
            const chunk = await reader.read()
            if (signal.aborted) throw signal.reason
            if (!chunk.done) { output.enqueue(chunk.value); return }
            timing?.mark('body_eof')
            await wait(() => this.queue.length > 0 || !this.liveKeys.has(entry.messageId ?? entry.key))
            if (this.queue[0]?.key !== entry.key || this.queue[0]?.messageId !== entry.messageId) { finish(); output.close(); return }
            await wait(() => (this.player?.currentTime ?? previousStart) >= previousStart)
            const ranges = this.player?.buffered
            previousStart = ranges?.length ? ranges.end(ranges.length - 1) : 0
            const next = this.queue.shift()!
            timing = next.timing; timing?.mark('request_start')
            const audio = await this.request(next, signal)
            if (signal.aborted) { if (!(audio instanceof Blob)) void audio.stream.cancel().catch(() => {}); throw signal.reason }
            if (audio instanceof Blob || mime(audio.contentType) !== mime(first.contentType) || (first.identity && audio.identity && first.identity !== audio.identity)) {
              if (!(audio instanceof Blob)) void audio.stream.cancel().catch(() => {})
              throw new Error('Playback configuration changed within a reply')
            }
            reader.releaseLock(); reader = audio.stream.getReader()
          }
        } catch (error) {
          void reader.cancel(error).catch(() => {})
          finish(); output.error(error)
        }
      },
      cancel: async reason => { if (closed) return; await reader.cancel(reason).catch(() => {}); finish() },
    }, { highWaterMark: 0 })
    return { contentType: first.contentType, stream, sequential: true }
  }
  private async next() {
    const entry = this.queue.shift()
    if (!entry) { this.timing = undefined; this.currentMessageId = undefined; this.publish('', 'idle'); return }
    const token = ++this.generation
    this.timing = entry.timing
    this.currentMessageId = entry.messageId
    this.abort = new AbortController()
    this.publish(entry.key, 'loading')
    try {
      const signal = this.abort.signal
      if (!entry.audio) entry.timing?.mark('request_start')
      const audio = entry.audio ?? await this.request(entry, signal, true)
      if (token !== this.generation) {
        if (!(audio instanceof Blob)) void audio.stream.cancel().catch(() => {})
        return
      }
      this.release()
      this.player ??= this.deps.createPlayer()
      if (audio instanceof Blob) this.url = this.deps.createURL(audio)
      else {
        const continuous = /^audio\/(?:mpeg|mp3)(?:;|$)/u.test(audio.contentType.trim()) && typeof MediaSource !== 'undefined' && MediaSource.isTypeSupported(audio.contentType.replace(/^audio\/mp3(?=;|$)/u, 'audio/mpeg'))
        const stream = await prepareSpeechSource(continuous && !entry.audio && !audio.incrementalInput ? this.continuous(audio, entry, signal) : audio, signal, source => this.deps.createURL(source), entry.timing)
        if (token !== this.generation) {
          stream.dispose(); this.deps.revokeURL(stream.url)
          void stream.done.catch(() => {})
          return
        }
        this.url = stream.url
        this.disposeStream = stream.dispose
        void stream.done.catch(error => this.fail(token, error))
      }
      this.player.src = this.url
      const handlers: Record<string, EventListener> = {
        playing: () => { if (token === this.generation) { entry.timing?.mark('native_playing'); this.publish(entry.key, 'playing') } },
        pause: () => { if (token === this.generation) this.publish(entry.key, 'paused') },
        waiting: () => { if (token === this.generation) this.publish(entry.key, 'loading') },
        ended: () => {
          if (token !== this.generation) return
          entry.timing?.mark('native_ended')
          this.abort?.abort(); this.abort = undefined
          this.release()
          void this.next()
        },
        error: () => this.fail(token, new Error('Audio playback failed')),
      }
      for (const [type, handler] of Object.entries(handlers)) this.player.addEventListener(type, handler)
      this.detach = () => {
        for (const [type, handler] of Object.entries(handlers)) this.player?.removeEventListener(type, handler)
      }
      this.tryPlay(token)
    } catch (error) { this.fail(token, error) }
  }
}
