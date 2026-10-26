// @vitest-environment jsdom
import { createHash, webcrypto } from 'node:crypto'
import { createApp, h, nextTick, type App } from 'vue'
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { client } from '@memohai/sdk/client'
import AttachmentBlock from '@/pages/home/components/attachment-block.vue'
import { notifyAuthSessionCleared } from './auth-session'
import { originalBlob } from './harmony-files'
import * as timing from './harmony-image-timing'

vi.mock('@/store/chat-list', () => ({ useChatStore: () => ({ currentBotId: 'bot-a' }) }))
vi.mock('@/pages/home/composables/useMediaGallery', () => ({ resolveUrl: () => '' }))
vi.mock('@felinic/ui', () => ({ Skeleton: 'span' }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

type Snapshot = { version: number; active: boolean; paintSupported: boolean; rows: Record<string, number>[] }
const control = (window as unknown as { __garnetImageTiming: { arm(): Snapshot; take(): Snapshot; clear(): void } }).__garnetImageTiming
const png = Uint8Array.from(atob('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a7X8AAAAASUVORK5CYII='), c => c.charCodeAt(0))
const hash = createHash('sha256').update(png).digest('hex')
const bot = '11111111-1111-4111-8111-111111111111'
let host: HTMLDivElement, app: App | undefined
const http = vi.fn<typeof fetch>()
const attachment = () => ({ type: 'image', name: 'PRIVATE_NAME', mime: 'image/png', size: png.length, bot_id: bot, content_hash: hash })
async function flush() { for (let i = 0; i < 15; i++) { await new Promise(resolve => setTimeout(resolve, 0)); await nextTick() } }
beforeEach(() => {
  control.clear(); vi.clearAllMocks()
  vi.stubGlobal('crypto', webcrypto)
  vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => { cb(0); return 0 })
  vi.stubGlobal('URL', Object.assign(class extends URL {}, { createObjectURL: () => 'blob:synthetic/1', revokeObjectURL: vi.fn() }))
  http.mockImplementation(async () => new Response(png.slice(), { status: 200 }))
  client.setConfig({ baseUrl: 'https://api.example.test/api', headers: { authorization: 'Bearer PRIVATE_TOKEN' }, fetch: http })
})
afterEach(() => { app?.unmount(); app = undefined; host?.remove(); control.clear(); vi.restoreAllMocks(); vi.unstubAllGlobals(); vi.useRealTimers() })
async function mountImage(variant: 'content' | 'attachment') {
  host = document.createElement('div'); document.body.append(host)
  app = createApp({ render: () => h(AttachmentBlock, { block: { id: 1, type: 'attachments', attachments: [attachment()] }, variant }) })
  app.mount(host); await flush()
  const image = host.querySelector('img'); expect(image).not.toBeNull()
  return image!
}

describe('temporary image timing at real phone component seams', () => {
  it('is off by default and leaves the configured authenticated fetch unchanged', async () => {
    await mountImage('content')
    expect(http).toHaveBeenCalledOnce()
    const req = new Request(http.mock.calls[0]![0], http.mock.calls[0]![1])
    expect(req.headers.get('authorization')).toBe('Bearer PRIVATE_TOKEN')
    expect(req.url).toBe(`https://api.example.test/api/bots/${bot}/media/${hash}`)
    expect(control.take()).toEqual({ version: 1, active: false, paintSupported: false, rows: [] })
  })
  it.each(['content', 'attachment'] as const)('records verified %s body/hash/blob/load/decode without changing visible content', async variant => {
    control.arm()
    const image = await mountImage(variant)
    Object.defineProperty(image, 'decode', { value: vi.fn().mockResolvedValue(undefined) })
    image.dispatchEvent(new Event('load')); await flush()
    const snap = control.take(), row = snap.rows[0]!
    expect(snap.rows).toHaveLength(1)
    const phases = ['card', 'request', 'headers', 'body', 'hashStart', 'hashEnd', 'blob', 'load', 'decodeStart', 'decodeEnd']
    for (const phase of phases) expect(typeof row[phase]).toBe('number')
    for (let i = 1; i < phases.length; i++) expect(row[phases[i]!]!).toBeGreaterThanOrEqual(row[phases[i - 1]!]!)
    expect(row.bytes).toBe(png.length)
    expect(http).toHaveBeenCalledOnce()
    expect(JSON.stringify(snap)).not.toMatch(/PRIVATE_|https:|blob:|11111111|[a-f0-9]{64}/)
    expect(host.textContent).not.toMatch(/timing|request|decode/)
    expect(row.render).toBeUndefined() // No invented paint on unsupported engines.
    expect(control.take().rows).toEqual([])
  })
  it('keeps corrupt originals hidden with timing armed', async () => {
    http.mockImplementation(async () => new Response(new Uint8Array(png.length), { status: 200 }))
    control.arm(); const att = attachment(); timing.imageTimingCard(att)
    await expect(originalBlob(att)).rejects.toThrow('原件校验未通过')
    expect(control.take().rows[0]?.blob).toBeUndefined()
  })
  it('does not weaken delegated paths or turn 403 into a hash fallback', async () => {
    localStorage.setItem('token', 'PRIVATE_TOKEN')
    const session = '22222222-2222-4222-8222-222222222222', delegation = '33333333-3333-4333-8333-333333333333'
    const path = `/bots/${bot}/sessions/${session}/delegations/${delegation}/attachments/${hash}`
    const att = { ...attachment(), metadata: { delegation_url: path } }
    const denied = vi.fn<typeof fetch>(async () => new Response(null, { status: 403 }))
    vi.stubGlobal('fetch', denied)
    control.arm(); timing.imageTimingCard(att)
    await expect(originalBlob(att)).rejects.toThrow('委派原件访问失败')
    expect(denied).toHaveBeenCalledOnce(); expect(http).not.toHaveBeenCalled()
    expect(new Request(denied.mock.calls[0]![0], denied.mock.calls[0]![1]).url).toBe('https://api.example.test/api' + path)
    localStorage.removeItem('token')
  })
  it('caps at five, expires in memory and clears on logout/pagehide', () => {
    vi.useFakeTimers(); control.arm()
    for (let i = 0; i < 9; i++) timing.imageTimingCard({})
    expect(control.take().rows).toHaveLength(5)
    control.arm(); timing.imageTimingCard({}); vi.advanceTimersByTime(120_000)
    expect(control.take().rows).toEqual([])
    control.arm(); timing.imageTimingCard({}); notifyAuthSessionCleared('logout')
    expect(control.take().rows).toEqual([])
    control.arm(); timing.imageTimingCard({}); window.dispatchEvent(new Event('pagehide'))
    expect(control.take().rows).toEqual([])
  })
  it('ignores a late response from a previous armed generation', async () => {
    let finish!: (value: Response) => void
    const pending = new Promise<Response>(resolve => { finish = resolve })
    control.arm(); const old = {}; timing.imageTimingCard(old)
    const call = timing.imageTimingFetch(timing.imageTimingHandle(old), () => pending)('https://example.test')
    control.arm(); timing.imageTimingCard({}); finish(new Response(null)); await call
    const row = control.take().rows[0]!
    expect(row.headers).toBeUndefined(); expect(row.request).toBeUndefined()
  })
  it('does not overwrite inline-card stages when the same image is explicitly read again', async () => {
    let clock = 0
    vi.spyOn(performance, 'now').mockImplementation(() => ++clock)
    const att = attachment(); control.arm(); timing.imageTimingCard(att)
    await originalBlob(att)
    const firstReadEnd = clock
    await originalBlob(att) // Existing lightbox/save reader may reuse this same object.
    const row = control.take().rows[0]!
    expect(http).toHaveBeenCalledTimes(2) // Both existing explicit reads still execute.
    expect(row.hashEnd).toBeLessThanOrEqual(firstReadEnd)
    expect(row.request).toBeLessThanOrEqual(firstReadEnd)
  })
  it('copies only a matching recent fetch responseEnd, never a URL or masked bytes', async () => {
    vi.useFakeTimers()
    vi.spyOn(performance, 'now').mockReturnValue(50)
    vi.spyOn(performance, 'getEntriesByName').mockReturnValue([
      { startTime: 1, responseEnd: 4, initiatorType: 'fetch' },
      { startTime: 51, responseEnd: 61, initiatorType: 'fetch', name: 'PRIVATE_URL', transferSize: 0 },
    ] as PerformanceResourceTiming[])
    control.arm(); const att = {}; timing.imageTimingCard(att)
    timing.imageTimingResource(timing.imageTimingHandle(att), 'https://PRIVATE_URL')
    await vi.advanceTimersByTimeAsync(0)
    const row = control.take().rows[0]!
    expect(row.responseEnd).toBe(61); expect(row.bytes).toBeUndefined()
    expect(JSON.stringify(row)).not.toContain('PRIVATE')
  })
  it('records element renderTime only when supported and actually delivered', () => {
    let callback!: (list: { getEntries(): object[] }) => void
    vi.stubGlobal('PerformanceObserver', class {
      static supportedEntryTypes = ['element']
      constructor(cb: typeof callback) { callback = cb }
      observe() {} disconnect() {}
    })
    control.arm(); const att = {}; timing.imageTimingCard(att)
    const blob = new Blob(); timing.imageTimingBlob(timing.imageTimingHandle(att), blob); timing.imageTimingURL(blob, 'blob:synthetic/element')
    const identifier = timing.imageTimingElement('blob:synthetic/element')
    expect(identifier).toMatch(/^garnet-image-\d+-1$/)
    callback({ getEntries: () => [{ identifier, renderTime: 88 }] })
    const snap = control.take(); expect(snap.paintSupported).toBe(true); expect(snap.rows[0]?.render).toBe(88)
  })
  it('records image/decode errors only as numeric categories', async () => {
    control.arm(); const image = await mountImage('content')
    image.dispatchEvent(new Event('error'))
    expect(control.take().rows[0]?.error).toBe(3)
    control.arm(); const att = {}; timing.imageTimingCard(att)
    const blob = new Blob(); timing.imageTimingBlob(timing.imageTimingHandle(att), blob); timing.imageTimingURL(blob, 'blob:synthetic/fail')
    const other = document.createElement('img'); other.src = 'blob:synthetic/fail'
    Object.defineProperty(other, 'decode', { value: () => Promise.reject(new Error('PRIVATE_ERROR')) })
    timing.imageTimingLoaded({ target: other } as unknown as Event); await flush()
    expect(control.take().rows[0]?.error).toBe(2)
  })
})
