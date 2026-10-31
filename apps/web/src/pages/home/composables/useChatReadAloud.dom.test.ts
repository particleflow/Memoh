// @vitest-environment jsdom
import { createApp, h, nextTick, ref, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ChatMessage } from '@/store/chat/types'
const control = vi.hoisted(() => ({ enqueue: vi.fn(), replay: vi.fn(), stop: vi.fn(), replayAvailability: vi.fn() }))
// Test only new DOM/composable scheduling; the unchanged audio/controller chain
// retains its 2b9c8fc evidence and is not exercised again here.
vi.mock('./chat-read-aloud', async original => ({
 ...await original<typeof import('./chat-read-aloud')>(),
 ChatReadAloud: class { enqueue = control.enqueue; replay = control.replay; stop = control.stop; replayAvailability = control.replayAvailability; setLive() {} clearCache() {} resume() {} },
}))
import { useChatReadAloud } from './useChatReadAloud'
let app: App | undefined, root: HTMLElement, host: HTMLElement
beforeEach(() => { vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] }); control.enqueue.mockReset(); control.replay.mockReset(); control.stop.mockReset(); control.replayAvailability.mockReset().mockReturnValue('missing') })
afterEach(() => { app?.unmount(); app = undefined; root?.remove(); host?.remove(); vi.useRealTimers() })
const settle = async () => { for (let i = 0; i < 15; i++) { await Promise.resolve(); await nextTick() } }
function message(text: string, streaming = true): ChatMessage { return { id: 'm', turnId: 't', runtimeRunId: 'r', role: 'assistant', timestamp: '', streaming, messages: [{ id: 0, type: 'text', content: text }] } }
function mount() {
 root = document.createElement('div'); document.body.append(root)
 host = document.createElement('div'); document.body.append(host)
 const messages = ref<ChatMessage[]>([]), enabled = ref(true), draft = ref('')
 let speech!: ReturnType<typeof useChatReadAloud>
 app = createApp({ setup() { speech = useChatReadAloud({ botId: ref('bot'), scope: ref('scope'), enabled, loading: ref(false), messages, draft, bodyRoot: () => root, messageElement: () => root }); return () => h('div') } })
 app.mount(host)
 return { messages, enabled, draft, get speech() { return speech } }
}
function body(html: string) { root.innerHTML = `<div data-speech-body data-speech-block-id="0">${html}</div>` }
describe('new DOM projection lifecycle through actual composable', () => {
 it('reads actual DOM for manual replay and excludes source destinations and code controls', async () => {
  const f = mount(), m = message('[文档](https://hidden.test)\n\n```text\ncode\n```', false)
  body('<p><a href="https://hidden.test" title="hidden title">文档</a></p><pre>code</pre><button>复制</button>')
  f.messages.value.push(m); await settle()
  expect(control.enqueue).not.toHaveBeenCalled()
  await f.speech.replay(m)
  expect(control.enqueue).toHaveBeenCalledWith(expect.objectContaining({ key: 't', text: '文档 code' }))
  expect(control.replay).not.toHaveBeenCalled()
 })
 it('collects the delayed visible suffix after final source without another message mutation', async () => {
  const f = mount(); body('<p>开头。</p>')
  f.messages.value.push(message('开头。后文。')); await settle()
  expect(control.enqueue.mock.calls.map(call => call[0].text)).toEqual(['开头。'])
  f.messages.value[0] = message('开头。后文。', false); await settle()
  body('<p>开头。后文。</p>'); await settle(); await vi.advanceTimersByTimeAsync(80); await settle()
  expect(control.enqueue.mock.calls.map(call => call[0].text).join('')).toBe('开头。后文。')
  body('<p>开头。后文。</p><button>Copy</button>'); await settle(); await vi.advanceTimersByTimeAsync(80)
  expect(control.enqueue).toHaveBeenCalledTimes(2)
 })
 it('typing and hidden/visible transitions consume pending DOM so an old run cannot resume', async () => {
  const f = mount(); body('<p>开头。</p>'); f.messages.value.push(message('开头。后文。')); await settle()
  f.draft.value = '新问题'; expect(control.stop).toHaveBeenCalled()
  f.enabled.value = false; await settle(); f.draft.value = ''; f.enabled.value = true
  f.messages.value[0] = message('开头。后文。', false); body('<p>开头。后文。</p>')
  await settle(); await vi.advanceTimersByTimeAsync(80)
  expect(control.enqueue).toHaveBeenCalledOnce()
 })
 it('late historical DOM never becomes an automatic reply, and unmount detaches the observer', async () => {
  const f = mount(); f.messages.value.push(message('历史。', false)); await settle(); body('<p>历史。</p>')
  await settle(); await vi.advanceTimersByTimeAsync(80); expect(control.enqueue).not.toHaveBeenCalled()
  app!.unmount(); app = undefined
  body('<p>迟到。</p>'); await settle(); await vi.advanceTimersByTimeAsync(100)
  expect(control.enqueue).not.toHaveBeenCalled()
 })
})
