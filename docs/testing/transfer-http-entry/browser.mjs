import fs from 'node:fs'
import assert from 'node:assert/strict'
import crypto from 'node:crypto'
import { chromium } from '/opt/codex/runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright/index.mjs'

const out = '/workspace/scratch/fast-transfer-path/http-origin'
const fixture = JSON.parse(fs.readFileSync('/workspace/scratch/fast-transfer-path/private-browser-fixture.json'))
const logicalOrigin = 'https://logical.fixture.test'
const physicalOrigin = 'http://fast.fixture.test:18683'
const policy = { version: 2, endpoint: { logical_origin: logicalOrigin, physical_origin: physicalOrigin } }
const browser = await chromium.launch({ headless: true, executablePath: '/usr/bin/chromium', args: ['--no-sandbox', '--disable-dev-shm-usage', '--no-proxy-server', '--host-resolver-rules=MAP fast.fixture.test 127.0.0.1'] })
const context = await browser.newContext({ viewport: { width: 1200, height: 850 }, acceptDownloads: true })
await context.addInitScript(token => { localStorage.setItem('token', token); localStorage.setItem('language', 'en') }, fixture.token)
let logicalFixtureRequests = 0
// Logical HTTPS metadata/control is a controlled fixture adapter. It preserves
// the original API identity; it does not prove public HTTPS/TLS/CORS transport.
await context.route(logicalOrigin + '/**', async route => {
  const request = route.request()
  const url = new URL(request.url())
  if (url.pathname === '/') { await route.fulfill({ status: 200, contentType: 'text/html', body: '<!doctype html><title>Original origin storage probe</title><h1>Original origin storage probe</h1>' }); return }
  if (url.pathname.startsWith('/api/')) {
    logicalFixtureRequests++
    const response = await route.fetch({ url: 'http://127.0.0.1:18680' + url.pathname.slice(4) + url.search })
    if (url.pathname === '/api/users/me') { const profile = await response.json(); profile.network_policy = policy; await route.fulfill({ response, json: profile }); return }
    await route.fulfill({ response }); return
  }
  const response = await context.request.get('http://127.0.0.1:18683' + url.pathname + url.search)
  await route.fulfill({ response })
})

const securePage = await context.newPage()
await securePage.goto(logicalOrigin)
const secureStorage = await securePage.evaluate(async ({ bot, logicalOrigin }) => {
  const api = await import('/src/lib/api-client.ts')
  const m = await import('/src/lib/media-transfer.ts')
  const storage = await import('/src/lib/media-transfer-storage.ts')
  api.setupApiClient({ baseUrl: logicalOrigin + '/api' })
  const identity = await m.mediaIdentity(bot, new AbortController().signal)
  const original = identity.fetch
  identity.fetch = async (input, init) => {
    const request = input instanceof Request ? input : new Request(input, init)
    if (request.method === 'POST' && request.url.endsWith('/media/uploads')) throw new DOMException('Synthetic pre-dispatch interruption', 'AbortError')
    return await original(input, init)
  }
  const file = new File(['original upload preserved'], 'http-origin-resume-probe.bin')
  let failure
  try { await m.uploadMedia(identity, file, 'http-origin-resume-probe', 1024 * 1024, new AbortController().signal) } catch (error) { failure = error.name }
  const key = m.mediaFileJournalKey(file)
  const journal = await storage.readJournal(key)
  return { secureContext: isSecureContext, locks: typeof navigator.locks?.request, opfs: typeof navigator.storage?.getDirectory, key, origin: journal.origin, journal: !!journal, source: await (await storage.localFile(key)).text(), phase: journal.phase, beforeCreateInterrupted: failure === 'AbortError' && !journal.receipt }
}, { bot: fixture.bot, logicalOrigin })
assert.equal(secureStorage.journal, true)
assert.equal(secureStorage.source, 'original upload preserved')
assert.equal(secureStorage.beforeCreateInterrupted, true)
await securePage.close()

const page = await context.newPage()
const pageErrors = []
const consoleMessages = []
const physicalRequests = []
page.on('pageerror', error => pageErrors.push(error.message))
page.on('console', message => consoleMessages.push(message.text().replace(/token=[^&\s]*/g, 'token=REDACTED')))
page.on('response', response => {
  const request = response.request(); const url = new URL(request.url())
  if (url.origin !== physicalOrigin || !url.pathname.startsWith('/api/')) return
  const timing = request.timing()
  physicalRequests.push({ method: request.method(), path: url.pathname, status: response.status(), sameOriginWithPage: true, bodyBytes: request.postDataBuffer()?.length ?? 0, ttfbMs: timing.responseStart >= 0 ? timing.responseStart - Math.max(timing.requestStart, 0) : null })
})
await page.goto(physicalOrigin + '/bot/endpoint-fixture')
await page.locator('input[type=file]:not([webkitdirectory])').first().waitFor({ state: 'attached', timeout: 30000 })
const fileBytes = Buffer.alloc(65573); for (let i = 0; i < fileBytes.length; i++) fileBytes[i] = i * 17 % 251
await page.locator('input[type=file]:not([webkitdirectory])').first().setInputFiles({ name: 'http-origin-original.bin', mimeType: 'application/octet-stream', buffer: fileBytes })
await page.getByText('File uploaded', { exact: true }).waitFor({ timeout: 20000 })
const filesButton = page.locator('[title="Files"],[aria-label="Files"]'); if (await filesButton.count()) await filesButton.first().click()
await page.getByText('http-origin-original.bin', { exact: true }).first().waitFor()
await page.screenshot({ path: out + '/actual-http-files.png', fullPage: true })

