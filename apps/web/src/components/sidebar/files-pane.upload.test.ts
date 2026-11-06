// @vitest-environment jsdom
import { createApp, h, nextTick, reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import FilesPane from './files-pane.vue'
import en from '@/i18n/locales/en.json'

const api = vi.hoisted(() => ({ list: vi.fn(), upload: vi.fn(), error: vi.fn(), success: vi.fn() }))
vi.mock('@memohai/sdk', () => ({
  getBotsByBotIdContainerFsList: api.list,
  postBotsByBotIdContainerFsUpload: api.upload,
  postBotsByBotIdContainerFsMkdir: vi.fn(), postBotsByBotIdContainerFsDelete: vi.fn(),
  postBotsByBotIdContainerFsRename: vi.fn(), postBotsByBotIdContainerFsWrite: vi.fn(),
}))
vi.mock('@felinic/ui', async importOriginal => ({ ...await importOriginal<object>(), toast: { error: api.error, success: api.success } }))
vi.mock('pinia', async importOriginal => ({ ...await importOriginal<object>(), storeToRefs: (await import('vue')).toRefs }))
const chat = reactive({ fsChangedAt: 0, markFsChanged() { chat.fsChangedAt++ } })
const tabs = reactive({ activeId: null })
vi.mock('@/store/chat-list', () => ({ useChatStore: () => chat }))
vi.mock('@/store/workspace-tabs', () => ({ useWorkspaceTabsStore: () => tabs }))

const props = reactive({ botId: 'original-bot', active: false, canWrite: true })
let app: ReturnType<typeof createApp> | undefined
let root: HTMLDivElement

async function settled() {
  await nextTick()
  await vi.advanceTimersByTimeAsync(5)
  await nextTick()
}

async function mount() {
  root = document.createElement('div')
  document.body.append(root)
  app = createApp({ render: () => h(FilesPane, props) })
  app.use(createI18n({ legacy: false, locale: 'en', messages: { en } }))
  app.mount(root)
  await settled()
  return root.querySelector<HTMLInputElement>('input[type="file"]:not([webkitdirectory])')!
}

function select(input: HTMLInputElement, files: File[]) {
  // Deliberate FileList-shaped jsdom fixture. Real native-selector FileList
  // observations remain separate from this compiled-component dispatch test.
  Object.defineProperty(input, 'files', { configurable: true, value: files })
  expect(input.files).toHaveLength(files.length)
  input.dispatchEvent(new Event('change', { bubbles: true }))
}

function threeFiles() {
  return [
    new File([new Uint8Array([0, 255, 3, 17])], 'first.bin', { type: 'application/octet-stream' }),
    new File(['second 雪'], 'second.txt', { type: 'text/plain' }),
    new File([new Uint8Array([5, 4, 3, 2, 1])], 'third.dat', { type: 'application/octet-stream' }),
  ]
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.clearAllMocks()
  api.upload.mockReset()
  api.upload.mockResolvedValue({ data: {} })
  api.list.mockResolvedValue({ data: { entries: [] } })
  props.botId = 'original-bot'
  props.active = false
  props.canWrite = true
  chat.fsChangedAt = 0
})
afterEach(() => { app?.unmount(); root?.remove(); vi.useRealTimers() })

describe('ordinary Files upload selector', () => {
  it('supports multiple loose files independently of the directory selector', async () => {
    const input = await mount()
    expect(input.multiple).toBe(true)
    expect(input.hasAttribute('webkitdirectory')).toBe(false)
    const folder = root.querySelector<HTMLInputElement>('input[webkitdirectory]')!
    expect(folder).not.toBe(input)
    expect(folder.multiple).toBe(true)
  })

  it('dispatches all three selected File objects once to their own paths', async () => {
    const input = await mount()
    const files = threeFiles()
    select(input, files)
    await settled()
    expect(api.upload).toHaveBeenCalledTimes(3)
    for (const file of files) {
      expect(api.upload).toHaveBeenCalledWith({
        path: { bot_id: 'original-bot' }, body: { path: `/data/${file.name}`, file }, throwOnError: true,
      })
    }
    expect(api.upload.mock.calls.map(([request]) => request.body.file)).toEqual(files)
    expect(api.success).toHaveBeenCalledOnce()
    expect(api.error).not.toHaveBeenCalled()
    expect(input.value).toBe('')
    expect(chat.fsChangedAt).toBe(1)
  })

  it('attempts the other files and reports partial failure when one upload fails', async () => {
    api.upload.mockImplementation(async ({ body }) => {
      if (body.file.name === 'second.txt') throw { status: 403, code: 'http.forbidden' }
      return { data: {} }
    })
    const input = await mount()
    select(input, threeFiles())
    await settled()
    expect(api.upload).toHaveBeenCalledTimes(3)
    expect(api.error).toHaveBeenCalledWith(en.bots.files.uploadPartialFailed.replace('{failed}', '1').replace('{total}', '3'))
    expect(api.success).not.toHaveBeenCalled()
    expect(input.disabled).toBe(false)
    expect(chat.fsChangedAt).toBe(1)
  })

  it('does not dispatch a read-only selection or an empty selection', async () => {
    props.canWrite = false
    const input = await mount()
    expect(input.disabled).toBe(true)
    select(input, threeFiles())
    await settled()
    props.canWrite = true
    await settled()
    select(input, [])
    await settled()
    expect(api.upload).not.toHaveBeenCalled()
    expect(api.success).not.toHaveBeenCalled()
  })

  it('keeps the busy gate and original Bot for queued work after switching Bots', async () => {
    const release: (() => void)[] = []
    api.upload.mockImplementation(() => new Promise(resolve => release.push(() => resolve({ data: {} }))))
    const input = await mount()
    const files = Array.from({ length: 6 }, (_, index) => new File([`body-${index}`], `file-${index}.txt`))
    select(input, files)
    await settled()
    expect(api.upload).toHaveBeenCalledTimes(4)
    expect(input.disabled).toBe(true)
    select(input, threeFiles())
    await settled()
    expect(api.upload).toHaveBeenCalledTimes(4)
    props.botId = 'different-bot'
    release.splice(0).forEach(finish => finish())
    await settled()
    expect(api.upload).toHaveBeenCalledTimes(6)
    for (const [request] of api.upload.mock.calls) expect(request.path.bot_id).toBe('original-bot')
    release.splice(0).forEach(finish => finish())
    await settled()
    expect(input.disabled).toBe(false)
    expect(api.success).toHaveBeenCalledOnce()
  })
})
