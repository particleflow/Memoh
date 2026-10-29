// @vitest-environment jsdom
import { createApp, nextTick, type Ref } from 'vue'
import { createPinia, defineStore } from 'pinia'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import paneSource from './components/chat-pane.vue?raw'
import type { ChatMessage } from '@/store/chat/types'
import type { TaskHistoryAnchor } from '@/utils/task-history-link'
import type { useChatReadAloud } from './composables/useChatReadAloud'
import Home from './index.vue'

class Player extends EventTarget { src = ''; play = vi.fn(async () => {}); pause = vi.fn(); load = vi.fn() }
const api = vi.hoisted(() => ({
  bot: vi.fn(), session: vi.fn(), lookup: vi.fn(), open: vi.fn(), select: vi.fn(), task: vi.fn(), playback: vi.fn(),
  speech: undefined as ReturnType<typeof useChatReadAloud> | undefined,
  messages: undefined as Ref<ChatMessage[]> | undefined,
  pane: undefined as { taskAnchor?: TaskHistoryAnchor } | undefined,
  player: undefined as Player | undefined,
}))
vi.mock('@/composables/api/useBot', () => ({ fetchBot: api.bot }))
vi.mock('@/composables/api/useChat', () => ({ fetchSession: api.session }))
vi.mock('@memohai/sdk', () => ({ getBotsByBotIdTasksByTaskTypeByRecordId: api.task, getBotsById: api.lookup, getBotsByBotIdAgents: vi.fn(), postBotsByBotIdTtsPlayback: api.playback }))
vi.mock('@felinic/ui', () => ({ PanePlaceholder: { template: '<div><slot /></div>' }, toast: { error: vi.fn() } }))
vi.mock('./components/chat-workspace.vue', async () => {
  const { computed, defineComponent, h, reactive, ref } = await import('vue')
  const { useRouter } = await import('vue-router')
  const { useChatReadAloud } = await import('./composables/useChatReadAloud')
  return { default: defineComponent({ setup() {
    const messages = ref<ChatMessage[]>([reply('old')])
    const props = reactive<{ taskAnchor?: TaskHistoryAnchor }>({ taskAnchor: oldAnchor() })
    const expression = paneSource.slice(paneSource.indexOf('const readAloud = useChatReadAloud({')).match(/enabled: (computed\([\s\S]*?\)),\n\s*loading:/)?.[1]
    if (!expression) throw new Error('Missing pane speech enable expression')
    const gate = new Function('computed', 'botSettings', 'isActive', 'isVisible', 'chatStore', 'router', 'props', 'taskRunAnchor', `return (${expression})`)
    const store = useChat()
    const enabled = gate(computed, ref({ tts_model_id: 'synthetic' }), ref(true), ref(true), store, useRouter(), props, { status: ref('located') }) as Ref<boolean>
    api.pane = props
    api.messages = messages
    api.speech = useChatReadAloud({ botId: computed(() => store.currentBotId), scope: computed(() => `${store.currentBotId}:history`), enabled, loading: ref(false), draft: ref(''), messages })
    return () => h('div', { 'data-chat': '' }, messages.value.map(message => h('div', { 'data-speech-state': api.speech!.status(message) }, message.id)))
  } }) }
})
vi.mock('./components/contact-choice-dialog.vue', () => ({ default: { render: () => null } }))
vi.mock('./composables/useComposerPrefillResolver', () => ({ useComposerPrefillResolver: vi.fn() }))
vi.mock('@/store/composer-prefill', () => ({ useComposerPrefillStore: () => ({ pending: null }) }))
vi.mock('@/store/workspace-tabs', () => ({ useWorkspaceTabsStore: () => ({ api: {}, openSessionChatPinned: api.open }) }))
const useChat = defineStore('read-aloud-home-admission', {
  state: () => ({ currentBotId: 'source', bots: [{ id: 'source', name: 'source' }, { id: 'expert', name: 'expert' }], activeSession: null, loadingChats: false, contactNavigationPending: false, contactOpenRequested: null as { botId: string, sessionId: string } | null }),
  actions: {
    async selectBot(botId: string, options?: { sessionId?: string }) {
      this.currentBotId = botId
      if (!options?.sessionId) this.contactNavigationPending = true
      await api.select(botId, options)
      if (!options?.sessionId) this.contactOpenRequested = { botId, sessionId: 'main' }
    },
    completeContactOpen(request: { botId: string, sessionId: string }) { if (this.contactOpenRequested === request) { this.contactOpenRequested = null; this.contactNavigationPending = false } },
    cancelContactChoice() {}, isSessionTentative() { return false },
  },
})
vi.mock('@/store/chat-list', () => ({ useChatStore: () => useChat() }))
function oldAnchor(): TaskHistoryAnchor { return { directoryBotId: 'source', taskType: 'native_agent', taskId: 'old', recordId: 'old', runId: 'old', turnId: 'old-turn' } }
function reply(id: string, streaming = false): ChatMessage { return { id, role: 'assistant', turnId: `${id}-turn`, runtimeRunId: id, timestamp: '', streaming, messages: [{ id: 0, type: 'text', content: `Reply ${id}` }] } }
function task(id: string, bot = 'source', session = 'history') { return { data: { task_type: 'native_agent', task_id: id, record_id: id, destination: { bot_id: bot, session_id: session, run_id: id, turn_id: `${id}-turn` } } } }
function url(id: string, bot = 'source', session = 'history') { return `/bot/${bot}?session=${session}&task_bot=source&task_type=native_agent&task_id=${id}&task_record=${id}&run=${id}` }
function deferred<T>() { let resolve!: (value: T) => void, reject!: (reason: unknown) => void; const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail }); return { promise, resolve, reject } }
let app: ReturnType<typeof createApp> | undefined, root: HTMLDivElement
async function settle() { for (let i = 0; i < 40; i++) { await Promise.resolve(); await nextTick() } }
async function mount() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', name: 'home', component: Home }, { path: '/bot/:botName', name: 'bot', component: Home }] })
  await router.push(url('old'))
  root = document.createElement('div'); document.body.append(root)
  app = createApp(Home); app.use(createPinia()); app.use(router)
  app.use(createI18n({ legacy: false, locale: 'en', messages: { en: {} }, missingWarn: false, fallbackWarn: false }))
  app.mount(root); await settle()
  expect(masked()).toBe(false)
  api.speech!.replay(api.messages!.value[0]!); await settle()
  api.player!.dispatchEvent(new Event('playing'))
  expect(api.speech!.state.value.status).toBe('playing')
  api.messages!.value.push(reply('queued', true)); await settle()
  api.messages!.value[1] = reply('queued'); await settle()
  return router
}
function masked() { return root.querySelector('[data-chat]')!.classList.contains('invisible') }
function stoppedBeforeNewAnchor() {
  expect(masked()).toBe(true)
  expect(api.open).toHaveBeenCalledTimes(1)
  expect(api.pane!.taskAnchor?.runId).toBe('old')
  expect(api.speech!.state.value.status).toBe('idle')
  expect(api.speech!.enabled.value).toBe(false)
  expect(api.player!.src).toBe(''); expect(api.player!.pause).toHaveBeenCalled()
  expect(root.querySelectorAll('[data-speech-state=playing]')).toHaveLength(0)
  expect(api.playback).toHaveBeenCalledOnce()
}
async function normalNewReply() {
  expect(masked()).toBe(false); expect(api.speech!.enabled.value).toBe(true)
  api.player!.dispatchEvent(new Event('ended')); await settle()
  expect(api.speech!.state.value.status).toBe('idle'); expect(api.playback).toHaveBeenCalledOnce()
  api.messages!.value.push(reply('primary-new', true)); await settle()
  api.messages!.value[api.messages!.value.length - 1] = reply('primary-new'); await settle()
  expect(api.playback).toHaveBeenCalledTimes(2)
  expect(api.playback.mock.calls[1]![0].body.text).toBe('Reply primary-new')
}
beforeEach(() => {
  api.bot.mockReset(); api.session.mockReset().mockImplementation(async (bot, session) => ({ id: session, bot_id: bot, title: session }))
  api.lookup.mockReset().mockImplementation(async ({ path }) => ({ data: { name: path.id } }))
  api.open.mockReset().mockImplementation((options: { taskAnchor?: TaskHistoryAnchor }) => { if (api.pane) api.pane.taskAnchor = options.taskAnchor })
  api.select.mockReset(); api.task.mockReset().mockResolvedValue(task('old'))
  api.playback.mockReset().mockResolvedValue({ data: new Blob(['synthetic'], { type: 'audio/wav' }) })
  api.pane = undefined
  vi.stubGlobal('Audio', class extends Player { constructor() { super(); api.player = this } })
  vi.stubGlobal('URL', { createObjectURL: () => 'blob:synthetic', revokeObjectURL: vi.fn() })
})
afterEach(() => { app?.unmount(); root?.remove(); app = undefined; vi.unstubAllGlobals() })

