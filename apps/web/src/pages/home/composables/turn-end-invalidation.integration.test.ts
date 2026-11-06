// @vitest-environment jsdom
import { createApp, defineComponent, h, nextTick, ref } from 'vue'
import { createPinia } from 'pinia'
import { PiniaColada, useQuery, useQueryCache } from '@pinia/colada'
import { afterEach, expect, it, vi } from 'vitest'
import { installTurnEndInvalidation } from './turn-end-invalidation'

let app: ReturnType<typeof createApp> | undefined
let root: HTMLElement | undefined
afterEach(() => { app?.unmount(); root?.remove(); vi.useRealTimers() })

function mount(query: (context: { signal: AbortSignal }) => Promise<{ revision: string }>) {
  const streaming = ref<string[]>(['synthetic-session'])
  let observed: ReturnType<typeof useQuery<{ revision: string }>>
  root = document.createElement('div'); document.body.append(root)
  app = createApp(defineComponent({ setup() {
    observed = useQuery({ key: ['session-status', 'synthetic-bot', 'synthetic-session', ''], query, refetchOnWindowFocus: false })
    installTurnEndInvalidation(streaming, useQueryCache())
    return () => h('span', observed.data.value?.revision)
  } }))
  app.use(createPinia()).use(PiniaColada).mount(root)
  return { streaming, state: () => observed! }
}

it('handles cancellation of the detached first refetch when the follow-up replaces it', async () => {
  vi.useFakeTimers()
  let requests = 0, aborted = 0
  const state = mount(async ({ signal }) => {
    requests++
    if (requests !== 2) return { revision: String(requests) }
    return new Promise<{ revision: string }>((_resolve, reject) => {
      signal.addEventListener('abort', () => { aborted++; reject(signal.reason) }, { once: true })
    })
  })
  await vi.advanceTimersByTimeAsync(0)
  expect(state.state().data.value).toEqual({ revision: '1' })
  state.streaming.value = []; await nextTick()
  expect(requests).toBe(2)
  await vi.advanceTimersByTimeAsync(1500)
  expect(aborted).toBe(1)
  expect(requests).toBe(3)
  expect(state.state().data.value).toEqual({ revision: '3' })
  expect(state.state().error.value).toBeNull()
})

it('keeps a real refetch failure in the query state and preserves prior data without an extra retry', async () => {
  vi.useFakeTimers()
  let requests = 0
  const failure = new Error('synthetic status unavailable')
  const state = mount(async () => { requests++; if (requests > 1) throw failure; return { revision: '1' } })
  await vi.advanceTimersByTimeAsync(0)
  state.streaming.value = []; await nextTick(); await vi.advanceTimersByTimeAsync(0)
  expect(state.state().error.value).toBe(failure)
  expect(state.state().data.value).toEqual({ revision: '1' })
  await vi.advanceTimersByTimeAsync(1500)
  expect(state.state().error.value).toBe(failure)
  expect(state.state().data.value).toEqual({ revision: '1' })
  expect(requests).toBe(3)
})
