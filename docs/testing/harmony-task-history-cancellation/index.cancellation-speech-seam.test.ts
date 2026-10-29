// @vitest-environment jsdom
import { readFileSync } from 'node:fs'
import { createApp, nextTick } from 'vue'
import { createPinia, defineStore } from 'pinia'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import Home from './index.vue'

const api = vi.hoisted(() => ({ bot: vi.fn(), session: vi.fn(), lookup: vi.fn(), open: vi.fn(), select: vi.fn(), task: vi.fn(), playback: vi.fn(), speech: undefined as any, speechMessages: undefined as any, player: undefined as any }))
vi.mock('@/composables/api/useBot', () => ({ fetchBot: api.bot }))
vi.mock('@/composables/api/useChat', () => ({ fetchSession: api.session }))
vi.mock('@memohai/sdk', () => ({ getBotsByBotIdTasksByTaskTypeByRecordId: api.task, getBotsById: api.lookup, getBotsByBotIdAgents: vi.fn(), postBotsByBotIdTtsPlayback: api.playback }))
vi.mock('@felinic/ui', () => ({ PanePlaceholder: { template: '<div><slot /></div>' }, toast: { error: vi.fn() } }))
vi.mock('./components/chat-workspace.vue', async () => {
  const { computed, defineComponent, h, ref } = await import('vue')
  const { useRouter } = await import('vue-router')
  const { useChatReadAloud } = await import('./composables/useChatReadAloud')
  const source = readFileSync(new URL('./components/chat-pane.vue', import.meta.url), 'utf8')
  const expression = source.slice(source.indexOf('const readAloud = useChatReadAloud({')).match(/enabled: (computed\([\s\S]*?\)),\n\s*loading:/)?.[1]
  const gate = new Function('computed', 'botSettings', 'isActive', 'isVisible', 'chatStore', 'router', 'props', 'taskRunAnchor', `return (${expression})`)
  return { default: defineComponent({ setup() {
    const messages = ref([{ id: 'old-message', role: 'assistant', turnId: 'old-turn', runtimeRunId: 'old', timestamp: '', streaming: false, messages: [{ id: 1, type: 'text', content: 'synthetic old reply' }] }])
    const enabled = gate(computed, ref({ tts_model_id: 'synthetic' }), ref(true), ref(true), useChat(), useRouter(), { taskAnchor: { runId: 'old', turnId: 'old-turn' } }, { status: ref('located') })
    api.speech = useChatReadAloud({ botId: ref('source'), scope: ref('source:history'), enabled, loading: ref(false), draft: ref(''), messages: messages as any })
    api.speechMessages = messages
    return () => h('div', { 'data-chat': '' }, api.speech.state.value.status)
  } }) }
})
vi.mock('./components/contact-choice-dialog.vue', () => ({ default: { render: () => null } }))
vi.mock('./composables/useComposerPrefillResolver', () => ({ useComposerPrefillResolver: vi.fn() }))
vi.mock('@/store/composer-prefill', () => ({ useComposerPrefillStore: () => ({ pending: null }) }))
vi.mock('@/store/workspace-tabs', () => ({ useWorkspaceTabsStore: () => ({ api: {}, openSessionChatPinned: api.open }) }))
const useChat = defineStore('navigation-fixture', {
  state: () => ({ currentBotId: 'source', bots: [{ id: 'source', name: 'source' }, { id: 'expert', name: 'expert' }], activeSession: null, loadingChats: false, contactNavigationPending: false }),
  actions: {
    async selectBot(botId: string, options?: { sessionId?: string }) { this.currentBotId = botId; await api.select(botId, options) },
    async refreshBots() {}, cancelContactChoice() {}, isSessionTentative() { return false },
  },
})
vi.mock('@/store/chat-list', () => ({ useChatStore: () => useChat() }))
let app: ReturnType<typeof createApp> | undefined, root: HTMLDivElement
async function settle() { for (let i = 0; i < 40; i++) { await Promise.resolve(); await nextTick() } }
async function mount(path: string) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/bot/:botName', name: 'bot', component: Home }, { path: '/settings', name: 'settings', component: { render: () => null } }] })
  await router.push(path)
  root = document.createElement('div'); document.body.append(root)
  app = createApp(Home); app.use(createPinia()); app.use(router)
  app.use(createI18n({ legacy: false, locale: 'en', messages: { en: {} }, missingWarn: false, fallbackWarn: false }))
  app.mount(root)
  return router
}
beforeEach(() => {
  api.bot.mockReset(); api.session.mockReset().mockImplementation(async (bot, session) => ({ id: session, bot_id: bot, title: session }))
  api.lookup.mockReset().mockImplementation(async ({ path }) => ({ data: { name: path.id } }))
  api.open.mockReset(); api.select.mockReset(); api.task.mockReset(); api.playback.mockReset()
})
afterEach(() => { app?.unmount(); root?.remove(); app = undefined })


