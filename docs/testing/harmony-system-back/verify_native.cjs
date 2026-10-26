// Actual ArkTS back method through TypeScript transpilation + Kit seams.
// Not an ArkWeb or device test; official SDK compilation is checked separately.
const fs = require('node:fs'), vm = require('node:vm'), assert = require('node:assert/strict'), path = require('node:path')
const root = path.resolve(__dirname, '../../..')
const ts = require(path.join(root, 'mobile/client/tests/.isolated/ci-web/checkout/node_modules/typescript'))
const source = fs.readFileSync(path.join(root, 'mobile/client/mobile/harmony/app/entry/src/main/ets/pages/SharedChat.ets'), 'utf8')
const back = source.slice(source.indexOf('  private backPending:'), source.indexOf('  aboutToDisappear():'))
assert(back.includes('onBackPress(): boolean'))
const code = ts.transpileModule(`class Harness {${back}}; globalThis.Harness = Harness`, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText
let events = 0
const sandbox = { sharedWebAvailable: () => true, ClientDiagnostics: { event() { events++ } } }
vm.runInNewContext(code, sandbox)
function create(fetch) {
  const instance = new sandbox.Harness()
  Object.assign(instance, {active:true,loadFailed:false,renderWaiting:false,probeGeneration:1,controller:{runJavaScript:fetch},context:{moves:0,async moveAbilityToBackground(){this.moves++}}})
  return instance
}
const settle = () => new Promise(resolve => setImmediate(resolve))
;(async () => {
  let scripts=0, resolve
  const pending = new Promise(r => {resolve=r})
  const h=create(script => {assert(script.includes('window.__memohBack'));scripts++;return pending})
  assert.equal(h.onBackPress(),true);assert.equal(h.onBackPress(),true);assert.equal(scripts,1)
  resolve('true');await settle();assert.equal(h.context.moves,0);assert.equal(h.backPending,false)
  const rootPage=create(async()=> 'false');assert(rootPage.onBackPress());await settle();assert.equal(rootPage.context.moves,1)
  const stale=create(async()=> 'false');assert(stale.onBackPress());stale.probeGeneration++;await settle();assert.equal(stale.context.moves,0)
  const gone=create(async()=> 'false');assert(gone.onBackPress());gone.active=false;await settle();assert.equal(gone.context.moves,0)
  const failed=create(async()=>{throw new Error('synthetic unavailable engine')});assert(failed.onBackPress());await settle();assert.equal(failed.backPending,false);assert.equal(events,1);assert.equal(failed.context.moves,0)
  for(const flag of ['active','loadFailed','renderWaiting']) {
    const fallback=create(()=>{throw new Error('must not execute')});fallback[flag]=flag!=='active'
    assert.equal(fallback.onBackPress(),false)
  }
  console.log(JSON.stringify({scenarios:9,passed:true,coalescesPending:true,staleDoesNotExit:true,rootBackgroundOnly:true,scope:'Production method with SDK seams; not device gesture acceptance'}))
})().catch(error=>{console.error(error);process.exitCode=1})
