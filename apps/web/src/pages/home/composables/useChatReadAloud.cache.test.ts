// @vitest-environment jsdom
import { createApp, h, nextTick, ref, type App } from 'vue'
import { useLocalStorage } from '@vueuse/core'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import type { ChatAssistantTurn, ChatMessage } from '@/store/chat/types'
import { useChatReadAloud } from './useChatReadAloud'
import { createTranscriptHistory } from '@/store/chat/transcript-history'

const api = vi.hoisted(() => ({ synthesize: vi.fn(), config: vi.fn() }))
vi.mock('@memohai/sdk', () => ({ postBotsByBotIdTtsPlayback: api.synthesize, getBotsByBotIdTtsPlaybackConfig: api.config }))
let app: App | undefined, root: HTMLDivElement | undefined
const players: (EventTarget & { src: string, play: ReturnType<typeof vi.fn> })[] = []
const tick = async () => { await nextTick(); for (let i = 0; i < 40; i++) await Promise.resolve(); await nextTick() }
const message = (id: string, content: string, streaming = true): ChatAssistantTurn => ({ id, turnId: id, runtimeRunId: 'run-' + id, role: 'assistant', streaming, timestamp: '', messages: [{ id: 0, type: 'text', content }] })
beforeEach(() => {
  localStorage.setItem('token', 'isolated-account-one'); players.length = 0
  api.synthesize.mockReset().mockImplementation(async () => ({ data: new ReadableStream({ start(c) { c.enqueue(new Uint8Array([1, 2, 3])); c.close() } }), response: new Response(null, { headers: { 'Content-Type': 'audio/wav', 'X-Speech-Playback-Identity': 'original-config' } }) }))
  api.config.mockReset().mockResolvedValue({ data: { identity: 'original-config' } })
  vi.stubGlobal('Audio', class extends EventTarget { src = ''; play = vi.fn(async () => {}); pause() {}; load() {}; constructor() { super(); players.push(this) } })
  vi.stubGlobal('URL', { createObjectURL: vi.fn(() => 'blob:cached-audio'), revokeObjectURL: vi.fn() })
})
afterEach(() => { app?.unmount(); app = undefined; root?.remove(); vi.unstubAllGlobals(); localStorage.clear() })
function mount() {
  const messages = ref<ChatMessage[]>([message('history', 'History.', false)]), scope = ref('bot:session'), draft = ref('')
  let speech!: ReturnType<typeof useChatReadAloud>
  root = document.createElement('div'); document.body.append(root)
  app = createApp({ setup() {
    speech = useChatReadAloud({ botId: ref('bot'), scope, modelId: ref('configured-model'), enabled: ref(true), loading: ref(false), messages, draft })
    return () => h('div')
  } }); app.mount(root)
  return { messages, scope, draft, get speech() { return speech } }
}
async function automatic(f: ReturnType<typeof mount>) {
  f.messages.value.push(message('live', '')); await tick()
  f.messages.value[1] = message('live', 'Fresh complete reply.', false); await tick()
  expect(f.speech.canReplay(f.messages.value[1]!)).toBe(true)
}
it('synthesizes an uncached reply on demand, then replays saved bytes after stop without another generation', async () => {
  const f = mount(); await tick()
  expect(f.speech.canReplay(f.messages.value[0]!)).toBe(true)
  await f.speech.replay(f.messages.value[0]!); await tick()
  expect(api.config).not.toHaveBeenCalled(); expect(api.synthesize).toHaveBeenCalledOnce()
  expect(api.synthesize.mock.calls[0]![0].body.text).toBe('History.')
  await automatic(f); f.speech.stop()
  await f.speech.replay(f.messages.value[1]!); await tick()
  expect(api.config).toHaveBeenCalledOnce(); expect(api.synthesize).toHaveBeenCalledTimes(2)
  expect(players[0]!.play).toHaveBeenCalledTimes(3)
})
it('re-synthesizes a cached reply when the server voice identity changes', async () => {
  const f = mount(); await tick(); await automatic(f); f.speech.stop()
  await f.speech.replay(f.messages.value[1]!); await tick()
  expect(api.synthesize).toHaveBeenCalledOnce()
  api.config.mockResolvedValueOnce({ data: { identity: 'changed-voice-format' } }); f.speech.stop()
  await f.speech.replay(f.messages.value[1]!); await tick()
  expect(api.synthesize).toHaveBeenCalledTimes(2)
  expect(api.synthesize.mock.calls[1]![0].body.text).toBe('Fresh complete reply.')
  expect(players[0]!.play).toHaveBeenCalledTimes(3)
  expect(f.speech.canReplay(f.messages.value[1]!)).toBe(true)
})
it('input/scope cancellation fences a late replay validation and a new account cannot inherit cached audio', async () => {
  const f = mount(); await tick(); await automatic(f); f.speech.stop()
  let complete!: (value: { data: { identity: string } }) => void
  api.config.mockImplementationOnce(() => new Promise(done => { complete = done }))
  const pending = f.speech.replay(f.messages.value[1]!)
  const signal = api.config.mock.calls.at(-1)![0].signal as AbortSignal
  f.draft.value = 'interrupt'; expect(signal.aborted).toBe(true)
  complete({ data: { identity: 'original-config' } }); await pending; await tick()
  expect(players[0]!.play).toHaveBeenCalledOnce(); expect(api.synthesize).toHaveBeenCalledOnce()
  f.draft.value = ''; f.scope.value = 'bot:other-session'; await tick()
  expect(f.speech.canReplay(f.messages.value[1]!)).toBe(true)
  f.scope.value = 'bot:session'; await tick()
  expect(f.speech.canReplay(f.messages.value[1]!)).toBe(true)
  const account = useLocalStorage('token', ''); account.value = 'isolated-account-two'; await tick()
  expect(f.speech.canReplay(f.messages.value[1]!)).toBe(true)
  await f.speech.replay(f.messages.value[1]!); await tick()
  expect(api.synthesize).toHaveBeenCalledTimes(2)
  expect(api.synthesize.mock.calls[1]![0].body.text).toBe('Fresh complete reply.')
})
it('keeps the generated run identity through the real settled-history reconciliation', async () => {
  const f = mount(); await tick(); await automatic(f); f.speech.stop()
  const history = createTranscriptHistory({ messages: f.messages.value, rememberBackgroundTask: task => task, applyPendingBackgroundEventsToTool() {} })
  history.replaceMessages([{ id: 'persisted-live', turn_id: 'live', role: 'assistant', timestamp: '', messages: [{ id: 0, type: 'text', content: 'Fresh complete reply.' }] }])
  await tick()
  expect(f.messages.value[0]!.id).toBe('live')
  expect(f.messages.value[0]!.serverId).toBe('persisted-live')
  expect(f.speech.canReplay(f.messages.value[0]!)).toBe(true)
  await f.speech.replay(f.messages.value[0]!); await tick()
  expect(api.synthesize).toHaveBeenCalledOnce(); expect(api.config).toHaveBeenCalledOnce()
})
it('a native direct token replacement cannot reuse another account cache even without a storage event', async () => {
  const f = mount(); await tick(); await automatic(f); f.speech.stop()
  localStorage.setItem('token', 'native-account-two')
  expect(f.speech.canReplay(f.messages.value[1]!)).toBe(true)
  await f.speech.replay(f.messages.value[1]!); await tick()
  expect(api.config).not.toHaveBeenCalled(); expect(api.synthesize).toHaveBeenCalledTimes(2)
  expect(api.synthesize.mock.calls[1]![0].body.text).toBe('Fresh complete reply.')
})

