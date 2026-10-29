// Actual ArkTS selector + actual snapshot/hash handoff with real filesystem
// bytes. SDK boundaries and image codecs are host adapters, not device proof.
const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const os = require('node:os')
const vm = require('node:vm')
const crypto = require('node:crypto')
const { spawnSync } = require('node:child_process')
const { fileURLToPath, pathToFileURL } = require('node:url')
const root = path.resolve(__dirname, '../..')
const ts = require(process.env.HARMONY_TEST_TYPESCRIPT || path.join(root, 'mobile/.build/memoh-web/checkout/node_modules/typescript'))
const baseline = process.argv.includes('--baseline')
const sourceDir = path.join(root, 'mobile/harmony/app/entry/src/main/ets/common')
const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'harmony-photo-dual-'))
const fixtureTool = path.join(__dirname, 'harmony_photo_fixtures.py')
const sha = bytes => crypto.createHash('sha256').update(bytes).digest('hex')
const results = [], faults = [], batchChecks = [], diagnostics = [], positions = new Map(), openFiles = new Map()
let uris = [], original = false, mode = '', failed = false, failUnlink = false, readGrant, shortWrites = false
let context, imageDecodes = 0, selectorReads = [], options
function python(...args) {
  const result = spawnSync('python3', [fixtureTool, ...args], { maxBuffer: 4 * 1024 * 1024 })
  assert.equal(result.status, 0, result.stderr.toString())
  return result.stdout
}
const fixtures = JSON.parse(python('create', path.join(temp, 'fixtures')))
const fromUri = value => value.startsWith('file:') ? fileURLToPath(value) : value
function open(filePath, flags) {
  const file = { fd: fs.openSync(fromUri(filePath), flags) }
  positions.set(file.fd, 0); openFiles.set(file.fd, fromUri(filePath))
  return file
}
function close(file) {
  const fd = typeof file === 'number' ? file : file.fd
  fs.closeSync(fd); positions.delete(fd); openFiles.delete(fd)
}
const io = {
  OpenMode: { READ_ONLY: fs.constants.O_RDONLY, WRITE_ONLY: fs.constants.O_WRONLY, READ_WRITE: fs.constants.O_RDWR,
    CREATE: fs.constants.O_CREAT, TRUNC: fs.constants.O_TRUNC, NOFOLLOW: fs.constants.O_NOFOLLOW },
  WhenceType: { SEEK_SET: 0 },
  open: async (uri, flags) => {
    selectorReads.push(uri)
    if (mode === 'output-open' && uri.includes('/memoh-photo-') && !failed) {
      failed = true; throw Error('TEST_OUTPUT_ENOSPC')
    }
    return open(uri, flags)
  },
  openSync: (uri, flags) => { if (readGrant) readGrant(uri); return open(uri, flags) },
  close: async file => close(file), closeSync: close,
  accessSync: file => fs.existsSync(file), mkdirSync: dir => fs.mkdirSync(dir),
  statSync: file => typeof file === 'number' ? fs.fstatSync(file) : fs.statSync(file),
  listFileSync: dir => fs.readdirSync(dir), readTextSync: file => fs.readFileSync(file, 'utf8'),
  renameSync: (source, target) => fs.renameSync(source, target), fsyncSync: fd => fs.fsyncSync(fd),
  unlinkSync: file => {
    if (failUnlink) { failUnlink = false; throw Error('TEST_CACHE_BUSY') }
    fs.unlinkSync(file)
  }, rmdirSync: dir => fs.rmdirSync(dir), lseek: (fd, at) => positions.set(fd, at),
  read: async (fd, buffer, options = {}) => {
    const at = options.offset ?? positions.get(fd) ?? 0
    const count = fs.readSync(fd, new Uint8Array(buffer), 0, options.length ?? buffer.byteLength, at)
    positions.set(fd, at + count); return count
  },
  write: async (fd, buffer, options = {}) => {
    const filePath = openFiles.get(fd)
    if (mode === 'output-write' && filePath.includes('/memoh-photo-') && !failed) {
      failed = true; throw Error('TEST_OUTPUT_EIO')
    }
    const at = options.offset ?? positions.get(fd) ?? 0
    const count = fs.writeSync(fd, new Uint8Array(buffer), 0, Math.min(buffer.byteLength, shortWrites ? 17 : buffer.byteLength), at)
    positions.set(fd, at + count); return count
  }, writeSync: (fd, buffer) => fs.writeSync(fd, new Uint8Array(buffer))
}
const mimeTypes = { '.jpg': 'image/jpeg', '.jpeg': 'image/jpeg', '.png': 'image/png',
  '.gif': 'image/gif', '.webp': 'image/webp', '.tiff': 'image/tiff' }
