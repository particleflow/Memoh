// Actual NetReport ArkTS module over the real ClientDiagnostics journal file.
// rcp/bundleManager boundaries are controlled; every posted body is validated
// against the server's strict /client/net-report schema (PR #175 rules).
const fs=require('node:fs'),path=require('node:path'),os=require('node:os'),vm=require('node:vm'),crypto=require('node:crypto'),assert=require('node:assert/strict')
const root=path.resolve(__dirname,'../..'),ts=require(process.env.HARMONY_TEST_TYPESCRIPT||path.join(root,'mobile/.build/memoh-web/checkout/node_modules/typescript'))
const common='mobile/harmony/app/entry/src/main/ets/common/'
const dir=fs.mkdtempSync(path.join(os.tmpdir(),'harmony-net-report-'))
let origin='https://nas.example/api',postOutcome=204
const posts=[]
class Request{constructor(url,method,headers){this.url=url;this.method=method;this.headers=headers;this.configuration={}}}
const rcp={Request,createSession:()=>({
 close(){},
 async fetch(request){posts.push({url:request.url,method:request.method,headers:request.headers,content:request.content,configuration:request.configuration});
  return {statusCode:postOutcome,headers:{}}}
})}
const io={OpenMode:{READ_ONLY:fs.constants.O_RDONLY,CREATE:fs.constants.O_CREAT,READ_WRITE:fs.constants.O_RDWR,APPEND:fs.constants.O_APPEND,TRUNC:fs.constants.O_TRUNC},
 accessSync:p=>fs.existsSync(p),statSync:p=>typeof p==='number'?fs.fstatSync(p):fs.statSync(p),readTextSync:p=>fs.readFileSync(p,'utf8'),
 openSync:(p,m)=>({fd:fs.openSync(p,m)}),closeSync:file=>fs.closeSync(typeof file==='number'?file:file.fd),renameSync:(a,b)=>fs.renameSync(a,b),unlinkSync:p=>fs.unlinkSync(p),truncateSync:(fd,n)=>fs.ftruncateSync(fd,n),
 readSync:(fd,b,o)=>fs.readSync(fd,Buffer.from(b),0,o.length,o.offset),
 writeSync:(fd,b)=>fs.writeSync(fd,Buffer.from(b)),fsyncSync:fd=>fs.fsyncSync(fd)}
