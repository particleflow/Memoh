import { shallowReactive } from 'vue'
import { createApiFetch, sdkApiUrl, sdkAuthQuery } from '@/lib/api-client'
import { authSessionGeneration, onAuthSessionCleared } from '@/lib/auth-session'
import { useUserStore } from '@/store/user'
import type { NativeUploadFailure } from './harmony-resumable'

const storageKey = 'memoh:harmony:upload-failures:v1'
// Same lifetime as media.UploadTTL; this is metadata, never a transfer budget.
const retention = 24 * 60 * 60 * 1000
export interface QueuedUploadFailure {
  operation: string; botId: string; binding: string; failure: NativeUploadFailure; handle?: string
  report: 'pending' | 'reported' | 'conflict'; expires: number
}
export const nativeUploadFailureQueue = shallowReactive(new Map<string, QueuedUploadFailure>())
const active = new Map<string, AbortController>()
let diskWritable = true
let currentBinding: string | undefined
const phases = ['owner-read', 'limits-read', 'picker', 'source-metadata', 'source-open', 'snapshot-create',
  'snapshot-copy', 'snapshot-hash', 'source-verify', 'journal-publish', 'journal-dir-sync']
const codes = ['MEDIA_SHA_MISMATCH', 'MEDIA_SIZE_MISMATCH', 'MEDIA_SHORT_READ', 'MEDIA_SHORT_WRITE',
  'UPLOAD_CANCELLED', 'UPLOAD_REQUEST', 'UPLOAD_IDENTITY', 'UPLOAD_LIMIT_INVALID', 'UPLOAD_JOURNAL_SIZE',
  'UPLOAD_JOURNAL_WRITE', 'UPLOAD_RECEIPT_SIZE', 'UPLOAD_HTTP_413', 'VOICE_UNAVAILABLE', 'UNCLASSIFIED']
