// Temporary, explicitly armed local measurement. No IO, logging or automatic fetch.
import { onAuthSessionCleared } from './auth-session'

type Handle = { generation: number; id: number }
type Row = { id: number; card: number; request?: number; headers?: number; responseEnd?: number;
  body?: number; bytes?: number; hashStart?: number; hashEnd?: number; blob?: number;
  load?: number; decodeStart?: number; decodeEnd?: number; render?: number; error?: number }
const lifetime = 120_000
let generation = 0, armed = false, timer: ReturnType<typeof setTimeout> | undefined
let rows: Row[] = [], cards = new WeakMap<object, Handle>(), blobs = new WeakMap<Blob, Handle>()
let requested = new WeakSet<object>()
const urls = new Map<string, Handle>() // Private transient Blob URLs; never serialized.
let observer: PerformanceObserver | undefined, paintSupported = false
const now = () => performance.now()
const row = (handle?: Handle) => handle?.generation === generation ? rows[handle.id - 1] : undefined

function clear() {
  generation++; armed = false; clearTimeout(timer); timer = undefined
  observer?.disconnect(); observer = undefined; paintSupported = false
  rows = []; cards = new WeakMap(); requested = new WeakSet(); blobs = new WeakMap(); urls.clear()
}
function snapshot() {
  // Whitelist numeric fields again on native readback; never return entry/attachment objects.
  return { version: 1, active: armed, paintSupported, rows: rows.map(value => ({ ...value })) }
}
function arm() {
  clear(); armed = true
  if (typeof PerformanceObserver !== 'undefined' && PerformanceObserver.supportedEntryTypes?.includes('element')) {
    try {
      observer = new PerformanceObserver(list => {
        for (const entry of list.getEntries()) {
          const element = entry as PerformanceEntry & { identifier?: string; renderTime?: number }
          const match = /^garnet-image-(\d+)-(\d+)$/.exec(element.identifier ?? '')
          if (!match || Number(match[1]) !== generation) continue
          const value = row({ generation, id: Number(match[2]) })
          if (value && typeof element.renderTime === 'number' && element.renderTime > 0) value.render = element.renderTime
        }
      })
      observer.observe({ type: 'element', buffered: false }); paintSupported = true
    } catch { observer?.disconnect(); observer = undefined }
  }
  timer = setTimeout(clear, lifetime)
  return snapshot()
}
function take() {
  const result = snapshot(); clear(); return result
}

export function imageTimingCard(attachment: object): void {
  if (!armed || cards.has(attachment) || rows.length >= 5) return
  const handle = { generation, id: rows.length + 1 }
  cards.set(attachment, handle); rows.push({ id: handle.id, card: now() })
}
export function imageTimingHandle(attachment: object): Handle | undefined {
  // Claim only the inline card's first read; lightbox/save reuse must not overwrite it.
  if (!armed || requested.has(attachment)) return undefined
  const handle = cards.get(attachment)
  if (handle) requested.add(attachment)
  return handle
}
export function imageTimingMark(handle: Handle | undefined, phase: 'body' | 'hashStart' | 'hashEnd', bytes?: number): void {
  const value = row(handle); if (!value || !armed) return
  value[phase] = now()
  if (typeof bytes === 'number' && Number.isSafeInteger(bytes) && bytes >= 0) value.bytes = bytes
}
export function imageTimingFetch(handle: Handle | undefined, source: typeof fetch): typeof fetch {
  if (!handle || !armed) return source
  return async (input, init) => {
    const value = row(handle)
    if (value && armed) value.request = now()
    try {
      const response = await source(input, init)
      if (row(handle) === value && value && armed) value.headers = now()
      return response
    } catch (error) {
      if (row(handle) === value && value && armed) value.error = 1
      throw error
    }
  }
}
export function imageTimingResource(handle: Handle | undefined, url: string): void {
  if (!row(handle) || !armed || typeof performance.getEntriesByName !== 'function') return
  // The original fetch may queue its entry after arrayBuffer resolves. Do not invent a value.
  setTimeout(() => {
    const value = row(handle); if (!value || !armed) return
    const entries = performance.getEntriesByName(url, 'resource') as PerformanceResourceTiming[]
    const entry = entries.filter(entry => ['fetch', 'xmlhttprequest'].includes(entry.initiatorType)
      && entry.startTime >= value.card).at(-1)
    if (entry && entry.responseEnd > 0) value.responseEnd = entry.responseEnd
  }, 0)
}
export function imageTimingBlob(handle: Handle | undefined, blob: Blob): void {
  if (handle && row(handle) && armed) blobs.set(blob, handle)
}
export function imageTimingURL(blob: Blob, url: string): void {
  const handle = blobs.get(blob), value = row(handle)
  if (!handle || !value || !armed) return
  urls.set(url, handle); value.blob = now()
}
export function imageTimingElement(url: string): string | undefined {
  const handle = urls.get(url)
  return armed && paintSupported && row(handle) ? `garnet-image-${generation}-${handle!.id}` : undefined
}
export function imageTimingFailed(event: Event): void {
  if (!armed || !(event.target instanceof HTMLImageElement)) return
  const value = row(urls.get(event.target.currentSrc || event.target.src))
  if (value) value.error = 3
}
export function imageTimingLoaded(event: Event): void {
  if (!armed || !(event.target instanceof HTMLImageElement)) return
  const image = event.target, handle = urls.get(image.currentSrc || image.src), value = row(handle)
  if (!value) return
  if (value.load !== undefined) return
  value.load = now()
  if (typeof image.decode !== 'function') return
  value.decodeStart = now()
  void image.decode().then(() => {
    if (row(handle) === value && armed) value.decodeEnd = now()
  }).catch(() => { if (row(handle) === value && armed) value.error = 2 })
}

// The native controller calls only these fixed operations; no network/console/debug entry.
Object.defineProperty(window, '__garnetImageTiming', { value: Object.freeze({ arm, take, clear }), configurable: false })
onAuthSessionCleared(clear)
window.addEventListener('pagehide', clear)
