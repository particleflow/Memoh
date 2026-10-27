// @vitest-environment jsdom
import { expect, it, vi } from 'vitest'
import { prepareAttachmentsForMessage } from './harmony-files'

it('does not bypass NAS references for a direct attachment without a producer callback', async () => {
  await expect(prepareAttachmentsForMessage('bot', [{ type: 'file', name: 'inline-only' }], undefined, new AbortController().signal)).rejects.toThrow()
})
it('runs the existing producer once and returns its same ready reference', async () => {
  const references = [{ type: 'file', content_hash: 'a'.repeat(64), size: 5 }]
  const prepare = vi.fn().mockResolvedValue(references)
  const signal = new AbortController().signal
  expect(await prepareAttachmentsForMessage('bot', undefined, prepare, signal)).toBe(references)
  expect(prepare).toHaveBeenCalledExactlyOnceWith(signal)
})
it('keeps cancellation after preparation and the original abort cause', async () => {
  const controller = new AbortController()
  const prepare = vi.fn(async () => { controller.abort(); return [] })
  await expect(prepareAttachmentsForMessage('bot', undefined, prepare, controller.signal)).rejects.toMatchObject({ message: '附件传输已取消', cause: { name: 'AbortError' } })
  expect(prepare).toHaveBeenCalledOnce()
})
