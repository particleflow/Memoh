// @vitest-environment jsdom
import { createApp, defineComponent, h, nextTick, ref, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ChatMessage } from '@/store/chat/types'
import { useChatReadAloud } from './useChatReadAloud'

const api = vi.hoisted(() => ({ playback: vi.fn() }))
vi.mock('@memohai/sdk', () => ({ postBotsByBotIdTtsPlayback: api.playback }))
class Player extends EventTarget {
  src = ''
  play = vi.fn(async () => {})
  pause = vi.fn()
  load = vi.fn()
}
const players: Player[] = []
const tick = async () => { await nextTick(); for (let i = 0; i < 10; i++) await Promise.resolve(); await nextTick() }
const reply = (streaming: boolean, id = 'one'): ChatMessage => ({ id, turnId: `turn-${id}`, role: 'assistant', timestamp: '', streaming, messages: [{ id: 0, type: 'text', content: '文字回复' }] })
let app: App | undefined, root: HTMLDivElement | undefined
beforeEach(() => {
  api.playback.mockReset().mockResolvedValue({ data: new Blob(['mock'], { type: 'audio/wav' }) })
  players.length = 0
  vi.stubGlobal('Audio', class extends Player { constructor() { super(); players.push(this) } })
  vi.stubGlobal('URL', { createObjectURL: vi.fn(() => 'blob:mock'), revokeObjectURL: vi.fn() })
})
afterEach(() => { app?.unmount(); root?.remove(); vi.unstubAllGlobals() })
function mount() {
  const messages = ref<ChatMessage[]>([]), draft = ref(''), enabled = ref(true), scope = ref('bot:session')
  let speech!: ReturnType<typeof useChatReadAloud>
  root = document.createElement('div'); document.body.append(root)
  app = createApp(defineComponent({ setup() {
    speech = useChatReadAloud({ botId: ref('bot'), scope, enabled, loading: ref(false), draft, messages })
    return () => h('div', [h('textarea', { onInput: (event: Event) => { speech.stop(); draft.value = (event.target as HTMLTextAreaElement).value } }), h('button', { onClick: speech.stop }, 'message')])
  } }))
  app.mount(root)
  return { messages, draft, enabled, scope, speech }
}

describe('read aloud composition and interruption', () => {
  it('uses the bot playback contract and stops immediately when input starts', async () => {
    const f = mount()
    f.messages.value = [reply(true)]; await tick(); f.messages.value = [reply(false)]; await tick()
    expect(api.playback).toHaveBeenCalledOnce()
    expect(api.playback.mock.calls[0]![0]).toMatchObject({ path: { bot_id: 'bot' }, body: { text: '文字回复' }, parseAs: 'blob', throwOnError: true })
    players[0]!.dispatchEvent(new Event('playing')); expect(f.speech.status(f.messages.value[0]!)).toBe('playing')
    const input = root!.querySelector('textarea')!; input.value = 'new'; input.dispatchEvent(new Event('input', { bubbles: true }))
    expect(players[0]!.src).toBe(''); expect(f.speech.state.value.status).toBe('idle')
    f.draft.value = ''; await tick(); expect(api.playback).toHaveBeenCalledOnce()
  })
  it('cancels pending synthesis on click and does not play a late response', async () => {
    let resolve!: (value: { data: Blob }) => void
    api.playback.mockImplementation(() => new Promise(done => { resolve = done }))
    const f = mount(); f.messages.value = [reply(true)]; await tick(); f.messages.value = [reply(false)]; await tick()
    const signal = api.playback.mock.calls[0]![0].signal as AbortSignal
    root!.querySelector('button')!.click(); expect(signal.aborted).toBe(true)
    resolve({ data: new Blob(['late'], { type: 'audio/wav' }) }); await tick()
    expect(players).toHaveLength(0); expect(f.speech.state.value.status).toBe('idle')
  })
  it('stops on pane/config disable, scope change and unmount; no catch-up on return', async () => {
    const f = mount(); f.messages.value = [reply(true)]; await tick(); f.messages.value = [reply(false)]; await tick()
    players[0]!.dispatchEvent(new Event('playing')); f.enabled.value = false
    expect(f.speech.state.value.status).toBe('idle'); expect(players[0]!.src).toBe('')
    f.enabled.value = true; await tick(); expect(api.playback).toHaveBeenCalledOnce()
    f.speech.replay(f.messages.value[0]!); await tick(); players[0]!.dispatchEvent(new Event('playing'))
    f.scope.value = 'bot:other'; expect(f.speech.state.value.status).toBe('idle')
    f.speech.replay(f.messages.value[0]!); await tick(); app!.unmount(); app = undefined
    expect(players[0]!.src).toBe('')
  })
  it('marks at most one assistant bubble even when a projection contains duplicate turn keys', async () => {
    const f = mount(); f.messages.value = [reply(true)]; await tick(); f.messages.value = [reply(false)]; await tick()
    players[0]!.dispatchEvent(new Event('playing'))
    f.messages.value.push({ ...reply(false), id: 'duplicate' }); await tick()
    expect(f.messages.value.filter(message => f.speech.status(message) === 'playing')).toHaveLength(1)
  })
})
