import { effectScope, reactive } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AttachmentUploadLimitError, acknowledgeComposerAttachments, uploadedAttachmentFile, uploadComposerAttachments, useComposerUpload } from './useComposerUpload'

const api = vi.hoisted(() => ({ identity: vi.fn(), policy: vi.fn(), upload: vi.fn(), ack: vi.fn() }))
vi.mock('@/lib/media-transfer', () => ({ mediaIdentity: api.identity, uploadPolicy: api.policy, uploadMedia: api.upload, acknowledgeMedia: api.ack }))
const hash = 'a'.repeat(64)
beforeEach(() => {
  vi.clearAllMocks()
  api.identity.mockImplementation(async (bot: string, signal: AbortSignal) => ({ bot, check: () => signal.throwIfAborted() }))
  api.policy.mockResolvedValue({ limit: 256 * 1024 * 1024, chunk: 1048576 })
  api.upload.mockImplementation(async (_, file: File) => ({ key: file.name, hash, size: file.size, mime: file.type }))
})

describe('durable composer admission', () => {
  it('passes original File and draft scope, retains retry source, sends references only', async () => {
    const file = new File(['synthetic'], 'fixture.mp4', { type: 'video/mp4' })
    const result = await uploadComposerAttachments('bot', [file], new AbortController().signal, 'session-draft')
    expect(api.upload).toHaveBeenCalledWith(expect.objectContaining({ bot: 'bot' }), file, 'session-draft', 1048576, expect.any(AbortSignal))
    expect(result).toEqual([{ type: 'video', name: file.name, mime: file.type, size: 9, content_hash: hash }])
    expect(JSON.stringify(result)).not.toMatch(/base64|synthetic|storage_key|receipt|url/)
    expect(uploadedAttachmentFile(reactive(result[0]!))).toBe(file)
    await acknowledgeComposerAttachments(result)
    expect(api.ack).toHaveBeenCalledWith([file.name])
  })
  it('prechecks all files against the configured policy before any upload or model send', async () => {
    api.policy.mockResolvedValue({ limit: 3, chunk: 0 })
    const send = vi.fn()
    await expect(uploadComposerAttachments('bot', [new File(['ok'], 'one'), new File(['over'], 'two')], new AbortController().signal).then(send))
      .rejects.toMatchObject(new AttachmentUploadLimitError('two', 3))
    expect(api.upload).not.toHaveBeenCalled(); expect(send).not.toHaveBeenCalled()
  })
  it.each([413, 403, 500])('blocks model admission after a partial batch and HTTP %i', async status => {
    api.upload.mockRejectedValueOnce({ status })
    const send = vi.fn()
    await expect(uploadComposerAttachments('bot', [new File(['x'], 'one')], new AbortController().signal).then(send)).rejects.toMatchObject({ status })
    expect(send).not.toHaveBeenCalled(); expect(api.ack).not.toHaveBeenCalled()
  })
  it('does not clear journals on transport uncertainty', async () => {
    api.upload.mockRejectedValueOnce(new TypeError('lost response'))
    await expect(uploadComposerAttachments('bot', [new File(['x'], 'one')], new AbortController().signal)).rejects.toThrow()
    expect(api.ack).not.toHaveBeenCalled()
  })
  it('scope disposal stops a late upload result before chat admission', async () => {
    let finish!: (value: unknown) => void
    api.upload.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const scope = effectScope(); const composer = scope.run(useComposerUpload)!
    const send = vi.fn()
    const pending = composer.upload('bot', [new File(['x'], 'one')]).then(send)
    const rejected = expect(pending).rejects.toMatchObject({ name: 'AbortError' })
    await vi.waitFor(() => expect(api.upload).toHaveBeenCalledOnce())
    await expect(composer.upload('bot', [])).rejects.toThrow('already running')
    scope.stop(); finish({ key: 'one', hash, size: 1, mime: 'text/plain' })
    await rejected; expect(send).not.toHaveBeenCalled(); expect(composer.uploading.value).toBe(false)
  })
})
