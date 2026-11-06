/** Opt-in, bounded, page-memory diagnostics. Never retain text, URLs, message
 * identities, audio bytes or credentials. Durations use one performance clock. */
export type SpeechTimingEvent = 'audio_signature' | 'source_branch' | 'operation_start' | 'text_observed' | 'text_chunk_ready' | 'request_start' | 'response_headers' | 'client_first_bytes' | 'decoder_append_start' | 'decoder_append_done' | 'native_play_call' | 'native_playing' | 'native_ended' | 'body_eof' | 'blob_ready' | 'interrupted' | 'error' | 'blocked'
interface TimingData { signature?: 'id3_prefix' | 'mpeg_header_prefix' | 'riff_wave_prefix' | 'unknown', bytes?: number, serverFirstAudioMS?: number, branch?: 'mse' | 'blob', mime?: 'mp3' | 'wav' | 'pcm' | 'opus' | 'other' }
interface TimingRecord { id: string, operationId: string, trigger: 'manual' | 'automatic' | 'direct', clock: 'browser-performance', startMS: number, events: ({ event: SpeechTimingEvent, ms: number } & TimingData)[] }
export interface SpeechTiming { id: string, mark(event: SpeechTimingEvent, data?: TimingData): void, fork(): SpeechTiming }
const records: TimingRecord[] = []
const limit = 32
function enabled() { try { return typeof window !== 'undefined' && sessionStorage.getItem('memoh.speechTiming') === '1' && typeof crypto !== 'undefined' && typeof crypto.getRandomValues === 'function' } catch { return false } }
function randomID() {
  if (typeof crypto.randomUUID === 'function') return crypto.randomUUID()
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  bytes[6] = (bytes[6]! & 15) | 64; bytes[8] = (bytes[8]! & 63) | 128
  const hex = [...bytes].map(value => value.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}
export function beginSpeechTiming(trigger: TimingRecord['trigger']): SpeechTiming | undefined {
  if (!enabled()) return
  const start = performance.now(), operationId = randomID()
  // Console export only. No fetch, audio action, UI or persistent content log.
  Object.defineProperty(window, '__memohSpeechTiming', { configurable: true, value: { snapshot: () => JSON.parse(JSON.stringify(records)), clear: () => { records.length = 0 } } })
  function create(id: string): SpeechTiming {
    const record: TimingRecord = { id, operationId, trigger, clock: 'browser-performance', startMS: start, events: [] }
    records.push(record); if (records.length > limit) records.shift()
    const seen = new Set<SpeechTimingEvent>()
    return { id, mark(event, data = {}) { if (seen.has(event)) return; seen.add(event); record.events.push({ event, ms: performance.now() - start, ...data }) }, fork: () => create(randomID()) }
  }
  const result = create(operationId); result.mark('operation_start'); return result
}
export function speechMimeClass(type: string): TimingData['mime'] {
  const mime = type.split(';', 1)[0]?.trim().toLowerCase()
  return mime === 'audio/mpeg' || mime === 'audio/mp3' ? 'mp3' : mime === 'audio/wav' || mime === 'audio/x-wav' ? 'wav' : mime === 'audio/pcm' ? 'pcm' : mime === 'audio/opus' ? 'opus' : 'other'
}
export function serverFirstAudioMS(header: string | null): number | undefined {
  const match = /(?:^|,)\s*speech_first_audio;dur=(\d+(?:\.\d+)?)(?:\s*,|\s*$)/u.exec(header ?? '')
  return match ? Number(match[1]) : undefined
}

/** Classify only a bounded header prefix; never retain it in the export. This
 * is a signature hint, not a verified codec or a playback capability gate. */
export function audioTimingObserver(timing?: SpeechTiming) {
  const prefix: number[] = []
  return (chunk: Uint8Array) => {
    if (!timing || !chunk.length) return
    timing.mark('client_first_bytes', { bytes: chunk.length })
    if (prefix.length >= 12) return
    for (let i = 0; i < chunk.length && prefix.length < 12; i++) prefix.push(chunk[i]!)
    if (prefix.length === 12) {
      const signature = prefix[0] === 73 && prefix[1] === 68 && prefix[2] === 51 ? 'id3_prefix'
        : prefix[0] === 255 && (prefix[1]! & 224) === 224 ? 'mpeg_header_prefix'
          : prefix.slice(0, 4).join() === '82,73,70,70' && prefix.slice(8).join() === '87,65,86,69' ? 'riff_wave_prefix' : 'unknown'
      timing.mark('audio_signature', { signature })
    }
  }
}
