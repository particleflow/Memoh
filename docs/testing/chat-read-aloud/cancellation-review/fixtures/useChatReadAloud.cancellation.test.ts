// @vitest-environment jsdom
import { createApp, computed, defineComponent, h, nextTick, reactive, ref, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import paneSource from '../components/chat-pane.vue?raw'
import type { ChatMessage } from '@/store/chat/types'
import type { TaskHistoryAnchor } from '@/utils/task-history-link'
import { useChatReadAloud } from './useChatReadAloud'
import { useTaskRunAnchor } from './useTaskRunAnchor'
import { revalidateTaskHistoryAnchor } from '@/utils/task-history-link'

const api = vi.hoisted(() => ({ playback: vi.fn(), task: vi.fn() }))
vi.mock('@memohai/sdk', () => ({ postBotsByBotIdTtsPlayback: api.playback, getBotsByBotIdTasksByTaskTypeByRecordId: api.task }))
class Player extends EventTarget {
  src = ''
  play = vi.fn(async () => {})
  pause = vi.fn()
  load = vi.fn()
}
const players: Player[] = []
const settle = async () => { await nextTick(); for (let i = 0; i < 12; i++) await Promise.resolve(); await nextTick() }
const reply = (id: string, streaming = false): ChatMessage => ({ id, turnId: `turn-${id}`, runtimeRunId: `run-${id}`, role: 'assistant', timestamp: '', streaming, messages: [{ id: 0, type: 'text', content: `Reply ${id}` }] })
const anchor = (id: string): TaskHistoryAnchor => ({ directoryBotId: 'bot', taskType: 'native_agent', taskId: `task-${id}`, recordId: `record-${id}`, runId: `run-${id}`, turnId: `turn-${id}` })
let app: App | undefined, root: HTMLDivElement | undefined
beforeEach(() => {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  api.task.mockReset().mockImplementation(async ({ path }) => task(path.record_id.slice(7)))
  api.playback.mockReset().mockResolvedValue({ data: new Blob(['synthetic'], { type: 'audio/wav' }) })
  players.length = 0
  vi.stubGlobal('Audio', class extends Player { constructor() { super(); players.push(this) } })
  vi.stubGlobal('URL', { createObjectURL: vi.fn(() => 'blob:synthetic'), revokeObjectURL: vi.fn() })
})
afterEach(() => { app?.unmount(); app = undefined; root?.remove(); vi.unstubAllGlobals(); vi.useRealTimers() })

function mount(taskAnchor: TaskHistoryAnchor | null = anchor('old')) {
  const props = reactive({ taskAnchor: taskAnchor ?? undefined, sessionId: 'same-session' })
  const messages = ref<ChatMessage[]>([reply('old'), reply('new')])
  const visible = ref(true)
  const validate = (value: TaskHistoryAnchor, signal: AbortSignal) => revalidateTaskHistoryAnchor(value, 'bot', props.sessionId, signal)
  const scope = ref('bot:same-session')
  let speech!: ReturnType<typeof useChatReadAloud>, locator!: ReturnType<typeof useTaskRunAnchor>
  root = document.createElement('div'); document.body.append(root)
  app = createApp(defineComponent({ setup() {
    locator = useTaskRunAnchor({ anchor: () => props.taskAnchor, session: () => props.sessionId, visible: () => visible.value, messages: () => messages.value, loading: () => false, loadingOlder: () => false, hasMore: () => false, validate, loadOlder: async () => 0, scroll: async () => true })
    // Execute the actual pane enable expression so a missing consumer gate
    // cannot be hidden by duplicating the intended expression in this fixture.
    const expression = paneSource.slice(paneSource.indexOf('const readAloud = useChatReadAloud({')).match(/enabled: (computed\([\s\S]*?\)),\n\s*loading:/)?.[1]
    if (!expression) throw new Error('Missing pane speech enable expression')
    const enabled = new Function('computed', 'botSettings', 'isActive', 'isVisible', 'chatStore', 'router', 'props', 'taskRunAnchor', `return (${expression})`)(computed, ref({ tts_model_id: 'configured-tts' }), ref(true), visible, reactive({ contactNavigationPending: false }), { currentRoute: ref({ name: 'bot' }) }, props, locator)
    speech = useChatReadAloud({ botId: ref('bot'), scope, enabled, loading: ref(false), draft: ref(''), messages })
    return () => h('div', messages.value.map(message => h('div', { 'data-speech-state': speech.status(message) }, message.id)))
  } }))
  app.mount(root)
  return { props, messages, visible, scope, get speech() { return speech }, get locator() { return locator } }
}
async function start(f: ReturnType<typeof mount>) {
  await settle(); f.speech.replay(f.messages.value[0]!); await settle()
  players[0]!.dispatchEvent(new Event('playing'))
  expect(f.speech.state.value.status).toBe('playing')
}

function task(id: string) { const a = anchor(id); return { data: { task_type: a.taskType, task_id: a.taskId, record_id: a.recordId, destination: { bot_id: 'bot', session_id: 'same-session', run_id: a.runId, turn_id: a.turnId } } } }
function deferred<T>() { let resolve!: (value: T) => void, reject!: (reason: unknown) => void; const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail }); return { promise, resolve, reject } }
async function fresh(f: ReturnType<typeof mount>) {
  f.messages.value.push(reply('fresh', true)); await settle()
  f.messages.value[f.messages.value.length - 1] = reply('fresh'); await settle()
  expect(api.playback).toHaveBeenCalledTimes(2)
  players.at(-1)!.dispatchEvent(new Event('playing'))
  expect(f.speech.status(f.messages.value.at(-1)!)).toBe('playing')
}
describe('read aloud and cancelable pane admission', () => {
  it('pane deadline stays unavailable and a late success never restores speech before returning to ordinary chat', async () => {
    const f = mount(), old = deferred<ReturnType<typeof task>>(); await start(f)
    api.task.mockImplementationOnce(() => old.promise)
    f.props.taskAnchor = anchor('waiting'); await settle()
    const signal = api.task.mock.calls.at(-1)![0].signal as AbortSignal
    expect(f.speech.state.value.status).toBe('idle')
    await vi.advanceTimersByTimeAsync(15000); await settle()
    expect(signal.aborted).toBe(true); expect(f.locator.status.value).toBe('unavailable')
    old.resolve(task('waiting')); await settle()
    expect(f.locator.status.value).toBe('unavailable'); expect(f.speech.state.value.status).toBe('idle')
    expect(players[0]!.src).toBe(''); expect(api.playback).toHaveBeenCalledOnce()
    f.props.taskAnchor = undefined; await settle(); await fresh(f)
  })

  it('a new selection can speak while old transport ignores abort; old rejection and deadline cannot stop the new bubble', async () => {
    const f = mount(), old = deferred<ReturnType<typeof task>>(); await start(f)
    api.task.mockImplementationOnce(() => old.promise)
    f.props.taskAnchor = anchor('waiting'); await settle()
    const signal = api.task.mock.calls.at(-1)![0].signal as AbortSignal
    f.props.taskAnchor = anchor('new'); await settle()
    expect(signal.aborted).toBe(true); expect(f.locator.status.value).toBe('located')
    expect(f.speech.state.value.status).toBe('idle'); expect(api.playback).toHaveBeenCalledOnce()
    await fresh(f)
    old.reject({ status: 403 }); await settle(); await vi.advanceTimersByTimeAsync(15001); await settle()
    expect(f.locator.status.value).toBe('located')
    expect(f.speech.status(f.messages.value.at(-1)!)).toBe('playing')
    expect(api.playback).toHaveBeenCalledTimes(2); expect(vi.getTimerCount()).toBe(0)
  })

  it.each(['hidden', 'disposed'] as const)('%s cancellation and late success never resurrect the old player', async boundary => {
    const f = mount(), old = deferred<ReturnType<typeof task>>(); await start(f)
    api.task.mockImplementationOnce(() => old.promise)
    f.props.taskAnchor = anchor('new'); await settle()
    const signal = api.task.mock.calls.at(-1)![0].signal as AbortSignal
    if (boundary === 'hidden') { f.visible.value = false; await settle(); f.visible.value = true; await settle(); expect(f.locator.status.value).toBe('located'); await fresh(f) }
    else { app!.unmount(); app = undefined }
    expect(signal.aborted).toBe(true)
    old.resolve(task('new')); await settle(); await vi.advanceTimersByTimeAsync(15001); await settle()
    expect(vi.getTimerCount()).toBe(0)
    if (boundary === 'hidden') { expect(f.speech.status(f.messages.value.at(-1)!)).toBe('playing'); expect(api.playback).toHaveBeenCalledTimes(2) }
    else { expect(players.every(player => player.src === '')).toBe(true); expect(f.speech.state.value.status).toBe('idle'); expect(api.playback).toHaveBeenCalledOnce() }
  })
})