const image = {
  PropertyKey: { ORIENTATION: 'orientation' },
  createImageSource(fd) {
    imageDecodes++
    if (mode === 'decode' || mode === 'decode-cleanup' && imageDecodes === 2) throw Error('TEST_DECODE_FAILED')
    const sourcePath = openFiles.get(fd), info = fixtures[path.basename(sourcePath)]
    return {
      getImageInfo: async () => ({ mimeType: info.mime, size: { width: info.width, height: info.height } }),
      getImageProperty: async () => String(info.orientation),
      createPixelMap: async ({ desiredSize }) => ({ sourcePath, size: desiredSize, rotation: 0,
        rotate: async function (degrees) { this.rotation += degrees },
        flip: async () => { throw Error('Unexpected mirror fixture') }, release: async () => {} }), release: async () => {}
    }
  },
  createImagePacker: () => ({ packing: async (pixels, { format, quality }) => {
    assert.equal(quality, 85)
    if (mode === 'encode') throw Error('TEST_ENCODE_FAILED')
    if (mode === 'empty') return new ArrayBuffer(0)
    const bytes = python('encode', pixels.sourcePath, String(pixels.size.width), String(pixels.size.height),
      String(pixels.rotation), format === 'image/png' ? 'PNG' : 'JPEG')
    return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.length)
  }, release: async () => {} })
}
const modules = {
  '@kit.CoreFileKit': { fileIo: io, fileUri: { getUriFromPath: file => pathToFileURL(file).href,
    FileUri: class { constructor(uri) { this.path = fromUri(uri); this.name = path.basename(this.path) } } },
    picker: { DocumentViewPicker: class { async select() { return uris } } } },
  '@kit.MediaLibraryKit': { photoAccessHelper: { PhotoViewMIMETypes: { IMAGE_TYPE: 'image/*' },
    PhotoViewPicker: class { async select(value) { options = value; return { photoUris: uris, isOriginalPhoto: original } } } } },
  '@kit.ImageKit': { image }, '@kit.ArkTS': { util: { generateRandomUUID: () => crypto.randomUUID().replaceAll('-', ''),
    TextEncoder: class { encodeInto(value) { return new TextEncoder().encode(value) } } } },
  '@kit.ArkData': { uniformTypeDescriptor: { UniformDataType: { IMAGE: 'image' },
    getUniformDataTypeByFilenameExtension: extension => mimeTypes[extension] ? extension : '',
    getTypeDescriptor: type => ({ mimeTypes: [mimeTypes[type]], belongsTo: () => true }) } },
  '@kit.CryptoArchitectureKit': { cryptoFramework: { createMd: () => {
    const hash = crypto.createHash('sha256')
    return { update: async ({ data }) => { hash.update(data) }, digest: async () => ({ data: new Uint8Array(hash.digest()) }) }
  } } }, '@kit.RemoteCommunicationKit': { rcp: { createSession: () => ({ close() {} }) } },
  './RawImage': { rawPixelMap: async () => { throw Error('Unexpected RAW fixture') } },
  './MemohClientConfig': { MEMOH_ORIGIN: 'https://nas.example', MEMOH_NETWORK_POLICY: undefined },
  './NativeTransferNetwork': { NativeTransferNetwork: class {} },
  './ClientDiagnostics': { ClientDiagnostics: { event: (...args) => diagnostics.push(args), failure: (...args) => diagnostics.push(args) } }
}
const sourceHashes = {}
function load(name) {
  const module = { exports: {} }
  const sourcePath = baseline && name !== 'NativeMediaFile' ? path.join(process.env.HARMONY_PHOTO_BASELINE_DIR, name + '-baseline.ets') : path.join(sourceDir, name + '.ets')
  const source = fs.readFileSync(sourcePath, 'utf8'); sourceHashes[name] = sha(source)
  const compiled = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS }, reportDiagnostics: true })
  assert.equal(compiled.diagnostics.length, 0)
  vm.runInNewContext(compiled.outputText, { module, exports: module.exports, require: name => {
    assert.ok(name in modules, name); return modules[name]
  }, ArrayBuffer, Uint8Array, Map, Set, Promise, Error, Date, JSON, Math, Number, String })
  return module.exports
}
const { NativePhotoSelection } = modules['./NativePhotoSelection'] = load('NativePhotoSelection')
modules['./NativeMediaFile'] = load('NativeMediaFile')
const { NativeResumableUpload } = load('NativeResumableUpload')
function prepare(label) {
  context = { filesDir: path.join(temp, label, 'files'), cacheDir: path.join(temp, label, 'cache') }
  fs.mkdirSync(context.filesDir, { recursive: true }); fs.mkdirSync(context.cacheDir, { recursive: true })
  mode = ''; failed = false; failUnlink = false; imageDecodes = 0; selectorReads = []; readGrant = undefined
  const native = new NativeResumableUpload(context, () => true)
  // No HTTP/auth changes are under test. Leave stagePath/copy/hash/journal real.
  native.owner = async () => 'b'.repeat(64)
  native.limits = async () => ({ max_upload_bytes: 209715200, resumable_enabled: true, chunk_max_bytes: 262144 })
  return native
}
const input = { operation: 'a'.repeat(32), botId: 'bot-a', scope: 'session-a', authorization: 'Bearer test', photos: true }
const cacheCount = () => fs.readdirSync(context.cacheDir).length
async function main() {
  if (baseline) assert.equal(sourceHashes.NativePhotoSelection, '7de4bca9669a9217e9aa5f48bfe88b3322f256529f9f6eacbe3c26fb0f6415f0')
  for (const [name, info] of Object.entries(fixtures)) {
    for (const photos of [true, false]) for (const isOriginal of photos ? [true, false] : [false]) {
      const native = prepare('success-' + results.length)
      const sourcePath = path.join(temp, 'fixtures', name), source = fs.readFileSync(sourcePath)
      uris = [pathToFileURL(sourcePath).href]; original = isOriginal; shortWrites = true
      const [selected] = await native.select({ ...input, photos })
      shortWrites = false
      const snapshot = path.join(context.filesDir, 'memoh-upload-journal-v1', selected.handle + '.original')
      const bytes = fs.readFileSync(snapshot), decoded = JSON.parse(python('describe', snapshot))
      const compressed = !isOriginal
      assert.equal(selected.size, bytes.length); assert.equal(selected.hash, sha(bytes)); assert.equal(selected.type, decoded.mime)
      assert.equal(sha(bytes) !== sha(source), compressed)
      assert.deepEqual(fs.readFileSync(sourcePath), source)
      if (!compressed) { assert.deepEqual(decoded, info); assert.equal(imageDecodes, 0); assert.equal(selectorReads.length, 0) }
      else { assert.ok(Math.max(decoded.width, decoded.height) <= 2048); assert.ok(['image/png', 'image/jpeg'].includes(decoded.mime)) }
      assert.equal(cacheCount(), baseline && compressed ? 1 : 0)
      assert.ok(fs.existsSync(snapshot)); assert.equal(openFiles.size, 0)
      results.push({ name, photos, isOriginal, input: { ...info, size: source.length, sha256: sha(source) },
        snapshot: { ...decoded, size: selected.size, mime: selected.type, sha256: selected.hash, name: selected.name },
        conversionCacheDirectoriesAfterSnapshot: cacheCount(), journalRetained: true })
    }
  }
  const batchNative = prepare('two-image-batch')
  uris = ['transparent.png', 'oriented.jpg'].map(name => pathToFileURL(path.join(temp, 'fixtures', name)).href)
  original = false
  const batchFiles = await batchNative.select(input)
  assert.equal(batchFiles.length, 2)
  for (const file of batchFiles) {
    const snapshot = path.join(context.filesDir, 'memoh-upload-journal-v1', file.handle + '.original')
    assert.equal(sha(fs.readFileSync(snapshot)), file.hash)
    assert.equal(JSON.parse(python('describe', snapshot)).mime, file.type)
  }
  assert.equal(cacheCount(), baseline ? 2 : 0)
  batchChecks.push({ count: 2, snapshotsVerifiedAfterHandoff: true, conversionCacheDirectories: cacheCount() })
  const grantNative = prepare('compressed-grant-uri')
  uris = [pathToFileURL(path.join(temp, 'fixtures', 'oriented.jpg')).href + '?grant=opaque%2Fvalue']
  original = false
  const [granted] = await grantNative.select(input)
  assert.equal(selectorReads[0], uris[0])
  assert.equal(granted.type, 'image/jpeg')
  batchChecks.push({ compressedFullGrantUriUsed: true, snapshotVerified: true })
  for (const [fault, expected] of [['decode', 'TEST_DECODE_FAILED'], ['encode', 'TEST_ENCODE_FAILED'],
    ['empty', 'IMAGE_ENCODING_EMPTY'], ['output-open', 'TEST_OUTPUT_ENOSPC'], ['output-write', 'TEST_OUTPUT_EIO']]) {
    const native = prepare('fault-' + fault)
    uris = [pathToFileURL(path.join(temp, 'fixtures', 'transparent.png')).href]; original = false; mode = fault
    await assert.rejects(native.select(input), error => error.message === expected)
    const leftovers = cacheCount()
    assert.equal(leftovers, baseline && fault === 'output-open' ? 1 : 0)
    // The fault cannot change the user's original choice; explicitly selecting
    // original on the next attempt still bypasses the broken preview codec.
    original = true
    const [selected] = await native.select(input)
    assert.equal(selected.hash, sha(fs.readFileSync(fromUri(uris[0]))))
    assert.equal(openFiles.size, 0)
    faults.push({ fault, rejectedWith: expected, leftoverCacheDirectories: leftovers, explicitOriginalRetrySucceeded: true })
  }
  prepare('cleanup-retry')
  uris = [pathToFileURL(path.join(temp, 'fixtures', 'transparent.png')).href]; original = false
  const selector = new NativePhotoSelection(context), [uri] = await selector.select(4, true)
  failUnlink = true; selector.close()
  assert.ok(fs.existsSync(fromUri(uri)))
  selector.close()
  assert.equal(fs.existsSync(fromUri(uri)), baseline)
  faults.push({ fault: 'cache unlink busy once', retainedForCleanupRetry: !baseline })
  prepare('cleanup-preserves-primary-error')
  uris = ['transparent.png', 'oriented.jpg'].map(name => pathToFileURL(path.join(temp, 'fixtures', name)).href)
  original = false; mode = 'decode-cleanup'; failUnlink = true
  const batch = new NativePhotoSelection(context)
  const expected = baseline ? 'TEST_CACHE_BUSY' : 'TEST_DECODE_FAILED'
  await assert.rejects(batch.select(4, true), error => error.message === expected)
  batch.close()
  assert.equal(cacheCount(), baseline ? 1 : 0)
  faults.push({ fault: 'second decode plus cleanup failure', rejectedWith: expected, primaryConversionErrorPreserved: !baseline })
  prepare('grant-expired')
  original = true; uris = [pathToFileURL(path.join(temp, 'fixtures', 'oriented.jpg')).href + '?grant=opaque%2Fvalue']
  readGrant = uri => { assert.equal(uri, uris[0]); throw Error('TEST_SOURCE_GRANT_EXPIRED') }
  const grant = new NativeResumableUpload(context, () => true)
  grant.owner = async () => 'b'.repeat(64); grant.limits = async () => ({ max_upload_bytes: 209715200 })
  await assert.rejects(grant.select(input), /TEST_SOURCE_GRANT_EXPIRED/)
  assert.equal(cacheCount(), 0)
  faults.push({ fault: 'expired grant adapter at snapshot open', noSuccessReceipt: true, fullUriUsed: true })
  assert.equal(options.isOriginalSupported, true)
  assert.equal(openFiles.size, 0)
  console.log(JSON.stringify({ mode: baseline ? 'baseline' : 'corrected', sourceHashes,
    boundary: 'Real .ets/file IO/snapshot/SHA256; host SDK and Pillow codec adapters; no phone grant or ImageKit compatibility claim',
    results, batchChecks, faults }, null, 2))
}
main().catch(error => { console.error(error); process.exitCode = 1 }).finally(() => {
  for (const fd of openFiles.keys()) { try { fs.closeSync(fd) } catch {} }
  fs.rmSync(temp, { recursive: true, force: true })
})
