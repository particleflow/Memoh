// @vitest-environment jsdom
import { createApp, h, nextTick, reactive, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { ToolCallBlock } from '@/store/chat-list'
import en from '@/i18n/locales/en.json'
import zh from '@/i18n/locales/zh.json'
import ja from '@/i18n/locales/ja.json'

vi.mock('@/i18n', () => ({
  default: { global: { t: (key: string) => key } },
  i18nRef: (key: string) => ({ value: key }),
}))

// The real composable needs a pinia settings store and the shiki bundle; the
// DOM assertions only need the parsed diff rows, so feed the real parser's
// output through with the raw text as the (unhighlighted) row html.
vi.mock('@/composables/useShikiHighlighter', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/composables/useShikiHighlighter')>()
  return {
    ...actual,
    useShikiHighlighter: () => {
      const diffRows = ref<{ kind: string, lineNumber: number, html: string }[]>([])
      return {
        loading: ref(false),
        html: ref(''),
        diffRows,
        highlightContextDiff: async (diff: string) => {
          diffRows.value = actual.parseUnifiedDiffRows(diff).map((row) => ({ ...row, html: row.text }))
        },
        highlight: async () => {},
        highlightLang: async () => {},
        highlightDiff: async () => {},
        highlightLanguage: async () => {},
      }
    },
  }
})
import ToolCallInline from './tool-call-inline.vue'

const mounted: ReturnType<typeof createApp>[] = []
afterEach(() => { for (const app of mounted.splice(0)) app.unmount() })
let sequence = 0
function mountTool(name: string, input: Record<string, unknown>, result: unknown, inGroup = false, locale = 'en') {
  const id = ++sequence
  const block = reactive<ToolCallBlock>({
    id, type: 'tool', name, toolName: name, tool_call_id: `call-${id}`,
    toolCallId: `call-${id}`, input, result, running: false, done: true,
  })
  const root = document.createElement('div')
  const app = createApp({ render: () => h(ToolCallInline, { block, inGroup, messageId: `message-${id}` }) })
  app.use(createI18n({ legacy: false, locale, messages: { en, zh, ja } }))
  app.mount(root)
  mounted.push(app)
  return { root, block }
}
async function open(root: HTMLElement) {
  const header = root.querySelector<HTMLElement>('.tool-header-row')
  expect(header).not.toBeNull()
  header!.click()
  await nextTick()
  expect(header!.getAttribute('aria-expanded')).toBe('true')
  return header!
}

const failures = [
  { isError: true, content: [{ type: 'text', text: 'Delivery unavailable' }] },
  { structuredContent: { isError: true, content: [{ type: 'text', text: 'Delivery unavailable' }] } },
  { structuredContent: { isError: true, message: 'Delivery unavailable' } },
]
describe.each([false, true])('tool failure detail (inGroup=%s)', (inGroup) => {
  it.each(failures)('shows the search failure instead of an empty result: %j', async (result) => {
    const { root } = mountTool('web_search', { query: 'review' }, result, inGroup)
    expect(root.textContent).not.toContain('Delivery unavailable')
    const header = await open(root)
    expect(header.classList.contains('text-destructive')).toBe(false)
    expect(header.textContent).not.toContain('Delivery unavailable')
    expect(root.textContent).toContain('Delivery unavailable')
    expect(root.textContent).not.toContain('No results')
    expect(root.querySelector('.text-destructive')?.textContent).toContain('Delivery unavailable')
  })
  it('shows send failure together with the attempted input', async () => {
    const { root } = mountTool('send', { text: 'attempted message' }, failures[0], inGroup)
    await open(root)
    expect(root.textContent).toContain('attempted message')
    expect(root.textContent).toContain('Delivery unavailable')
  })
  it('lets an empty-content write expand when it failed', async () => {
    const { root } = mountTool('write', { path: '/data/empty.txt', content: '' }, failures[0], inGroup)
    await open(root)
    expect(root.textContent).toContain('Delivery unavailable')
  })
  it('lets an empty-content write expand to show its deletion diff', async () => {
    const { root, block } = mountTool('write', { path: '/data/f.txt', content: '' }, { ok: true }, inGroup)
    block.diff = '--- a/f.txt\n+++ b/f.txt\n@@ -1 +0,0 @@\n-old line\n'
    await nextTick()
    const header = await open(root)
    expect(header.textContent).toContain('-1')
    expect(root.textContent).toContain('old line')
  })
})