function query(id: string) { return '?session=history&task_bot=source&task_type=native_agent&task_id='+id+'&task_record='+id+'&run='+id }
function response(id: string) { return { data:{ task_type:'native_agent',task_id:id,record_id:id,destination:{bot_id:'source',session_id:'history',run_id:id,turn_id:id+'-turn'} } } }
async function playing() {
  class Player extends EventTarget { src=''; play=vi.fn(async()=>{}); pause=vi.fn(); load=vi.fn() }
  vi.stubGlobal('Audio', class extends Player { constructor() { super(); api.player=this } })
  vi.stubGlobal('URL', { createObjectURL:()=> 'blob:synthetic', revokeObjectURL:vi.fn() })
  api.playback.mockResolvedValue({ data:new Blob(['synthetic'], { type:'audio/wav' }) })
  api.task.mockResolvedValue(response('old'))
  const router=await mount('/bot/source'+query('old')); await settle()
  api.speech.replay(api.speechMessages.value[0]); await settle()
  api.player.dispatchEvent(new Event('playing')); expect(api.speech.state.value.status).toBe('playing')
  return router
}
it('keeps the combined Home speech mask stopped while a superseded identity read cannot block the next task', async () => {
  try {
    const router=await playing()
    let late!: (value: ReturnType<typeof response>) => void
    api.task.mockImplementationOnce(()=>new Promise(resolve=>{late=resolve})).mockImplementation(async ({path})=>response(path.record_id))
    await router.push('/bot/source'+query('pending')); await settle()
    const oldSignal=api.task.mock.calls.at(-1)![0].signal as AbortSignal
    expect(root.querySelector('[data-chat]')?.classList.contains('invisible')).toBe(true)
    expect(api.speech.state.value.status).toBe('idle')
    await router.push('/bot/source'+query('fresh')); await settle()
    expect(oldSignal.aborted).toBe(true)
    expect(api.open).toHaveBeenCalledTimes(2)
    expect(api.open.mock.calls.at(-1)![0].taskAnchor.runId).toBe('fresh')
    expect(root.querySelector('[data-chat]')?.classList.contains('invisible')).toBe(false)
    late(response('pending')); await settle()
    expect(api.open).toHaveBeenCalledTimes(2)
    expect(api.speech.state.value.status).toBe('idle')
    expect(api.playback).toHaveBeenCalledTimes(1)
  } finally { vi.unstubAllGlobals() }
})
it('cancels the masked task on leaving Home and a late denial cannot hide returned contact or restart speech', async () => {
  try {
    const router=await playing()
    let deny!: (error: unknown) => void
    api.task.mockImplementationOnce(()=>new Promise((_,reject)=>{deny=reject}))
    await router.push('/bot/source'+query('pending')); await settle()
    const oldSignal=api.task.mock.calls.at(-1)![0].signal as AbortSignal
    await router.push('/settings'); await settle()
    expect(oldSignal.aborted).toBe(true)
    await router.push('/bot/source'); await settle()
    deny({status:403}); await settle()
    expect(api.open).toHaveBeenCalledTimes(1)
    expect(root.querySelector('[data-chat]')?.classList.contains('invisible')).toBe(false)
    expect(api.speech.state.value.status).toBe('idle')
    expect(api.playback).toHaveBeenCalledTimes(1)
  } finally { vi.unstubAllGlobals() }
})