const modules={
 '@kit.CoreFileKit':{fileIo:io},
 '@kit.PerformanceAnalysisKit':{hilog:{info(){},error(){}}},
 '@kit.ArkTS':{util:{TextEncoder:class{encodeInto(v){return new TextEncoder().encode(v)}},TextDecoder:class{decodeToString(v){return new TextDecoder().decode(v)}}}},
 '@kit.CryptoArchitectureKit':{cryptoFramework:{createMd(){const h=crypto.createHash('sha256');return{async update(x){h.update(x.data)},async digest(){return{data:new Uint8Array(h.digest())}}}}}},
 '@kit.RemoteCommunicationKit':{rcp},
 '@kit.AbilityKit':{bundleManager:{BundleFlag:{GET_BUNDLE_INFO_DEFAULT:0},getBundleInfoForSelfSync:()=>({versionName:'1.0.45-fixture',versionCode:1000045})}},
 '@kit.BasicServicesKit':{},
 '@kit.NetworkKit':{connection:{}},
 './MemohClientConfig':{get MEMOH_ORIGIN(){return origin},resetMemohOrigin(){},configureMemohOrigin(){}},
}
function load(name,extra={}){const m={exports:{}},file=path.join(root,common,name+'.ets')
 vm.runInNewContext(ts.transpileModule(fs.readFileSync(file,'utf8'),{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText,
  {module:m,exports:m.exports,require:id=>{if(modules[id])return modules[id];if(extra[id])return extra[id];return{}},Uint8Array,ArrayBuffer,Map,Set,Date,JSON,Number,Error,Promise,Math},{filename:file});return m.exports}
const diagnostics=load('ClientDiagnostics')
diagnostics.ClientDiagnostics.bind({filesDir:dir})
modules['./ClientDiagnostics']=diagnostics
const {NetReport}=load('NetReport',{'./AppNetBinding':{AppNetBinding:{dump:()=>['GarnetAppNet:'+JSON.stringify({version:1,started:true,boundNetId:218,action:'bind',reason:'cellular-ipv6',
 nets:[{netId:100,wifi:true,cellular:false,ipv6:false},{netId:218,wifi:false,cellular:true,ipv6:true}]})]}},'./ClientDiagnostics':diagnostics})
NetReport.bind({filesDir:dir})
// Server-side validation rules ported from internal/handlers/client_net_report.go (PR #175).
const token=(value,limit)=>typeof value==='string'&&value.length>=1&&value.length<=limit&&/^[A-Za-z0-9_.-]+$/.test(value)
function serverAccepts(raw){
 const body=JSON.parse(raw)
 assert.equal(Buffer.byteLength(raw)<=65536,true,'size')
 assert.deepEqual(Object.keys(body).sort(),['at','binding','build','journal','reason','schema','version'].sort(),'unknown/missing fields')
 assert.equal(body.schema,1);assert.ok(body.at>0)
 assert.ok(['client-failed','page-failed','client-timeout','manual'].includes(body.reason))
 assert.ok(typeof body.version==='string'&&body.version.length>=1&&body.version.length<=32)
 assert.ok(Number.isSafeInteger(body.build)&&body.build>=0)
 if(body.binding!==undefined){
  assert.deepEqual(Object.keys(body.binding).sort(),['action','boundNetId','nets','reason','started','version'].sort())
  assert.equal(body.binding.version,1);assert.ok(body.binding.boundNetId>=0)
  assert.ok(token(body.binding.action,32));assert.ok(token(body.binding.reason,64));assert.ok(body.binding.nets.length<=16)
  for(const net of body.binding.nets){assert.deepEqual(Object.keys(net).sort(),['cellular','ipv6','netId','wifi'].sort());assert.ok(net.netId>=0)}
 }
 assert.ok(Array.isArray(body.journal)&&body.journal.length<=200)
 for(const event of body.journal){
  assert.deepEqual(Object.keys(event).sort(),['area','at','component','detail','phase'].sort())
  assert.ok(event.at>0);assert.ok(['NETWORK','SYSTEM'].includes(event.area))
  assert.ok(token(event.component,64));assert.ok(token(event.phase,64));assert.ok(event.detail.length<=512)
 }
 return body
}
const wait=async predicate=>{for(let i=0;i<200&&!predicate();i++)await new Promise(r=>setImmediate(r));assert.ok(predicate(),'condition not reached')}
let checks=0
async function check(name,fn){await fn();checks++;console.log('ok -',name)}
;(async()=>{
 diagnostics.ClientDiagnostics.event('NETWORK','app-net','bound','net=218')
 diagnostics.ClientDiagnostics.event('SYSTEM','web-network','transport-error','code=ERR_NAME_NOT_RESOLVED')
 diagnostics.ClientDiagnostics.event('VOICE','outside-areas','ignored','detail')
 await check('client-failed trigger posts a schema-valid report immediately',async()=>{
  NetReport.report('client-failed')
  await wait(()=>posts.length===1)
  assert.equal(posts[0].url,origin+'/client/net-report')
  assert.equal(posts[0].method,'POST')
  assert.equal(posts[0].headers['content-type'],'application/json')
  assert.equal(posts[0].configuration.security.remoteValidation,'system')
  const body=serverAccepts(posts[0].content)
  assert.equal(body.reason,'client-failed');assert.equal(body.version,'1.0.45-fixture');assert.equal(body.build,1000045)
  assert.equal(body.binding.boundNetId,218)
  assert.deepEqual(body.journal.map(event=>[event.area,event.component,event.phase]),[['NETWORK','app-net','bound'],['SYSTEM','web-network','transport-error']])
 })
 await check('same reason is debounced for 30 minutes',async()=>{
  NetReport.report('client-failed')
  await new Promise(r=>setTimeout(r,50))
  assert.equal(posts.length,1)
 })
 await check('failed post queues and a later flush delivers it',async()=>{
  postOutcome=502
  NetReport.report('page-failed')
  await wait(()=>posts.length===2)
  await new Promise(r=>setTimeout(r,50))
  assert.ok(fs.existsSync(path.join(dir,'net-report-queue.jsonl')))
  assert.equal(fs.readFileSync(path.join(dir,'net-report-queue.jsonl'),'utf8').split('\n').filter(Boolean).length,1)
  postOutcome=204
  NetReport.flush()
  await new Promise(r=>setTimeout(r,50))
  assert.equal(posts.length,3)
  assert.equal(posts[2].content,posts[1].content)
  assert.equal(fs.readFileSync(path.join(dir,'net-report-queue.jsonl'),'utf8').split('\n').filter(Boolean).length,0)
 })
 await check('unconfigured origin and unknown reasons never post or queue',async()=>{
  const previous=posts.length
  origin=''
  NetReport.report('client-timeout')
  NetReport.report('invented-reason')
  await new Promise(r=>setTimeout(r,50))
  assert.equal(posts.length,previous)
  origin='https://nas.example/api'
  NetReport.report('invented-reason')
  await new Promise(r=>setTimeout(r,50))
  assert.equal(posts.length,previous)
  assert.equal(fs.readFileSync(path.join(dir,'net-report-queue.jsonl'),'utf8').split('\n').filter(Boolean).length,0)
 })
 console.log(JSON.stringify({actualArkTsNetReport:true,serverSchemaMachineValidated:true,queueRetry:true,debounce:true,nativeDevice:false,checks}))
})().catch(error=>{console.error(error);process.exitCode=1})
