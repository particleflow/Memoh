import type { ChatMessage } from '@/store/chat/types'
import { visibleSpeechText } from './speech-visible-text'
import { openSpeechStream, type SpeechAudioStream } from './speech-audio-stream'

export type SpeechStatus = 'idle' | 'loading' | 'playing' | 'paused' | 'blocked' | 'error'
export interface SpeechState { key: string, status: SpeechStatus, error?: unknown }
export interface SpeechEntry { key: string, text: string }
export interface SpeechPlayer {
  src: string
  play(): Promise<void>
  pause(): void
  load(): void
  addEventListener(type: string, listener: EventListener): void
  removeEventListener(type: string, listener: EventListener): void
}
export interface SpeechDependencies {
  synthesize(text: string, signal: AbortSignal): Promise<Blob | SpeechAudioStream>
  createPlayer(): SpeechPlayer
  createURL(blob: Blob | MediaSource): string
  revokeURL(url: string): void
  onState(state: SpeechState): void
}

// Match the server's rune limit, including emoji, without splitting a surrogate pair.
export function speechChunks(text: string, firstLimit = 500): string[] {
  const points = Array.from(text.trim())
  const chunks: string[] = []
  while (points.length) {
    let end = Math.min(chunks.length ? 500 : firstLimit, points.length)
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
  interrupt(messages: ChatMessage[]) {
    for (const message of messages) {
      const key = speechKey(message)
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
      const key = speechKey(message)
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
        const prior = this.spoken.get(observed && !message.runtimeRunId ? observed : revision) ?? ''
        // A replacement is not an append. Never replay previously spoken text.
        if (text.startsWith(prior)) {
          const tail = text.slice(prior.length)
          let end = tail.length
          if (message.streaming && !flush) {
            const boundary = /[。！？\n]|[.!?](?:\s|$)|[，,；;：:]\s*/u.exec(tail)
            end = boundary ? boundary.index + boundary[0].length : Array.from(tail).length >= 32 ? Array.from(tail).slice(0, 32).join('').length : 0
          }
          const delta = tail.slice(0, end)
          if (delta.trim()) { result.push({ key, text: delta }); this.spoken.set(revision, prior + delta) }
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

/** One player, one synthesis request, one generation. No automatic retry of a
 * failed/uncertain synthesis. Late promises and media events cannot restart it. */
export class ChatReadAloud {
  private generation = 0
  private queue: SpeechEntry[] = []
  private abort?: AbortController
  private player?: SpeechPlayer
  private url = ''
  private detach?: () => void
  private disposeStream?: () => void
  private state: SpeechState = { key: '', status: 'idle' }
  private deps: SpeechDependencies
  constructor(deps: SpeechDependencies) { this.deps = deps }

  private publish(key: string, status: SpeechStatus, error?: unknown) {
    this.state = { key, status, error }
    this.deps.onState(this.state)
  }
  private release() {
    this.disposeStream?.(); this.disposeStream = undefined
    this.detach?.(); this.detach = undefined
    if (this.player) { this.player.pause(); this.player.src = ''; this.player.load() }
    if (this.url) { this.deps.revokeURL(this.url); this.url = '' }
  }
  stop() {
    ++this.generation
    this.queue = []
    this.abort?.abort(); this.abort = undefined
    this.release()
    this.publish('', 'idle')
  }
  enqueue(entry: SpeechEntry) {
    // Bound the first manual request too; do not send a 500-character paragraph
    // before its opening phrase can start synthesizing.
    this.queue.push(...speechChunks(entry.text, 80).map(text => ({ key: entry.key, text })))
    if (this.state.status === 'idle' || this.state.status === 'error') void this.next()
  }
  replay(entry: SpeechEntry) { this.stop(); this.enqueue(entry) }
  resume() {
    // Called directly by the user's button gesture. Reuse the already loaded
    // audio; do not await a network request before calling play().
    if (this.player && (this.state.status === 'blocked' || this.state.status === 'paused')) {
      this.tryPlay(this.generation)
    }
  }
  private fail(token: number, error: unknown) {
    if (token !== this.generation) return
    ++this.generation
    this.queue = []
    this.abort?.abort(); this.abort = undefined
    this.release()
    this.publish(this.state.key, 'error', error)
  }
  private tryPlay(token: number) {
    try {
      void this.player!.play().catch(error => {
        if (token !== this.generation) return
        if (error && typeof error === 'object' && error.name === 'NotAllowedError') {
          this.publish(this.state.key, 'blocked')
        } else this.fail(token, error)
      })
    } catch (error) { this.fail(token, error) }
  }
  private async next() {
    const entry = this.queue.shift()
    if (!entry) { this.publish('', 'idle'); return }
    const token = ++this.generation
    this.abort = new AbortController()
    this.publish(entry.key, 'loading')
    try {
      const signal = this.abort.signal
      const audio = await this.deps.synthesize(entry.text, signal)
      if (token !== this.generation) {
        if (!(audio instanceof Blob)) void audio.stream.cancel().catch(() => {})
        return
      }
      this.release()
      this.player ??= this.deps.createPlayer()
      if (audio instanceof Blob) this.url = this.deps.createURL(audio)
      else {
        const stream = openSpeechStream(audio, signal, source => this.deps.createURL(source))
        this.url = stream.url
        this.disposeStream = stream.dispose
        void stream.done.catch(error => this.fail(token, error))
      }
      this.player.src = this.url
      const handlers: Record<string, EventListener> = {
        playing: () => { if (token === this.generation) this.publish(entry.key, 'playing') },
        pause: () => { if (token === this.generation) this.publish(entry.key, 'paused') },
        waiting: () => { if (token === this.generation) this.publish(entry.key, 'loading') },
        ended: () => {
          if (token !== this.generation) return
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
