// E8 attachment transfer progress — stage2 acceptance checks (composed tree level).
// Slow-replayed large attachment: ring progress fills monotonically to full then
// the state switches to complete; injected failure flips the failed state; the
// composed card carries no status-copy text nodes; speed formatting is unit
// checked. Controlled seams only: no device, no live API.
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path'), vm = require('node:vm'), crypto = require('node:crypto')
const root = path.resolve(__dirname, '../..')
const ts = require(process.env.HARMONY_TEST_TYPESCRIPT || path.join(root, 'mobile/.build/memoh-web/checkout/node_modules/typescript'))
const contextTar = process.env.HARMONY_CONTEXT_TAR || path.join(root, 'mobile/.build/memoh-web/official-context.tar')

function load(file, imports, globals = {}) {
  const module = { exports: {} }
  const code = ts.transpileModule(fs.readFileSync(path.join(root, file), 'utf8'),
    { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const clock = { now: 1_000_000 }
  class FakeDate extends Date { static now() { return clock.now } }
  vm.runInNewContext(code, {
    exports: module.exports, module, require: request => {
      if (request in imports) return imports[request]
      throw new Error('unexpected import ' + request)
    },
    console, ArrayBuffer, Uint8Array, Map, Set, Promise, Error, TypeError, DOMException,
    Request, Response, AbortController, URL, TextEncoder, crypto: crypto.webcrypto,
    Date: FakeDate, ...globals,
  })
  return { exports: module.exports, clock }
}

// Minimal uncompressed-tar reader for composed-tree assertions.
function readTarEntry(file, name) {
  const fd = fs.openSync(file, 'r')
  try {
    const header = Buffer.alloc(512)
    for (;;) {
      const read = fs.readSync(fd, header, 0, 512, null)
      if (read < 512 || !header[0]) break
      const type = String.fromCharCode(header[156])
      const entry = header.toString('utf8', 0, 100).replace(/\0.*$/, '')
      const size = parseInt(header.toString('utf8', 124, 136).replace(/0*$/, '').trim() || '0', 8) || 0
      const data = Buffer.alloc(size)
      if (size) fs.readSync(fd, data, 0, size, null)
      const pad = (512 - (size % 512)) % 512
      if (pad) fs.readSync(fd, Buffer.alloc(pad), 0, pad, null)
      if (entry === name && type === '0') return data
    }
  } finally { fs.closeSync(fd) }
  throw new Error('composed entry missing: ' + name)
}

const vue = { shallowReactive: value => value, toRaw: value => value }
const progressModule = load('mobile/memoh/harmony-transfer-progress.ts', { vue })
const progress = progressModule.exports
const clockRef = progressModule.clock
const hash = 'c'.repeat(64)
const RING_CIRCUMFERENCE = 2 * Math.PI * 8
const dashoffset = fraction => RING_CIRCUMFERENCE * (1 - fraction)
const SPEED_SHAPE = /^\d+(\.\d)? (B|KB|MB)\/s$/

// --- speed formatting: magnitude-selected units, one decimal, MB ceiling ---
assert.equal(progress.formatTransferSpeed(0), '')
assert.equal(progress.formatTransferSpeed(-5), '')
assert.equal(progress.formatTransferSpeed(512.4), '512 B/s')
assert.equal(progress.formatTransferSpeed(1023.9), '1024 B/s')
assert.equal(progress.formatTransferSpeed(1536), '1.5 KB/s')
assert.equal(progress.formatTransferSpeed(200 * 1024), '200.0 KB/s')
assert.equal(progress.formatTransferSpeed(1.2 * 1024 * 1024), '1.2 MB/s')
assert.equal(progress.formatTransferSpeed(512 * 1024 * 1024), '512.0 MB/s')
assert.equal(progress.formatTransferSpeed(3 * 1024 * 1024 * 1024), '3072.0 MB/s')

// --- slow large-attachment replay: ring fills monotonically to full ---
const total = 5 * 1024 * 1024, chunks = 10, step = total / chunks, tick = 250
const attachment = { content_hash: hash, metadata: {} }
{
  try {
    const fractions = []
    let rated = 0
    for (let index = 1; index <= chunks; index++) {
      progress.noteDownloadBytes(hash, index * step, total)
      const view = progress.attachmentTransferView(attachment)
      assert.equal(view.phase, 'transferring')
      fractions.push(view.fraction)
      const message = progress.messageTransferView([attachment])
      assert.equal(message.active, true)
      assert.equal(message.fraction, view.fraction)
      if (message.rate != null) {
        assert.ok(SPEED_SHAPE.test(progress.formatTransferSpeed(message.rate)))
        rated++
      }
      clockRef.now += tick
    }
    assert.ok(rated >= chunks - 1, 'speed label must render after the first window sample')
    assert.equal(fractions[fractions.length - 1], 1)
    const offsets = fractions.map(dashoffset)
    for (let index = 1; index < offsets.length; index++) {
      assert.ok(offsets[index] < offsets[index - 1], 'ring fill must be strictly monotonic')
      assert.ok(offsets[index] >= 0)
    }
    // Measured replay rate: 512 KiB per 250 ms window.
    const view = progress.attachmentTransferView(attachment)
    assert.ok(view.rate > 0)
    assert.equal(progress.formatTransferSpeed(view.rate), `${(step * 1000 / tick / (1024 * 1024)).toFixed(1)} MB/s`)
    // Completion: the read resolves, the entry retires and the indicator ends.
    progress.clearDownloadBytes(hash)
    const done = progress.attachmentTransferView(attachment)
    assert.equal(done.phase, 'idle')
    assert.equal(progress.messageTransferView([attachment]).active, false)
  } catch (error) { throw error }
}

// --- upload wire bytes: same fill from the registry side, then ready switch ---
const upload = { content_hash: undefined, metadata: { harmony_transfer: 'uploading' } }
progress.noteUploadBytes(upload, 250, 1000)
assert.equal(progress.attachmentTransferView(upload).fraction, 0.25)
progress.noteUploadBytes(upload, 1000, 1000)
assert.equal(progress.attachmentTransferView(upload).fraction, 1)
Object.assign(upload.metadata, { harmony_transfer: 'ready' })
progress.clearUploadBytes(upload)
assert.equal(progress.attachmentTransferView(upload).phase, 'ready')

// --- injected upload failure marks the failed card state (real submission) ---
const submissionFailure = (async () => {
  const bytes = new Uint8Array(8)
  const file = { name: 'original.bin', type: 'application/octet-stream', size: 8, arrayBuffer: async () => bytes.buffer }
  const files = load('mobile/memoh/harmony-files.ts', {
    vue,
    '@memohai/sdk': {
      postBotsByBotIdMedia: async () => { throw new Error('injected upload failure') },
      getBotsByBotIdMediaLimits: async () => ({ data: { max_upload_bytes: 1024 * 1024 } }),
      getBotsByBotIdMediaByContentHash: async () => ({ response: { status: 404 } }),
      getBotsByBotIdContainerFsDownload: {}, getBotsByBotIdContainerFsRead: {},
    },
    '@memohai/sdk/client': { client: { getConfig: () => ({}) } },
    './harmony-image-timing': { imageTimingHandle: () => undefined, imageTimingFetch: (_h, s) => s, imageTimingMark: () => {}, imageTimingResource: () => {}, imageTimingBlob: () => {} },
    '@/lib/api-client': { createApiFetch: fn => fn ?? (async (...args) => fetch(...args)), sdkApiUrl: ({ url }) => 'https://api.example.test/api' + url, sdkAuthQuery: () => ({ token: 'synthetic-test-only' }) },
    './harmony-diagnostics': { diagnosticTransfer: () => () => {}, responseRequestTag: () => '' },
    './harmony-service-url': { normalizeDelegationPath: (url, h) => `/delegated/${h}` },
    './harmony-download': { saveNativeOriginal: async () => {} },
    './harmony-resumable': { nativeAttachmentCommand: async () => { throw new Error('native unavailable') }, stageNativeFile: async () => { throw new Error('native unavailable') }, forgetNativeFile: async () => {}, closeNativeAttachments: async () => {}, nativeAttachmentsAvailable: false, validateNativeAttachments: () => [], nativeUploadFailure: () => undefined },
    './harmony-transfer-progress': progress,
  }, { window: {} }).exports
  const submission = files.prepareSubmission('bot-a', [file])
  const card = submission.attachments[0]
  assert.equal(card.metadata.harmony_transfer, 'uploading')
  await assert.rejects(submission.prepareAttachments(new AbortController().signal), /injected upload failure/)
  assert.equal(card.metadata.harmony_transfer, 'failed')
  assert.equal(progress.attachmentTransferView(card).phase, 'failed')
})()

// --- composed tree: card states are visual-only; ring sits after the bubbles ---

// --- composed tree: card states are visual-only; ring sits after the bubbles ---
submissionFailure.then(() => {
  const card = readTarEntry(contextTar, 'apps/web/src/pages/home/components/chat-attachment-card.vue').toString('utf8')
  assert.ok(card.includes('HarmonyTransferRing'), 'transferring ring visual')
  assert.ok(card.includes('CircleAlert'), 'failed visual')
  assert.ok(card.includes("<Check class=\"size-4\" />"), 'completed visual')
  assert.ok(!card.includes('transferLabel'), 'no transfer status copy binding')
  assert.ok(!/%\}|title=.*transfer|aria-label=.*transfer/.test(card), 'no percentage or transfer tooltip text')
  assert.ok(!/上传|校验|准备|传输中|失败/.test(card), 'no status copy anywhere on the card')
  const block = readTarEntry(contextTar, 'apps/web/src/pages/home/components/attachment-block.vue').toString('utf8')
  assert.ok(block.includes('attachmentTransferView') && block.includes('noteDownloadBytes') && block.includes('clearDownloadBytes'))
  const item = readTarEntry(contextTar, 'apps/web/src/pages/home/components/message-item.vue').toString('utf8')
  assert.ok(item.indexOf('</div>\n        <HarmonyTransferRing') > 0, 'ring after the assistant speech bubble')
  assert.ok(/userBubbleText"\n([\s\S]*?)<\/div>\n        <HarmonyTransferRing/.test(item), 'ring after the user bubble')
  assert.ok(item.includes('messageTransferView'))
  const ring = readTarEntry(contextTar, 'apps/web/src/lib/HarmonyTransferRing.vue').toString('utf8')
  assert.ok(ring.includes('stroke-dashoffset') && ring.includes('harmony-transfer-ring-spin') && ring.includes('formatTransferSpeed'))
  const library = readTarEntry(contextTar, 'apps/web/src/lib/harmony-transfer-progress.ts').toString('utf8')
  assert.ok(library.includes('formatTransferSpeed') && library.includes('messageTransferView'))

  console.log(JSON.stringify({
    actualComposedTree: true, slowReplayMonotonicToFull: true, completionSwitch: true,
    injectedFailureFailedState: true, noStatusCopyTextNodes: true, speedFormatting: true,
    ringAfterUserAndAssistantBubble: true, scope: 'composed official-context.tar, controlled SDK seams; not device or live API',
  }))
}, error => { console.error(error); process.exitCode = 1 })
