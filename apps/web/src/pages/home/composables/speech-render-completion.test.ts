// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest'
import type { MarkstreamRendererHandle, MarkstreamVirtualMetrics } from 'markstream-vue'
import type { ChatAssistantTurn } from '@/store/chat/types'
import { bindSpeechRenderer, speechRenderComplete } from './speech-render-completion'

const roots: HTMLElement[] = []
afterEach(() => roots.splice(0).forEach(root => root.remove()))
const metrics = (stable: boolean, final = true) => ({ stable, final, renderedCount: 1, nodeCount: 1 }) as MarkstreamVirtualMetrics
function fixture() {
  const root = document.createElement('div'); root.innerHTML = '<div data-speech-body data-speech-block-id="0">Visible final tail.</div>'; document.body.append(root); roots.push(root)
  const body = root.firstElementChild!, notify = vi.fn()
  let current = metrics(false), done!: (value: MarkstreamVirtualMetrics) => void
  const handle = { getVirtualMetrics: () => current, settle: vi.fn(() => new Promise<MarkstreamVirtualMetrics>(resolve => { done = resolve })) } as unknown as MarkstreamRendererHandle
  const message: ChatAssistantTurn = { id: 'row', role: 'assistant', streaming: false, timestamp: '', messages: [{ id: 0, type: 'text', content: 'Visible final tail.' }] }
  return { root, body, notify, handle, message, set current(value: MarkstreamVirtualMetrics) { current = value }, finish(value: MarkstreamVirtualMetrics) { done(value) } }
}
it('waits for real renderer settlement and rejects a stale source acknowledgement without inventing a quiet timer', async () => {
  const f = fixture(); bindSpeechRenderer(f.body, f.handle, 'Visible final tail.', true, f.notify)
  expect(speechRenderComplete(f.message, f.root)).toBe(false)
  expect(f.handle.settle).toHaveBeenCalledOnce()
  bindSpeechRenderer(f.body, f.handle, 'A replacement source.', true, f.notify)
  f.finish(metrics(true)); await Promise.resolve()
  expect(f.notify).not.toHaveBeenCalled()
  expect(speechRenderComplete(f.message, f.root)).toBe(false)
})
it('allows final source only after all renderer nodes settle and wakes the existing DOM observer once', async () => {
  const f = fixture(); bindSpeechRenderer(f.body, f.handle, 'Visible final tail.', true, f.notify)
  expect(speechRenderComplete(f.message, f.root)).toBe(false)
  f.finish(metrics(true)); await Promise.resolve()
  expect(speechRenderComplete(f.message, f.root)).toBe(true)
  expect(speechRenderComplete(f.message, f.root)).toBe(true)
  expect(f.notify).toHaveBeenCalledOnce()
})
it('never treats streaming or detached source as final, and an unstable settle result remains pending', async () => {
  const f = fixture(); bindSpeechRenderer(f.body, f.handle, 'Visible final tail.', false, f.notify)
  expect(speechRenderComplete(f.message, f.root)).toBe(false); expect(f.handle.settle).not.toHaveBeenCalled()
  bindSpeechRenderer(f.body, f.handle, 'Visible final tail.', true, f.notify)
  expect(speechRenderComplete(f.message, f.root)).toBe(false); f.finish(metrics(false)); await Promise.resolve()
  expect(f.notify).not.toHaveBeenCalled()
  f.current = metrics(true); expect(speechRenderComplete(f.message, f.root)).toBe(true)
  f.root.remove(); expect(speechRenderComplete(f.message, f.root)).toBe(false)
})