function valid(row: QueuedUploadFailure): boolean {
  const f = row?.failure
  return !!f && /^[a-f0-9]{32}$/.test(row.operation) && /^[A-Za-z0-9_-]{1,128}$/.test(row.botId)
    && /^[a-f0-9]{64}$/.test(row.binding) && (!row.handle || /^[a-f0-9]{32}$/.test(row.handle))
    && f.version === 1 && phases.includes(f.phase) && codes.includes(f.code)
    && Number.isSafeInteger(f.sdkCode) && f.sdkCode >= 0 && f.sdkCode <= 2147483647
    && Number.isSafeInteger(f.at) && f.at > 0 && Number.isSafeInteger(row.expires) && row.expires > Date.now()
    && ['pending', 'reported', 'conflict'].includes(row.report)
}
function safe(row: QueuedUploadFailure): QueuedUploadFailure {
  const f = row.failure
  return { operation: row.operation, botId: row.botId, binding: row.binding, report: row.report, expires: row.expires,
    ...(row.handle ? { handle: row.handle } : {}), failure: { version: 1, phase: f.phase, code: f.code, sdkCode: f.sdkCode, at: f.at } }
}
function diskRows(): QueuedUploadFailure[] {
  try {
    const rows = JSON.parse(localStorage.getItem(storageKey) || '[]')
    // Unknown/corrupt metadata is not permission to overwrite its old bytes.
    // Expired, otherwise-valid records follow normal TTL cleanup.
    if (!Array.isArray(rows) || rows.some(row => !row || !row.failure || !Number.isSafeInteger(row.expires) || row.expires <= 0
      || Object.keys(row).some(key => !['operation', 'botId', 'binding', 'failure', 'handle', 'report', 'expires'].includes(key))
      || Object.keys(row.failure).some(key => !['version', 'phase', 'code', 'sdkCode', 'at'].includes(key))
      || !valid({ ...row, expires: Date.now() + 1 }))) { diskWritable = false; return [] }
    return rows.filter(valid).map(safe)
  } catch { diskWritable = false; return [] }
}
function persist() {
  if (!diskWritable) return
  // A storage failure must not discard the in-memory failure or old disk data.
  const previous = diskRows(); if (!diskWritable) return
  const rows = [...previous.filter(row => row.binding !== currentBinding), ...[...nativeUploadFailureQueue.values()].filter(valid).map(safe)]
  try { localStorage.setItem(storageKey, JSON.stringify(rows)) } catch { /* Retain current state. */ }
}
function context() {
  const token = sdkAuthQuery().token, owner = useUserStore().userInfo.id
  if (!token || !owner) return
  const generation = authSessionGeneration(), base = sdkApiUrl({ url: '/users/me' })
  return { token, owner, base, current: () => generation === authSessionGeneration() && token === sdkAuthQuery().token
    && owner === useUserStore().userInfo.id && base === sdkApiUrl({ url: '/users/me' }) }
}
async function bindingFor(ctx: NonNullable<ReturnType<typeof context>>) {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(ctx.base + '\n' + ctx.owner))
  return [...new Uint8Array(digest)].map(n => n.toString(16).padStart(2, '0')).join('')
}
function restore(binding: string) {
  currentBinding = binding
  for (const [key, row] of nativeUploadFailureQueue) if (!valid(row) || row.binding !== binding) nativeUploadFailureQueue.delete(key)
  for (const row of diskRows()) if (row.binding === binding && !nativeUploadFailureQueue.has(row.operation)) nativeUploadFailureQueue.set(row.operation, row)
}
async function report(row: QueuedUploadFailure, ctx: NonNullable<ReturnType<typeof context>>) {
  if (!ctx.current() || row.report !== 'pending' || active.has(row.operation)) return
  const controller = new AbortController(); active.set(row.operation, controller)
  const f = row.failure
  try {
    const response = await createApiFetch()(sdkApiUrl({ url: '/bots/{bot_id}/media/upload-failures/{operation}',
      path: { bot_id: row.botId, operation: row.operation } }), {
      method: 'PUT', cache: 'no-store', redirect: 'error', signal: controller.signal,
      headers: { authorization: 'Bearer ' + ctx.token, 'content-type': 'application/json' },
      body: JSON.stringify({ version: 1, phase: f.phase, code: f.code, sdk_code: f.sdkCode, at: f.at,
        ...(row.handle ? { handle: row.handle } : {}) }),
    })
    if (!ctx.current() || nativeUploadFailureQueue.get(row.operation) !== row) return
    if (response.status === 409) { nativeUploadFailureQueue.set(row.operation, { ...row, report: 'conflict' }); persist(); return }
    if (response.status !== 200) return // Pending, including old servers and unknown outcomes. Never upload bytes.
    const saved = await response.json()
    if (!ctx.current() || nativeUploadFailureQueue.get(row.operation) !== row || saved.operation !== row.operation || saved.version !== 1 || saved.phase !== f.phase
      || saved.code !== f.code || saved.sdk_code !== f.sdkCode || saved.at !== f.at || (saved.handle || '') !== (row.handle || '')) return
    const expires = Date.parse(saved.expires_at)
    if (!Number.isFinite(expires) || expires <= Date.now()) return
    nativeUploadFailureQueue.set(row.operation, { ...row, report: 'reported', expires }); persist()
  } catch { /* Same-ID metadata stays pending; no timer, file retry or new operation. */ }
  finally { if (active.get(row.operation) === controller) active.delete(row.operation) }
}
export async function rememberNativeUploadFailure(botId: string, operation: string, failure: NativeUploadFailure, handle?: string) {
  const ctx = context(); if (!ctx) return
  const binding = await bindingFor(ctx); if (!ctx.current()) return
  restore(binding)
  const prior = nativeUploadFailureQueue.get(operation)
  const row = prior || safe({ operation, botId, binding, failure, handle, report: 'pending', expires: Date.now() + retention })
  if (!valid(row) || row.binding !== binding || row.botId !== botId) return
  nativeUploadFailureQueue.set(operation, row); persist(); await report(row, ctx)
}
export async function restoreNativeUploadFailures(botId: string) {
  const ctx = context(); if (!ctx) return
  const binding = await bindingFor(ctx); if (!ctx.current()) return
  restore(binding); persist()
  for (const row of nativeUploadFailureQueue.values()) if (row.botId === botId) { if (!ctx.current()) return; await report(row, ctx) }
}
export function forgetNativeUploadFailures(botId: string, handle: string) {
  for (const [key, row] of nativeUploadFailureQueue) if (row.botId === botId && row.handle === handle) {
    active.get(key)?.abort(); active.delete(key); nativeUploadFailureQueue.delete(key)
  }
  persist()
}
onAuthSessionCleared(() => {
  for (const controller of active.values()) controller.abort()
  active.clear(); nativeUploadFailureQueue.clear(); currentBinding = undefined
  try { localStorage.removeItem(storageKey); diskWritable = true } catch { diskWritable = false }
})
