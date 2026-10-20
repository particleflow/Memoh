import { effectScope, reactive } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getBotsByBotIdMediaLimits, postBotsByBotIdMedia } from '@memohai/sdk'
import {
  AttachmentUploadLimitError,
  uploadedAttachmentFile,
  uploadComposerAttachments,
  useComposerUpload,
} from './useComposerUpload'

vi.mock('@memohai/sdk', () => ({
  getBotsByBotIdMediaLimits: vi.fn(),
  postBotsByBotIdMedia: vi.fn(),
}))

const limits = vi.mocked(getBotsByBotIdMediaLimits)
const upload = vi.mocked(postBotsByBotIdMedia)
const hash = 'a'.repeat(64)

beforeEach(() => {
  vi.clearAllMocks()
  limits.mockResolvedValue({ data: { max_upload_bytes: 256 * 1024 * 1024 } } as never)
  upload.mockImplementation(async ({ body }) => ({
    data: { content_hash: hash, size_bytes: body!.file.size, mime: body!.file.type },
  }) as never)
})

describe('HTTP attachment pre-upload', () => {
  it('passes the original File to SDK, then sends only references and retains retry sources', async () => {
    const file = new File(['synthetic'], 'fixture.mp4', { type: 'video/mp4' })
    const arrayBuffer = vi.spyOn(file, 'arrayBuffer')
    const text = vi.spyOn(file, 'text')
    const files = await uploadComposerAttachments('bot-a', [file], new AbortController().signal)
    expect(upload).toHaveBeenCalledWith(expect.objectContaining({
      path: { bot_id: 'bot-a' }, body: { file }, throwOnError: true,
    }))
    expect(files).toEqual([{
      type: 'video', name: 'fixture.mp4', mime: 'video/mp4', size: 9, content_hash: hash,
    }])
    expect(JSON.stringify(files)).not.toMatch(/base64|synthetic|storage_key|path|url/)
    expect(arrayBuffer).not.toHaveBeenCalled()
    expect(text).not.toHaveBeenCalled()
    expect(uploadedAttachmentFile(files[0]!)).toBe(file)
    expect(uploadedAttachmentFile(reactive(files[0]!))).toBe(file)
    expect(await uploadComposerAttachments('bot-a', [file], new AbortController().signal)).toEqual(files)
    expect(upload).toHaveBeenCalledTimes(1)
    await uploadComposerAttachments('bot-b', [file], new AbortController().signal)
    expect(upload).toHaveBeenCalledTimes(2)
  })

  it('uses a configured limit above 200MiB rather than a fixed browser constant', async () => {
    const file = new File(['metadata-only unit fixture'], 'large.bin')
    Object.defineProperty(file, 'size', { value: 201 * 1024 * 1024 })
    await expect(uploadComposerAttachments('bot-a', [file], new AbortController().signal)).resolves.toHaveLength(1)
    expect(upload).toHaveBeenCalledTimes(1)
  })

  it('prechecks the entire batch before uploading or allowing a model send', async () => {
    limits.mockResolvedValue({ data: { max_upload_bytes: 3 } } as never)
    const send = vi.fn()
    const result = uploadComposerAttachments('bot-a', [
      new File(['ok'], 'small.txt'), new File(['over'], 'large.txt'),
    ], new AbortController().signal).then(send)
    await expect(result).rejects.toMatchObject(new AttachmentUploadLimitError('large.txt', 3))
    expect(upload).not.toHaveBeenCalled()
    expect(send).not.toHaveBeenCalled()
  })

  it.each([413, 403, 500])('does not invoke a model after HTTP %i, even if an earlier file succeeded', async (status) => {
    upload.mockResolvedValueOnce({ data: { content_hash: hash, size_bytes: 3, mime: 'text/plain' } } as never)
    upload.mockRejectedValueOnce({ code: status === 413 ? 'http.payload_too_large' : 'internal', status })
    const send = vi.fn()
    await expect(uploadComposerAttachments('bot-a', [
      new File(['one'], 'one.txt'), new File(['two'], 'two.txt'),
    ], new AbortController().signal).then(send)).rejects.toMatchObject({ status })
    expect(send).not.toHaveBeenCalled()
  })

  it('fails closed if the limit cannot be fetched, without a legacy base64 fallback', async () => {
    limits.mockRejectedValue({ code: 'http.not_found', status: 404 })
    await expect(uploadComposerAttachments('bot-a', [new File(['x'], 'x')], new AbortController().signal))
      .rejects.toMatchObject({ status: 404 })
    expect(upload).not.toHaveBeenCalled()
  })

  it('rejects an incomplete or malformed upload receipt before chat admission', async () => {
    upload.mockResolvedValue({ data: { content_hash: 'partial', size_bytes: 0 } } as never)
    const send = vi.fn()
    await expect(uploadComposerAttachments('bot-a', [new File(['x'], 'x')], new AbortController().signal).then(send))
      .rejects.toThrow('Invalid upload receipt')
    expect(send).not.toHaveBeenCalled()
  })

  it('cancellation wins even when a transport resolves late, and scope disposal cancels requests', async () => {
    let complete!: (value: never) => void
    upload.mockImplementation(() => new Promise<never>(resolve => { complete = resolve }))
    const scope = effectScope()
    const composer = scope.run(useComposerUpload)!
    const send = vi.fn()
    const result = composer.upload('bot-a', [new File(['x'], 'x')]).then(send)
    const rejected = expect(result).rejects.toMatchObject({ name: 'AbortError' })
    await vi.waitFor(() => expect(upload).toHaveBeenCalledTimes(1))
    expect(composer.uploading.value).toBe(true)
    await expect(composer.upload('bot-a', [])).rejects.toThrow('already running')
    const signal = upload.mock.calls[0]![0].signal!
    scope.stop()
    expect(signal.aborted).toBe(true)
    complete({ data: { content_hash: hash, size_bytes: 1, mime: 'text/plain' } } as never)
    await rejected
    expect(send).not.toHaveBeenCalled()
    expect(composer.uploading.value).toBe(false)
  })
})
