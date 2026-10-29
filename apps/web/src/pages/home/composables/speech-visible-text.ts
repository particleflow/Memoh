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
function visibleNode(node: ParsedNode, streaming: boolean): string {
  const n = node as ParsedNode & { children?: ParsedNode[], items?: ParsedNode[], cells?: ParsedNode[], rows?: ParsedNode[], header?: ParsedNode, term?: ParsedNode[], definition?: ParsedNode[], content?: string, code?: string, title?: string, tag?: string }
  if (n.type === 'image' || n.type === 'thematic_break' || n.type === 'footnote_anchor') return ''
  if (n.type === 'html_block' || n.type === 'html_inline') return htmlText(n.content ?? '')
  if (n.type === 'math_inline' || n.type === 'math_block') {
    return htmlText(renderToString(n.content ?? '', { output: 'mathml', throwOnError: false }))
  }
  if (n.type === 'table') return [n.header, ...(n.rows ?? [])].filter(Boolean).map(item => visibleNode(item!, streaming)).join('\n') + '\n'
  if (n.type === 'table_row') return (n.cells ?? []).map(item => visibleNode(item, streaming)).join(' ') + '\n'
  if (n.type === 'definition_item') return [...(n.term ?? []), ...(n.definition ?? [])].map(item => visibleNode(item, streaming)).join(' ') + '\n'
  if (n.children || n.items) {
    const text = (n.children ?? n.items ?? []).map(item => visibleNode(item, streaming)).join('')
    return (n.type === 'admonition' ? `${n.title ?? ''}\n` : '') + text + (/^(paragraph|heading|list_item|blockquote|footnote)$/u.test(n.type) ? '\n' : '')
  }
  if (n.type === 'hardbreak' || n.type === 'softbreak') return '\n'
  if (n.type === 'code_block' || n.type === 'inline_code') return (n.code ?? '') + (n.type === 'code_block' ? '\n' : '')
  if (n.type === 'text') {
    // An unfinished link label is still a text node. Hold only that unstable
    // tail until the parser recognizes the link; its target must never leak.
    return streaming ? (n.content ?? '').replace(/\[[^\]]*(?:\](?:\[[^\]]*\]?)?)?$/u, '') : n.content ?? ''
  }
  return n.content ?? ''
}
export function visibleSpeechText(source: string, streaming = false): string {
  return parseMarkdownToStructure(source, markdown).map(node => visibleNode(node, streaming)).join('').trim()
}