describe('actual Home URL admission stops speech before pane anchor delivery', () => {
  it.each([['same Bot/session, different run', 'source', 'history'], ['cross Bot/session', 'expert', 'other-history']])('%s: stops during authorization and never restores the old queue after admission', async (_label, bot, session) => {
    const router = await mount(), admission = deferred<ReturnType<typeof task>>()
    api.task.mockImplementationOnce(() => admission.promise)
    await router.push(url('new', bot, session)); await settle()
    stoppedBeforeNewAnchor()
    admission.resolve(task('new', bot, session)); await settle()
    expect(masked()).toBe(false); expect(api.pane!.taskAnchor?.runId).toBe('new')
    expect(api.speech!.state.value.status).toBe('idle'); expect(api.playback).toHaveBeenCalledOnce()
    await normalNewReply()
  })

  it.each(['denied', 'expired', 'wrong-run'] as const)('%s: stays silent under the error mask, then returns to the actual primary contact path', async reason => {
    const router = await mount(), admission = deferred<ReturnType<typeof task>>()
    api.task.mockImplementationOnce(() => admission.promise)
    await router.push(url('new')); await settle(); stoppedBeforeNewAnchor()
    if (reason === 'wrong-run') admission.resolve(task('another-run'))
    else admission.reject({ status: reason === 'denied' ? 403 : 404, code: reason === 'denied' ? 'http.forbidden' : 'http.not_found' })
    await settle(); stoppedBeforeNewAnchor()
    expect(api.speech!.canReplay(api.messages!.value[0]!)).toBe(false)
    const contact = deferred<void>(); api.select.mockImplementationOnce(() => contact.promise)
    await router.push('/bot/source'); await settle(); stoppedBeforeNewAnchor()
    contact.resolve(); await settle()
    expect(api.open).toHaveBeenLastCalledWith({ sessionId: 'main', explicitSelection: false, contactView: true })
    await normalNewReply()
  })

  it('ignores a late task admission after returning to the primary contact without restoring old audio', async () => {
    const router = await mount(), admission = deferred<ReturnType<typeof task>>()
    api.task.mockImplementationOnce(() => admission.promise)
    await router.push(url('new')); await settle(); stoppedBeforeNewAnchor()
    await router.push('/bot/source'); await settle()
    const opens = api.open.mock.calls.length
    admission.resolve(task('new')); await settle()
    expect(api.open).toHaveBeenCalledTimes(opens)
    expect(api.open).toHaveBeenLastCalledWith({ sessionId: 'main', explicitSelection: false, contactView: true })
    await normalNewReply()
  })

  it('cancels pending synthesis before anchor delivery and discards the response after returning to contact', async () => {
    const router = await mount(), synthesis = deferred<{ data: Blob }>(), admission = deferred<ReturnType<typeof task>>()
    api.playback.mockImplementationOnce(() => synthesis.promise)
    api.speech!.replay(api.messages!.value[0]!); await settle()
    const signal = api.playback.mock.calls[1]![0].signal as AbortSignal
    api.task.mockImplementationOnce(() => admission.promise)
    await router.push(url('new')); await settle()
    expect(api.pane!.taskAnchor?.runId).toBe('old'); expect(masked()).toBe(true)
    expect(signal.aborted).toBe(true); expect(api.speech!.state.value.status).toBe('idle')
    await router.push('/bot/source'); await settle()
    synthesis.resolve({ data: new Blob(['late'], { type: 'audio/wav' }) }); admission.resolve(task('new')); await settle()
    expect(api.player!.src).toBe(''); expect(api.speech!.state.value.status).toBe('idle')
    expect(api.playback).toHaveBeenCalledTimes(2)
  })

  it('stops the old pane when an invalid history URL is rejected before a task request starts', async () => {
    const router = await mount()
    await router.push('/bot/source?run=incomplete'); await settle()
    stoppedBeforeNewAnchor()
    expect(api.task).toHaveBeenCalledOnce()
    await router.push('/bot/source'); await settle()
    await normalNewReply()
  })
})