it.each([['en', 'Tool execution failed.'], ['zh', '工具执行失败。'], ['ja', 'ツールの実行に失敗しました。']])('localizes a failure without diagnostics (%s)', async (locale, text) => {
  const { root } = mountTool('web_search', { query: 'review' }, { isError: true }, false, locale)
  await open(root)
  expect(root.textContent).toContain(text)
})
// The backend emits "cancelled" (two l) while external runtimes may send the
// American spelling — both must hit the same localized label rather than
// falling through to the raw status.
it.each([
  ['cancelled', 'Canceled'],
  ['canceled', 'Canceled'],
  ['expired', 'Expired'],
])('localizes approval status %s', async (status, label) => {
  const { root, block } = mountTool('read', { path: '/f.txt' }, { ok: true })
  block.approval = { approval_id: `ap-${status}`, status }
  await nextTick()
  expect(root.textContent).toContain(label)
  expect(root.textContent).not.toContain(status)
})
it('switches an open specialized detail when the streamed result completes', async () => {
  const { root, block } = mountTool('web_search', { query: 'review' }, null)
  block.running = true; block.done = false
  await nextTick()
  await open(root)
  expect(root.textContent).toContain('No results')
  block.result = failures[0]; block.running = false; block.done = true
  await nextTick()
  expect(root.textContent).toContain('Delivery unavailable')
  expect(root.textContent).not.toContain('No results')
})
it('preserves successful search rendering even when its content mentions errors', async () => {
  const { root } = mountTool('web_search', { query: 'errors' }, { isError: false, results: [{ title: 'Error handling guide', url: 'https://example.com/guide' }] })
  await open(root)
  expect(root.querySelector('a')?.textContent).toContain('Error handling guide')
  expect(root.querySelector('.text-destructive')).toBeNull()
})
it.each([1, -1, 127])('keeps native exec exit %s and stderr in the exec detail', async (code) => {
  const { root } = mountTool('exec', { command: 'false' }, { exit_code: code, stdout: 'output', stderr: 'diagnostic' })
  const header = await open(root)
  expect(header.textContent).not.toContain('exit')
  expect(root.textContent).toContain(`exit ${code}`)
  expect(root.textContent).toContain('output')
  expect(root.querySelector('.text-destructive')?.textContent).toContain('diagnostic')
})
it('retains structured diagnostics alongside MCP error text', async () => {
  const { root } = mountTool('exec', { command: 'false' }, { isError: true, content: [{ type: 'text', text: 'Command failed' }], structuredContent: { exit_code: 1, stderr: 'diagnostic' } })
  await open(root)
  expect(root.textContent).toContain('Command failed')
  expect(root.textContent).toContain('"exit_code":1')
  expect(root.textContent).toContain('diagnostic')
})

it('shows Agenvo merged output and retained workspace without claiming separate streams', async () => {
  const { root } = mountTool('exec', { command: 'Write-Output ok' }, { state: 'completed', output: 'SYNTHETIC_WINDOWS_OK', exit_code: 0, cleanup_required: true, workspace_id: 'w-test' })
  await open(root)
  expect(root.textContent).toContain('SYNTHETIC_WINDOWS_OK')
  expect(root.textContent).toContain('combines stdout and error text')
  expect(root.textContent).toContain('w-test is retained')
  expect(root.textContent).not.toContain('No output')
})
it('shows an uncertain Agenvo result without claiming cancellation succeeded', async () => {
  const { root } = mountTool('exec', { command: 'Start-Sleep 30' }, { result: { state: 'unknown', cleanup_required: true, workspace_id: 'w-test' }, error: 'inspect before retrying' })
  await open(root)
  expect(root.textContent).toContain('outcome is unknown or rejected')
  expect(root.textContent).toContain('does not stop Windows execution')
  expect(root.textContent).not.toContain('No output')
})

