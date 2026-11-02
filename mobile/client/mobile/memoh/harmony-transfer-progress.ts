// Attachment transfer progress: pure client-side byte/time bookkeeping for the
// ring and speed indicator. No network, storage, native bridge or serialized
// chat state lives here; callers report measured bytes, this module only keeps
// a rolling window and derived views.
import { shallowReactive } from 'vue'

export interface TransferBytes { loaded: number; total: number | undefined; samples: readonly { loaded: number; at: number }[]; rate: number | undefined }
export type AttachmentTransferPhase = 'idle' | 'transferring' | 'failed' | 'ready'
export interface AttachmentTransferView { phase: AttachmentTransferPhase; fraction: number | null; rate: number | undefined; bytes: { loaded: number; total: number | undefined } | undefined }
export interface MessageTransferView { active: boolean; fraction: number | null; rate: number | undefined }

const WINDOW_MS = 3000
// Upload wire bytes are keyed by the optimistic attachment object (its hash only
// exists after the receipt); verified media reads are keyed by content hash so
// every card of the same original shares one measurement.
const uploadBytes = shallowReactive(new Map<object, TransferBytes>())
const downloadBytes = shallowReactive(new Map<string, TransferBytes>())

const TRANSFERRING_PHASES = new Set(['uploading', 'checking', 'verifying', 'finalizing'])
const FAILED_PHASES = new Set(['failed'])

function withSample(previous: TransferBytes | undefined, loaded: number, total: number | undefined): TransferBytes {
  const at = Date.now()
  const samples = [...(previous?.samples ?? []).filter(item => at - item.at <= WINDOW_MS), { loaded, at }]
  const first = samples[0]!, last = samples[samples.length - 1]!
  const span = last.at - first.at
  const rate = samples.length > 1 && span > 0
    ? Math.max(0, (last.loaded - first.loaded) * 1000 / span)
    : previous?.rate
  return { loaded, total: Number.isSafeInteger(total) && total! > 0 ? total : previous?.total, samples, rate }
}

export function noteUploadBytes(key: object, loaded: number, total?: number): void {
  uploadBytes.set(key, withSample(uploadBytes.get(key), loaded, total))
}

export function clearUploadBytes(key: object): void {
  uploadBytes.delete(key)
}

export function noteDownloadBytes(key: string, loaded: number, total?: number): void {
  downloadBytes.set(key, withSample(downloadBytes.get(key), loaded, total))
}

export function clearDownloadBytes(key: string): void {
  downloadBytes.delete(key)
}

export function uploadTransferBytes(key: object): TransferBytes | undefined {
  return uploadBytes.get(key)
}

export function downloadTransferBytes(key: string): TransferBytes | undefined {
  return downloadBytes.get(key)
}

const fractionOf = (state: TransferBytes): number | null =>
  state.total != null && state.total > 0 ? Math.min(1, state.loaded / state.total) : null

export function attachmentTransferView(attachment: { content_hash?: string | number | null; metadata?: Record<string, unknown> } | undefined): AttachmentTransferView {
  if (!attachment) return { phase: 'idle', fraction: null, rate: undefined, bytes: undefined }
  const phase = typeof attachment.metadata?.harmony_transfer === 'string' ? attachment.metadata.harmony_transfer : ''
  const hash = typeof attachment.content_hash === 'string' && attachment.content_hash ? attachment.content_hash : undefined
  const read = hash != null ? downloadBytes.get(hash) : undefined
  if (FAILED_PHASES.has(phase)) return { phase: 'failed', fraction: null, rate: undefined, bytes: undefined }
  if (TRANSFERRING_PHASES.has(phase)) {
    const state = uploadBytes.get(attachment) ?? read
    return state
      ? { phase: 'transferring', fraction: fractionOf(state), rate: state.rate, bytes: { loaded: state.loaded, total: state.total } }
      : { phase: 'transferring', fraction: null, rate: undefined, bytes: undefined }
  }
  if (read) return { phase: 'transferring', fraction: fractionOf(read), rate: read.rate, bytes: { loaded: read.loaded, total: read.total } }
  if (phase === 'ready') return { phase: 'ready', fraction: 1, rate: undefined, bytes: undefined }
  return { phase: 'idle', fraction: null, rate: undefined, bytes: undefined }
}

// One indicator per message: an aggregate over its attachments. The fraction is
// known only while every active transfer reports bytes; one opaque native
// transfer keeps the ring indeterminate instead of inventing a share.
export function messageTransferView(attachments: readonly ({ content_hash?: string | number | null; metadata?: Record<string, unknown> })[]): MessageTransferView {
  let loaded = 0, total = 0, rate = 0, active = false, fractionKnown = true
  for (const attachment of attachments) {
    const view = attachmentTransferView(attachment)
    if (view.phase !== 'transferring') continue
    active = true
    if (view.rate != null) rate += view.rate
    if (view.fraction == null || view.bytes == null || view.bytes.total == null) { fractionKnown = false; continue }
    loaded += view.bytes.loaded
    total += view.bytes.total
  }
  if (!active) return { active: false, fraction: null, rate: undefined }
  return { active: true, fraction: fractionKnown && total > 0 ? Math.min(1, loaded / total) : null, rate: rate > 0 ? rate : undefined }
}

// Magnitude-selected units, one decimal above KB. MB/s is the ceiling; larger
// rates stay in MB/s rather than stepping to GB/s for a phone transfer.
export function formatTransferSpeed(bytesPerSecond: number): string {
  if (!Number.isFinite(bytesPerSecond) || bytesPerSecond <= 0) return ''
  if (bytesPerSecond < 1024) return `${Math.round(bytesPerSecond)} B/s`
  if (bytesPerSecond < 1024 * 1024) return `${(bytesPerSecond / 1024).toFixed(1)} KB/s`
  return `${(bytesPerSecond / (1024 * 1024)).toFixed(1)} MB/s`
}
