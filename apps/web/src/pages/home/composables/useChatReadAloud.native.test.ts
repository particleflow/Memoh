// @vitest-environment jsdom
import { createApp, h, nextTick, ref, type App } from 'vue'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import type { ChatAssistantTurn, ChatMessage } from '@/store/chat/types'
import { useChatReadAloud } from './useChatReadAloud'
const api = vi.hoisted(() => ({ config: vi.fn(), fixed: vi.fn(), start: vi.fn(), append: vi.fn() }))
vi.mock('@memohai/sdk', () => ({ getBotsByBotIdTtsPlaybackConfig: api.config, postBotsByBotIdTtsPlayback: api.fixed, postBotsByBotIdTtsPlaybackInput: api.start, putBotsByBotIdTtsPlaybackInput: api.append }))
let app: App | undefined, root: HTMLDivElement | undefined
const settle = async () => { for (let i = 0; i < 50; i++) { await Promise.resolve(); await nextTick() } }
const message = (streaming: boolean): ChatAssistantTurn => ({ id: 'row', turnId: 'turn', runtimeRunId: 'run', role: 'assistant', streaming, timestamp: '', messages: [{ id: 0, type: 'text', content: streaming ? '' : 'A complete new reply.' }] })
beforeEach(() => {
  localStorage.setItem('token', 'synthetic-account'); api.config.mockReset(); api.fixed.mockReset(); api.start.mockReset(); api.append.mockReset()
  api.fixed.mockResolvedValue({ data: new ReadableStream({ start(c) { c.enqueue(new Uint8Array([1])); c.close() } }), response: new Response(null, { headers: { 'Content-Type': 'audio/wav', 'X-Speech-Playback-Identity': 'configured' } }) })
  vi.stubGlobal('Audio', class extends EventTarget { src = ''; play = vi.fn(async () => {}); pause() {}; load() {} })
  vi.stubGlobal('URL', { createObjectURL: () => 'blob:fixed-compatibility', revokeObjectURL() {} })
  vi.stubGlobal('MediaSource', { isTypeSupported: (mime: string) => mime === 'audio/mpeg' })
})
afterEach(() => { app?.unmount(); app = undefined; root?.remove(); vi.unstubAllGlobals(); localStorage.clear() })
function mount() {
  const messages = ref<ChatMessage[]>([]), draft = ref('')
  let speech!: ReturnType<typeof useChatReadAloud>
  root = document.createElement('div'); document.body.append(root)
  app = createApp({ setup() { speech = useChatReadAloud({ nativeInput: true, botId: ref('bot'), scope: ref('bot:session'), enabled: ref(true), loading: ref(false), messages, draft }); return () => h('div') } }); app.mount(root)
  return { messages, draft, get speech() { return speech } }
}
function emit(f: ReturnType<typeof mount>) { f.messages.value = [message(true)]; f.messages.value = [message(false)] }
it('keeps a fresh automatic reply compatible with a backend that has no native capability route, without retrying any synthesis', async () => {
  api.config.mockImplementation(async options => { if (options.throwOnError) throw { status: 404 }; return { error: { status: 404 }, response: new Response(null, { status: 404 }) } })
  const f = mount(); emit(f); await settle()
  expect(api.config).toHaveBeenCalledOnce(); expect(api.fixed).toHaveBeenCalledOnce(); expect(api.start).not.toHaveBeenCalled(); expect(api.append).not.toHaveBeenCalled()
})
it('does not convert a denied capability read into an automatic paid request', async () => {
  api.config.mockImplementation(async options => { if (options.throwOnError) throw { status: 403 }; return { error: { status: 403 }, response: new Response(null, { status: 403 }) } })
  const f = mount(); emit(f); await settle()
  expect(api.fixed).not.toHaveBeenCalled(); expect(api.start).not.toHaveBeenCalled(); expect(f.speech.state.value.status).toBe('error')
})
it.each(['native', 'missing'])('fences a late %s capability response after reactive input and never starts its old task when the draft is cleared', async mode => {
  let complete!: (result: unknown) => void
  api.config.mockImplementation(() => new Promise(resolve => { complete = resolve }))
  const f = mount(); emit(f); await settle()
  const signal = api.config.mock.calls[0]![0].signal as AbortSignal
  f.draft.value = 'new input'; expect(signal.aborted).toBe(true)
  complete(mode === 'native' ? { data: { identity: 'configured', input_content_type: 'audio/mpeg' }, response: new Response(null, { status: 200 }) } : { error: { status: 404 }, response: new Response(null, { status: 404 }) }); await settle()
  f.draft.value = ''; f.messages.value = [message(false)]; await settle()
  expect(api.start).not.toHaveBeenCalled(); expect(api.fixed).not.toHaveBeenCalled(); expect(f.speech.state.value.status).toBe('idle')
})
