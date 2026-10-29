export interface SpeechAudioStream { stream: ReadableStream<Uint8Array>, contentType: string }
export interface SpeechStreamSource { url: string, done: Promise<void>, dispose(): void }

/** Preserve native Audio compatibility when this browser cannot stream the
 * response MIME through MSE. Consume the same response; never synthesize again. */
export async function prepareSpeechSource(audio: SpeechAudioStream, signal: AbortSignal, createURL: (source: Blob | MediaSource) => string): Promise<SpeechStreamSource> {
  const mime = audio.contentType.trim().replace(/^audio\/mp3(?=;|$)/u, 'audio/mpeg')
  if (typeof MediaSource !== 'undefined' && MediaSource.isTypeSupported(mime)) return openSpeechStream(audio, signal, createURL)
  const reader = audio.stream.getReader()
  const onAbort = () => { void reader.cancel(signal.reason).catch(() => {}) }
  signal.addEventListener('abort', onAbort, { once: true })
  try {
    if (signal.aborted) { onAbort(); throw signal.reason }
    const chunks: ArrayBuffer[] = []
    for (;;) {
      const chunk = await reader.read()
      if (signal.aborted) throw signal.reason
      if (chunk.done) break
      if (chunk.value.length) chunks.push(new Uint8Array(chunk.value).buffer)
    }
    const url = createURL(new Blob(chunks, { type: audio.contentType }))
    return { url, done: Promise.resolve(), dispose() {} }
  } finally {
    signal.removeEventListener('abort', onAbort)
    // Preserve the first read/abort error even if upstream cancellation stalls.
    void reader.cancel().catch(() => {})
    reader.releaseLock()
  }
}

function mediaEvent(target: EventTarget, event: string, signal: AbortSignal, action?: () => void): Promise<void> {
  return new Promise((resolve, reject) => {
    const clean = () => { target.removeEventListener(event, success); target.removeEventListener('error', failure); signal.removeEventListener('abort', failure) }
    const success = () => { clean(); resolve() }
    const failure = () => { clean(); reject(signal.reason ?? new Error('Audio stream failed')) }
    if (signal.aborted) { failure(); return }
    target.addEventListener(event, success, { once: true })
    target.addEventListener('error', failure, { once: true })
    signal.addEventListener('abort', failure, { once: true })
    try { action?.() } catch (error) { clean(); reject(error) }
  })
}

/** Bounded streaming into the native decoder: no Blob, arrayBuffer, full-body
 * decode or fixed prebuffer. The browser may still need several codec frames. */
export function openSpeechStream(audio: SpeechAudioStream, signal: AbortSignal, createURL: (source: MediaSource) => string): SpeechStreamSource {
  const mime = audio.contentType.trim().replace(/^audio\/mp3(?=;|$)/u, 'audio/mpeg')
  if (typeof MediaSource === 'undefined' || !MediaSource.isTypeSupported(mime)) {
    void audio.stream.cancel().catch(() => {})
    throw new Error(`Streaming playback is unsupported for ${mime}`)
  }
  const media = new MediaSource()
  let url: string
  try { url = createURL(media) } catch (error) { void audio.stream.cancel().catch(() => {}); throw error }
  const reader = audio.stream.getReader()
  const onAbort = () => { void reader.cancel(signal.reason).catch(() => {}) }
  signal.addEventListener('abort', onAbort, { once: true })
  const opened = mediaEvent(media, 'sourceopen', signal)
  const done = (async () => {
    try {
      await opened
      if (signal.aborted) throw signal.reason
      const buffer = media.addSourceBuffer(mime)
      for (;;) {
        const chunk = await reader.read()
        if (signal.aborted) throw signal.reason
        if (chunk.done) break
        if (!chunk.value.length) continue
        // Register first: a synthetic/native append may finish immediately.
        await mediaEvent(buffer, 'updateend', signal, () => buffer.appendBuffer(new Uint8Array(chunk.value).buffer))
      }
      if (media.readyState === 'open') media.endOfStream()
    } finally {
      signal.removeEventListener('abort', onAbort)
      await reader.cancel().catch(() => {})
      reader.releaseLock()
    }
  })()
  return { url, done, dispose: onAbort }
}
