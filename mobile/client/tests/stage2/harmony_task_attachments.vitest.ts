// Inserted only into the phone's generated session-task-results.test.ts.
// Keep the shared components, SDK and original-byte verification real. Only
// HTTP, image decoding and the OS download/save boundary are synthetic.
import { createHash, webcrypto } from 'node:crypto'

const owner = '55555555-5555-4555-8555-555555555555'
const session = '66666666-6666-4666-8666-666666666666'
const operation = '99999999-9999-4999-8999-999999999999'
const apiBase = 'https://api.example.test/api'
const bytes = Uint8Array.from(atob('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a7X8AAAAASUVORK5CYII='), c => c.charCodeAt(0))
const hash = createHash('sha256').update(bytes).digest('hex')

describe.each(['web', 'native'])('phone committed attachment access in %s host', host => {
  let commands: Record<string, unknown>[]
  let http: ReturnType<typeof vi.fn<typeof fetch>>
  let created: Map<string, Blob>
  let revoked: ReturnType<typeof vi.fn>
  let httpStatus: number
  let corrupt: boolean
  let nativeDenied: boolean

  beforeEach(() => {
    commands = []; created = new Map(); revoked = vi.fn()
    httpStatus = 200; corrupt = false; nativeDenied = false
    vi.stubGlobal('crypto', webcrypto)
    vi.stubGlobal('URL', Object.assign(class extends URL {}, {
      createObjectURL: vi.fn((blob: Blob) => {
        const url = `blob:phone-test/${created.size + 1}`
        created.set(url, blob)
        return url
      }),
      revokeObjectURL: revoked,
    }))
    vi.stubGlobal('Image', class { src = ''; decode() { return Promise.resolve() } })
    http = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request = new Request(input, init)
      expect(request.url).toBe(`${apiBase}/bots/${owner}/media/${hash}`)
      expect(request.method).toBe('GET')
      expect(request.headers.get('authorization')).toBe('Bearer synthetic-token')
      const body = bytes.slice()
      if (corrupt) {
        const first = body[0]
        if (first === undefined) throw new Error('Synthetic image is empty')
        body[0] = first ^ 1
      }
      return new Response(body, { status: httpStatus })
    })
    client.setConfig({ baseUrl: apiBase, headers: { authorization: 'Bearer synthetic-token' }, fetch: http })
    vi.stubGlobal('begoniaNative', { request: vi.fn(async (raw: string) => {
      const q = JSON.parse(raw)
      commands.push(q)
      expect(q.version).toBe(1)
      expect(q.botId).toBe(owner)
      if (q.command === 'download.fetch') {
        expect(q.url).toBe(`${apiBase}/bots/${owner}/media/${hash}`)
        expect(q.authorization).toBe('Bearer synthetic-token')
        expect(q.hash).toBe(hash)
        expect(q.size).toBe(bytes.byteLength)
        expect(q.name).toBe('output')
        if (nativeDenied) return JSON.stringify({ ok: false, error: 'MEDIA_DOWNLOAD_HTTP_403' })
        return JSON.stringify({ ok: true, data: { operation: q.operation, hash, size: bytes.byteLength } })
      }
      expect(['download.save', 'download.release']).toContain(q.command)
      const first = commands[0]
      if (!first) throw new Error('Native command history is empty')
      expect(q.operation).toBe(first.operation)
      return JSON.stringify({ ok: true, data: true })
    }) })
  })

  function mountFiles(mime: string) {
    const deliveries = ref<ComputerTaskDeliveryView>({ botId: owner, files: [
      { delivery_id: mime === 'image/png' ? 'picture' : 'output', state: 'committed', reference: { version: 1, direction: 'upload', content_hash: hash, size_bytes: bytes.byteLength, mime } },
      { delivery_id: 'waiting', state: 'pending', reference: { version: 1, direction: 'upload', content_hash: 'b'.repeat(64), size_bytes: 42, mime } },
    ], loaded: true, loading: false, refresh: async () => {} })
    const preview = vi.fn(), opened = vi.fn()
    root = document.createElement('div'); document.body.append(root)
    app = createApp({ render: () => h(host === 'native' ? 'dialog' : 'section', { open: true }, [h(SessionTaskResults, {
      items: [], selected: { task_type: 'computer_operation', task_id: operation },
      detail: { task_type: 'computer_operation', task_id: operation, status: 'unknown', content_available: false, attachments_available: false, session_id: session, parent_session_id: '', created_at: '', updated_at: '', summary: '' },
      loading: false, deliveries: deliveries.value, onAssetOpened: opened,
    })]) })
    if (host === 'web') app.provide(openAssetPreviewKey, preview)
    app.use(createI18n({ legacy: false, locale: 'en', messages: { en } })); app.mount(root)
    return { deliveries, preview, opened }
  }

  function downloads() {
    return [...root.querySelectorAll('button')].filter(button => button.textContent?.trim() === en.bots.files.download)
  }

  function firstDownload() {
    const button = downloads()[0]
    if (!button) throw new Error('Expected a download button')
    return button
  }

  it('uses the reader Bot and verified native save, hides pending refs and clears on denial', async () => {
    const { deliveries, preview, opened } = mountFiles('application/octet-stream')
    await nextTick()
    expect(root.querySelectorAll('a')).toHaveLength(0)
    expect(downloads()).toHaveLength(1)
    expect(root.querySelector('button[title="waiting"]')).toBeNull()
    expect(http).not.toHaveBeenCalled()
    expect(commands).toHaveLength(0)
    root.querySelector<HTMLButtonElement>('button[title="output"]')!.click()
    if (host === 'web') {
      expect(preview).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ botId: owner, contentHash: hash, src: undefined }))
      expect(opened).toHaveBeenCalledOnce()
      expect(commands).toHaveLength(0)
      firstDownload().click()
    }
    await vi.waitFor(() => expect(commands.map(q => q.command)).toEqual(['download.fetch', 'download.save', 'download.release']))
    expect(http).not.toHaveBeenCalled() // Original bytes stay in the native cache on save.
    deliveries.value = { ...deliveries.value, error: { status: 403 } }
    await nextTick()
    expect(downloads()).toHaveLength(0)
    expect(root.querySelector('button[title="output"]')).toBeNull()
    deliveries.value = { ...deliveries.value, error: undefined, botId: undefined }
    await nextTick()
    expect(downloads()).toHaveLength(0)
    expect(root.querySelector('button[title="output"]')).toBeNull()
    expect(commands).toHaveLength(3) // Never fall back to the global Bot.
  })

  it('keeps verified image bytes across polling and revokes them on a scope change', async () => {
    const { deliveries } = mountFiles('image/png')
    expect(root.querySelector('img')).toBeNull() // No unverified direct media URL.
    await vi.waitFor(() => expect(root.querySelectorAll('img')).toHaveLength(1))
    const thumbnail = root.querySelector('img')!.getAttribute('src')!
    expect(created.get(thumbnail)?.size).toBe(bytes.byteLength)
    expect(created.get(thumbnail)?.type).toBe('image/png')
    root.querySelector('img')!.closest('button')!.click()
    await vi.waitFor(() => expect(root.querySelector('[data-keyboard-scope="mediaLightbox"] img')).not.toBeNull())
    const preview = root.querySelector('[data-keyboard-scope="mediaLightbox"] img')!.getAttribute('src')!
    expect(created.get(preview)?.size).toBe(bytes.byteLength)
    expect(http).toHaveBeenCalledTimes(2)
    deliveries.value = { ...deliveries.value, files: [...deliveries.value.files] }
    await nextTick(); await vi.advanceTimersByTimeAsync(50)
    expect(root.querySelector('[data-keyboard-scope="mediaLightbox"] img')?.getAttribute('src')).toBe(preview)
    expect(http).toHaveBeenCalledTimes(2)
    expect(revoked).not.toHaveBeenCalled()
    deliveries.value = { ...deliveries.value, botId: '77777777-7777-4777-8777-777777777777', files: [] }
    await nextTick(); await vi.advanceTimersByTimeAsync(400)
    expect(root.querySelectorAll('img')).toHaveLength(0)
    expect(root.querySelector('[data-keyboard-scope="mediaLightbox"]')).toBeNull()
    expect(revoked).toHaveBeenCalledWith(thumbnail)
    expect(revoked).toHaveBeenCalledWith(preview)
    expect(commands).toHaveLength(0)
  })

  it('rejects corrupt or denied originals without an image or a fallback request', async () => {
    corrupt = true
    const { deliveries } = mountFiles('image/png')
    await vi.waitFor(() => expect(http).toHaveBeenCalledOnce())
    await vi.advanceTimersByTimeAsync(50)
    expect(created.size).toBe(0)
    expect(root.querySelector('img')).toBeNull()
    deliveries.value = { ...deliveries.value, files: [] }; await nextTick()
    corrupt = false; httpStatus = 403
    deliveries.value = { ...deliveries.value, files: [{ delivery_id: 'denied', state: 'committed', reference: { version: 1, direction: 'upload', content_hash: hash, size_bytes: bytes.byteLength, mime: 'image/png' } }] }
    await vi.waitFor(() => expect(http).toHaveBeenCalledTimes(2))
    await vi.advanceTimersByTimeAsync(50)
    expect(created.size).toBe(0)
    expect(root.querySelectorAll('img,a')).toHaveLength(0)
    expect(commands).toHaveLength(0)
  })

  it('never opens the picker or browser fallback after native access is denied', async () => {
    nativeDenied = true
    mountFiles('application/octet-stream')
    await nextTick(); firstDownload().click()
    await vi.waitFor(() => expect(commands.map(q => q.command)).toEqual(['download.fetch', 'download.release']))
    expect(http).not.toHaveBeenCalled()
    expect(root.querySelectorAll('a')).toHaveLength(0)
  })
})
