// Execute the real API wrapper with a controlled SDK boundary. No credential is logged.
const fs = require('node:fs'), path = require('node:path'), vm = require('node:vm'), assert = require('node:assert/strict')
const root = path.resolve(__dirname, '../..')
const checkout = process.env.HARMONY_TEST_CHECKOUT || path.join(root, 'mobile/.build/memoh-web/checkout')
const ts = require(path.join(checkout, 'node_modules/typescript'))
const file = path.join(checkout, 'apps/web/src/lib/api-client.ts')
let source = fs.readFileSync(file, 'utf8').replaceAll('import.meta.env', '({})')
let generation = 0, notifications = 0, redirects = 0
const values = new Map(), storage = { getItem:k=>values.get(k)??null, setItem:(k,v)=>values.set(k,v), removeItem:k=>values.delete(k) }
const client = { setConfig(){}, interceptors:{request:{exists:()=>false,use(){}}} }
const moduleValue = {exports:{}}
vm.runInNewContext(ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText,
 {module:moduleValue,exports:moduleValue.exports,require:name=>name==='@memohai/sdk/client'?{client}:{
   authSessionGeneration:()=>generation,notifyAuthSessionCleared:()=>{generation++;notifications++}},
  localStorage:storage,Request,Response,Headers,URL,globalThis:{fetch},window:{location:{origin:'http://example.test'}}})
const api=moduleValue.exports
api.setupApiClient({onUnauthorized:()=>redirects++})
async function late401({before,after,bearer,sessionChange=false}) {
 values.clear();notifications=redirects=0;if(before)values.set('token',before)
 let resolve
 const request=api.createApiFetch(()=>new Promise(r=>resolve=r))('http://example.test/api/users/me',
   {headers:bearer?{Authorization:'Bearer '+bearer}:{}})
 if(sessionChange)generation++
 if(after)values.set('token',after);else values.delete('token')
 resolve(new Response('',{status:401}));await request
 return {preserved:storage.getItem('token')===after,notifications,redirects}
}
;(async()=>{
 const anonymous=await late401({before:'',after:'synthetic-new-login',bearer:''})
 const sameTokenNewSession=await late401({before:'synthetic-same-token',after:'synthetic-same-token',bearer:'synthetic-same-token',sessionChange:true})
 const oldBearer=await late401({before:'synthetic-old',after:'synthetic-new',bearer:'synthetic-old'})
 const report={scope:'Actual api-client.ts under Node; deferred synthetic 401 and auth-session boundary',
  anonymous401CannotClearNewLogin:anonymous.preserved&&anonymous.notifications===0,
  priorSession401CannotClearSameTokenLogin:sameTokenNewSession.preserved&&sameTokenNewSession.notifications===0,
  oldBearerCannotClearNewLogin:oldBearer.preserved&&oldBearer.notifications===0}
 console.log(JSON.stringify(report))
 if(process.argv.includes('--baseline'))return
 assert.ok(report.anonymous401CannotClearNewLogin)
 assert.ok(report.priorSession401CannotClearSameTokenLogin)
 assert.ok(report.oldBearerCannotClearNewLogin)
 values.set('token','synthetic-current');notifications=redirects=0
 await api.createApiFetch(async()=>new Response('',{status:401}))('http://example.test/api/users/me',{headers:{Authorization:'Bearer synthetic-current'}})
 assert.equal(storage.getItem('token'),null);assert.equal(notifications,1);assert.equal(redirects,1)
 values.set('token','synthetic-offline');notifications=redirects=0
 await assert.rejects(api.createApiFetch(async()=>{throw Error('network offline')})('http://example.test/api/users/me'))
 assert.equal(storage.getItem('token'),'synthetic-offline');assert.equal(notifications,0)
 console.log(JSON.stringify({current401StillClears:true,networkFailureRetainsToken:true}))
})().catch(()=>{console.error('AUTH_RACE_ASSERTION_FAILED');process.exitCode=1})
