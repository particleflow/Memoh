import { getCurrentInstance, onDeactivated, onScopeDispose, ref, toRaw } from 'vue'
import { acknowledgeMedia, mediaIdentity, uploadMedia, uploadPolicy } from '@/lib/media-transfer'
import type { ChatAttachment } from '@/composables/api/useChat'

const attachmentJournals = new WeakMap<ChatAttachment, string>()
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

// Fetch authoritative policy and authenticated identity before admitting a batch.
// The durable implementation retains the original receipt across every retry.
export async function uploadComposerAttachments(
  botId: string,
  files: File[],
  signal: AbortSignal,
  draft = '',
): Promise<ChatAttachment[]> {
  const identity = await mediaIdentity(botId, signal)
  const policy = await uploadPolicy(identity, files, signal)
  for (const file of files) {
    if (file.size > policy.limit) throw new AttachmentUploadLimitError(file.name, policy.limit)
  }
  const attachments: ChatAttachment[] = []
  for (const file of files) {
    identity.check()
    const receipt = await uploadMedia(identity, file, draft, policy.chunk, signal)
    const mime = receipt.mime
    const attachment: ChatAttachment = {
      type: mime.startsWith('image/') ? 'image'
        : mime.startsWith('audio/') ? 'audio'
          : mime.startsWith('video/') ? 'video' : 'file',
      content_hash: receipt.hash,
      mime,
      name: file.name,
      size: receipt.size,
    }
    sourceFiles.set(attachment, file)
    attachmentJournals.set(attachment, receipt.key)
    attachments.push(attachment)
  }
  identity.check()
  return attachments
}

// Only call after the chat transport explicitly confirms the message. Never
// cancel/delete a server original when consuming browser-private recovery state.
export async function acknowledgeComposerAttachments(attachments: ChatAttachment[]) {
  await acknowledgeMedia(attachments.flatMap(a => {
    const key = attachmentJournals.get(toRaw(a))
    return key ? [key] : []
  }))
}

export function useComposerUpload() {
  const uploading = ref(false)
  let active: AbortController | null = null

  function cancel() { active?.abort() }
  onScopeDispose(cancel)
  // Dock tabs are cached by KeepAlive: leaving a tab does not dispose its scope
  // or change its bot/session props, but must still stop an unfinished send.
  if (getCurrentInstance()) onDeactivated(cancel)

  async function upload(botId: string, files: File[], draft = '') {
    if (active) throw new Error('An attachment upload is already running')
    const controller = new AbortController()
    active = controller
    uploading.value = true
    try {
      return await uploadComposerAttachments(botId, files, controller.signal, draft)
    } finally {
      if (active === controller) {
        active = null
        uploading.value = false
      }
    }
  }

  return { uploading, upload, cancel }
}
