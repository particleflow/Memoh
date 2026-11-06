// @vitest-environment jsdom
import { createApp, h, nextTick, reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import FilesPane from './files-pane.vue'
import en from '@/i18n/locales/en.json'

const api = vi.hoisted(() => ({ list: vi.fn(), error: vi.fn() }))
vi.mock('@memohai/sdk', () => ({
  getBotsByBotIdContainerFsList: api.list,
  postBotsByBotIdContainerFsUpload: vi.fn(), postBotsByBotIdContainerFsMkdir: vi.fn(),
  postBotsByBotIdContainerFsDelete: vi.fn(), postBotsByBotIdContainerFsRename: vi.fn(), postBotsByBotIdContainerFsWrite: vi.fn(),
}))
vi.mock('@felinic/ui', async importOriginal => ({ ...await importOriginal<object>(), toast: { error: api.error } }))
vi.mock('pinia', async importOriginal => ({ ...await importOriginal<object>(), storeToRefs: (await import('vue')).toRefs }))
const chat = reactive({ fsChangedAt: 0 })
const tabs = reactive({ activeId: null })
vi.mock('@/store/chat-list', () => ({ useChatStore: () => chat }))
vi.mock('@/store/workspace-tabs', () => ({ useWorkspaceTabsStore: () => tabs }))

const props = reactive({ botId: 'cloud-bot', active: false })
let app: ReturnType<typeof createApp> | undefined, root: HTMLDivElement
async function settled() { await nextTick(); await vi.advanceTimersByTimeAsync(5); await nextTick() }
async function mount() {
  root = document.createElement('div'); document.body.append(root)
  app = createApp({ render: () => h(FilesPane, props) })
  app.use(createI18n({ legacy: false, locale: 'en', messages: { en } })); app.mount(root)
  await settled()
}
beforeEach(() => {
  vi.useFakeTimers(); api.list.mockReset(); api.error.mockReset()
  props.botId = 'cloud-bot'; props.active = false; chat.fsChangedAt = 0
  api.list.mockResolvedValue({ data: { entries: [] } })
})
afterEach(() => { app?.unmount(); root?.remove(); vi.useRealTimers() })

describe('file tree reads belong to the visible Files panel', () => {
  it('does not fetch a hidden cloud file tree or emit its workspace failure into the chat/task list', async () => {
    api.list.mockRejectedValue({ status: 503, code: 'workspace.unreachable' })
    await mount(); chat.fsChangedAt++; props.botId = 'another-cloud-bot'; await settled()
    await vi.advanceTimersByTimeAsync(10000)
    expect(api.list).not.toHaveBeenCalled(); expect(api.error).not.toHaveBeenCalled()
  })
  it('reads the cloud Bot when Files is explicitly opened and retains its real workspace error', async () => {
    api.list.mockRejectedValue({ status: 503, code: 'workspace.unreachable' })
    await mount(); props.active = true; await settled(); await vi.advanceTimersByTimeAsync(1600)
    expect(api.list).toHaveBeenCalledTimes(3)
    expect(api.list).toHaveBeenCalledWith(expect.objectContaining({ path: { bot_id: 'cloud-bot' }, query: { path: '/data' } }))
    expect(api.error).toHaveBeenCalledWith(en.errors.workspace.unreachable)
  })
  it('does not retry or toast a delayed failure after leaving Files, and reads afresh on return', async () => {
    let reject!: (error: unknown) => void
    api.list.mockReturnValueOnce(new Promise((_resolve, fail) => { reject = fail }))
    await mount(); props.active = true; await settled(); expect(api.list).toHaveBeenCalledOnce()
    props.active = false; await settled()
    reject({ status: 503, code: 'workspace.unreachable' }); await settled(); await vi.advanceTimersByTimeAsync(1600)
    expect(api.list).toHaveBeenCalledOnce(); expect(api.error).not.toHaveBeenCalled()
    props.active = true; await settled(); expect(api.list).toHaveBeenCalledTimes(2)
    expect(api.error).not.toHaveBeenCalled()
  })
  it('does not carry a delayed failure from another Bot into the selected cloud Bot', async () => {
    let reject!: (error: unknown) => void
    api.list.mockReturnValueOnce(new Promise((_resolve, fail) => { reject = fail }))
    props.active = true; await mount()
    props.botId = 'second-cloud-bot'; await settled()
    reject({ status: 503, code: 'workspace.unreachable' }); await settled()
    expect(api.error).not.toHaveBeenCalled()
    expect(api.list).toHaveBeenLastCalledWith(expect.objectContaining({ path: { bot_id: 'second-cloud-bot' } }))
  })
})
