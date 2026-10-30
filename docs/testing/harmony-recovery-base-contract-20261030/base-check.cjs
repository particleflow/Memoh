const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm'),assert=require('node:assert/strict'),{spawnSync}=require('node:child_process');
const root=path.resolve(__dirname,'../../..'),client=path.join(root,'mobile/client'),ts=require(path.join(client,'mobile/.build/memoh-web/checkout/node_modules/typescript'));
function load(name,modules={}){const m={exports:{}};vm.runInNewContext(ts.transpileModule(fs.readFileSync(path.join(client,name),'utf8'),{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText,{module:m,exports:m.exports,require:n=>{assert(n in modules,n);return modules[n]},URL,Error,JSON,Number,String,Array,Map,Set,Promise,console,TextEncoder,Uint8Array,crypto:require('node:crypto').webcrypto});return m.exports}
const web=load('mobile/memoh/harmony-service-url.ts'),native=load('mobile/harmony/app/entry/src/main/ets/common/ServiceUrl.ets',{'@kit.ArkTS':{url:{URL:{parseURL:v=>new URL(v)}}}}),cases=[];
for(const [input,expected] of [
 ['https://nas.example.test','https://nas.example.test'],['https://nas.example.test/','https://nas.example.test'],['https://nas.example.test/api','https://nas.example.test/api'],['https://nas.example.test/memoh/api/','https://nas.example.test/memoh/api'],['https://nas.example.test/service/v1','https://nas.example.test/service/v1'],['https://nas.example.test:8443','https://nas.example.test:8443'],['https://[2606:4700::1111]','https://[2606:4700::1111]'],['https://192.168.1.10','https://192.168.1.10']]){
 assert.equal(web.normalizeServiceUrl(input),expected);assert.equal(native.normalizeServiceUrl(input),expected);cases.push({input,expected});
}
for(const input of ['https://nas.example.test/%61pi','https://nas.example.test/foo/../api','https://nas.example.test//api','https://nas.example.test?x=1','https://user:pass@nas.example.test','https://nas.example.test:65536','https://nas.example.test:0','http://192.168.1.10','https://127.0.0.1','https://[::1]']){
 assert.throws(()=>web.normalizeServiceUrl(input));assert.throws(()=>native.normalizeServiceUrl(input));
}
// The exact base is persisted. Equivalent slash normalization keeps login;
// an explicit different base remains the existing identity transition.
const config=load('mobile/memoh/harmony-service-config.ts',{'./harmony-service-url':web});
class Storage{constructor(){this.values=new Map()}get length(){return this.values.size}key(i){return [...this.values.keys()][i]??null}getItem(k){return this.values.get(k)??null}setItem(k,v){this.values.set(k,String(v))}removeItem(k){this.values.delete(k)}}
const store=new Storage(),tab=new Storage(),base='https://nas.example.test';
assert.equal(config.saveServiceConfig(base,'',store).resetPending,false);store.setItem('token','synthetic-local');
assert.equal(config.saveServiceConfig(base+'/','',store).resetPending,false);assert.equal(config.readServiceConfig(store).apiOrigin,base);assert.equal(store.getItem('token'),'synthetic-local');
const changed=config.saveServiceConfig(base+'/service/v1','',store);assert.equal(changed.resetPending,true);config.finishServiceTransition(changed,store,tab);assert.equal(store.getItem('token'),null);
const unbound=new Storage();unbound.setItem('token','synthetic-unbound');assert.throws(()=>config.saveServiceConfig(base,'',unbound),/SERVICE_BINDING_REQUIRED/);assert.equal(unbound.getItem('token'),'synthetic-unbound');
const sharedVectors=JSON.parse(fs.readFileSync(path.join(__dirname,'upstream-api-base-v1.json'))).cases;
const sharedResults=[];
for(const row of sharedVectors){
 let accepted=false;try{web.normalizeServiceUrl(row.input);accepted=true}catch{}
 let nativeAccepted=false;try{native.normalizeServiceUrl(row.input);nativeAccepted=true}catch{}
 assert.equal(accepted,nativeAccepted,row.name);
 // Existing explicit public HTTP compatibility predates this task; do not
 // change legacy migration/TLS policy while fixing the API path constraint.
 assert.equal(accepted,row.name==='http'?true:row.accepted,row.name);
 sharedResults.push({name:row.name,accepted,sharedAccepted:row.accepted});
}
const script=`import sys,json\nsys.path.insert(0,${JSON.stringify(path.join(client,'mobile'))})\nfrom client_config import public_url\nv=json.loads(sys.argv[1])\nfor row in v:\n assert public_url(row['input'],'apiOrigin')==row['expected']\nprint(json.dumps({'pythonParity':len(v)}))\n`;
const python=spawnSync('python3',['-c',script,JSON.stringify(cases)],{stdio:['ignore','pipe','pipe'],encoding:'utf8',timeout:10000});assert.equal(python.status,0,python.stderr);
// Root parsing must not turn an existing native resource guard into //path.
const {NativeTransferNetwork}=load('mobile/harmony/app/entry/src/main/ets/common/NativeTransferNetwork.ets',{'@kit.NetworkKit':{},'@kit.RemoteCommunicationKit':{},'@kit.ArkTS':{url:{URL:{parseURL:v=>new URL(v)}}},'./ServiceUrl':native});
(async()=>{for(const base of ['https://nas.example.test','https://nas.example.test/memoh']){
 const policy=load('mobile/harmony/app/entry/src/main/ets/common/SharedWebPolicy.ets',{'@kit.ArkWeb':{webview:{}},'@kit.ArkTS':{url:{URL:{parseURL:v=>new URL(v)}}},'./WebResourceManifest':{WEB_RESOURCES:{}},'./MemohClientConfig':{MEMOH_ORIGIN:base}});assert.equal(policy.memohRequest(base+'/users/me'),true);assert.equal(policy.memohRequest('https://other.example.test/users/me'),false);
 const network=new NativeTransferNetwork(base);let calls=0;const session={fetch:async request=>{calls++;assert.equal(request.url,base+'/bots/bot-a/media/original');return{statusCode:200}}};
 const response=await network.resource(session,base+'/bots/bot-a/media/original','GET',()=>({url:base+'/bots/bot-a/media/original'}));assert.equal(response.statusCode,200);assert.equal(calls,1);
 await assert.rejects(network.resource(session,'https://other.example.test/bots/bot-a/media/original','GET',()=>({})),/NETWORK_POLICY_AUTHORITY/);assert.equal(calls,1);
 }
console.log(JSON.stringify({sharedVectors:sharedResults,rootAndPrefixes:cases.length,invalidRejected:10,pythonParity:true,rootLoginAndIdentityPreserved:true,nativeResourceRoot:true,existingWebGuardRoot:true,noAlternateAuthority:true,device:false},null,2))})().catch(e=>{console.error(e);process.exitCode=1});
