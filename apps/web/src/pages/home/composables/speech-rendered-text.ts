import type { ChatMessage } from '@/store/chat/types'
import { visibleSpeechText } from './speech-visible-text'

export function normalizeSpeechText(text: string): string { return text.replace(/\u200B/gu, '').replace(/\s+/gu, ' ').trim() }
const excluded = 'button,[role="button"],input,select,textarea,[role="tooltip"],[role="status"],script,style,template,annotation,.sr-only,.katex-mathml,title,desc'
const boundaries = /^(P|DIV|PRE|BLOCKQUOTE|LI|H[1-6]|TR|TD|TH|TABLE|SECTION|BR)$/u
const sourceBoundaries = new WeakMap<Element, { source: string, complete: string, mathFallback: string, prefix: string | undefined }>()

function renderedText(node: Node, projection: { held: boolean }, expected: string, inCode = false): string {
  if (projection.held) return ''
  if (node.nodeType === Node.TEXT_NODE) {
    const text = node.textContent ?? ''
    // Final source can arrive before the old plain-text DOM is replaced by a
    // link/table node. Hold syntax absent from that final display projection.
    const pending = !inCode && !expected.includes(normalizeSpeechText(text)) && /\[|\||<[A-Za-z][^>]*$/u.exec(text)
    if (pending) { projection.held = true; return text.slice(0, pending.index) }
    return text
  }
  if (!(node instanceof Element) || node.matches(excluded) || node.hasAttribute('hidden')) return ''
  const style = node.ownerDocument.defaultView?.getComputedStyle(node)
  if (style?.display === 'none' || style?.visibility === 'hidden' || style?.visibility === 'collapse') return ''
  // These are the actual markstream renderer contracts (also used by its own
  // pending-node logic). Do not read a diagram's temporary source/error chrome
  // or speak later paragraphs before its final visible labels arrive.
  if (node.matches('[data-markstream-pending="true"],[data-markstream-mode="pending"]')) { projection.held = true; return '' }
  const text = Array.from(node.childNodes).map(child => renderedText(child, projection, expected, inCode || node.tagName === 'PRE' || node.tagName === 'CODE')).join('')
  return boundaries.test(node.tagName) || (node instanceof SVGElement && node.localName === 'text') ? ` ${text} ` : text
}

/** Only renderer-owned assistant text roots are read. Attributes, link targets,
 * process cards, attachment captions and off-root overlays cannot enter speech.
 * Real CSS visibility and custom renderer output determine the spoken body. */
export function renderedSpeechText(message: ChatMessage, element: Element | null): string {
  if (!element?.isConnected || message.role !== 'assistant' || message.messages.some(block => block.type === 'attachments' && block.attachments.some(attachment => attachment.type === 'audio' || attachment.type === 'voice' || attachment.mime?.startsWith('audio/')))) return ''
  for (let ancestor: Element | null = element; ancestor; ancestor = ancestor.parentElement) {
    const style = ancestor.ownerDocument.defaultView?.getComputedStyle(ancestor)
    if (ancestor.hasAttribute('hidden') || style?.display === 'none' || style?.visibility === 'hidden') return ''
  }
  const projection = { held: false }
  const output: string[] = []
  for (const body of element.querySelectorAll('[data-speech-body]')) {
    if (projection.held) break
    const source = message.messages.find(block => block.type === 'text' && String(block.id) === body.getAttribute('data-speech-block-id'))
    if (source?.type !== 'text') break // A stale renderer root cannot speak for a different source block.
    let boundary = sourceBoundaries.get(body)
    if (boundary?.source !== source.content) {
      const complete = normalizeSpeechText(visibleSpeechText(source.content))
      const safe = normalizeSpeechText(visibleSpeechText(source.content, true))
      const mathFallback = normalizeSpeechText(visibleSpeechText(source.content, false, true))
      boundary = { source: source.content, complete, mathFallback, prefix: safe === complete ? undefined : safe }
      sourceBoundaries.set(body, boundary)
    }
    const complete = boundary?.complete ?? ''
    const mathFallback = boundary?.mathFallback ?? complete
    let text = normalizeSpeechText(renderedText(body, projection, `${complete} ${mathFallback}`))
    if (message.streaming && boundary?.prefix !== undefined) {
      text = text.startsWith(boundary.prefix) ? boundary.prefix : ''
      projection.held = true
    }
    output.push(text)
  }
  return normalizeSpeechText(output.join(' '))
}
