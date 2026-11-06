// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest'
import { audioTimingObserver, beginSpeechTiming, serverFirstAudioMS, speechMimeClass } from './speech-timing'
const snapshot = () => (window as unknown as { __memohSpeechTiming: { snapshot(): { id: string, operationId: string, events: { event: string, ms: number, signature?: string }[] }[], clear(): void } }).__memohSpeechTiming
function arm() { sessionStorage.setItem('memoh.speechTiming', '1'); snapshot()?.clear() }
afterEach(() => { sessionStorage.removeItem('memoh.speechTiming'); snapshot()?.clear(); vi.restoreAllMocks(); vi.unstubAllGlobals() })
it('is disabled by default, without exporting a trace or changing a request', () => {
  sessionStorage.removeItem('memoh.speechTiming'); expect(beginSpeechTiming('manual')).toBeUndefined()
})
it('correlates request children on one operation clock and records each phase once', () => {
  arm(); let now = 100; vi.spyOn(performance, 'now').mockImplementation(() => now)
  const operation = beginSpeechTiming('manual')!; now = 130; operation.mark('text_observed')
  const request = operation.fork(); now = 150; request.mark('request_start'); now = 200; request.mark('client_first_bytes', { bytes: 4 }); request.mark('client_first_bytes', { bytes: 8 })
  const rows = snapshot().snapshot(); expect(rows.at(-1)?.operationId).toBe(operation.id); expect(request.id).not.toBe(operation.id)
  expect(rows.at(-1)?.events.map(e => [e.event, e.ms])).toEqual([['request_start', 50], ['client_first_bytes', 100]])
})
it('retains at most 32 anonymous records and returns a detached snapshot', () => {
  arm(); for (let i = 0; i < 40; i++) beginSpeechTiming('automatic')
  const rows = snapshot().snapshot(); expect(rows).toHaveLength(32); rows[0]!.events.length = 0
  expect(snapshot().snapshot()[0]?.events).toHaveLength(1)
})
it('exports only a signature enum across split headers, without raw bytes or MIME parameters', () => {
  arm(); const trace = beginSpeechTiming('manual')!, observe = audioTimingObserver(trace)
  observe(new Uint8Array([82, 73])); observe(new Uint8Array([70, 70, 9, 8, 7, 6, 87, 65, 86, 69, 65, 66, 67]))
  expect(snapshot().snapshot().at(-1)?.events.at(-1)?.signature).toBe('riff_wave_prefix')
  expect(speechMimeClass('audio/wav; private=secret-token')).toBe('wav')
  const text = JSON.stringify(snapshot().snapshot()); expect(text).not.toMatch(/secret-token|private|82,73|http|ABC/u)
})
it('accepts only the numeric server duration and keeps absent/invalid values unknown', () => {
  expect(serverFirstAudioMS('other;dur=1, speech_first_audio;dur=70.961')).toBe(70.961)
  expect(serverFirstAudioMS(null)).toBeUndefined(); expect(serverFirstAudioMS('speech_first_audio;dur=secret')).toBeUndefined()
})

it('can correlate on a custom-scheme engine without randomUUID, using cryptographic v4 IDs', () => {
  arm(); vi.stubGlobal('crypto', { getRandomValues: crypto.getRandomValues.bind(crypto) })
  const trace = beginSpeechTiming('manual')!
  expect(trace.id).toMatch(/^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$/u)
})