// Fixed Images and native Media results carry a reference, not inline image bytes.
describe('generated image reference detail', () => {
  const hash = 'a'.repeat(64)
  const completed = { state: 'completed', content_hash: hash, media_type: 'image/png', size_bytes: 123, delivered: 'current_conversation' }

  it.each([completed, { structuredContent: completed }, { structured_content: completed }])('shows the returned attachment reference: %j', async (result) => {
    const { root } = mountTool('generate_image', { prompt: 'synthetic circle', size: '1024x1024' }, result)
    await open(root)
    expect(root.textContent).toContain('Image attachment returned to this conversation.')
    expect(root.textContent).toContain(hash)
    expect(root.textContent).not.toContain('No image')
    expect(root.textContent).not.toContain('1024x1024')
    // A hash alone does not supply a media authorization scope or preview URL.
    expect(root.querySelector('img')).toBeNull()
    expect(root.querySelector('a')).toBeNull()
  })

  it.each([{ ...completed, delivered: undefined }, { content_hash: hash, media_type: 'image/png' }])('shows a stored reference without claiming delivery: %j', async (result) => {
    const { root } = mountTool('generate_image', {}, result)
    await open(root)
    expect(root.textContent).toContain('Image saved.')
    expect(root.textContent).toContain(hash)
    expect(root.textContent).not.toContain('returned to this conversation')
  })

  it.each(failures)('preserves real tool errors: %j', async (result) => {
    const { root } = mountTool('generate_image', {}, result)
    await open(root)
    expect(root.textContent).toContain('Delivery unavailable')
    expect(root.textContent).not.toContain('Image saved')
    expect(root.textContent).not.toContain('No image')
  })

  it('keeps an unknown result unknown even if it contains a reference', async () => {
    const { root } = mountTool('generate_image', {}, { state: 'unknown', content_hash: hash, retryable: false })
    await open(root)
    expect(root.textContent).toContain('Image result is unconfirmed. Do not automatically generate again.')
    expect(root.textContent).not.toContain(hash)
    expect(root.textContent).not.toContain('Image saved')
  })

  it('distinguishes returned bytes from a confirmed stored attachment', async () => {
    const { root } = mountTool('generate_image', {}, { state: 'generated', storage_state: 'unconfirmed', retryable: false })
    await open(root)
    expect(root.textContent).toContain('Image bytes were returned, but file storage is unconfirmed. Do not generate again to retry delivery.')
    expect(root.textContent).not.toContain('Image saved')
  })

  it.each([
    ['en', 'Image result is unconfirmed. Do not automatically generate again.'],
    ['zh', '图像结果尚未确认，请勿自动重新生成。'],
    ['ja', '画像の結果は未確認です。自動で再生成しないでください。'],
  ])('keeps a finished call without a receipt unconfirmed (%s)', async (locale, text) => {
    const { root } = mountTool('generate_image', {}, null, false, locale)
    await open(root)
    expect(root.textContent).toContain(text)
    expect(root.textContent).not.toContain('Image saved')
    expect(root.querySelector('img')).toBeNull()
  })

  it.each([{}, { state: 'completed' }, { content_hash: 'invalid' }, { content: [{ type: 'text', text: 'Generated a 1536x1024 image successfully.' }] }])('does not infer an attachment from missing data or model prose: %j', async (result) => {
    const { root } = mountTool('generate_image', {}, result)
    await open(root)
    expect(root.textContent).toContain('No image preview or file reference was returned.')
    expect(root.textContent).not.toContain('Image saved')
    expect(root.textContent).not.toContain('1536x1024')
  })

  it.each([['zh', '图片附件已返回当前对话。'], ['ja', '画像添付がこの会話に返されました。']])('localizes the returned reference (%s)', async (locale, text) => {
    const { root } = mountTool('generate_image', {}, completed, false, locale)
    await open(root)
    expect(root.textContent).toContain(text)
    expect(root.textContent).toContain(hash)
  })

  it('preserves legacy inline image and path rendering', async () => {
    const { root } = mountTool('generate_image', {}, { content: [{ type: 'image', data: 'c3ludGhldGlj', mimeType: 'image/png' }], structuredContent: { path: '/synthetic/image.png' } })
    await open(root)
    expect(root.querySelector('img')?.getAttribute('src')).toBe('data:image/png;base64,c3ludGhldGlj')
    expect(root.textContent).toContain('/synthetic/image.png')
  })
})
