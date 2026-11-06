// @vitest-environment jsdom
import { createApp, computed, defineComponent, h, nextTick, reactive, ref, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import paneSource from '../components/chat-pane.vue?raw'
import type { ChatMessage } from '@/store/chat/types'
import type { TaskHistoryAnchor } from '@/utils/task-history-link'
import { useChatReadAloud } from './useChatReadAloud'
import { useTaskRunAnchor } from './useTaskRunAnchor'

const api = vi.hoisted(() => ({ playback: vi.fn() }))
vi.mock('@memohai/sdk', () => ({ postBotsByBotIdTtsPlayback: api.playback }))
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
  api.playback.mockReset().mockResolvedValue({ data: new Blob(['synthetic'], { type: 'audio/wav' }) })
  players.length = 0
  vi.stubGlobal('Audio', class extends Player { constructor() { super(); players.push(this) } })
  vi.stubGlobal('URL', { createObjectURL: vi.fn(() => 'blob:synthetic'), revokeObjectURL: vi.fn() })
})
afterEach(() => { app?.unmount(); app = undefined; root?.remove(); vi.unstubAllGlobals() })

function mount(taskAnchor: TaskHistoryAnchor | null = anchor('old')) {
  const props = reactive({ taskAnchor: taskAnchor ?? undefined, sessionId: 'same-session' })
  const messages = ref<ChatMessage[]>([reply('old'), reply('new')])
  const validate = vi.fn(async (_anchor: TaskHistoryAnchor) => {})
  const scope = ref('bot:same-session')
  let speech!: ReturnType<typeof useChatReadAloud>, locator!: ReturnType<typeof useTaskRunAnchor>
  root = document.createElement('div'); document.body.append(root)
  app = createApp(defineComponent({ setup() {
    locator = useTaskRunAnchor({ anchor: () => props.taskAnchor, session: () => props.sessionId, visible: () => true, messages: () => messages.value, loading: () => false, loadingOlder: () => false, hasMore: () => false, validate, loadOlder: async () => 0, scroll: async () => true })
    // Execute the actual pane enable expression so a missing consumer gate
    // cannot be hidden by duplicating the intended expression in this fixture.
    const expression = paneSource.slice(paneSource.indexOf('const readAloud = useChatReadAloud({')).match(/enabled: (computed\([\s\S]*?\)),\n\s*loading:/)?.[1]
    if (!expression) throw new Error('Missing pane speech enable expression')
    const enabled = new Function('computed', 'botSettings', 'isActive', 'isVisible', 'chatStore', 'router', 'props', 'taskRunAnchor', `return (${expression})`)(computed, ref({ tts_model_id: 'configured-tts' }), ref(true), ref(true), reactive({ contactNavigationPending: false }), { currentRoute: ref({ name: 'bot' }) }, props, locator)
    speech = useChatReadAloud({ botId: ref('bot'), scope, enabled, loading: ref(false), draft: ref(''), messages })
    return () => h('div', messages.value.map(message => h('div', { 'data-speech-state': speech.status(message) }, message.id)))
  } }))
  app.mount(root)
  return { props, messages, validate, scope, get speech() { return speech }, get locator() { return locator } }
}
async function start(f: ReturnType<typeof mount>) {
  await settle(); f.speech.replay(f.messages.value[0]!); await settle()
  players[0]!.dispatchEvent(new Event('playing'))
  expect(f.speech.state.value.status).toBe('playing')
}

describe('read aloud at exact historical run boundaries', () => {
  it('stops the old player and queue during same-session authorization; locating the new run never restores them', async () => {
    const f = mount(); await start(f)
    f.messages.value.push(reply('queued', true)); await settle()
    f.messages.value[2] = reply('queued'); await settle()
    let release!: () => void
    f.validate.mockImplementationOnce(() => new Promise<void>(resolve => { release = resolve }))
    f.props.taskAnchor = anchor('new'); await settle()
    expect(f.scope.value).toBe('bot:same-session')
    expect(f.locator.status.value).toBe('pending')
    expect(f.speech.state.value.status).toBe('idle')
    expect(players[0]!.src).toBe(''); expect(players[0]!.pause).toHaveBeenCalled()
    expect(f.speech.enabled.value).toBe(false)
    expect(root!.querySelectorAll('[data-speech-state=playing]')).toHaveLength(0)
    release(); await settle()
    expect(f.locator.status.value).toBe('located'); expect(f.speech.enabled.value).toBe(true)
    players[0]!.dispatchEvent(new Event('ended')); await settle()
    expect(api.playback).toHaveBeenCalledOnce(); expect(f.speech.state.value.status).toBe('idle')
    f.speech.replay(f.messages.value[1]!); await settle()
    expect(api.playback).toHaveBeenCalledTimes(2)
    expect(api.playback.mock.calls[1]![0].body.text).toBe('Reply new')
  })

  it('keeps unavailable history silent and disables replay while later live projection changes arrive', async () => {
    const f = mount(); await start(f)
    f.validate.mockRejectedValueOnce(new Error('unavailable'))
    f.props.taskAnchor = anchor('missing'); await settle()
    expect(f.locator.status.value).toBe('unavailable')
    expect(f.speech.state.value.status).toBe('idle'); expect(f.speech.enabled.value).toBe(false)
    expect(f.speech.canReplay(f.messages.value[0]!)).toBe(false)
    f.speech.replay(f.messages.value[0]!)
    f.messages.value.push(reply('late', true)); await settle()
    f.messages.value[2] = reply('late'); await settle()
    expect(api.playback).toHaveBeenCalledOnce()
    expect(root!.querySelectorAll('[data-speech-state=playing]')).toHaveLength(0)
  })

  it('aborts in-flight synthesis at the pending boundary and discards its late result after locating another run', async () => {
    const f = mount(); await settle()
    let complete!: (result: { data: Blob }) => void, release!: () => void
    api.playback.mockImplementationOnce(() => new Promise(resolve => { complete = resolve }))
    f.speech.replay(f.messages.value[0]!); await settle()
    const signal = api.playback.mock.calls[0]![0].signal as AbortSignal
    f.validate.mockImplementationOnce(() => new Promise<void>(resolve => { release = resolve }))
    f.props.taskAnchor = anchor('new'); await settle()
    expect(signal.aborted).toBe(true); expect(f.speech.state.value.status).toBe('idle')
    release(); await settle(); complete({ data: new Blob(['late'], { type: 'audio/wav' }) }); await settle()
    expect(players).toHaveLength(0); expect(api.playback).toHaveBeenCalledOnce()
    expect(f.speech.state.value.status).toBe('idle')
  })

  it('does not stop ordinary primary chat when no history anchor is selected', async () => {
    const f = mount(null)
    f.props.taskAnchor = undefined
    await settle()
    expect(f.locator.status.value).toBe('pending')
    f.messages.value.push(reply('primary', true)); await settle()
    f.messages.value[2] = reply('primary'); await settle()
    expect(api.playback).toHaveBeenCalledOnce()
    players[0]!.dispatchEvent(new Event('playing'))
    f.locator.status.value = 'unavailable'; await settle()
    expect(f.speech.enabled.value).toBe(true); expect(f.speech.state.value.status).toBe('playing')
    expect(players[0]!.pause).not.toHaveBeenCalled()
  })
})
