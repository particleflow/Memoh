// @vitest-environment jsdom
import { createApp, h, nextTick, ref, type App } from 'vue'
import { createPinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it, vi } from 'vitest'
import MarkdownRender, { enableKatex } from 'markstream-vue'
import { registerSharedMarkdownComponents } from '@/components/markdown'
import ChatCodeBlock from '../components/chat-code-block.vue'
import { renderToString } from 'katex'
import { withLiteralSingleDollars } from '@/utils/markdown-dollar'
import { renderedSpeechText } from './speech-rendered-text'
import { LiveSpeechReplies } from './chat-read-aloud'
import { normalizeSpeechText } from './speech-rendered-text'
import type { ChatMessage } from '@/store/chat/types'

// Block only external icon/workspace side effects; every renderer is the real product component.
vi.mock('@/composables/useWorkspaceLink', () => ({ useWorkspaceLink: () => () => {} }))
vi.mock('@/composables/useSiteIcon', () => ({ useSiteIcon: () => ({ ready: false, mask: '', usable: false }) }))
enableKatex()
registerSharedMarkdownComponents('speech-renderer-delta', { code_block: ChatCodeBlock, shell: ChatCodeBlock })
const apps: App[] = [], roots: HTMLElement[] = []
afterEach(() => { apps.splice(0).forEach(app => app.unmount()); roots.splice(0).forEach(root => root.remove()) })
const settle = async () => { for (let i = 0; i < 20; i++) { await Promise.resolve(); await nextTick() } }
function message(source: string | string[], streaming = false): ChatMessage { return { id: 'm', turnId: 't', runtimeRunId: 'r', role: 'assistant', timestamp: '', streaming, messages: (Array.isArray(source) ? source : [source]).map((content, id) => ({ id, type: 'text', content })) } }
async function actualRenderer(source: string | string[]) {
 const current = ref(Array.isArray(source) ? source : [source])
 const root = document.createElement('div'); document.body.append(root); roots.push(root)
 const app = createApp({ render: () => h('div', current.value.map((content, index) => h('div', { 'data-speech-body': '', 'data-speech-block-id': String(index) }, [h(MarkdownRender, { content, customId: 'speech-renderer-delta', customMarkdownIt: withLiteralSingleDollars, smoothStreaming: false, typewriter: false, batchRendering: false, showTooltips: false })]))) })
 app.use(createI18n({ legacy: false, locale: 'en', messages: { en: { common: { copy: 'Copy', copied: 'Copied', copyFailed: 'Copy failed' } } } }))
 app.use(createPinia()); apps.push(app); app.mount(root); await settle(); return { root, async update(source: string | string[]) { current.value = Array.isArray(source) ? source : [source]; await settle() } }
}
describe('actual product renderer speech semantics delta', () => {
 it('keeps complete rendered text coverage across per-character emphasis reclassification and final tails', async () => {
  for (const source of ['**重要。**后续也必须朗读。', '**重要。** 后续🙂。请看[文档](https://hidden.test)，价格3.14。最后一句。']) {
   const renderer = await actualRenderer(''), live = new LiveSpeechReplies(), spoken: string[] = []
   const project = (m: ChatMessage) => renderedSpeechText(m, renderer.root)
   let prefix = ''
   for (const point of Array.from(source)) {
    prefix += point; await renderer.update(prefix)
    spoken.push(...live.update([message(prefix, true)], true, false, false, project).map(entry => entry.text))
   }
   spoken.push(...live.update([message(source, false)], true, false, false, project).map(entry => entry.text))
   expect(normalizeSpeechText(spoken.join(''))).toBe(project(message(source)))
   expect(spoken.join('')).not.toContain('hidden.test')
  }
 })
 it('uses actual product code/text/footnote components without reading code chrome or hidden link destinations', async () => {
  const source = '# 标题\n请看[文档](https://hidden.test/target) 与 `x < 2`。\n\n```text\nconst x = 3\n```\n\n|甲|乙|\n|-|-|\n|1|2|\n\n> 引用正文\n\n注释[^1]\n\n[^1]: 脚注正文'
  const { root } = await actualRenderer(source)
  const text = renderedSpeechText(message(source), root)
  expect(text).toContain('文档'); expect(text).toContain('const x = 3'); expect(text).toContain('甲 乙'); expect(text).toContain('1 2'); expect(text).toContain('引用正文'); expect(text).toContain('注释1'); expect(text).toContain('脚注正文')
  expect(text).not.toMatch(/hidden.test|Copy|```|\[\^|href|title=/u)
 })
 it('keeps actual renderer math fallback and following visible body instead of deleting a nonmatching suffix', async () => {
  const source = '正文 \\(x^2 + y\\) 后文 US$3–6/月'
  const { root } = await actualRenderer(source)
  const text = renderedSpeechText(message(source), root)
  expect(text).toContain('正文'); expect(text).toContain('x'); expect(text).toContain('2'); expect(text).toContain('后文 US$3–6/月')
  expect(text).toContain('x^2 + y') // This isolated renderer currently displays its literal math fallback.

 })

 it('reads real KaTeX HTML output without its hidden MathML, annotations or layout spacer characters', () => {
  const root = document.createElement('div'); document.body.append(root); roots.push(root)
  root.innerHTML = `<div data-speech-body data-speech-block-id="0">正文 ${renderToString('x^2+y+\\sqrt{z}')} 后文</div>`
  const text = renderedSpeechText(message('正文 \\(x^2+y+\\sqrt{z}\\) 后文'), root)
  expect(text).toContain('x'); expect(text).toContain('2'); expect(text).toContain('后文')
  expect(text).not.toContain('x^2'); expect(text).not.toContain('annotation'); expect(text).not.toContain(String.fromCodePoint(0x200B))
 })

 it('retains actual admonition title/body and the body after its custom presentation', async () => {
  const source = '::: warning 提示标题\n正文内容\n:::\n\n后文。'
  const { root } = await actualRenderer(source)
  const text = renderedSpeechText(message(source), root)
  expect(text).toContain('提示标题'); expect(text).toContain('正文内容'); expect(text).toContain('后文。')
 })

})


describe('token and block boundaries use rendered semantics without replay or lost suffixes', () => {
 const cases: [string, string[]][] = [
  ['inline link tokens', ['开头 [文', '开头 [文档]', '开头 [文档](https://hidden.test/sec', '开头 [文档](https://hidden.test/secret)。结束。']],
  ['late reference definition', ['开头 [文档][ref]。后文', '开头 [文档][ref]。后文\n\n[ref]: https://hidden.test/secret']],
  ['footnote with late definition', ['正文[^1] 后文', '正文[^1] 后文\n\n[^1]: 脚注正文']],
  ['table reclassification', ['甲|乙', '甲|乙\n--|--', '甲|乙\n--|--\n1|', '甲|乙\n--|--\n1|2']],
  ['inline and fenced code', ['代码 `x', '代码 `x < 2`。\n\n```text\nconst a = ', '代码 `x < 2`。\n\n```text\nconst a = "[可见](https://code-visible.test)"\n```']],
  ['blockquote and literal brackets', ['> 引用正文\n\n真实 [括', '> 引用正文\n\n真实 [括号] 正文']],
 ]
 it.each(cases)('%s', async (_name, sources) => {
  const renderer = await actualRenderer(sources[0]!), live = new LiveSpeechReplies(), spoken: string[] = []
  const project = (m: ChatMessage) => renderedSpeechText(m, renderer.root)
  for (const source of sources) {
   await renderer.update(source)
   spoken.push(...live.update([message(source, true)], true, false, true, project).map(entry => entry.text))
  }
  const final = message(sources.at(-1)!, false)
  spoken.push(...live.update([final], true, false, true, project).map(entry => entry.text))
  expect(normalizeSpeechText(spoken.join(''))).toBe(project(final))
  if (_name !== 'inline and fenced code') expect(spoken.join('')).not.toContain('https://hidden.test')
  if (_name === 'table reclassification') expect(spoken.join('')).not.toContain('|')
  if (_name === 'late reference definition') expect(spoken.join('')).not.toContain('[ref]')
  if (_name === 'footnote with late definition') expect(spoken.join('')).not.toContain('[^1]')
  expect(live.update([final], true, false, true, project)).toEqual([])
 })
 it('preserves a suffix across separately rendered text blocks and a persisted message id', async () => {
  const renderer = await actualRenderer('第一块。'), live = new LiveSpeechReplies(), spoken: string[] = []
  const project = (m: ChatMessage) => renderedSpeechText(m, renderer.root)
  spoken.push(...live.update([message('第一块。', true)], true, false, true, project).map(entry => entry.text))
  const blocks = ['第一块。', '第二块[文档](https://hidden.test)。', '> 引用第三块']
  await renderer.update(blocks)
  const final = { ...message(blocks), serverId: 'persisted-render-id' }
  spoken.push(...live.update([final], true, false, true, project).map(entry => entry.text))
  expect(normalizeSpeechText(spoken.join(''))).toBe(project(final))
  expect(project(final)).toContain('引用第三块')
 })
 it('defers old raw DOM after final reference source until its actual link render arrives', () => {
  const root = document.createElement('div'); roots.push(root); document.body.append(root)
  root.innerHTML = '<div data-speech-body data-speech-block-id="0">开头 [文档][ref]。后文</div>'
  const source = '开头 [文档][ref]。后文\n\n[ref]: https://hidden.test/secret'
  const live = new LiveSpeechReplies(), project = (m: ChatMessage) => renderedSpeechText(m, root)
  const first = live.update([message('开头 [文档][ref]。后文', true)], true, false, true, project)
  const middle = live.update([message(source)], true, false, true, project)
  expect(middle).toEqual([])
  root.innerHTML = '<div data-speech-body data-speech-block-id="0">开头 <a href="https://hidden.test/secret">文档</a>。后文</div>'
  const last = live.update([message(source)], true, false, true, project)
  expect(normalizeSpeechText([...first, ...last].map(entry => entry.text).join(''))).toBe('开头 文档。后文')
 })
 it('reads only final diagram labels and then the following body, retaining late final DOM updates', () => {
  const root = document.createElement('div'); roots.push(root); document.body.append(root)
  const source = '开头。\n\n```mermaid\ngraph TD; A[显示甲]-->B[显示乙]\n```\n\n后文。'
  root.innerHTML = '<div data-speech-body data-speech-block-id="0"><p>开头。</p><div data-markstream-mermaid="1" data-markstream-mode="pending"><pre>graph TD; https://hidden.test</pre></div><p>后文。</p></div>'
  const live = new LiveSpeechReplies(), project = (m: ChatMessage) => renderedSpeechText(m, root)
  const first = live.update([message(source, true)], true, false, true, project)
  expect(first.map(entry => entry.text).join('')).toBe('开头。')
  expect(live.update([message(source)], true, false, true, project)).toEqual([])
  root.querySelector('[data-markstream-mermaid]')!.outerHTML = '<div data-markstream-mermaid="1" data-markstream-mode="preview"><svg><title>隐藏提示</title><text>显示甲</text><text>显示乙</text></svg><pre hidden>graph TD; https://hidden.test</pre><button>复制源码</button></div>'
  const last = live.update([message(source)], true, false, true, project)
  expect([...first, ...last].map(entry => entry.text).join('')).toBe('开头。 显示甲 显示乙 后文。')
 })
 it('honors real CSS visibility while preserving visible aria-hidden body text and literal URLs', () => {
  const root = document.createElement('div'); roots.push(root); document.body.append(root)
  root.innerHTML = '<style>.speech-css-hidden{display:none}</style><div data-speech-body data-speech-block-id="0"><span aria-hidden="true">可见正文</span><span class="speech-css-hidden">隐藏源码</span><p>https://visible.test</p><span role="tooltip">后台URL</span><button>复制</button></div><p>工具输出</p>'
  expect(renderedSpeechText(message('可见正文 https://visible.test'), root)).toBe('可见正文 https://visible.test')
  root.style.display = 'none'
  expect(renderedSpeechText(message('可见正文 https://visible.test'), root)).toBe('')
 })
 it('does not read detached roots or unmatched old source-block ids', () => {
  const root = document.createElement('div'); roots.push(root); document.body.append(root)
  const current = message('新回复。', true)
  root.innerHTML = '<div data-speech-body data-speech-block-id="old-block">新回复。</div>'
  expect(renderedSpeechText(current, root)).toBe('')
  root.innerHTML = '<div data-speech-body data-speech-block-id="0">新回复。</div>'
  expect(renderedSpeechText(current, root)).toBe('新回复。')
  root.remove()
  expect(renderedSpeechText(current, root)).toBe('')
 })
})
