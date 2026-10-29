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
let context, imageDecodes = 0, selectorReads = [], options, authorized = true
function python(...args) {
  const result = spawnSync('python3', [fixtureTool, ...args], { maxBuffer: 4 * 1024 * 1024 })
  assert.equal(result.status, 0, result.stderr.toString())
  return result.stdout
}
fs.mkdirSync(path.join(temp, 'fixtures'))
const one = path.join(temp, 'fixtures', 'one.png')
const made = spawnSync('python3', ['-c', 'from PIL import Image; import sys; Image.new("RGBA",(7,5),(18,30,60,200)).save(sys.argv[1])', one])
assert.equal(made.status, 0)
const fixtures = { 'one.png': JSON.parse(python('describe', one)) }
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
  renameSync: (source, target) => fs.renameSync(source, target), fsyncSync: fd => {
    if (mode === 'journal-dir-sync' && !failed && openFiles.get(fd) === path.join(context.filesDir, 'memoh-upload-journal-v1')) {
      failed = true; const error = Error('TEST_DIRECTORY_SYNC'); error.code = 13900020; throw error
    }
    fs.fsyncSync(fd)
  },
  unlinkSync: file => {
    if (failUnlink && file.includes('/memoh-photo-')) { failUnlink = false; throw Error('TEST_CACHE_BUSY') }
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
  authorized = true; const native = new NativeResumableUpload(context, () => authorized)
  // No HTTP/auth changes are under test. Leave stagePath/copy/hash/journal real.
  native.owner = async () => 'b'.repeat(64)
  native.limits = async () => ({ max_upload_bytes: 209715200, resumable_enabled: true, chunk_max_bytes: 262144 })
  return native
}
const input = { operation: 'a'.repeat(32), botId: 'bot-a', scope: 'session-a', authorization: 'Bearer test', photos: true }
const cacheCount = () => fs.readdirSync(context.cacheDir).length
async function main() {
  let native = prepare('published-snapshot-with-photo-cleanup')
  uris = [pathToFileURL(one).href]; original = false; mode = 'journal-dir-sync'
  let caught
  try { await native.select(input) } catch (error) { caught = error }
  assert.equal(caught?.message, 'TEST_DIRECTORY_SYNC')
  assert.equal(caught.uploadFailure.phase, 'journal-dir-sync')
  assert.equal(caught.uploadFailure.sdkCode, 13900020)
  assert.equal(cacheCount(), 0)
  const journalDir = path.join(context.filesDir, 'memoh-upload-journal-v1')
  const journals = fs.readdirSync(journalDir).filter(n => n.endsWith('.json')); assert.equal(journals.length, 1)
  const journal = JSON.parse(fs.readFileSync(path.join(journalDir, journals[0])))
  const snapshot = path.join(journalDir, journal.handle+'.original'); assert.equal(sha(fs.readFileSync(snapshot)), journal.hash)
  const restored = await native.list(input); assert.equal(restored[0].handle, journal.handle); assert.equal(restored[0].hash, journal.hash)
  authorized = false; assert.equal(caught.uploadFailure, undefined)
  const first = { name:'published7e snapshot survives75 compressed-cache release;6a metadata revoked on scope loss', snapshotBytes:fs.statSync(snapshot).size, snapshotSHA256:journal.hash }
  native = prepare('decode-failure-plus-cache-busy')
  uris = [pathToFileURL(one).href, pathToFileURL(one).href]; original = false; mode = 'decode-cleanup'; failUnlink = true
  caught = undefined
  try { await native.select(input) } catch (error) { caught = error }
  assert.equal(caught?.message, 'TEST_DECODE_FAILED')
  assert.equal(caught.uploadFailure.phase, 'picker')
  assert.equal(caught.uploadFailure.sdkCode, 0)
  assert.equal(cacheCount(), 0); assert.equal(openFiles.size, 0)
  assert.equal(fs.existsSync(path.join(context.filesDir,'memoh-upload-journal-v1')) ? fs.readdirSync(path.join(context.filesDir,'memoh-upload-journal-v1')).length : 0, 0)
  console.log(JSON.stringify({ cases:[first,{name:'primary decode failure preserved through cleanup retry and6a scope-safe metadata', caches:0}], tests:2, fiveFormatSuiteRerun:false, boundary:'actual source and real filesystem/SHA; one synthetic PNG; host SDK/Pillow codecs, not phone grants or ImageKit' }))
}

main().catch(error => { console.error(error); process.exitCode = 1 }).finally(() => {
  for (const fd of openFiles.keys()) { try { fs.closeSync(fd) } catch {} }
  fs.rmSync(temp, { recursive: true, force: true })
})
