import { getCurrentInstance, onDeactivated, onScopeDispose, ref, toRaw } from 'vue'
import { getBotsByBotIdMediaLimits, postBotsByBotIdMedia } from '@memohai/sdk'
import type { ChatAttachment } from '@/composables/api/useChat'

const uploadedFiles = new WeakMap<File, Map<string, ChatAttachment>>()
const sourceFiles = new WeakMap<ChatAttachment, File>()

export class AttachmentUploadLimitError extends Error {
  readonly fileName: string
  readonly maxBytes: number

  constructor(fileName: string, maxBytes: number) {
    super('Attachment exceeds the server upload limit')
    this.fileName = fileName
    this.maxBytes = maxBytes
  }
}

export function uploadedAttachmentFile(attachment: ChatAttachment): File | undefined {
  return sourceFiles.get(toRaw(attachment))
}

// Fetch the authoritative policy before any bytes, then upload sequentially so
// selecting several large files doesn't multiply browser/server memory pressure.
// No chat/session operation can start until this whole batch resolves.
export async function uploadComposerAttachments(
  botId: string,
  files: File[],
  signal: AbortSignal,
): Promise<ChatAttachment[]> {
  signal.throwIfAborted()
  const { data: policy } = await getBotsByBotIdMediaLimits({
    path: { bot_id: botId }, signal, throwOnError: true,
  })
  signal.throwIfAborted()
  const limit = policy.max_upload_bytes
  if (!Number.isSafeInteger(limit) || limit <= 0) throw new Error('Invalid upload policy')
  for (const file of files) {
    if (file.size > limit) throw new AttachmentUploadLimitError(file.name, limit)
  }
  const attachments: ChatAttachment[] = []
  for (const file of files) {
    signal.throwIfAborted()
    let attachment = uploadedFiles.get(file)?.get(botId)
    if (!attachment) {
      // The generated SDK uses FormData with the File itself. Never encode the
      // file or put its contents into a WebSocket message.
      const { data } = await postBotsByBotIdMedia({
        path: { bot_id: botId }, body: { file }, signal, throwOnError: true,
      })
      signal.throwIfAborted()
      if (!/^[a-f0-9]{64}$/.test(data.content_hash) || data.size_bytes !== file.size) {
        throw new Error('Invalid upload receipt')
      }
      const mime = data.mime || file.type || 'application/octet-stream'
      attachment = {
        type: mime.startsWith('image/') ? 'image'
          : mime.startsWith('audio/') ? 'audio'
            : mime.startsWith('video/') ? 'video' : 'file',
        content_hash: data.content_hash,
        mime,
        name: file.name,
        size: data.size_bytes,
      }
      let byBot = uploadedFiles.get(file)
      if (!byBot) uploadedFiles.set(file, byBot = new Map())
      byBot.set(botId, attachment)
      // Keep retry's File without making it enumerable or serializable in chat.
      sourceFiles.set(attachment, file)
    }
    attachments.push(attachment)
  }
  signal.throwIfAborted()
  return attachments
}

export function useComposerUpload() {
  const uploading = ref(false)
  let active: AbortController | null = null

  function cancel() { active?.abort() }
  onScopeDispose(cancel)
  // Dock tabs are cached by KeepAlive: leaving a tab does not dispose its scope
  // or change its bot/session props, but must still stop an unfinished send.
  if (getCurrentInstance()) onDeactivated(cancel)

  async function upload(botId: string, files: File[]) {
    if (active) throw new Error('An attachment upload is already running')
    const controller = new AbortController()
    active = controller
    uploading.value = true
    try {
      return await uploadComposerAttachments(botId, files, controller.signal)
    } finally {
      if (active === controller) {
        active = null
        uploading.value = false
      }
    }
  }

  return { uploading, upload, cancel }
}
