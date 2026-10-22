// @vitest-environment jsdom
import { createApp, h, nextTick, type App, type Slots } from 'vue'
import { createPinia } from 'pinia'
import { PiniaColada, useQueryCache } from '@pinia/colada'
import type { BotsBot } from '@memohai/sdk'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import BotsPage from './index.vue'

const mocks = vi.hoisted(() => ({ list: vi.fn(), push: vi.fn() }))
vi.mock('@memohai/sdk/colada', () => ({
  getBotsQuery: () => ({ key: ['bots'], query: mocks.list }),
}))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: mocks.push }) }))
vi.mock('@/composables/usePendingBotsRefresh', () => ({ usePendingBotsRefresh: vi.fn() }))
vi.mock('./components/bot-card.vue', () => ({
  default: (props: { bot: BotsBot }) => h('div', { 'data-bot': props.bot.id }, props.bot.name),
}))
vi.mock('@felinic/ui', async () => {
  const { h } = await import('vue')
  const pass = (_: unknown, { slots }: { slots: Slots }) => h('div', slots.default?.())
  return {
    SurfaceCard: pass,
    Skeleton: () => h('span', { 'data-loading': '' }),
    Button: (_: unknown, { slots, attrs }: { slots: Slots, attrs: Record<string, unknown> }) =>
      h('button', attrs, slots.default?.()),
    PersonaTile: (props: { name: string }, { attrs }: { attrs: Record<string, unknown> }) =>
      h('button', { ...attrs, 'data-create': '' }, props.name),
    Input: () => h('input'),
    CalloutBanner: (props: { title: string }, { slots }: { slots: Slots }) =>
      h('div', { role: 'alert' }, [props.title, slots.default?.()]),
  }
})

let app: App | undefined
let host: HTMLDivElement
let queryCache: ReturnType<typeof useQueryCache>
async function flush() {
  for (let i = 0; i < 4; i += 1) {
    await new Promise(resolve => setTimeout(resolve, 0))
    await nextTick()
  }
}
async function mount() {
  host = document.createElement('div')
  document.body.append(host)
  app = createApp(BotsPage)
  app.config.globalProperties.$t = (key: string) => key
  app.use(createPinia()).use(PiniaColada)
  queryCache = app.runWithContext(() => useQueryCache())
  app.mount(host)
  await flush()
}
beforeEach(() => vi.clearAllMocks())
afterEach(() => {
  app?.unmount()
  host?.remove()
})

describe('Bot list query state', () => {
  it.each([401, 403, 'network'])('keeps a %s failure separate from an empty list and retries only on request', async (failure) => {
    mocks.list.mockRejectedValueOnce(new Error(`PRIVATE_RESPONSE_${failure}`))
    await mount()
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('common.loadFailed')
    expect(host.textContent).not.toContain('PRIVATE_RESPONSE')
    expect(host.textContent).not.toContain('bots.emptyDescription')
    expect(host.querySelector('[data-create]')).toBeNull()
    expect(mocks.list).toHaveBeenCalledTimes(1)

    const retryResponse = Promise.withResolvers<{ items: BotsBot[] }>()
    mocks.list.mockReturnValueOnce(retryResponse.promise)
    const retry = host.querySelector<HTMLButtonElement>('[role="alert"] button')
    expect(retry).not.toBeNull()
    retry?.click()
    await flush()
    expect(retry?.disabled).toBe(true)
    retry?.click()
    await flush()
    expect(mocks.list).toHaveBeenCalledTimes(2)
    retryResponse.resolve({ items: [] })
    await flush()
    expect(host.querySelector('[role="alert"]')).toBeNull()
    expect(host.textContent).toContain('bots.emptyDescription')
    expect(host.querySelector('[data-create]')).not.toBeNull()
  })

  it('keeps cached Bots visible with a safe error when a background refresh fails', async () => {
    mocks.list.mockResolvedValueOnce({ items: [{ id: 'bot-1', name: 'synthetic-bot' }] })
    await mount()
    mocks.list.mockRejectedValueOnce(new Error('PRIVATE_RESPONSE'))
    await expect(queryCache.invalidateQueries({ key: ['bots'] })).rejects.toThrow('PRIVATE_RESPONSE')
    await flush()
    expect(host.querySelector('[data-bot="bot-1"]')).not.toBeNull()
    expect(host.querySelector('[role="alert"]')).not.toBeNull()
    expect(host.textContent).not.toContain('PRIVATE_RESPONSE')
    expect(host.textContent).not.toContain('bots.emptyDescription')
  })

  it('shows the skeleton while the first request is pending, then the create entry', async () => {
    const response = Promise.withResolvers<{ items: BotsBot[] }>()
    mocks.list.mockReturnValue(response.promise)
    await mount()
    expect(host.querySelector('[data-loading]')).not.toBeNull()
    expect(host.querySelector('[data-create]')).toBeNull()
    expect(host.textContent).not.toContain('bots.emptyDescription')

    response.resolve({ items: [] })
    await flush()
    expect(host.querySelector('[data-loading]')).toBeNull()
    expect(host.textContent).toContain('bots.emptyDescription')
    host.querySelector<HTMLButtonElement>('[data-create]')?.click()
    expect(mocks.push).toHaveBeenCalledWith({ name: 'bot-new' })
  })

  it('keeps real bots and every requested development preview without undefined entries', async () => {
    mocks.list.mockResolvedValue({ items: [{ id: 'bot-1', name: 'synthetic-bot' }] })
    await mount()
    expect(host.querySelectorAll('[data-bot]')).toHaveLength(1)
    window.__previewBots?.(7)
    await nextTick()
    const ids = [...host.querySelectorAll('[data-bot]')].map(node => node.getAttribute('data-bot'))
    expect(ids).toHaveLength(7)
    expect(new Set(ids).size).toBe(7)
    expect(ids[0]).toBe('bot-1')
    window.__previewBots?.()
    await nextTick()
    expect(host.querySelectorAll('[data-bot]')).toHaveLength(1)
  })
})
