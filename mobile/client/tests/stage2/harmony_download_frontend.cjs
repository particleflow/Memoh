// Production Web adapter and native command router with controlled SDK/OS seams.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm'),crypto=require('node:crypto')
const root=path.resolve(__dirname,'../..'),ts=require(path.join(root,'mobile/.build/memoh-web/checkout/node_modules/typescript'))
function load(file,imports,globals={}){const m={exports:{}},code=ts.transpileModule(fs.readFileSync(path.join(root,file),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText;vm.runInNewContext(code,{exports:m.exports,require:n=>{assert.ok(n in imports,n);return imports[n]},ArrayBuffer,Uint8Array,Map,Promise,Error,Request,Response,AbortController,DOMException,crypto:crypto.webcrypto,...globals});return m.exports}
let generation=0,token='synthetic-test-only',listeners=new Set(),commands=[],nativeMode='normal',releaseReady,onFetch,action,pickerSaves=0,authResponses=[]
const bot='bot-a',hash='a'.repeat(64),size=209715200,origin='https://api.example.test/api'
const host={begoniaNative:{request:async raw=>{
 assert.ok(Buffer.byteLength(raw)<16384);const q=JSON.parse(raw);commands.push(q);assert.equal(q.version,1)
 if(q.command==='download.fetch'){
  assert.equal(q.url,origin+'/bots/'+bot+'/media/'+hash);assert.equal(q.authorization,'Bearer synthetic-test-only');assert.equal(q.size,size)
  onFetch?.();if(nativeMode==='hold')await new Promise(r=>releaseReady=r)
  if(nativeMode==='unauthorized')return JSON.stringify({ok:false,error:'MEDIA_DOWNLOAD_HTTP_401'})
  return JSON.stringify({ok:true,data:{operation:q.operation,hash:nativeMode==='wrong'?'b'.repeat(64):hash,size}})
 }
 if(q.command==='download.save'){pickerSaves++;return JSON.stringify({ok:true,data:true})}
 assert.equal(q.command,'download.abort');return'{"ok":true}'
}}}
const imports={
 './harmony-service-url':load('mobile/memoh/harmony-service-url.ts',{}, {URL}),
 '@memohai/sdk':{getBotsByBotIdMediaByContentHash:async options=>{assert.equal(options.parseAs,'stream');assert.equal(options.query,undefined);assert.equal(options.path.bot_id,bot);const response=await options.fetch(new Request(origin+'/bots/'+bot+'/media/'+hash,{headers:{authorization:'Bearer '+token},signal:options.signal}));assert.equal(response.body,null);if(!response.ok)throw Error('SDK denied');return{response}}},
 '@/lib/api-client':{createApiFetch:f=>async(...args)=>{const response=await f(...args);authResponses.push(response.status);return response},sdkApiUrl:({url})=>origin+url,sdkAuthQuery:()=>({token})},
 '@/lib/auth-session':{authSessionGeneration:()=>generation,onAuthSessionCleared:f=>{listeners.add(f);return()=>listeners.delete(f)}},
 '@felinic/ui':{toast:{info:(_message,options)=>{action=options.action.onClick;return'test-notice'},dismiss:()=>{}}}
}
const {saveNativeOriginal}=load('mobile/memoh/harmony-download.ts',imports,{window:host})
;(async()=>{
 await saveNativeOriginal(bot,hash,size,'original.bin');assert.deepEqual(commands.map(q=>q.command),['download.fetch','download.save','download.abort']);assert.equal(pickerSaves,1);assert.equal(listeners.size,0)
 const reset=()=>{commands=[];pickerSaves=0;nativeMode='normal';onFetch=undefined}
 reset();const pre=new AbortController();pre.abort();await assert.rejects(saveNativeOriginal(bot,hash,size,'x',pre.signal));assert.equal(commands.length,0)
 reset();nativeMode='wrong';await assert.rejects(saveNativeOriginal(bot,hash,size,'x'));assert.equal(pickerSaves,0);assert.equal(commands.at(-1).command,'download.abort')
 reset();nativeMode='unauthorized';await assert.rejects(saveNativeOriginal(bot,hash,size,'x'));assert.equal(authResponses.at(-1),401);assert.equal(pickerSaves,0)
 reset();nativeMode='hold';let started;const seen=new Promise(r=>started=r);onFetch=started;const cancelled=saveNativeOriginal(bot,hash,size,'x');await seen;action();releaseReady();await assert.rejects(cancelled);assert.equal(pickerSaves,0)
 reset();nativeMode='hold';const authSeen=new Promise(r=>onFetch=r),stale=saveNativeOriginal(bot,hash,size,'x');await authSeen;generation++;token='different-account';for(const f of listeners)f();releaseReady();await assert.rejects(stale);assert.equal(pickerSaves,0);assert.equal(listeners.size,0)
 token='synthetic-test-only';reset();nativeMode='hold';const changeSeen=new Promise(r=>onFetch=r),changed=saveNativeOriginal(bot,hash,size,'x');await changeSeen;token='silent-token-change';releaseReady();await assert.rejects(changed);assert.equal(pickerSaves,0)
 // Execute the actual native bridge dispatch, not just an isolated fetch mock.
 let fetched,aborted,closed=0;class Voice{prepare(){}close(){}}class VoiceTransfer{close(){}}
 class Downloads{constructor(_context,auth){this.auth=auth}async fetch(q){assert.ok(this.auth());fetched=q;return{operation:q.operation,hash:q.hash,size:q.size}}async save(op,owner){assert.equal(op,fetched.operation);assert.equal(owner,fetched.botId);return true}abort(op,owner){aborted={op,owner}}close(){closed++}}
 const service=load('mobile/harmony/app/entry/src/main/ets/common/ServiceUrl.ets',{'@kit.ArkTS':{url:{URL:{parseURL:value=>new URL(value)}}}})
 const config=load('mobile/harmony/app/entry/src/main/ets/common/MemohClientConfig.ets',{'./ServiceUrl':service})
 const {NativeClient}=load('mobile/harmony/app/entry/src/main/ets/common/NativeClient.ets',{'./MemohClientConfig':config,'./NativeResumableUpload':{NativeResumableUpload:class{close(){}}},'./NativeVoice':{NativeVoice:Voice},'./NativeVoiceTransfer':{NativeVoiceTransfer:VoiceTransfer},'./NativeMediaDownload':{NativeMediaDownload:Downloads}})
 let frame='begonia-client://app/index.html';const bridge=new NativeClient({registerJavaScriptProxy(){},deleteJavaScriptRegister(){},getUrl:()=>frame,getLastJavascriptProxyCallingFrameUrl:()=>frame},()=>{},async()=>{},{})
 bridge.register();assert.equal(JSON.parse(await bridge.request(JSON.stringify({version:1,command:'client.state',state:'ready'}))).error,'SERVICE_NOT_CONFIGURED');
 assert.equal(JSON.parse(await bridge.request(JSON.stringify({version:1,command:'client.configure',apiOrigin:origin}))).ok,true);
 assert.equal(JSON.parse(await bridge.request(JSON.stringify({version:1,command:'client.state',state:'ready'}))).ok,true);const op='d'.repeat(32)
 const request={version:1,command:'download.fetch',operation:op,botId:bot,hash,size,name:'x',url:origin,authorization:'Bearer synthetic-test-only'}
 assert.equal(JSON.parse(await bridge.request(JSON.stringify(request))).ok,true);assert.equal(fetched.hash,hash)
 assert.equal(JSON.parse(await bridge.request(JSON.stringify({...request,command:'download.save'}))).data,true)
 await bridge.request(JSON.stringify({...request,command:'download.abort'}));assert.equal(aborted.owner,bot)
 frame='https://untrusted.example/';assert.equal(JSON.parse(await bridge.request(JSON.stringify(request))).ok,false);frame='begonia-client://app/index.html'
 await bridge.request(JSON.stringify({version:1,command:'client.state',state:'login-page'}));assert.equal(JSON.parse(await bridge.request(JSON.stringify(request))).ok,false);assert.ok(closed>0)
 console.log(JSON.stringify({actualFrontend:true,actualNativeRouter:true,metadataOnlyBridge:true,authenticatedSDKEnvelope:true,unauthorizedToExistingSDKHandler:true,noResponseBodyInWebView:true,wrongReceiptRejected:true,preAbortNoBridge:true,userCancelIgnoresLateReady:true,authChangeIgnoresLateReady:true,silentTokenChangeRefused:true,nativeFrameGuard:true,nativeReadyGuard:true,scope:'SDK, native transport and UI are controlled seams; not device or live API'}))
})().catch(e=>{console.error(e);process.exitCode=1})