it('keeps sibling rows in one turn/run distinct for synthesis, cache and the single active bubble', async () => {
  const f = mount(); await tick()
  const old = { ...message('old-row', 'Old reply.', false), turnId: 'shared', runtimeRunId: 'shared-run' }
  f.messages.value = [old]; await tick()
  f.messages.value.push({ ...message('new-row', '', true), turnId: 'shared', runtimeRunId: 'shared-run' }); await tick()
  f.messages.value[1] = { ...message('new-row', 'Fresh complete reply.', false), turnId: 'shared', runtimeRunId: 'shared-run' }; await tick()
  expect(api.synthesize).toHaveBeenCalledOnce()
  expect(api.synthesize.mock.calls[0]![0].body.text).toBe('Fresh complete reply.')
  players[0]!.dispatchEvent(new Event('playing')); await tick()
  expect(f.speech.status(f.messages.value[0]!)).toBe('idle')
  expect(f.speech.status(f.messages.value[1]!)).toBe('playing')
  f.speech.stop()
  expect(f.speech.canReplay(f.messages.value[0]!)).toBe(true)
  expect(f.speech.canReplay(f.messages.value[1]!)).toBe(true)
  await f.speech.replay(f.messages.value[0]!); await tick()
  expect(api.synthesize).toHaveBeenCalledTimes(2)
  expect(api.synthesize.mock.calls[1]![0].body.text).toBe('Old reply.')
  await f.speech.replay(f.messages.value[1]!); await tick()
  expect(api.synthesize).toHaveBeenCalledTimes(2)
  expect(api.config).toHaveBeenCalledOnce()
})
