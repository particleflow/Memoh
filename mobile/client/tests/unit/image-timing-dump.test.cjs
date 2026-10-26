// Isolated execution of the actual ArkTS class using a synthetic ArkWeb controller.
// This is not the official ArkTS compiler, a device test or a permission test.
const fs = require('node:fs'), vm = require('node:vm'), assert = require('node:assert/strict')
const ts = require('typescript')
const path = require('node:path')
const file = path.resolve(__dirname, '../../mobile/harmony/app/entry/src/main/ets/common/ImageTimingDump.ets')
const source = fs.readFileSync(file, 'utf8')
const javascript = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
let passed = 0
async function test(name, run) { await run(); passed++; console.log('PASS ' + name) }
function setup() {
  const timers = new Map(); let next = 0
  const box = { exports: {}, require: name => { assert.equal(name, '@kit.ArkWeb'); return {} },
    setTimeout: fn => { timers.set(++next, fn); return next }, clearTimeout: id => timers.delete(id) }
  vm.runInNewContext(javascript, box, { filename: file })
  const D = box.exports.ImageTimingDump
  const result = () => JSON.parse(D.dump()[0].slice('GarnetImageTiming:'.length))
  const flush = async () => { for (let i = 0; i < 5; i++) await Promise.resolve() }
  return { D, result, flush, timers }
}
;(async () => {
  await test('only literal commands on the app-owned origin', async () => {
    const { D, flush } = setup(); const scripts = []
    const c = { getUrl: () => 'begonia-client://app/index.html#/chat', runJavaScript: s => { scripts.push(s); return Promise.resolve('null') } }
    D.bind(c, () => true); D.command('arm'); D.command('take'); D.command('clear'); D.command('fetch("PRIVATE")'); await flush()
    assert.equal(scripts.length, 3)
    assert(scripts[0].includes('.arm()')); assert(scripts[1].includes('.take()')); assert(scripts[2].includes('.clear()'))
    assert(!scripts.join('').includes('PRIVATE'))
  })
  await test('foreign page and inactive chat cannot arm or read', async () => {
    const { D } = setup(); let calls = 0
    const c = { getUrl: () => 'https://example.test/', runJavaScript: () => { calls++; return Promise.resolve('null') } }
    D.bind(c, () => true); D.command('arm'); D.command('take'); assert.equal(calls, 0)
    c.getUrl = () => 'begonia-client://app/index.html'; D.bind(c, () => false); D.command('arm'); D.command('take'); assert.equal(calls, 0)
    D.command('clear'); assert.equal(calls, 1) // Clear still works after readiness is lost.
  })
  await test('whitelist strips secrets and rejects invalid numeric values', async () => {
    const { D, result, flush } = setup()
    const input = { version: 1, active: true, paintSupported: false, url: 'PRIVATE', rows: [{ id: 1, card: 1, body: -1, bytes: 42, load: 'PRIVATE', hash: 'PRIVATE', token: 'PRIVATE' }] }
    D.bind({ getUrl: () => 'begonia-client://app/index.html', runJavaScript: () => Promise.resolve(JSON.stringify(input)) }, () => true)
    D.command('take'); await flush()
    assert.deepEqual(result().rows, [{ id: 1, card: 1, bytes: 42 }]); assert(!JSON.stringify(result()).includes('PRIVATE'))
  })
  await test('accepts one ArkWeb string wrapper and expires local output', async () => {
    const { D, result, flush, timers } = setup()
    const input = { version: 1, active: false, paintSupported: false, rows: [{ id: 1, card: 2 }] }
    D.bind({ getUrl: () => 'begonia-client://app/index.html', runJavaScript: () => Promise.resolve(JSON.stringify(JSON.stringify(input))) }, () => true)
    D.command('take'); await flush(); assert.equal(result().rows.length, 1)
    for (const fn of [...timers.values()]) fn()
    assert.equal(result().rows.length, 0)
  })
  await test('rejects oversized and malformed payloads without logging them', async () => {
    for (const input of ['PRIVATE', 'null', JSON.stringify({ version: 1, rows: Array(6).fill({ id: 1 }) }), JSON.stringify({ version: 1, rows: [{ id: 9 }] })]) {
      const { D, result, flush } = setup()
      D.bind({ getUrl: () => 'begonia-client://app/index.html', runJavaScript: () => Promise.resolve(input) }, () => true)
      D.command('take'); await flush(); assert.equal(result().rows.length, 0)
    }
  })
  await test('late native completion cannot restore a cleared snapshot', async () => {
    const { D, result, flush } = setup(); let finish
    D.bind({ getUrl: () => 'begonia-client://app/index.html', runJavaScript: () => new Promise(resolve => { finish = resolve }) }, () => true)
    D.command('take'); const done = finish; D.detach()
    done(JSON.stringify({ version: 1, active: false, rows: [{ id: 1, card: 3 }] })); await flush()
    assert.equal(result().rows.length, 0)
  })
  await test('missing controller and failed WebView do not throw', async () => {
    const { D, result, flush } = setup(); D.command('take')
    D.bind({ getUrl: () => { throw new Error('PRIVATE') } }, () => true); D.command('arm')
    D.bind({ getUrl: () => 'begonia-client://app/index.html', runJavaScript: () => Promise.reject(new Error('PRIVATE')) }, () => true)
    D.command('take'); await flush(); assert.equal(result().rows.length, 0)
  })
  console.log(JSON.stringify({ passed, officialArkTSCompiler: false, actualArkWeb: false, device: false }))
})().catch(error => { console.error(error); process.exitCode = 1 })
