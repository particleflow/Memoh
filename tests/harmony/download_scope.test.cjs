// Actual TS/ETS modules with controlled SDK, bridge and RCP boundaries.
// These tests exercise routing/auth guards, not native file integrity or TLS.
const assert = require('node:assert/strict')
const fs = require('node:fs'), path = require('node:path'), os = require('node:os'), vm = require('node:vm')
const crypto = require('node:crypto')
const ts = require(process.env.HARMONY_TEST_TYPESCRIPT || 'typescript')
const root = path.resolve(__dirname, '../../mobile/client')
function load(file, imports, globals = {}) {
  const m = { exports: {} }
  const code = ts.transpileModule(fs.readFileSync(path.join(root, file), 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText
  vm.runInNewContext(code, { exports: m.exports, require: name => {
    assert.ok(name in imports, name); return imports[name]
  }, URL, Request, Response, AbortController, DOMException, ArrayBuffer, Uint8Array,
  Map, Set, Promise, Error, crypto: crypto.webcrypto, ...globals })
  return m.exports
}
const validator = load('mobile/memoh/harmony-service-url.ts', {})
const botId = '10000000-0000-4000-8000-000000000001'
const sessionId = '20000000-0000-4000-8000-000000000002'
const delegationId = '30000000-0000-4000-8000-000000000003', hash = 'a'.repeat(64)
const origin = 'https://nas.lan/api', scoped = `/bots/${botId}/sessions/${sessionId}/delegations/${delegationId}/attachments/${hash}`
const ordinary = `/bots/${botId}/media/${hash}`
const calls = [], authResponses = [], listeners = new Set()
let ordinaryCalls = 0, bridgeStatus = 200, generation = 0, token = 'synthetic-only', changeToken = false
const host = { begoniaNative: { request: async raw => {
  const q = JSON.parse(raw); calls.push(q)
  assert.ok(Buffer.byteLength(raw) < 4096)
  if (q.command === 'download.fetch') {
    assert.equal(q.authorization, 'Bearer synthetic-only')
    if (changeToken) token = 'synthetic-new-identity'
    return JSON.stringify(bridgeStatus === 200
      ? { ok: true, data: { operation: q.operation, hash: q.hash, size: q.size } }
      : { ok: false, error: `MEDIA_DOWNLOAD_HTTP_${bridgeStatus}` })
  }
  return JSON.stringify({ ok: true, data: true })
} } }
const api = {
  sdkApiUrl: ({ url }) => origin + url,
  sdkAuthQuery: () => ({ token }),
  createApiFetch: fn => async (...args) => {
    const response = await fn(...args); authResponses.push(response.status); return response
  },
}
const adapter = load('mobile/memoh/harmony-download.ts', {
  './harmony-service-url': validator,
  '@/lib/api-client': api,
  '@/lib/auth-session': { authSessionGeneration: () => generation, onAuthSessionCleared: fn => { listeners.add(fn); return () => listeners.delete(fn) } },
  '@felinic/ui': { toast: { info: () => 'synthetic-notice', dismiss() {} } },
  '@memohai/sdk': { getBotsByBotIdMediaByContentHash: async options => {
    ordinaryCalls++
    const response = await options.fetch(origin + ordinary, {
      method: 'GET', signal: options.signal, headers: { authorization: 'Bearer ' + token },
    })
    assert.equal(response.body, null)
    if (!response.ok) throw Error('ordinary request denied')
  } },
}, { window: host })

async function main() {
  await adapter.saveNativeOriginal(botId, hash, 1, 'file', undefined, scoped)
  assert.equal(ordinaryCalls, 0)
  assert.equal(calls[0].url, origin + scoped)
  assert.deepEqual(calls.map(q => q.command), ['download.fetch', 'download.save', 'download.abort'])
  calls.length = 0
  await adapter.saveNativeOriginal(botId, hash, 1, 'file')
  assert.equal(ordinaryCalls, 1)
  assert.equal(calls[0].url, origin + ordinary)
  const invalid = [null, '', [scoped], { path: scoped }, 'https://other.example' + scoped, '//other.example' + scoped,
    scoped + '?token=stale', scoped + '#x', scoped.replace(hash, 'b'.repeat(64)), scoped.replace('/sessions/', '/%73essions/')]
  for (const value of invalid) {
    calls.length = 0
    await assert.rejects(adapter.saveNativeOriginal(botId, hash, 1, 'file', undefined, value), /MEDIA_DOWNLOAD_SCOPE/)
    assert.equal(calls.length, 0); assert.equal(ordinaryCalls, 1)
  }
  for (const status of [401, 403]) {
    calls.length = 0; bridgeStatus = status
    await assert.rejects(adapter.saveNativeOriginal(botId, hash, 1, 'file', undefined, scoped))
    assert.equal(authResponses.at(-1), status)
    assert.equal(calls.filter(q => q.command === 'download.fetch').length, 1)
    assert.equal(calls.some(q => q.command === 'download.save'), false)
    assert.equal(ordinaryCalls, 1)
  }
  calls.length = 0; bridgeStatus = 200; changeToken = true
  await assert.rejects(adapter.saveNativeOriginal(botId, hash, 1, 'file', undefined, scoped))
  assert.equal(calls.some(q => q.command === 'download.save'), false)
  assert.equal(listeners.size, 0)

  // Run native request admission and authorization failures without inventing
  // the missing NativeMediaFile implementation. Its methods must not be reached.
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'harmony-scope-'))
  const requests = []
  let status = 401, authorized = true
  const io = {
    OpenMode: { READ_WRITE: fs.constants.O_RDWR, CREATE: fs.constants.O_CREAT, NOFOLLOW: fs.constants.O_NOFOLLOW },
    accessSync: fs.existsSync, mkdirSync: fs.mkdirSync, unlinkSync: fs.unlinkSync,
    openSync: (name, flags) => ({ fd: fs.openSync(name, flags) }), closeSync: file => fs.closeSync(file.fd),
  }
  class NativeRequest { constructor(url, method, headers) { Object.assign(this, { url, method, headers }) } }
  const rcp = { Request: NativeRequest, createSession: () => ({ close() {}, cancel() {}, async fetch(q) {
    requests.push(q)
    assert.equal(q.configuration.transfer.autoRedirect, false)
    assert.deepEqual(Object.keys(q.configuration).sort(), ['tracing', 'transfer'])
    q.configuration.tracing.httpEventsHandler.onStatusCodeReceive(status)
    return { statusCode: status, headers: {} }
  } }) }
  const missing = () => { throw Error('Missing NativeMediaFile must not be exercised by scope tests') }
  const native = load('mobile/harmony/app/entry/src/main/ets/common/NativeMediaDownload.ets', {
    '@kit.CoreFileKit': { fileIo: io, picker: {} }, '@kit.RemoteCommunicationKit': { rcp },
    './MemohClientConfig': { MEMOH_ORIGIN: origin }, './ServiceUrl': validator,
    './NativeMediaFile': { copyMediaBytes: missing, verifyMediaFd: missing },
  })
  const service = new native.NativeMediaDownload({ cacheDir: directory }, () => authorized)
  let sequence = 0
  const input = url => ({ operation: (++sequence).toString(16).padStart(32, '0'), botId,
    hash, size: 1, name: 'file', url, authorization: 'Bearer synthetic-only' })
  try {
    for (status of [401, 403]) {
      requests.length = 0
      await assert.rejects(service.fetch(input(origin + scoped)), new RegExp(`MEDIA_DOWNLOAD_HTTP_${status}`))
      assert.equal(requests.length, 1); assert.equal(requests[0].url, origin + scoped)
      assert.equal(requests[0].headers.authorization, 'Bearer synthetic-only')
    }
    status = 302; requests.length = 0
    await assert.rejects(service.fetch(input(origin + scoped)), /MEDIA_DOWNLOAD_INCOMPLETE/)
    assert.equal(requests.length, 1); assert.equal(requests[0].url, origin + scoped)
    status = 403
    for (const url of ['https://other.example/api' + scoped, origin + '-other' + scoped,
      origin + scoped + '?token=x', origin + scoped.replace(hash, 'b'.repeat(64)), origin + '/bots/other/media/' + hash]) {
      requests.length = 0
      await assert.rejects(service.fetch(input(url)), /MEDIA_DOWNLOAD_REQUEST/)
      assert.equal(requests.length, 0)
    }
    requests.length = 0
    await assert.rejects(service.fetch(input(origin + ordinary)), /MEDIA_DOWNLOAD_HTTP_403/)
    assert.equal(requests[0].url, origin + ordinary)
    requests.length = 0; authorized = false
    await assert.rejects(service.fetch(input(origin + scoped)), /MEDIA_DOWNLOAD_REQUEST/)
    assert.equal(requests.length, 0)
  } finally { service.close(); fs.rmSync(directory, { recursive: true, force: true }) }
  console.log(JSON.stringify({ passed: true, malformedScopes: invalid.length, nativeInvalidUrls: 5,
    distinctBotSessionDelegationIds: true, scopedWebEnvelope: true, scopedNativeAdmission: true, noBotHashFallbackOnDenial: true,
    unchangedOrdinaryDownload: true, accountChangeRejected: true, redirectsDisabled: true,
    scope: 'Actual TS/ETS routing with controlled SDK/RCP seams; no native integrity, bridge router, TLS or phone acceptance' }))
}
main().catch(error => { console.error(error); process.exitCode = 1 })
