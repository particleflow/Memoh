import { shallowReactive } from 'vue'
import { enumerateNativeAttachments, nativeAttachmentCommand, nativeAttachmentsAvailable, type NativeUploadCensus } from './harmony-resumable'
import { authSessionGeneration, onAuthSessionCleared } from '@/lib/auth-session'
import { sdkAuthQuery } from '@/lib/api-client'

// Killing the app pauses its durable uploads; it never cancels them. After the
// next sign-in this module re-drives the existing journals through the existing
// single-task upload engine and stops at ready: sending keeps its user gates,
// and no second queue, journal or retry loop is introduced here.
export interface NativeUploadRecovery {
  handle: string; name: string; size: number; bot: string; scope: string
  status: 'resuming' | 'fenced' | 'failed'; reason?: string
}
export const nativeUploadRecovery = shallowReactive(new Map<string, NativeUploadRecovery>())
const resumable: string[] = ['staged', 'uploading']
let reconciled = -1
let controller: AbortController | undefined
let pending: Promise<void> = Promise.resolve()

function entry(item: NativeUploadCensus, status: NativeUploadRecovery['status'], reason?: string): NativeUploadRecovery {
  return { handle: item.handle, name: item.name, size: item.size, bot: item.bot, scope: item.scope, status,
    ...(reason ? { reason } : {}) }
}

async function reconcile(signal: AbortSignal): Promise<void> {
  for (const item of await enumerateNativeAttachments(signal)) {
    if (signal.aborted) return
    if (item.state === 'ready') {
      // Ready originals surface through the existing per-scope restore and
      // consume windows; the recovery list only tracks unfinished work.
      nativeUploadRecovery.delete(item.handle)
      continue
    }
    if (!resumable.includes(item.state)) {
      // Unknown outcomes never resend: single-list them for manual
      // verification instead of guessing a new identity for them.
      nativeUploadRecovery.set(item.handle, entry(item, 'fenced', item.state))
      continue
    }
    nativeUploadRecovery.set(item.handle, entry(item, 'resuming'))
    try {
      await nativeAttachmentCommand('upload.run', item.bot, item.scope, { handle: item.handle }, signal)
      if (!signal.aborted) nativeUploadRecovery.delete(item.handle)
    } catch (cause) {
      // A missing or expired receipt is not permission to create another one;
      // the snapshot and journal identity stay for the user to verify or drop.
      if (signal.aborted) return
      nativeUploadRecovery.set(item.handle, entry(item, 'failed', cause instanceof Error && cause.message ? cause.message : '续传未完成'))
    }
  }
}

function drive(): void {
  if (controller || !sdkAuthQuery().token) return
  const mine = new AbortController()
  controller = mine
  pending = reconcile(mine.signal).catch((): void => {}).finally((): void => { if (controller === mine) controller = undefined })
}

export function ensureNativeUploadReconciliation(): void {
  if (!nativeAttachmentsAvailable) return
  const generation = authSessionGeneration()
  if (!sdkAuthQuery().token || generation === reconciled) return
  reconciled = generation
  drive()
}

export function nativeUploadReconciliationSettled(): Promise<void> { return pending }

export async function discardNativeRecoveryItem(item: NativeUploadRecovery): Promise<void> {
  // Stop any in-flight resume first (pause semantics), then remove the journal
  // and snapshot through the existing discard command.
  const mine = controller
  if (mine) { mine.abort(); await pending }
  try {
    await nativeAttachmentCommand('upload.discard', item.bot, item.scope, { handle: item.handle })
  } finally { nativeUploadRecovery.delete(item.handle) }
  drive()
}

onAuthSessionCleared(() => {
  controller?.abort()
  nativeUploadRecovery.clear()
  reconciled = -1
})
