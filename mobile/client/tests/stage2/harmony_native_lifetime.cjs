// Real production bridge/engines, disk and HTTP; Kit boundaries only. No device claim.
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path'), os = require('node:os'), http = require('node:http'), crypto = require('node:crypto'), vm = require('node:vm')
const sourceRef = process.env.HARMONY_LIFETIME_SOURCE_REF
if (sourceRef) assert.match(sourceRef, /^[a-f0-9]{40}$/)
const root = path.resolve(__dirname, '../..'), ts = require(process.env.HARMONY_TEST_TYPESCRIPT || path.join(root, 'mobile/.build/memoh-web/checkout/node_modules/typescript'))
const common = path.join(root, 'mobile/harmony/app/entry/src/main/ets/common'), dir = fs.mkdtempSync(path.join(os.tmpdir(), 'harmony-lifetime-'))
const positions = new Map(), fds = new Set(), receipts = new Map(), events = [], cache = new Map()
const bytes = Buffer.alloc(524301, 67), hash = crypto.createHash('sha256').update(bytes).digest('hex'), context = { filesDir: dir }
let origin, holdUpload = false, holdDownload = false, uploadHeld, downloadHeld, pickerCalls = 0, cancelled = 0
const io = {
  OpenMode: { READ_ONLY: 0, WRITE_ONLY: 1, READ_WRITE: 2, CREATE: fs.constants.O_CREAT, TRUNC: fs.constants.O_TRUNC, NOFOLLOW: fs.constants.O_NOFOLLOW }, WhenceType: { SEEK_SET: 0 },
  openSync(p, mode) { const fd = fs.openSync(p, mode); fds.add(fd); positions.set(fd, 0); return { fd } },
  async open(p, mode) { return io.openSync(p.startsWith('file:') ? new URL(p) : p, mode) },
  closeSync(f) { const fd = typeof f === 'number' ? f : f.fd; fds.delete(fd); positions.delete(fd); fs.closeSync(fd) },
  accessSync: fs.existsSync, mkdirSync: fs.mkdirSync, listFileSync: fs.readdirSync, unlinkSync: fs.unlinkSync, renameSync: fs.renameSync, readTextSync: p => fs.readFileSync(p, 'utf8'),
  statSync: p => typeof p === 'number' ? fs.fstatSync(p) : fs.statSync(p), fsyncSync: fs.fsyncSync, truncateSync: fs.ftruncateSync,
  lseek: (fd, at) => positions.set(fd, at),
  async read(fd, b, options = {}) { const at = options.offset ?? positions.get(fd) ?? 0; const n = fs.readSync(fd, new Uint8Array(b), 0, options.length ?? b.byteLength, at); positions.set(fd, at + n); return n },
  writeSync(fd, b, options = {}) { const at = options.offset ?? positions.get(fd) ?? 0; const n = fs.writeSync(fd, new Uint8Array(b), 0, options.length ?? b.byteLength, at); positions.set(fd, at + n); return n },
  async write(...args) { return io.writeSync(...args) },
}
class Request { constructor(url, method, headers, content) { Object.assign(this, { url, method, headers, content }) } }
const rcp = { Request, createSession() {
  const active = new Set()
  return { cancel() { cancelled++; for (const q of active) q.destroy(Error('cancelled')) }, close() { for (const q of active) q.destroy() }, fetch(request) {
    assert.equal(request.configuration.transfer.autoRedirect, false)
    return new Promise((resolve, reject) => {
      const wire = new URL(request.url); assert.equal(wire.hostname, 'nas.example'); wire.hostname = '127.0.0.1'
      const q = http.request(wire, { method: request.method, headers: request.headers }, res => {
        request.configuration.tracing?.httpEventsHandler?.onStatusCodeReceive(res.statusCode)
        request.configuration.tracing?.httpEventsHandler?.onHeaderReceive(res.headers)
        res.on('aborted', () => reject(Error('aborted'))); res.on('error', reject)
        res.on('data', b => { try { request.destination?.(b.buffer.slice(b.byteOffset, b.byteOffset + b.length)) } catch (e) { q.destroy(e) } })
        res.on('end', () => { active.delete(q); resolve({ statusCode: res.statusCode, headers: res.headers }) })
      }); active.add(q); q.on('error', e => { active.delete(q); reject(e) })
      q.end(typeof request.content === 'string' ? request.content : request.content === undefined ? undefined : Buffer.from(request.content))
    })
  } }
} }
const modules = {
  '@kit.CoreFileKit': { fileIo: io, picker: { DocumentSaveOptions: class {}, DocumentViewPicker: class { async save() { pickerCalls++; const target = path.join(dir, 'saved.bin'); fs.writeFileSync(target, ''); return ['file://' + target] } } } },
  '@kit.ArkTS': { url: { URL: { parseURL: v => new URL(v) } }, util: { TextEncoder: class { encodeInto(v) { return new TextEncoder().encode(v) } }, TextDecoder: class { decodeToString(v) { return new TextDecoder().decode(v) } }, Base64Helper: class { decodeSync(v) { return new Uint8Array(Buffer.from(v, 'base64')) } }, generateRandomUUID: () => crypto.randomUUID().replaceAll('-', '') } },
  '@kit.CryptoArchitectureKit': { cryptoFramework: { createMd() { const h = crypto.createHash('sha256'); return { async update({ data }) { h.update(data) }, async digest() { return { data: new Uint8Array(h.digest()) } } } } } },
  '@kit.RemoteCommunicationKit': { rcp }, '@kit.ArkData': { uniformTypeDescriptor: {} }, '@kit.NetworkKit': {},
  './NativePhotoSelection': { NativePhotoSelection: class {} }, './ClientDiagnostics': { ClientDiagnostics: { event() {}, failure() {} } },
  './NativeVoice': { NativeVoice: class { prepare() {} close() {} } },
}
function load(name) {
  if (cache.has(name)) return cache.get(name)
  const file = name === 'ServiceUrl' ? path.join(root, 'mobile/memoh/harmony-service-url.ts') : path.join(common, name + '.ets')
  const m = { exports: {} }; cache.set(name, m.exports)
  vm.runInNewContext(ts.transpileModule(sourceRef ? require('node:child_process').execFileSync('git', ['show', sourceRef + ':mobile/client/' + path.relative(root, file)], { cwd: root, encoding: 'utf8' }) : fs.readFileSync(file, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText, {
    module: m, exports: m.exports, require: n => n in modules ? modules[n] : n.startsWith('./') ? load(n.slice(2)) : assert.fail('unexpected module ' + n),
    ArrayBuffer, Uint8Array, Map, Set, Promise, Error, Date, JSON, Math, Number, String, URL, setTimeout,
  }); return m.exports
}
const json = (res, status, value) => { if (!res.destroyed) { res.writeHead(status, { 'content-type': 'application/json' }); res.end(JSON.stringify(value)) } }
const server = http.createServer(async (req, res) => {
  try {
    const route = new URL(req.url, 'http://fixture').pathname, token = req.headers.authorization
    events.push([req.method, route, token])
    if (route === '/api/users/me') return json(res, 200, { id: token === 'Bearer bob' ? 'bob' : 'alice' })
    if (route.endsWith('/limits')) return json(res, 200, { max_upload_bytes: bytes.length, resumable_enabled: true, chunk_max_bytes: 262144 })
    if (route === '/api/bots/bot-a/media/' + hash) {
      const start = Number(/^bytes=(\d+)-$/.exec(req.headers.range || '')?.[1] || 0)
      const headers = { etag: '"' + hash + '"', 'content-length': bytes.length - start, 'content-type': 'application/octet-stream' }
      if (req.method === 'HEAD') { res.writeHead(200, headers); return res.end() }
      headers['content-range'] = `bytes ${start}-${bytes.length - 1}/${bytes.length}`; res.writeHead(206, headers)
      if (holdDownload) { holdDownload = false; res.write(bytes.subarray(start, start + 65536)); downloadHeld = () => res.end(bytes.subarray(start + 65536)); return }
      return res.end(bytes.subarray(start))
    }
    const body = []; for await (const b of req) body.push(b); const payload = Buffer.concat(body)
    if (route.endsWith('/uploads') && req.method === 'POST') {
      const input = JSON.parse(payload), id = crypto.randomUUID(), receipt = { id, state: 'uploading', size_bytes: input.size_bytes, offset: 0, mime: input.mime, expires_at: new Date(Date.now() + 86400000).toISOString() }
      receipts.set(id, receipt); return json(res, 201, receipt)
    }
    const match = /\/uploads\/([^/]+)(\/complete)?$/.exec(route), receipt = match && receipts.get(match[1])
    if (!receipt) return json(res, 404, {})
    if (req.method === 'GET') return json(res, 200, receipt)
    if (req.method === 'DELETE') { receipt.state = 'canceled'; return json(res, 200, receipt) }
    if (req.method === 'PATCH') {
      assert.equal(Number(req.headers['upload-offset']), receipt.offset)
      assert.equal(crypto.createHash('sha256').update(payload).digest('hex'), req.headers['upload-sha256'])
      fs.appendFileSync(path.join(dir, receipt.id), payload); receipt.offset += payload.length
      if (holdUpload) { holdUpload = false; uploadHeld = () => json(res, 200, receipt); return }
      return json(res, 200, receipt)
    }
    if (match[2] && req.method === 'POST') { receipt.state = 'ready'; receipt.content_hash = hash; return json(res, 200, receipt) }
    json(res, 400, {})
  } catch (e) { res.destroy(e) }
})
const wait = async predicate => { for (let i = 0; i < 300; i++) { if (predicate()) return; await new Promise(r => setTimeout(r, 5)) } assert.fail('fixture did not reach held I/O') }
let Client
async function page() {
  const frame = 'begonia-client://app/index.html', client = new Client({ registerJavaScriptProxy() {}, deleteJavaScriptRegister() {}, getUrl: () => frame, getLastJavascriptProxyCallingFrameUrl: () => frame }, () => {}, async () => {}, context)
  assert.equal(client.register(), true)
  const send = fields => client.request(JSON.stringify({ version: 1, ...fields })).then(JSON.parse)
  assert.equal((await send({ command: 'client.configure', apiOrigin: origin })).ok, true)
  assert.equal((await send({ command: 'client.state', state: 'ready', authorization: 'Bearer alice' })).ok, true)
  return { client, send }
}
function pageHost(client) {
  const source = fs.readFileSync(path.join(root, 'mobile/harmony/app/entry/src/main/ets/pages/SharedChat.ets'), 'utf8')
  const methods = [['  aboutToDisappear()', '  private attachNative()'], ['  private attachNative()', '  private cancelWebDownloads()'], ['  private renderExited(', '  private renderNotResponding(']]
    .map(([a, b]) => source.slice(source.indexOf(a), source.indexOf(b, source.indexOf(a))))
  const m = { exports: {} }, frame = 'begonia-client://app/index.html'
  vm.runInNewContext(ts.transpileModule('export class PageHost { ' + methods.join('\n') + ' }', { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText,
    { exports: m.exports, NativeClient: Client, Promise, ImageTimingDump: { detach() {}, bind() {} }, ClientDiagnostics: { event() {} } })
  return Object.assign(new m.exports.PageHost(), { active: true, nativeClient: client, probeGeneration: 0, context,
    controller: { registerJavaScriptProxy() {}, deleteJavaScriptRegister() {}, getUrl: () => frame, getLastJavascriptProxyCallingFrameUrl: () => frame },
    photoSelection: { close() {} }, clearClientWait() {}, cancelWebDownloads() {}, releaseKeyboardInsets() {}, prepareWindow: async () => {}, applyChatInsets() {}, attachDownloads() {}, clientState() {}, setClientTheme: async () => {} })
}
let seq = 0
const scope = () => ({ operation: (++seq).toString(16).padStart(32, '0'), botId: 'bot-a', scope: 'session-a', authorization: 'Bearer alice' })
async function stage(p, authorization = 'Bearer alice') {
  const input = { ...scope(), authorization }, result = await p.send({ command: 'upload.stage.begin', ...input, name: 'small.bin', type: 'application/octet-stream', size: bytes.length })
  assert.equal(result.ok, true, result.error)
  for (let at = 0; at < bytes.length; at += 65536) assert.equal((await p.send({ command: 'upload.stage.append', ...input, offset: at, data: bytes.subarray(at, at + 65536).toString('base64') })).ok, true)
  const ready = await p.send({ command: 'upload.stage.finish', ...input }); assert.equal(ready.ok, true, ready.error); return ready.data
}
async function main() {
  await new Promise(r => server.listen(0, '127.0.0.1', r)); origin = 'http://nas.example:' + server.address().port + '/api'; Client = load('NativeClient').NativeClient
  let p = await page(), local = await stage(p)
  holdUpload = true; const input = { ...scope(), handle: local.handle }, old = p.send({ command: 'upload.run', ...input }); await wait(() => uploadHeld)
  const before = cancelled; pageHost(p.client).aboutToDisappear(); assert.equal(cancelled, before, 'page detach cancelled accepted native upload')
  p = await page(); pageHost(p.client).renderExited(0); assert.equal(cancelled, before, 'render exit cancelled accepted upload')
  p = await page(); const replacement = pageHost(p.client); replacement.attachNative(); assert.equal(cancelled, before, 'bridge rebind cancelled accepted upload')
  p = { client: replacement.nativeClient, send: fields => replacement.nativeClient.request(JSON.stringify({ version: 1, ...fields })).then(JSON.parse) }
  await p.send({ command: 'client.configure', apiOrigin: origin }); await p.send({ command: 'client.state', state: 'ready', authorization: 'Bearer alice' })
  const joined = p.send({ command: 'upload.run', ...scope(), handle: local.handle }); uploadHeld(); uploadHeld = undefined
  assert.equal((await old).ok, false, 'detached page received a late successful reply'); assert.equal((await joined).ok, true)
  assert.equal(events.filter(([method, route]) => method === 'POST' && route.endsWith('/uploads')).length, 1)
  const journalDir = path.join(dir, 'memoh-upload-journal-v1'), journal = JSON.parse(fs.readFileSync(path.join(journalDir, local.handle + '.json')))
  assert.equal(journal.state, 'ready'); assert.equal(crypto.createHash('sha256').update(fs.readFileSync(path.join(dir, journal.uploadId))).digest('hex'), hash)
  holdDownload = true; const download = { operation: (++seq).toString(16).padStart(32, '0'), botId: 'bot-a', hash, size: bytes.length, name: 'small.bin', url: origin + '/bots/bot-a/media/' + hash, authorization: 'Bearer alice' }
  const oldDownload = p.send({ command: 'download.fetch', ...download }); await wait(() => downloadHeld); const prior = cancelled; p.client.close(); assert.equal(cancelled, prior)
  p = await page(); const next = { ...download, operation: (++seq).toString(16).padStart(32, '0') }, resumed = p.send({ command: 'download.fetch', ...next }); downloadHeld(); downloadHeld = undefined
  assert.equal((await oldDownload).ok, false); const result = await resumed; assert.equal(result.ok, true, result.error); assert.equal(result.data.operation, next.operation); assert.equal(pickerCalls, 0)
  assert.equal((await p.send({ command: 'download.save', operation: next.operation, botId: next.botId })).ok, true)
  assert.equal(crypto.createHash('sha256').update(fs.readFileSync(path.join(dir, 'saved.bin'))).digest('hex'), hash); assert.equal(pickerCalls, 1)
  // Explicit pause cancels I/O, retains the receipt and resumes its accepted offset.
  local = await stage(p); holdUpload = true; const pausedInput = { ...scope(), handle: local.handle }
  const paused = p.send({ command: 'upload.run', ...pausedInput }); await wait(() => uploadHeld)
  const listing = await p.send({ command: 'upload.list', ...scope(), handle: local.handle }); assert.equal(listing.data[0].handle, local.handle)
  const beforePause = cancelled; assert.equal((await p.send({ command: 'upload.pause', operation: pausedInput.operation })).ok, true)
  assert.ok(cancelled > beforePause); assert.equal((await paused).ok, false); uploadHeld(); uploadHeld = undefined
  const saved = JSON.parse(fs.readFileSync(path.join(journalDir, local.handle + '.json'))), count = receipts.size
  assert.ok(saved.uploadId); assert.equal(saved.state, 'uploading')
  assert.equal((await p.send({ command: 'upload.run', ...scope(), handle: local.handle })).ok, true)
  assert.equal(receipts.size, count); assert.equal(JSON.parse(fs.readFileSync(path.join(journalDir, local.handle + '.json'))).uploadId, saved.uploadId)
  // Explicit abort keeps its tombstone and does not become a detached observer.
  local = await stage(p); holdUpload = true; const abortInput = { ...scope(), handle: local.handle }
  const aborted = p.send({ command: 'upload.run', ...abortInput }); await wait(() => uploadHeld)
  await p.send({ command: 'upload.abort', operation: abortInput.operation }); assert.equal((await aborted).ok, false)
  uploadHeld(); uploadHeld = undefined
  await wait(() => events.some(([method, route]) => method === 'DELETE' && route.endsWith(JSON.parse(fs.readFileSync(path.join(journalDir, local.handle + '.json'))).uploadId)))
  assert.equal(JSON.parse(fs.readFileSync(path.join(journalDir, local.handle + '.json'))).state, 'canceled')
  assert.equal(fs.existsSync(path.join(journalDir, local.handle + '.original')), false)
  // Incomplete Web File intake has no accepted original or successful journal.
  const partial = await p.send({ command: 'upload.stage.begin', ...scope(), name: 'partial.bin', type: 'application/octet-stream', size: bytes.length })
  assert.equal(partial.ok, true); p.client.close(); assert.equal(fs.existsSync(path.join(journalDir, partial.data.handle + '.original')), false)
  assert.equal(fs.existsSync(path.join(journalDir, partial.data.handle + '.json')), false); p = await page()
  // Unknown Create still refuses another POST under the original protocol.
  local = await stage(p); const uncertainPath = path.join(journalDir, local.handle + '.json'), uncertain = JSON.parse(fs.readFileSync(uncertainPath))
  uncertain.state = 'create-unknown'; fs.writeFileSync(uncertainPath, JSON.stringify(uncertain)); const creates = receipts.size
  const unknown = await p.send({ command: 'upload.run', ...scope(), handle: local.handle }); assert.equal(unknown.error, 'UPLOAD_CREATE_UNCERTAIN'); assert.equal(receipts.size, creates)
  // Explicit download cancellation still removes this operation's incomplete cache.
  holdDownload = true; const cancelledInput = { ...download, operation: (++seq).toString(16).padStart(32, '0') }
  const cancelledDownload = p.send({ command: 'download.fetch', ...cancelledInput }); await wait(() => downloadHeld)
  await p.send({ command: 'download.abort', operation: cancelledInput.operation, botId: cancelledInput.botId })
  assert.equal((await cancelledDownload).ok, false); downloadHeld(); downloadHeld = undefined
  assert.deepEqual(fs.readdirSync(path.join(dir, 'memoh-download-journal-v1')), [])
  // Account revocation stops the old engine; its retained original cannot be adopted by Bob.
  local = await stage(p); holdUpload = true; const stale = p.send({ command: 'upload.run', ...scope(), handle: local.handle }); await wait(() => uploadHeld)
  await p.send({ command: 'client.state', state: 'login-page' }); assert.equal((await stale).ok, false)
  await p.send({ command: 'client.state', state: 'ready' }); const bob = await p.send({ command: 'upload.run', ...scope(), authorization: 'Bearer bob', handle: local.handle }); assert.equal(bob.ok, false); assert.equal(bob.error, 'UPLOAD_IDENTITY')
  uploadHeld(); uploadHeld = undefined
  // Late Alice cleanup cannot close a newly ready Bob operation in the same document.
  await p.send({ command: 'client.state', state: 'ready', authorization: 'Bearer bob' })
  local = await stage(p, 'Bearer bob'); holdUpload = true; const bobInput = { ...scope(), handle: local.handle, authorization: 'Bearer bob' }
  const bobWork = p.send({ command: 'upload.run', ...bobInput }); await wait(() => uploadHeld); const bobCancels = cancelled
  assert.equal((await p.send({ command: 'upload.close', authorization: 'Bearer alice' })).ok, true)
  assert.equal(cancelled, bobCancels); uploadHeld(); uploadHeld = undefined; assert.equal((await bobWork).ok, true)
  // A new document's authenticated ready event revokes a different old token,
  // even before that document starts another transfer.
  await p.send({ command: 'client.state', state: 'ready', authorization: 'Bearer alice' })
  local = await stage(p); holdUpload = true; const changedSession = p.send({ command: 'upload.run', ...scope(), handle: local.handle }); await wait(() => uploadHeld)
  p.client.close(); p = await page(); await p.send({ command: 'client.state', state: 'ready', authorization: 'Bearer bob' })
  assert.equal((await changedSession).ok, false); uploadHeld(); uploadHeld = undefined
  await p.send({ command: 'client.state', state: 'login-page' }); p.client.close(); await new Promise(setImmediate)
  const owner = load('NativeTransferOwner').nativeTransferOwner(context); owner.destroyAbility(); owner.bindAbility({ filesDir: dir }); p = await page(); await p.send({ command: 'client.state', state: 'login-page' }); p.client.close()
  assert.equal(fds.size, 0)
  console.log(JSON.stringify({ realBridge: true, realEngines: true, realDiskHTTP: true, uploadSurvivesDetach: true, uploadSingleCreate: true, actualDisappearRenderRebind: true, downloadSurvivesDetach: true, noDetachedPicker: true, lateRepliesRejected: true, logoutStopsIO: true, crossAccountDenied: true, readyRevokesDifferentAccount: true, lateCleanupCannotCloseNewAccount: true, explicitPauseResumesSameReceipt: true, activeUploadListReadOnly: true, explicitAbortPreservesTombstone: true, partialIntakeNotAccepted: true, unknownCreateNotReplayed: true, explicitDownloadCancel: true, bytes: bytes.length, fds: fds.size, nativeDevice: false }))
}
main().catch(e => { console.error(e); process.exitCode = 1 }).finally(() => { server.closeAllConnections(); server.close(); fs.rmSync(dir, { recursive: true, force: true }) })
