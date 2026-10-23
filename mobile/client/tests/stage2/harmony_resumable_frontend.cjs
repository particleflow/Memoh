const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm'),assert=require('node:assert/strict'),crypto=require('node:crypto')
const root=path.resolve(__dirname,'../..'),ts=require(path.join(root,'mobile/.build/memoh-web/checkout/node_modules/typescript'))
let generation=0,token='synthetic-a',listeners=new Set(),calls=[],mode='normal',replyLater,authResponses=[]
const host={begoniaNative:{request:async raw=>{assert.ok(Buffer.byteLength(raw)<16384);const q=JSON.parse(raw);calls.push(q);assert.equal(q.file,undefined);assert.equal(q.bytes,undefined);assert.equal(q.base64,undefined);if(q.command==='upload.close'||q.command==='upload.abort')return '{"ok":true}';if(mode==='hold')return new Promise(r=>replyLater=r);if(mode==='401')return '{"ok":false,"error":"UPLOAD_HTTP_401"}';return JSON.stringify({ok:true,data:[{handle:'a'.repeat(32),size:209715200,hash:'b'.repeat(64),resumable:true}]})}}}
const imports={'@/lib/api-client':{sdkAuthQuery:()=>({token}),sdkApiUrl:()=> 'http://example.test/api/users/me',createApiFetch:f=>async(...args)=>{const r=await f(...args);authResponses.push(r.status);return r}},'@/lib/auth-session':{authSessionGeneration:()=>generation,onAuthSessionCleared:f=>{listeners.add(f);return()=>listeners.delete(f)}}}
const m={exports:{}},s=fs.readFileSync(path.join(root,'mobile/memoh/harmony-resumable.ts'),'utf8');vm.runInNewContext(ts.transpileModule(s,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText,{module:m,exports:m.exports,require:n=>imports[n],window:host,crypto:crypto.webcrypto,Response,DOMException,Error,JSON})
;(async()=>{
 const {nativeAttachmentCommand}=m.exports
 assert.equal((await nativeAttachmentCommand('upload.list','bot-a','session-a')).length,1);assert.equal(listeners.size,1)
 const before=calls.length,pre=new AbortController();pre.abort();await assert.rejects(nativeAttachmentCommand('upload.run','bot-a','session-a',{},pre.signal));assert.equal(calls.length,before)
 mode='401';await assert.rejects(nativeAttachmentCommand('upload.run','bot-a','session-a'));assert.equal(authResponses.at(-1),401)
 mode='hold';let pending=nativeAttachmentCommand('upload.run','bot-a','session-a');while(!replyLater)await new Promise(setImmediate)
 generation++;token='synthetic-b';for(const f of listeners)f();replyLater('{"ok":false,"error":"UPLOAD_HTTP_401"}');let n=authResponses.length;await assert.rejects(pending);assert.equal(authResponses.length,n);assert.ok(calls.some(x=>x.command==='upload.close'))
 replyLater=undefined;const signal=new AbortController();pending=nativeAttachmentCommand('upload.run','bot-a','session-a',{},signal.signal);while(!replyLater)await new Promise(setImmediate);signal.abort();replyLater('{"ok":true,"data":{}}');await assert.rejects(pending);assert.ok(calls.some(x=>x.command==='upload.abort'));assert.equal(listeners.size,1)
 console.log(JSON.stringify({actualFrontendBridge:true,noFileBytesInBridge:true,current401UsesExistingAuthHandler:true,late401AfterAccountChangeIgnored:true,lateReadyAfterCancelRejected:true,authClosesNative:true,perOperationListenersReleased:true,nativeDevice:false}))
})().catch(e=>{console.error(e);process.exitCode=1})
