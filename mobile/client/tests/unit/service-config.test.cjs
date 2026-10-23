const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm'),assert=require('node:assert/strict')
const root=path.resolve(__dirname,'../..'),ts=require(process.env.HARMONY_TEST_TYPESCRIPT || 'typescript')
function load(file,imports={}){const m={exports:{}};vm.runInNewContext(ts.transpileModule(fs.readFileSync(path.join(root,file),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText,{module:m,exports:m.exports,require:n=>{assert.ok(n in imports,n);return imports[n]},URL,Error,JSON,Number,String,Array,Map,Set,Promise,console});return m.exports}
const validator=load('mobile/memoh/harmony-service-url.ts'),{normalizeServiceUrl:normal}=validator
let checks=0;function check(f){f();checks++}
for(const value of ['','/api','http://localhost/api','http://127.0.0.1/api','http://10.0.0.2/api','http://[::1]/api','http://[fd00::1]/api','https://user:pass@alpha.example/api','https://alpha.example/api?token=x','https://alpha.example/api#x','https://alpha.example/foo/../api','https://alpha.example/%61pi','https://alpha.example//api','https://alpha.example/','ftp://alpha.example/api','https://0x7f000001/api','https://alpha.example./api'])check(()=>assert.throws(()=>normal(value)))
check(()=>assert.equal(normal('http://alpha.example:18000/api/'),'http://alpha.example:18000/api'))
check(()=>assert.equal(normal('https://[2606:4700::1111]/api'),'https://[2606:4700::1111]/api'))
check(()=>assert.equal(normal('https://download.example/releases',false),'https://download.example/releases'))
check(()=>assert.equal(normal('https://download.example/app-1.0.8.hap',false),'https://download.example/app-1.0.8.hap'))
for (const host of ['192.0.1.1','198.51.1.1','192.0.0.9','192.0.0.10']) check(()=>assert.equal(normal('https://'+host+'/api'),'https://'+host+'/api'))
for (const host of ['192.0.0.8','192.0.0.11','192.0.2.1','198.51.100.1','192.168.1.1','198.18.0.1','198.19.255.254']) check(()=>assert.throws(()=>normal('https://'+host+'/api')))
const cfg=load('mobile/memoh/harmony-service-config.ts',{'./harmony-service-url':validator})
class Storage{constructor(){this.values=new Map()}get length(){return this.values.size}key(i){return [...this.values.keys()][i]??null}getItem(k){return this.values.get(k)??null}setItem(k,v){this.values.set(k,String(v))}removeItem(k){this.values.delete(k)}}
const s=new Storage(),tab=new Storage(),a='https://alpha.example/api',b='https://beta.example/api'
const quota=new Storage();quota.setItem('token','synthetic-unbound');quota.setItem('chat-input-drafts','local-only');const originalSet=quota.setItem.bind(quota)
quota.setItem=(k,v)=>{if(k===cfg.LEGACY_STATE_KEY)throw Error('QuotaExceededError');originalSet(k,v)}
check(()=>assert.throws(()=>cfg.saveServiceConfig(a,'',quota)))
check(()=>assert.equal(quota.getItem('token'),'synthetic-unbound'))
check(()=>assert.equal(quota.getItem(cfg.SERVICE_CONFIG_KEY),null))
check(()=>assert.equal(cfg.readServiceConfig(s),null))
s.setItem('token','synthetic-legacy');s.setItem('chat-input-drafts','synthetic-draft')
let c=cfg.saveServiceConfig(a,'',s)
check(()=>assert.equal(c.resetPending,true))
check(()=>assert.equal(JSON.parse(s.getItem(cfg.LEGACY_STATE_KEY)).entries.token,undefined))
check(()=>assert.equal(s.getItem('chat-input-drafts'),'synthetic-draft'))
// Simulated process death after pending write: a fresh read cannot become active
// until auth cleanup has run. Transition is idempotent.
c=cfg.readServiceConfig(s);cfg.finishServiceTransition(c,s,tab);cfg.finishServiceTransition(c,s,tab);cfg.commitServiceTransition(c,s)
check(()=>assert.equal(s.getItem('token'),null))
s.setItem('token','synthetic-A');c=cfg.saveServiceConfig(a,'https://download.example/releases',s)
check(()=>assert.equal(c.resetPending,false));check(()=>assert.equal(s.getItem('token'),'synthetic-A'))
c=cfg.saveServiceConfig(b,'',s);check(()=>assert.equal(c.resetPending,true));cfg.finishServiceTransition(c,s,tab);cfg.commitServiceTransition(c,s)
check(()=>assert.equal(s.getItem('token'),null))
s.setItem(cfg.SERVICE_CONFIG_KEY,'{"version":99}');check(()=>assert.throws(()=>cfg.readServiceConfig(s)))
check(()=>assert.equal(cfg.saveServiceConfig(a,'',s).resetPending,true))
console.log(JSON.stringify({checks,passed:true,scope:'included TS configuration modules with synthetic storage; not Harmony OS or upgrade installation'}))
