import { computed, onScopeDispose, ref, shallowRef, watch, type Ref } from 'vue'
import { headBotsByBotIdSessionsBySessionIdDelegationsByDelegationIdAttachmentsByContentHash, type BotdelegationView } from '@memohai/sdk'
import { sdkApiUrl, sdkAuthQuery } from '@/lib/api-client'
import { apiErrorStatus } from '@/utils/api-error'
import type { TaskScope } from './useSessionTasks'
import type { useBotDelegations } from './useBotDelegations'

type Attachment = BotdelegationView['attachments'][number]
// Explicit host capability: observation-only hosts do not get download actions.
export interface BotDelegationDownloadAction {
  pending: boolean
  error?: unknown
  available: (attachment: Attachment) => boolean
  download: (attachment: Attachment) => Promise<void>
}

export function useBotDelegationDownload(scope: Ref<TaskScope | null>, visible: Ref<boolean>, reader: Pick<ReturnType<typeof useBotDelegations>, 'detail' | 'selected' | 'error'>) {
  const pending = ref(false)
  const error = shallowRef<unknown>()
  let generation = 0
  let controller: AbortController | undefined

  function pathFor(attachment: Attachment) {
    const current = scope.value
    const task = reader.detail.value
    if (!current || !visible.value || reader.error.value || !task || task.state !== 'completed'
      || task.source_session_id !== current.session || task.delegation_id !== reader.selected.value
      || !/^[0-9a-f]{64}$/.test(attachment.content_hash ?? '')
      || !task.attachments.some(a => a.content_hash === attachment.content_hash && a.url === attachment.url)) return
    const path = { bot_id: current.bot, session_id: current.session, delegation_id: task.delegation_id, content_hash: attachment.content_hash! }
    const expected = `/bots/${path.bot_id}/sessions/${path.session_id}/delegations/${path.delegation_id}/attachments/${path.content_hash}`
    // Use only the returned, exact scope-bound reference. No Bot/hash fallback.
    if (attachment.url !== expected) return
    return { path, url: expected }
  }
  function invalidate() {
    generation++
    controller?.abort()
    controller = undefined
    pending.value = false
    error.value = undefined
  }
  function available(a: Attachment) {
    return !!pathFor(a) && ![401, 403, 404].includes(apiErrorStatus(error.value) ?? 0)
  }
  async function download(a: Attachment) {
    if (pending.value || !available(a)) return
    const target = pathFor(a)
    if (!target) return
    const epoch = generation
    const requestController = new AbortController()
    controller = requestController
    pending.value = true
    error.value = undefined
    const timeout = setTimeout(() => requestController.abort(), 15000)
    try {
      // Metadata-only authorization check gives the view a localized failure.
      // The browser's subsequent GET rechecks authorization and streams bytes;
      // no whole-file Blob, cached token or success claim in this component.
      await headBotsByBotIdSessionsBySessionIdDelegationsByDelegationIdAttachmentsByContentHash({
        path: target.path, signal: requestController.signal, cache: 'no-store', throwOnError: true,
      })
      if (epoch !== generation || !pathFor(a)) return
      const link = document.createElement('a')
      link.href = sdkApiUrl({ url: target.url, query: sdkAuthQuery() })
      link.download = a.content_hash!
      link.click()
    } catch (cause) {
      if (epoch === generation) error.value = cause
    } finally {
      clearTimeout(timeout)
      if (controller === requestController) controller = undefined
      if (epoch === generation) pending.value = false
    }
  }
  watch([scope, visible, reader.selected], invalidate, { flush: 'sync' })
  // A denied parent read retires an in-flight download preparation too.
  watch(reader.error, cause => { if (cause) invalidate() }, { flush: 'sync' })
  onScopeDispose(invalidate)
  return computed<BotDelegationDownloadAction>(() => ({ pending: pending.value, error: error.value, available, download }))
}
