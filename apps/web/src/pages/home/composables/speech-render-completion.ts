import type { MarkstreamRendererHandle, MarkstreamVirtualMetrics } from 'markstream-vue'
import type { ChatMessage } from '@/store/chat/types'

interface Registration { source: string, final: boolean, renderer: MarkstreamRendererHandle, notify(): void, ready: boolean, settling: boolean }
const renderers = new WeakMap<Element, Registration>()
const complete = (metrics: MarkstreamVirtualMetrics) => metrics.final && metrics.stable && metrics.renderedCount >= metrics.nodeCount

/** markstream 2.0 emits render-final only with virtualScroll enabled. Chat keeps
 * its existing nonvirtual renderer and uses the public metrics/settle handle.
 * No own quiet timer, length guess or virtual-scroll policy is introduced. */
export function bindSpeechRenderer(body: Element, renderer: MarkstreamRendererHandle, source: string, final: boolean, notify: () => void) {
  const previous = renderers.get(body)
  if (previous?.renderer === renderer && previous.source === source && previous.final === final) return
  renderers.set(body, { source, final, renderer, notify, ready: false, settling: false })
}
function ready(body: Element, source: string) {
  const registered = renderers.get(body)
  if (!registered || registered.source !== source || !registered.final || !body.isConnected) return false
  if (registered.ready) return true
  if (complete(registered.renderer.getVirtualMetrics())) { registered.ready = true; registered.notify(); return true }
  if (!registered.settling) {
    registered.settling = true
    // Reuse renderer-owned settlement and cancellation/version checks. Its
    // defaults are not a product latency promise; unstable is never complete.
    void registered.renderer.settle({ flushPendingTimers: true }).then(metrics => {
      registered.settling = false
      if (renderers.get(body) !== registered || !body.isConnected || !complete(metrics)) return
      registered.ready = true; registered.notify()
    }).catch(() => { registered.settling = false })
  }
  return false
}
export function speechRenderComplete(message: ChatMessage, element: Element | null) {
  if (message.role !== 'assistant' || !element?.isConnected) return false
  const bodies = Array.from(element.querySelectorAll('[data-speech-body]'))
  return message.messages.filter(block => block.type === 'text' && block.content).every(block => {
    const body = bodies.find(body => body.getAttribute('data-speech-block-id') === String(block.id))
    return !!body && ready(body, block.type === 'text' ? block.content : '')
  })
}
