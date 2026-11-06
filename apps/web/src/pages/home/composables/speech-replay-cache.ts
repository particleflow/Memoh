import type { SpeechAudioStream } from './speech-audio-stream'
import { normalizeSpeechText } from './speech-rendered-text'

interface Part { source: string, identity: string, type: string, audio?: Blob }
interface Reply { parts: Part[], version?: string }
export type ReplayAvailability = 'ready' | 'missing' | 'incomplete' | 'changed'

/** Page-memory generated audio only. Stop is separate from unload, as in
 * Howler 2.2.4. No new synthesis, persistent history write, TTL or quota policy.
 * Keys are supplied by the owner (account/Bot/session/run); auth changes clear
 * this page memory. Server identity is revalidated before a cached replay. */
export class SpeechReplayCache {
  private replies = new Map<string, Reply>()
  private changed: () => void
  constructor(changed: () => void = () => {}) { this.changed = changed }
  clear() { this.replies.clear(); this.changed() }
  capture(key: string, source: string | (() => string), audio: SpeechAudioStream, signal: AbortSignal): SpeechAudioStream {
    const reply = this.replies.get(key) ?? { parts: [] }
    this.replies.set(key, reply)
    const sourceText = () => typeof source === 'function' ? source() : source
    const part: Part = { source: sourceText(), identity: audio.identity ?? '', type: audio.contentType }
    // Final source can precede the renderer's final visible tail. A new audio
    // part is a new completed version, not a mutation of the old cached bytes.
    reply.version = undefined
    reply.parts.push(part); this.changed()
    const reader = audio.stream.getReader(), chunks: ArrayBuffer[] = []
    let canceled = false
    // Tap the consumed stream instead of teeing into a faster unbounded branch.
    const stream = new ReadableStream<Uint8Array>({
      pull: async output => {
        try {
          const next = await reader.read()
          if (next.done) {
            part.source = sourceText()
            if (!canceled && !signal.aborted && chunks.length && part.identity) part.audio = new Blob(chunks, { type: part.type })
            reader.releaseLock(); this.changed(); output.close(); return
          }
          chunks.push(new Uint8Array(next.value).buffer)
          output.enqueue(next.value)
        } catch (error) { this.changed(); reader.releaseLock(); output.error(error) }
      },
      cancel: reason => { canceled = true; this.changed(); void reader.cancel(reason).catch(() => {}); reader.releaseLock() },
    }, { highWaterMark: 0 })
    return { ...audio, stream }
  }
  status(key: string, text: string): ReplayAvailability {
    const reply = this.replies.get(key)
    if (!reply) return 'missing'
    if (!reply.parts.length || reply.parts.some(part => !part.audio || !part.identity)) return 'incomplete'
    if (new Set(reply.parts.map(part => part.identity)).size !== 1 || new Set(reply.parts.map(part => part.type)).size !== 1) return 'changed'
    const version = normalizeSpeechText(text)
    if (normalizeSpeechText(reply.parts.map(part => part.source).join('')) !== version || (reply.version !== undefined && reply.version !== version)) return 'changed'
    reply.version ??= version
    return 'ready'
  }
  read(key: string, text: string, identity: string): Blob[] | undefined {
    if (this.status(key, text) !== 'ready') return
    const parts = this.replies.get(key)!.parts
    if (!identity || parts.some(part => part.identity !== identity)) return
    return parts.map(part => part.audio!)
  }
}
