const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm'),assert=require('node:assert/strict')
const root=path.resolve(__dirname,'../..'),ts=require(path.join(root,'mobile/.build/memoh-web/checkout/node_modules/typescript'))
function load(file,imports={}){const m={exports:{}};vm.runInNewContext(ts.transpileModule(fs.readFileSync(path.join(root,file),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText,{module:m,exports:m.exports,require:n=>{assert.ok(n in imports,n);return imports[n]},URL,Error,JSON,Number,String,Array,Map,Set,Promise,console,TextEncoder,Uint8Array,crypto:require('node:crypto').webcrypto});return m.exports}
const validator=load('mobile/memoh/harmony-service-url.ts'),{normalizeServiceUrl:normal}=validator
let checks=0;function check(f){f();checks++}
for(const value of ['','/api','http://localhost/api','http://127.0.0.1/api','http://10.0.0.2/api','http://[::1]/api','http://[fd00::1]/api','https://user:pass@alpha.example/api','https://alpha.example/api?token=x','https://alpha.example/api#x','https://alpha.example/foo/../api','https://alpha.example/%61pi','https://alpha.example//api','https://alpha.example/','ftp://alpha.example/api','https://0x7f000001/api','https://alpha.example./api'])check(()=>assert.throws(()=>normal(value)))
check(()=>assert.equal(normal('http://alpha.example:18000/api/'),'http://alpha.example:18000/api'))
check(()=>assert.equal(normal('https://[2606:4700::1111]/api'),'https://[2606:4700::1111]/api'))
check(()=>assert.equal(normal('https://download.example/releases',false),'https://download.example/releases'))
check(()=>assert.equal(normal('https://download.example/app-1.0.8.hap',false),'https://download.example/app-1.0.8.hap'))
for (const host of ['192.0.1.1','198.51.1.1','192.0.0.9','192.0.0.10']) check(()=>assert.equal(normal('https://'+host+'/api'),'https://'+host+'/api'))
for (const host of ['192.0.0.8','192.0.0.11','192.0.2.1','198.51.100.1','198.18.0.1','198.19.255.254']) check(()=>assert.throws(()=>normal('https://'+host+'/api')))
// Deliberately configured LAN service addresses; transport still validates TLS.
for(const host of ['10.0.0.2','172.16.0.2','172.31.255.254','192.168.1.1','[fd12::1]','[fc12::2]','nas.lan','nas.local']) {
 check(()=>assert.equal(normal('https://'+host+'/api'),'https://'+host+'/api'))
 check(()=>assert.throws(()=>normal('http://'+host+'/api')))
}
for(const value of ['https://127.0.0.1/api','https://[::1]/api','https://[fe80::1]/api','https://169.254.1.1/api','https://0x0a000002/api','https://167772162/api','https://010.0.0.2/api'])check(()=>assert.throws(()=>normal(value)))
const cfg=load('mobile/memoh/harmony-service-config.ts',{'./harmony-service-url':validator})
class Storage{constructor(){this.values=new Map()}get length(){return this.values.size}key(i){return [...this.values.keys()][i]??null}getItem(k){return this.values.get(k)??null}setItem(k,v){this.values.set(k,String(v))}removeItem(k){this.values.delete(k)}}
const s=new Storage(),tab=new Storage(),a='https://alpha.example/api',b='https://beta.example/api'
// The binding here is synthetic evidence, not a claim that a real v1.0.6
// storage key has been identified. A user-entered URL is never such evidence.
const binding={apiOrigin:a}
const unknown=new Storage();unknown.setItem('token','synthetic-unbound');unknown.setItem('chat-input-drafts','local-only')
const originalUnknown=JSON.stringify([...unknown.values])
check(()=>assert.equal(cfg.inspectServiceSetup(unknown).kind,'unbound'))
check(()=>assert.equal(JSON.stringify([...unknown.values]),originalUnknown))
for(const destination of [a,b]) {
 check(()=>assert.throws(()=>cfg.saveServiceConfig(destination,'',unknown),/SERVICE_BINDING_REQUIRED/))
 check(()=>assert.equal(JSON.stringify([...unknown.values]),originalUnknown))
}
check(()=>assert.equal(unknown.getItem(cfg.LEGACY_STATE_KEY),null))
check(()=>assert.equal(cfg.inspectServiceSetup(new Storage()).kind,'initial'))
check(()=>assert.equal(cfg.saveServiceConfig(a,'',new Storage()).resetPending,false))
const appearance=new Storage();appearance.setItem('language','zh');appearance.setItem('theme','dark')
check(()=>assert.equal(cfg.inspectServiceSetup(appearance).kind,'initial'))
appearance.setItem('unknown-legacy-record','keep')
check(()=>assert.equal(cfg.inspectServiceSetup(appearance).kind,'unbound'))
check(()=>assert.equal(cfg.inspectServiceSetup(unknown,binding).kind,'legacy-bound'))
check(()=>assert.equal(JSON.stringify([...unknown.values]),originalUnknown))
const quota=new Storage();quota.setItem('token','synthetic-unbound');quota.setItem('chat-input-drafts','local-only');const originalSet=quota.setItem.bind(quota)
quota.setItem=(k,v)=>{if(k===cfg.SERVICE_CONFIG_KEY)throw Error('QuotaExceededError');originalSet(k,v)}
check(()=>assert.throws(()=>cfg.saveServiceConfig(a,'',quota,binding)))
check(()=>assert.equal(quota.getItem('token'),'synthetic-unbound'))
check(()=>assert.equal(quota.getItem('chat-input-drafts'),'local-only'))
check(()=>assert.equal(quota.getItem(cfg.SERVICE_CONFIG_KEY),null))
check(()=>assert.equal(cfg.readServiceConfig(s),null))
s.setItem('token','synthetic-legacy');s.setItem('chat-input-drafts','synthetic-draft')
const originalLegacy=JSON.stringify([...s.values])
check(()=>assert.throws(()=>cfg.saveServiceConfig(b,'',s,binding),/SERVICE_LEGACY_ORIGIN_CHANGED/))
check(()=>assert.equal(JSON.stringify([...s.values]),originalLegacy))
let c=cfg.saveServiceConfig(a,'',s,binding)
check(()=>assert.equal(c.resetPending,false))
check(()=>assert.equal(s.getItem('token'),'synthetic-legacy'))
check(()=>assert.equal(s.getItem('chat-input-drafts'),'synthetic-draft'))
check(()=>assert.equal(cfg.inspectServiceSetup(s).kind,'configured'))
// Restart after same-origin migration must not turn it into a reset.
c=cfg.readServiceConfig(s);cfg.finishServiceTransition(c,s,tab)
check(()=>assert.equal(s.getItem('token'),'synthetic-legacy'))
// Existing configured installations may deliberately switch services.
c=cfg.saveServiceConfig(b,'',s)
check(()=>assert.equal(c.resetPending,true))
// Simulated process death after pending write: a fresh read cannot become active
// until auth cleanup has run. Transition is idempotent.
c=cfg.readServiceConfig(s);cfg.finishServiceTransition(c,s,tab);cfg.finishServiceTransition(c,s,tab);cfg.commitServiceTransition(c,s)
check(()=>assert.equal(s.getItem('token'),null))
s.setItem('token','synthetic-A');c=cfg.saveServiceConfig(b,'https://download.example/releases',s)
check(()=>assert.equal(c.resetPending,false));check(()=>assert.equal(s.getItem('token'),'synthetic-A'))
c=cfg.saveServiceConfig(a,'',s);check(()=>assert.equal(c.resetPending,true));cfg.finishServiceTransition(c,s,tab);cfg.commitServiceTransition(c,s)
check(()=>assert.equal(s.getItem('token'),null))
// A LAN/IPv6 address change cannot silently carry credentials to another origin.
s.setItem('token','synthetic-current')
c=cfg.saveServiceConfig('https://192.168.1.1/api','',s)
check(()=>assert.equal(c.resetPending,true))
cfg.finishServiceTransition(c,s,tab);cfg.commitServiceTransition(c,s)
check(()=>assert.equal(s.getItem('token'),null))
s.setItem('token','synthetic-lan')
c=cfg.saveServiceConfig('https://192.168.1.1/api','',s)
check(()=>assert.equal(c.resetPending,false));check(()=>assert.equal(s.getItem('token'),'synthetic-lan'))
c=cfg.saveServiceConfig('https://[2606:4700::1111]/api','',s)
check(()=>assert.equal(c.resetPending,true))
cfg.finishServiceTransition(c,s,tab);cfg.commitServiceTransition(c,s)
check(()=>assert.equal(s.getItem('token'),null))
s.setItem(cfg.SERVICE_CONFIG_KEY,'{"version":99}');check(()=>assert.throws(()=>cfg.readServiceConfig(s)))
check(()=>assert.throws(()=>cfg.saveServiceConfig(a,'',s)))
check(()=>assert.equal(s.getItem(cfg.SERVICE_CONFIG_KEY),'{"version":99}'))
// Same validator in the real native module, and real bridge guard. No OS Kit
// execution is claimed; only IO edges are stubbed.
const nativeValidator=load('mobile/harmony/app/entry/src/main/ets/common/ServiceUrl.ets',{'@kit.ArkTS':{url:{URL:{parseURL:v=>new URL(v)}}}})
check(()=>assert.equal(nativeValidator.normalizeServiceUrl(a),normal(a)))
for (const host of ['192.0.1.1','198.51.1.1','192.0.0.9','192.0.0.10']) check(()=>assert.equal(nativeValidator.normalizeServiceUrl('https://'+host+'/api'),normal('https://'+host+'/api')))
for (const host of ['192.0.0.8','192.0.0.11','192.0.2.1','198.51.100.1','198.18.0.1','198.19.255.254']) check(()=>assert.throws(()=>nativeValidator.normalizeServiceUrl('https://'+host+'/api')))
const nativeConfig=load('mobile/harmony/app/entry/src/main/ets/common/MemohClientConfig.ets',{'./ServiceUrl':nativeValidator})
const policy=load('mobile/harmony/app/entry/src/main/ets/common/SharedWebPolicy.ets',{'@kit.ArkWeb':{webview:{}},'@kit.ArkTS':{url:{URL:{parseURL:v=>new URL(v)}}},'./WebResourceManifest':{WEB_RESOURCES:{}},'./MemohClientConfig':nativeConfig})
const events=[];class Media{close(){events.push('close')}prepare(){events.push('prepare')}}
const downloads=[];class Download extends Media{
 constructor(context,authorized){super();this.authorized=authorized}
 async fetch(input){assert.equal(this.authorized(),true);downloads.push(['fetch',input]);return{operation:input.operation,hash:input.hash,size:input.size}}
 async save(operation,botId){assert.equal(this.authorized(),true);downloads.push(['save',operation,botId]);return true}
 abort(operation,botId){downloads.push(['abort',operation,botId])}
}
const {NativeClient}=load('mobile/harmony/app/entry/src/main/ets/common/NativeClient.ets',{'./MemohClientConfig':nativeConfig,'./NativeVoice':{NativeVoice:Media},'./NativeResumableUpload':{NativeResumableUpload:Media},'./NativeMediaDownload':{NativeMediaDownload:Download}})
let frame='begonia-client://app/index.html';const controller={getLastJavascriptProxyCallingFrameUrl:()=>frame,getUrl:()=>frame,registerJavaScriptProxy(){},deleteJavaScriptRegister(){}}
const bridge=new NativeClient(controller,s=>events.push(s),async()=>{},{});bridge.register()
async function call(command,rest={}){return JSON.parse(await bridge.request(JSON.stringify({version:1,command,...rest})))}
;(async()=>{
 const oldHTTP='http://alpha.example:18000/api', newHTTPS='https://alpha.example:18443/api'
 const proof={schemaVersion:1,bundleName:'cloud.begonia.workassistant',versionCode:1000006,sourceHapSHA256:'a'.repeat(64),apiOrigin:oldHTTP,apiBaseSHA256:require('node:crypto').createHash('sha256').update(oldHTTP).digest('hex')}
 const upgrade={apiOrigin:newHTTPS,hapDownloadUrl:'',productName:'Garnet-Assistant',legacyBinding:proof}
 for(const persisted of [false,true]) {
  const store=new Storage();store.setItem('token','synthetic-http-login');store.setItem('chat-input-drafts','keep')
  if(persisted)store.setItem(cfg.SERVICE_CONFIG_KEY,JSON.stringify({version:1,apiOrigin:oldHTTP,hapDownloadUrl:'',resetPending:false}))
  const migrated=await cfg.applyDeploymentConfig(upgrade,store)
  check(()=>assert.equal(migrated.apiOrigin,newHTTPS));check(()=>assert.equal(migrated.resetPending,false))
  check(()=>assert.equal(store.getItem('token'),'synthetic-http-login'));check(()=>assert.equal(store.getItem('chat-input-drafts'),'keep'))
  check(()=>assert.equal(cfg.readServiceConfig(store).apiOrigin,newHTTPS))
  check(()=>assert.equal(cfg.finishServiceTransition(migrated,store,new Storage()).apiOrigin,newHTTPS))
 }
 for(const mutation of [{apiOrigin:'https://beta.example:18443/api'},{apiOrigin:'http://alpha.example:18443/api'},{apiOrigin:'https://alpha.example:18443/other/api'},{legacyBinding:{...proof,apiBaseSHA256:'b'.repeat(64)}}]) {
  const store=new Storage();store.setItem('token','keep');store.setItem(cfg.SERVICE_CONFIG_KEY,JSON.stringify({version:1,apiOrigin:oldHTTP,hapDownloadUrl:'',resetPending:false}))
  const before=JSON.stringify([...store.values]);await assert.rejects(()=>cfg.applyDeploymentConfig({...upgrade,...mutation},store));checks++
  check(()=>assert.equal(JSON.stringify([...store.values]),before))
 }
 const unknownHTTP=new Storage();unknownHTTP.setItem('token','keep');unknownHTTP.setItem(cfg.SERVICE_CONFIG_KEY,JSON.stringify({version:1,apiOrigin:'http://beta.example:18000/api',hapDownloadUrl:'',resetPending:false}))
 const beforeUnknown=JSON.stringify([...unknownHTTP.values]);await assert.rejects(()=>cfg.applyDeploymentConfig(upgrade,unknownHTTP));checks++
 check(()=>assert.equal(JSON.stringify([...unknownHTTP.values]),beforeUnknown))
 const failingHTTP=new Storage();failingHTTP.setItem('token','keep');failingHTTP.setItem(cfg.SERVICE_CONFIG_KEY,JSON.stringify({version:1,apiOrigin:oldHTTP,hapDownloadUrl:'',resetPending:false}));const beforeFail=JSON.stringify([...failingHTTP.values]);failingHTTP.setItem=()=>{throw Error('QuotaExceededError')}
 await assert.rejects(()=>cfg.applyDeploymentConfig(upgrade,failingHTTP));checks++;check(()=>assert.equal(JSON.stringify([...failingHTTP.values]),beforeFail))
 const boundStorage=new Storage();boundStorage.setItem('token','synthetic-bound-old');boundStorage.setItem('chat-input-drafts','preserved-draft')
 const expected=require('node:crypto').createHash('sha256').update(a).digest('hex')
 const deployment={apiOrigin:a,hapDownloadUrl:'',productName:'Garnet-Assistant',legacyBinding:{schemaVersion:1,bundleName:'cloud.begonia.workassistant',versionCode:1000006,sourceHapSHA256:'a'.repeat(64),apiBaseSHA256:expected}}
 const unchanged=JSON.stringify([...boundStorage.values])
 assert.equal(await cfg.applyDeploymentConfig({apiOrigin:'',hapDownloadUrl:'',productName:'Garnet-Assistant'},boundStorage),null);checks++
 for(const bad of [{...deployment,legacyBinding:null},{...deployment,apiOrigin:b},{...deployment,token:'unwanted'},{...deployment,legacyBinding:{...deployment.legacyBinding,bundleName:'wrong.app'}}]) {
  await assert.rejects(()=>cfg.applyDeploymentConfig(bad,boundStorage));checks++
  check(()=>assert.equal(JSON.stringify([...boundStorage.values]),unchanged))
 }
 const migrated=await cfg.applyDeploymentConfig(deployment,boundStorage)
 check(()=>assert.equal(migrated.resetPending,false))
 check(()=>assert.equal(boundStorage.getItem('token'),'synthetic-bound-old'))
 check(()=>assert.equal(boundStorage.getItem('chat-input-drafts'),'preserved-draft'))
 const retained=await cfg.applyDeploymentConfig({...deployment,apiOrigin:b},boundStorage)
 check(()=>assert.equal(retained.apiOrigin,a));check(()=>assert.equal(boundStorage.getItem('token'),'synthetic-bound-old'))
 const first=await cfg.applyDeploymentConfig({...deployment,legacyBinding:null},new Storage())
 check(()=>assert.equal(first.resetPending,false));check(()=>assert.equal(first.apiOrigin,a))
 const failed=new Storage();failed.setItem('token','synthetic-keep');failed.setItem=()=>{throw Error('QuotaExceededError')}
 await assert.rejects(()=>cfg.applyDeploymentConfig(deployment,failed));checks++
 check(()=>assert.equal(failed.getItem('token'),'synthetic-keep'));check(()=>assert.equal(failed.getItem(cfg.SERVICE_CONFIG_KEY),null))
 check(()=>assert.equal(nativeConfig.MEMOH_ORIGIN,''))
 assert.equal((await call('client.state',{state:'ready'})).ok,false);checks++
 assert.equal((await call('client.configure',{apiOrigin:a})).ok,true);checks++
 check(()=>assert.equal(policy.memohRequest(a+'/bots/b/media/'+'a'.repeat(64)),true))
 check(()=>assert.equal(policy.memohRequest('wss://alpha.example/api/bots/b/chat'),true))
 for(const wrong of [b+'/bots/b/media','https://alpha.example/api-other/media','https://download.example/api/media','https://user@alpha.example/api/media','https://alpha.example/api/%2fsecret'])check(()=>assert.equal(policy.memohRequest(wrong),false))
 await call('client.state',{state:'ready'})
 const bot='11111111-1111-4111-8111-111111111111',session='22222222-2222-4222-8222-222222222222',delegation='33333333-3333-4333-8333-333333333333'
 const input={operation:'a'.repeat(32),botId:bot,hash:'b'.repeat(64),size:123,name:'original.bin',url:a+'/bots/'+bot+'/sessions/'+session+'/delegations/'+delegation+'/attachments/'+'b'.repeat(64),authorization:'Bearer synthetic-test-only'}
 const fetched=await call('download.fetch',input)
 check(()=>assert.equal(fetched.data.hash,input.hash));check(()=>assert.deepEqual(JSON.parse(JSON.stringify(downloads[0][1])),input))
 check(()=>assert.equal(downloads[0][1].url.includes('/sessions/'+session+'/delegations/'+delegation+'/'),true))
 assert.equal((await call('download.save',input)).data,true);checks++
 assert.equal((await call('download.abort',input)).ok,true);checks++
 check(()=>assert.equal(downloads[1][1],input.operation));check(()=>assert.equal(downloads[1][2],bot))
 const count=downloads.length
 assert.equal((await call('download.fetch',{...input,authorization:null})).ok,false);checks++
 check(()=>assert.equal(downloads.length,count))
 frame='https://attacker.example';assert.equal((await call('download.fetch',input)).ok,false);checks++
 frame='begonia-client://app/index.html'
 await call('client.state',{state:'login-page'})
 assert.equal((await call('download.fetch',input)).ok,false);checks++
 check(()=>assert.equal(downloads.length,count))
 await call('client.state',{state:'ready'})
 assert.equal((await call('client.configure',{apiOrigin:b})).ok,false);checks++
 await call('client.configuration');check(()=>assert.equal(nativeConfig.MEMOH_ORIGIN,''));check(()=>assert.deepEqual(events.slice(-4),['close','close','close','login-page']))
 assert.equal((await call('client.configure',{apiOrigin:b})).ok,true);checks++
 check(()=>assert.equal(policy.memohRequest(a+'/bots/b/media'),false))
 check(()=>assert.equal(policy.memohRequest(b+'/bots/b/media'),true))
 frame='https://attacker.example';assert.equal((await call('client.configure',{apiOrigin:a})).ok,false);checks++
 bridge.close();check(()=>assert.equal(nativeConfig.MEMOH_ORIGIN,''))
 console.log(JSON.stringify({checks,passed:true,scope:'actual TS/ETS with synthetic storage and native IO adapters; not Harmony runtime'}))
})().catch(e=>{console.error(e);process.exitCode=1})
