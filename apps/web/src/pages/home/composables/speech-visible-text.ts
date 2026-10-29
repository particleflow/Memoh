import { getMarkdown, parseMarkdownToStructure, type ParsedNode } from 'markstream-vue'
import { renderToString } from 'katex'
import { withLiteralSingleDollars } from '@/utils/markdown-dollar'

// Use the chat renderer's parser and currency policy, never raw Markdown or a
// URL-removal regex. A visible bare URL and visible code remain ordinary text.
const markdown = withLiteralSingleDollars(getMarkdown('chat-speech'))
function htmlText(html: string): string {
  if (typeof document === 'undefined') return ''
  const root = document.createElement('template')
  root.innerHTML = html
  root.content.querySelectorAll('script,style,template,annotation,[hidden]').forEach(node => node.remove())
  root.content.querySelectorAll('[style]').forEach(node => {
    const style = (node as HTMLElement).style
    if (style.display === 'none' || style.visibility === 'hidden') node.remove()
  })
  return root.content.textContent ?? ''
}
interface Projection { streaming: boolean, held: boolean, mathAsSource?: boolean }
function visibleNode(node: ParsedNode, projection: Projection): string {
  if (projection.held) return ''
  const n = node as ParsedNode & { children?: ParsedNode[], items?: ParsedNode[], cells?: ParsedNode[], rows?: ParsedNode[], header?: ParsedNode, term?: ParsedNode[], definition?: ParsedNode[], content?: string, code?: string, title?: string, tag?: string }
  if (n.type === 'image' || n.type === 'thematic_break' || n.type === 'footnote_anchor') return ''
  if (n.type === 'html_block' || n.type === 'html_inline') return htmlText(n.content ?? '')
  if (n.type === 'math_inline' || n.type === 'math_block') {
    if (projection.mathAsSource) return n.raw
    return htmlText(renderToString(n.content ?? '', { output: 'mathml', throwOnError: false }))
  }
  if (n.type === 'footnote_reference') return String((node as ParsedNode & { id: string }).id)
  if (n.type === 'table') return [n.header, ...(n.rows ?? [])].filter(Boolean).map(item => visibleNode(item!, projection)).join('\n') + '\n'
  if (n.type === 'table_row') return (n.cells ?? []).map(item => visibleNode(item, projection)).join(' ') + '\n'
  if (n.type === 'definition_item') return [...(n.term ?? []), ...(n.definition ?? [])].map(item => visibleNode(item, projection)).join(' ') + '\n'
  if (n.children || n.items) {
    const text = (n.children ?? n.items ?? []).map(item => visibleNode(item, projection)).join('')
    return (n.type === 'admonition' ? `${n.title ?? ''}\n` : '') + text + (/^(paragraph|heading|list_item|blockquote|footnote)$/u.test(n.type) ? '\n' : '')
  }
  if (n.type === 'hardbreak' || n.type === 'softbreak') return '\n'
  if (n.type === 'code_block' || n.type === 'inline_code') return (n.code ?? '') + (n.type === 'code_block' ? '\n' : '')
  if (n.type === 'text') {
    const text = n.content ?? ''
    if (!projection.streaming) return text
    // References may acquire definitions much later. Once an unresolved label
    // or prospective table starts, keep the entire subsequent suffix pending:
    // speaking its punctuation now would make later visible labels an insertion.
    const pending = /\[|\||(?:^|\n)\s*(?:`{1,2}|~{1,2})$|<[A-Za-z][^>]*$/u.exec(text)
    if (!pending) return text
    projection.held = true
    return text.slice(0, pending.index)
  }
  return n.content ?? ''
}
export function visibleSpeechText(source: string, streaming = false, mathAsSource = false): string {
  const projection = { streaming, held: false, mathAsSource }
  return parseMarkdownToStructure(source, markdown).map(node => visibleNode(node, projection)).join('').trim()
}