const result = await page.evaluate(async ({ bot, logicalOrigin, physicalOrigin, secureKey }) => {
  const api = await import('/src/lib/api-client.ts')
  const m = await import('/src/lib/media-transfer.ts')
  const storage = await import('/src/lib/media-transfer-storage.ts')
  const identity = await m.mediaIdentity(bot, new AbortController().signal)
  const url = api.sdkFileApiUrl({ url: '/bots/{bot}/container/fs/download', path: { bot }, query: { path: '/data/http-origin-original.bin', ...api.sdkAuthQuery() } })
  const start = performance.now(); const response = await api.fileApiFetch(url); const downloaded = [...new Uint8Array(await response.arrayBuffer())]; const durationMs = performance.now() - start
  const hash = '5eb802f8c45fdd95c61a40e9aedf8bf6e42964902307bcd7f7c331dfac32b26f'
  const asset = await identity.fetch(new Request(logicalOrigin + `/api/bots/${bot}/media/${hash}`, { headers: { Authorization: 'Bearer ' + api.sdkAuthQuery().token } }))
  const previewBytes = [...new Uint8Array(await asset.arrayBuffer())]
  const preview = api.sdkFileApiUrl({ url: '/bots/{bot}/media/{hash}', path: { bot, hash }, query: api.sdkAuthQuery() })
  const image = new Image(); image.id = 'same-origin-http-preview'; image.style.cssText = 'position:fixed;right:24px;top:80px;width:64px;height:64px;z-index:10000'; document.body.append(image)
  await new Promise((resolve, reject) => { image.onload = resolve; image.onerror = reject; image.src = preview })
  const before = (await storage.listJournals()).length
  const file = new File(['original upload preserved'], 'http-origin-resume-probe.bin')
  let failure
  try { await m.uploadMedia(identity, file, 'http-origin-resume-probe', 1024 * 1024, new AbortController().signal) } catch (error) { failure = { name: error.name, message: error.message } }
  return { documentOrigin: location.origin, secureContext: isSecureContext, logicalIdentity: identity.origin, physicalOrigin, sdkApiBase: api.sdkApiBaseUrl(), originalOriginJournalVisible: !!(await storage.readJournal(secureKey)), capabilities: { locks: typeof navigator.locks?.request, opfs: typeof navigator.storage?.getDirectory, indexedDB: typeof indexedDB }, downloaded, downloadStatus: response.status, durationMs, previewBytes, previewWidth: image.naturalWidth, previewSameOrigin: new URL(preview).origin === location.origin, resumedUploadFailure: failure, sourceFileJournalKey: m.mediaFileJournalKey(file), journalsBefore: before, journalsAfter: (await storage.listJournals()).length }
}, { bot: fixture.bot, logicalOrigin, physicalOrigin, secureKey: secureStorage.key })
assert.equal(result.secureContext, false)
assert.equal(result.logicalIdentity, logicalOrigin + '/api')
assert.equal(result.sdkApiBase, logicalOrigin + '/api')
assert.equal(result.sourceFileJournalKey, secureStorage.key)
assert.deepEqual(Buffer.from(result.downloaded), fileBytes)
assert.equal(result.previewSameOrigin, true); assert.equal(result.previewWidth, 8)
assert.equal(result.capabilities.locks, 'undefined'); assert.equal(result.capabilities.opfs, 'undefined')
assert.equal(result.originalOriginJournalVisible, false)
assert.equal(result.resumedUploadFailure.name, 'TypeError')
assert.equal(result.journalsBefore, result.journalsAfter)
await page.screenshot({ path: out + '/actual-http-preview-and-files.png', fullPage: true })

const restored = await context.newPage(); await restored.goto(logicalOrigin)
const originalPreserved = await restored.evaluate(async key => { const storage = await import('/src/lib/media-transfer-storage.ts'); return !!(await storage.readJournal(key)) && await (await storage.localFile(key)).text() === 'original upload preserved' }, secureStorage.key)
assert.equal(originalPreserved, true)
const hashes = { download: crypto.createHash('sha256').update(fileBytes).digest('hex'), preview: crypto.createHash('sha256').update(Buffer.from(result.previewBytes)).digest('hex') }
delete result.downloaded; delete result.previewBytes
assert.equal(physicalRequests.filter(row => row.path.endsWith('/media/uploads') && row.method === 'POST').length, 0)
assert.equal(pageErrors.length, 0)
const proof = { environment: 'Current actual Vite/Go/PostgreSQL/Chromium; non-loopback HTTP browser origin mapped to isolated local service', ...result, secureOriginalIntent: secureStorage, sameLogicalJournalKeyAcrossPageOrigins: result.sourceFileJournalKey === secureStorage.key, bytes: fileBytes.length, hashes, logicalFixtureRequests, originalSecureOriginStoragePreserved: originalPreserved, noNewMediaReceipt: true, pageErrors, mixedContentMessages: consoleMessages.filter(message => message.includes('Mixed Content')), securityBypass: false, publicNasOrPhone: false, noNewService: true }
fs.writeFileSync(out + '/proof.json', JSON.stringify(proof, null, 2) + '\n')
fs.writeFileSync(out + '/physical-requests.json', JSON.stringify(physicalRequests, null, 2) + '\n')
console.log(JSON.stringify(proof, null, 2))
await context.unrouteAll({ behavior: 'ignoreErrors' })
await browser.close()
