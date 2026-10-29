// @vitest-environment jsdom
import { createApp, h, nextTick, ref, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ContextMenu, ContextMenuTrigger, ContextMenuContent, ContextMenuItem } from '@felinic/ui'
import type { ChatMessage } from '@/store/chat/types'
import contract from '../../../../../../docs/testing/chat-read-aloud/streaming/resume-gesture/candidate-contract.json'
const api = vi.hoisted(() => ({ playback: vi.fn() }))
vi.mock('@memohai/sdk', () => ({ postBotsByBotIdTtsPlayback: api.playback }))
import { useChatReadAloud } from '@autoplay-reviewed-composable'

let trustedGesture = false
const players: Player[] = []
class Player extends EventTarget {
 src = ''
 play = vi.fn(() => trustedGesture ? Promise.resolve() : Promise.reject(Object.assign(new Error('controlled autoplay rejection'), { name: 'NotAllowedError' })))
 pause = vi.fn(); load = vi.fn()
 constructor() { super(); players.push(this) }
}
class MediaFixture extends EventTarget {
 static isTypeSupported() { return true }
 readyState = 'open'
 addSourceBuffer() { return Object.assign(new EventTarget(), { appendBuffer() { queueMicrotask(() => this.dispatchEvent(new Event('updateend'))) } }) }
 endOfStream() {}
}
let app: App | undefined, root: HTMLElement
const urls: string[] = [], revoke = vi.fn(), cancels: ReturnType<typeof vi.fn>[] = []
const settle = async () => { for (let i = 0; i < 20; i++) { await Promise.resolve(); await nextTick() } }
const message = (id = 'm', streaming = false): ChatMessage => ({ id, turnId: `turn-${id}`, runtimeRunId: `run-${id}`, role: 'assistant', timestamp: '', streaming, messages: [{ id: 0, type: 'text', content: `正文 ${id}。` }] })
beforeEach(() => {
 trustedGesture = false; players.length = 0; urls.length = 0; cancels.length = 0; revoke.mockClear()
 vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
 vi.stubGlobal('ResizeObserver', class { observe() {}; unobserve() {}; disconnect() {} })
 vi.stubGlobal('Audio', Player); vi.stubGlobal('MediaSource', MediaFixture)
 vi.stubGlobal('URL', { createObjectURL: (media: MediaFixture) => { const url = `blob:same-loaded-${urls.length}`; urls.push(url); queueMicrotask(() => media.dispatchEvent(new Event('sourceopen'))); return url }, revokeObjectURL: revoke })
 api.playback.mockReset().mockImplementation(async () => {
  const cancel = vi.fn(); cancels.push(cancel)
  return { data: new ReadableStream<Uint8Array>({ start(feed) { feed.enqueue(new Uint8Array([1, 2])) }, cancel }), response: { headers: new Headers({ 'Content-Type': 'audio/mpeg' }) } }
 })
})
afterEach(() => { app?.unmount(); app = undefined; root?.remove(); document.body.innerHTML = ''; vi.unstubAllGlobals(); vi.useRealTimers() })
function mount(streaming = false) {
 expect(contract.expressions).toEqual({ menuSelect: 'readAloud.replay(msg)', disabled: '!readAloud.canReplay(msg)', messageClick: 'readAloud.stop()' })
 // Compile only the verified exact candidate selection expression. Do not replace
 // the real menu's select timing with a direct synthetic resume-button callback.
 const select = new Function('readAloud', 'msg', contract.expressions.menuSelect) as (speech: ReturnType<typeof useChatReadAloud>, msg: ChatMessage) => void
 const messages = ref<ChatMessage[]>([message('m', streaming)]), draft = ref(''), enabled = ref(true)
 let speech!: ReturnType<typeof useChatReadAloud>
 root = document.createElement('div'); document.body.append(root)
 app = createApp({ setup() {
  speech = useChatReadAloud({ botId: ref('bot'), scope: ref('scope'), enabled, loading: ref(false), draft, messages })
  return () => h(ContextMenu, {}, { default: () => [
   h(ContextMenuTrigger, { asChild: true, disabled: !speech.canReplay(messages.value[0]!) }, { default: () => h('div', { 'data-bubble': '', onClickCapture: () => speech.stop() }, 'message') }),
   speech.canReplay(messages.value[0]!) ? h(ContextMenuContent, {}, { default: () => h(ContextMenuItem, { 'data-existing-replay': '', onSelect: () => select(speech, messages.value[0]!) }, { default: () => 'existing replay' }) }) : null,
  ] })
 } })
 app.mount(root)
 return { messages, draft, enabled, get speech() { return speech } }
}
async function blocked(f: ReturnType<typeof mount>) {
 await settle()
 if (!f.messages.value[0]!.streaming) f.speech.replay(f.messages.value[0]!)
 await settle()
 expect(f.speech.state.value.status).toBe('blocked'); expect(api.playback).toHaveBeenCalledOnce()
 return players[0]!
}
async function open(f: ReturnType<typeof mount>, mode: 'mouse' | 'touch') {
 const bubble = root.querySelector('[data-bubble]')!
 if (mode === 'mouse') bubble.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, button: 2, clientX: 10, clientY: 10 }))
 else {
  const event = new MouseEvent('pointerdown', { bubbles: true, cancelable: true, clientX: 10, clientY: 10 })
  Object.defineProperty(event, 'pointerType', { value: 'touch' }); Object.defineProperty(event, 'pointerId', { value: 1 })
  bubble.dispatchEvent(event); await settle(); await vi.advanceTimersByTimeAsync(701)
 }
 await settle()
 const item = document.querySelector('[data-existing-replay]')
 expect(item, `${mode} existing menu remains reachable`).not.toBeNull()
 return item!
}
describe('exact cleanup replay entry restores autoplay-blocked loaded audio', () => {
 it.each(['mouse', 'touch'] as const)('%s select calls play in the same gesture without another synthesis or URL', async mode => {
  const f = mount(), player = await blocked(f), sameURL = player.src
  const item = await open(f, mode), calls = player.play.mock.calls.length
  trustedGesture = true
  item.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
  expect(player.play).toHaveBeenCalledTimes(calls + 1) // Before the event handler yields.
  expect(api.playback).toHaveBeenCalledOnce(); expect(player.src).toBe(sameURL); expect(urls).toHaveLength(1); expect(revoke).not.toHaveBeenCalled()
  trustedGesture = false; await settle()
  player.dispatchEvent(new Event('playing')); expect(f.speech.state.value.status).toBe('playing')
 })
 it('makes the original menu reachable for the same blocked audio while reply text is still streaming', async () => {
  const f = mount(true), player = await blocked(f)
  const item = await open(f, 'mouse')
  trustedGesture = true; item.dispatchEvent(new MouseEvent('click', { bubbles: true })); trustedGesture = false
  expect(player.play).toHaveBeenCalledTimes(2); expect(api.playback).toHaveBeenCalledOnce()
 })
 it('retains the same source after a second rejection rather than retrying synthesis', async () => {
  const f = mount(), player = await blocked(f), source = player.src
  f.speech.replay(f.messages.value[0]!); await settle()
  expect(f.speech.state.value.status).toBe('blocked'); expect(player.src).toBe(source); expect(api.playback).toHaveBeenCalledOnce()
 })
 it.each(['click', 'typing'] as const)('%s consumes the interrupted stream and queue; a later media event cannot resume it', async action => {
  const f = mount(true), player = await blocked(f)
  f.messages.value[0] = { ...message('m', true), messages: [{ id: 0, type: 'text', content: '正文 m。排队的旧正文。' }] }; await settle()
  if (action === 'click') root.querySelector('[data-bubble]')!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  else f.draft.value = 'new input'
  expect(player.src).toBe(''); expect(f.speech.state.value.status).toBe('idle')
  player.dispatchEvent(new Event('playing')); player.dispatchEvent(new Event('ended'))
  f.draft.value = ''; f.messages.value[0] = message('m'); await settle(); await vi.advanceTimersByTimeAsync(100)
  expect(api.playback).toHaveBeenCalledOnce(); expect(f.speech.state.value.status).toBe('idle')
 })
 it('a different message is an explicit new replay, never a resume of another bubble', async () => {
  const f = mount(), player = await blocked(f)
  f.speech.replay(message('other')); await settle()
  expect(api.playback).toHaveBeenCalledTimes(2); expect(api.playback.mock.calls[1]![0].body.text).toBe('正文 other。'); expect(revoke).toHaveBeenCalledOnce(); expect(player.src).toBe(urls[1])
 })
})
