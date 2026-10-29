const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm'),assert=require('node:assert/strict'),crypto=require('node:crypto')
const root=path.resolve(__dirname,'../..'),ts=require(process.env.HARMONY_TEST_TYPESCRIPT||path.join(root,'mobile/.build/memoh-web/checkout/node_modules/typescript'))
let intake,stageBegins=0,maxLocalChunk=0;
let generation=0,token='synthetic-a',listeners=new Set(),calls=[],mode='normal',replyLater,authResponses=[]
const host={begoniaNative:{request:async raw=>{const q=JSON.parse(raw);assert.ok(Buffer.byteLength(raw)<100000);calls.push(q);assert.equal(q.file,undefined);assert.equal(q.bytes,undefined);assert.equal(q.base64,undefined);if(q.command==='upload.close'||q.command==='upload.pause')return '{"ok":true}';if(q.command==='upload.stage.begin'){stageBegins++;intake={handle:crypto.randomBytes(16).toString('hex'),offset:0,size:q.size,name:q.name,type:q.type,scope:q.scope,hash:crypto.createHash('sha256')};return JSON.stringify({ok:true,data:{handle:intake.handle}})}if(q.command==='upload.stage.append'){const bytes=Buffer.from(q.data,'base64');assert.ok(bytes.length<=65536);maxLocalChunk=Math.max(maxLocalChunk,bytes.length);assert.equal(q.offset,intake.offset);intake.hash.update(bytes);intake.offset+=bytes.length;return JSON.stringify({ok:true,data:{offset:intake.offset}})}if(q.command==='upload.stage.finish'){assert.equal(intake.offset,intake.size);return JSON.stringify({ok:true,data:{handle:intake.handle,name:intake.name,type:intake.type,size:intake.size,hash:intake.hash.digest('hex'),resumable:true,scope:intake.scope}})}if(mode==='hold')return new Promise(r=>replyLater=r);if(mode==='401')return '{"ok":false,"error":"UPLOAD_HTTP_401"}';return JSON.stringify({ok:true,data:[{handle:'a'.repeat(32),name:'test.bin',type:'application/octet-stream',size:209715200,hash:'b'.repeat(64),resumable:true,scope:q.scope}]})}}}
const imports={'@/lib/api-client':{sdkAuthQuery:()=>({token}),sdkApiUrl:()=> 'http://example.test/api/users/me',createApiFetch:f=>async(...args)=>{const r=await f(...args);authResponses.push(r.status);return r}},'@/lib/auth-session':{authSessionGeneration:()=>generation,onAuthSessionCleared:f=>{listeners.add(f);return()=>listeners.delete(f)}}}
const m={exports:{}},s=fs.readFileSync(path.join(root,'mobile/memoh/harmony-resumable.ts'),'utf8');vm.runInNewContext(ts.transpileModule(s,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText,{module:m,exports:m.exports,require:n=>imports[n],window:host,crypto:crypto.webcrypto,Response,DOMException,Error,JSON,File,Uint8Array,btoa})
;(async()=>{
 if(process.env.HARMONY_UPLOAD_FAILURE_ENVELOPE_ONLY==='1'){
  const {nativeAttachmentCommand,nativeUploadFailure}=m.exports,original=host.begoniaNative.request,at=Date.now(),checks=[]
  for(const [phase,sdkCode,code]of [['source-open',13900012,'VOICE_UNAVAILABLE'],['source-verify',0,'MEDIA_SHA_MISMATCH'],['limits-read',1007900007,'VOICE_UNAVAILABLE']]){
   let count=0;host.begoniaNative.request=async()=>{count++;return JSON.stringify({ok:false,error:code,failure:{version:1,phase,sdkCode,at,uri:'SECRET_URI',authorization:'SECRET_TOKEN'}})}
   let caught;try{await nativeAttachmentCommand('upload.select','bot-a','session-a')}catch(error){caught=error}
   assert.deepEqual(JSON.parse(JSON.stringify(nativeUploadFailure(caught))),{version:1,phase,sdkCode,at,code});assert.equal(count,1);assert.ok(!JSON.stringify(caught).includes('SECRET'));checks.push(phase)
  }
  const valid={version:1,phase:'source-open',sdkCode:13900012,at}
  for(const failure of [undefined,{...valid,version:2},{...valid,phase:'SECRET_URI'},{...valid,sdkCode:-1},{...valid,sdkCode:2147483648},{...valid,at:'SECRET'}]){
   host.begoniaNative.request=async()=>JSON.stringify({ok:false,error:'VOICE_UNAVAILABLE',failure});let caught;try{await nativeAttachmentCommand('upload.select','bot-a','session-a')}catch(error){caught=error}assert.ok(caught);assert.equal(nativeUploadFailure(caught),undefined)
  }
  host.begoniaNative.request=async()=>JSON.stringify({ok:false,error:'UPLOAD_HTTP_403',failure:valid});let denied;try{await nativeAttachmentCommand('upload.select','bot-a','session-a')}catch(error){denied=error}assert.equal(nativeUploadFailure(denied),undefined);assert.equal(authResponses.at(-1),403)
  let release;host.begoniaNative.request=async()=>new Promise(resolve=>release=resolve);const late=nativeAttachmentCommand('upload.select','bot-a','session-a');generation++;token='synthetic-new';release(JSON.stringify({ok:false,error:'VOICE_UNAVAILABLE',failure:valid}));await assert.rejects(late,error=>error.name==='AbortError'&&nativeUploadFailure(error)===undefined)
  host.begoniaNative.request=original;assert.equal(listeners.size,1)
  console.log(JSON.stringify({actualFrontend:true,optionalNativeFailure:checks,unknownAndLegacyAcceptedWithoutMetadata:true,extraFieldsDropped:true,noRawSDKMessage:true,shared403Handler:true,lateAccountReplyRejected:true,newNetworkRequest:false,newUI:false,nativeDevice:false}));return
 }
 const {nativeAttachmentCommand,validateNativeAttachments}=m.exports
 assert.equal((await nativeAttachmentCommand('upload.list','bot-a','session-a')).length,1);assert.equal(listeners.size,1)
 const before=calls.length,pre=new AbortController();pre.abort();await assert.rejects(nativeAttachmentCommand('upload.run','bot-a','session-a',{},pre.signal));assert.equal(calls.length,before)
 mode='401';await assert.rejects(nativeAttachmentCommand('upload.run','bot-a','session-a'));assert.equal(authResponses.at(-1),401)
 mode='hold';let pending=nativeAttachmentCommand('upload.run','bot-a','session-a');while(!replyLater)await new Promise(setImmediate)
 generation++;token='synthetic-b';for(const f of listeners)f();replyLater('{"ok":false,"error":"UPLOAD_HTTP_401"}');let n=authResponses.length;await assert.rejects(pending);assert.equal(authResponses.length,n);assert.ok(calls.some(x=>x.command==='upload.close'))
 replyLater=undefined;const signal=new AbortController();pending=nativeAttachmentCommand('upload.run','bot-a','session-a',{},signal.signal);while(!replyLater)await new Promise(setImmediate);signal.abort();replyLater('{"ok":true,"data":{}}');await assert.rejects(pending);assert.ok(calls.some(x=>x.command==='upload.pause'));assert.ok(!calls.some(x=>x.command==='upload.abort'));assert.equal(listeners.size,1)
 const file={handle:'a'.repeat(32),name:'test.bin',type:'application/octet-stream',size:10,hash:'b'.repeat(64),resumable:true,scope:'draft:old'}
 assert.equal(validateNativeAttachments([file],'draft:new',true)[0].scope,'draft:old')
 assert.throws(()=>validateNativeAttachments([file],'session:other',true));assert.throws(()=>validateNativeAttachments([file],'draft:new'))
 assert.throws(()=>validateNativeAttachments([{...file,hash:'bad'}],'draft:old'))
 mode='normal';const original=new File([Buffer.alloc(1048583,83)],'clipboard.bin',{type:'application/octet-stream'});
 original.arrayBuffer=()=>{throw Error('whole File read is forbidden')};
 const local=await m.exports.stageNativeFile('bot-a','session-a',original);
 assert.equal(local.hash,crypto.createHash('sha256').update(Buffer.alloc(1048583,83)).digest('hex'));
 assert.equal(maxLocalChunk,65536);assert.equal(stageBegins,1);
 assert.equal((await m.exports.stageNativeFile('bot-a','session-a',original)).handle,local.handle);assert.equal(stageBegins,1);
 await m.exports.forgetNativeFile('bot-a','session-a',original,local.handle);
 assert.notEqual((await m.exports.stageNativeFile('bot-a','session-a',original)).handle,local.handle);assert.equal(stageBegins,2);
 for(const code of ['UPLOAD_CREATE_UNCERTAIN','UPLOAD_RECEIPT_UNAVAILABLE','UPLOAD_COMPLETE_UNCERTAIN','UPLOAD_ORIGINAL_UNAVAILABLE']){
  const request=host.begoniaNative.request;host.begoniaNative.request=async()=>JSON.stringify({ok:false,error:code});
  await assert.rejects(nativeAttachmentCommand('upload.run','bot-a','session-a'),/核对|回执/);host.begoniaNative.request=request;
 }
 console.log(JSON.stringify({actualFrontendBridge:true,boundedLocalFileIntake:true,maxLocalChunk,wholeFileReadForbidden:true,snapshotReusedUntilConfirmedConsume:true,unknownStatesDoNotClaimRecreation:true,metadataOnlyUploadNetworkCommands:true,current401UsesExistingAuthHandler:true,late401AfterAccountChangeIgnored:true,lateReadyAfterCancelRejected:true,authClosesNative:true,perOperationListenersReleased:true,nativeDevice:false}))
})().catch(e=>{console.error(e);process.exitCode=1})
