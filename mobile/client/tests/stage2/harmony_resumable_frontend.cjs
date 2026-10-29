const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm'),assert=require('node:assert/strict'),crypto=require('node:crypto')
const root=path.resolve(__dirname,'../..'),ts=require(process.env.HARMONY_TEST_TYPESCRIPT||path.join(root,'mobile/.build/memoh-web/checkout/node_modules/typescript'))
let intake,stageBegins=0,maxLocalChunk=0;
let generation=0,token='synthetic-a',listeners=new Set(),calls=[],mode='normal',replyLater,authResponses=[]
const host={begoniaNative:{request:async raw=>{const q=JSON.parse(raw);assert.ok(Buffer.byteLength(raw)<100000);calls.push(q);assert.equal(q.file,undefined);assert.equal(q.bytes,undefined);assert.equal(q.base64,undefined);if(['upload.close','upload.pause','upload.observe.detach','upload.view.release'].includes(q.command))return '{"ok":true}';if(q.command==='upload.stage.begin'){stageBegins++;intake={handle:crypto.randomBytes(16).toString('hex'),offset:0,size:q.size,name:q.name,type:q.type,scope:q.scope,hash:crypto.createHash('sha256')};return JSON.stringify({ok:true,data:{handle:intake.handle}})}if(q.command==='upload.stage.append'){const bytes=Buffer.from(q.data,'base64');assert.ok(bytes.length<=65536);maxLocalChunk=Math.max(maxLocalChunk,bytes.length);assert.equal(q.offset,intake.offset);intake.hash.update(bytes);intake.offset+=bytes.length;return JSON.stringify({ok:true,data:{offset:intake.offset}})}if(q.command==='upload.stage.finish'){assert.equal(intake.offset,intake.size);return JSON.stringify({ok:true,data:{handle:intake.handle,name:intake.name,type:intake.type,size:intake.size,hash:intake.hash.digest('hex'),resumable:true,scope:intake.scope}})}if(mode==='hold')return new Promise(r=>replyLater=r);if(mode==='401')return '{"ok":false,"error":"UPLOAD_HTTP_401"}';return JSON.stringify({ok:true,data:[{handle:'a'.repeat(32),name:'test.bin',type:'application/octet-stream',size:209715200,hash:'b'.repeat(64),resumable:true,scope:q.scope}]})}}}
const lifetimeModule={exports:{}};vm.runInNewContext(ts.transpileModule(fs.readFileSync(path.join(root,'mobile/memoh/harmony-transfer-lifetime.ts'),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS}}).outputText,{exports:lifetimeModule.exports});
const imports={'./harmony-transfer-lifetime':lifetimeModule.exports,'./harmony-upload-failures':{rememberNativeUploadFailure:async()=>{},forgetNativeUploadFailures:()=>{}},'@/lib/api-client':{sdkAuthQuery:()=>({token}),sdkApiUrl:()=> 'http://example.test/api/users/me',createApiFetch:f=>async(...args)=>{const r=await f(...args);authResponses.push(r.status);return r}},'@/lib/auth-session':{authSessionGeneration:()=>generation,onAuthSessionCleared:f=>{listeners.add(f);return()=>listeners.delete(f)}}}
const m={exports:{}},s=fs.readFileSync(path.join(root,'mobile/memoh/harmony-resumable.ts'),'utf8');vm.runInNewContext(ts.transpileModule(s,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText,{module:m,exports:m.exports,require:n=>imports[n],window:host,crypto:crypto.webcrypto,Response,DOMException,Error,JSON,File,Uint8Array,btoa})
;(async()=>{
 if(process.env.HARMONY_UPLOAD_FAILURE_REPORT_ONLY==='1'||process.env.HARMONY_UPLOAD_FAILURE_HTTP_ONLY==='1'){await failureReportFlow();return}
 if(process.env.HARMONY_UPLOAD_FAILURE_ENVELOPE_ONLY==='1'){
  const {nativeAttachmentCommand,nativeUploadFailure}=m.exports,original=host.begoniaNative.request,at=Date.now(),checks=[]
  for(const [phase,sdkCode,code]of [['source-open',13900012,'VOICE_UNAVAILABLE'],['source-verify',0,'MEDIA_SHA_MISMATCH'],['limits-read',1007900007,'VOICE_UNAVAILABLE']]){
   let count=0;host.begoniaNative.request=async()=>{count++;return JSON.stringify({ok:false,error:code,failure:{version:1,phase,sdkCode,at,uri:'SECRET_URI',authorization:'SECRET_TOKEN'}})}
   let caught;try{await nativeAttachmentCommand('upload.select','bot-a','session-a')}catch(error){caught=error}
   const metadata=nativeUploadFailure(caught);assert.match(metadata.operation,/^[a-f0-9]{32}$/);assert.deepEqual(JSON.parse(JSON.stringify(metadata)),{version:1,phase,sdkCode,at,code,operation:metadata.operation});assert.equal(count,1);assert.ok(!JSON.stringify(caught).includes('SECRET'));checks.push(phase)
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
 replyLater=undefined;const detachedSignal=new AbortController(),detachStart=calls.length;pending=nativeAttachmentCommand('upload.run','bot-a','session-a',{},detachedSignal.signal);while(!replyLater)await new Promise(setImmediate);detachedSignal.abort(lifetimeModule.exports.NATIVE_VIEW_DETACHED);replyLater('{"ok":true,"data":{}}');await assert.rejects(pending);assert.ok(calls.slice(detachStart).some(x=>x.command==='upload.observe.detach'));assert.ok(!calls.slice(detachStart).some(x=>x.command==='upload.pause'))
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

async function failureReportFlow(){
 const live=process.env.HARMONY_UPLOAD_FAILURE_HTTP_ONLY==='1',restoreOnly=process.env.HARMONY_UPLOAD_FAILURE_RESTORE_ONLY==='1',loseAck=process.env.HARMONY_UPLOAD_FAILURE_UNKNOWN_ACK==='1',storagePath=process.env.HARMONY_UPLOAD_FAILURE_OUTBOX_PATH,botId=process.env.HARMONY_UPLOAD_FAILURE_BOT_ID||'bot-a',disk=new Map(storagePath&&fs.existsSync(storagePath)?JSON.parse(fs.readFileSync(storagePath,'utf8')):[]),remote=new Map(),requests=[],now=Date.now(),retention=86400000,op=n=>n.repeat(32);let owner=process.env.HARMONY_UPLOAD_FAILURE_OWNER||'owner-a',credential=process.env.HARMONY_UPLOAD_FAILURE_TOKEN||'synthetic-a',epoch=0,lost=false,held,hold=false,holdBody=false,wireFinished=false,clock=now
 class Clock extends Date{static now(){return clock}}
 const saveDisk=()=>{if(!storagePath)return;const fd=fs.openSync(storagePath,'w',0o600);try{fs.writeFileSync(fd,JSON.stringify([...disk]));fs.fsyncSync(fd)}finally{fs.closeSync(fd)}}
 const storage={getItem:k=>disk.get(k)||null,setItem:(k,v)=>{disk.set(k,v);saveDisk()},removeItem:k=>{disk.delete(k);saveDisk()}}
 const base=process.env.HARMONY_UPLOAD_FAILURE_SERVER_URL||'https://example.test/api';let configuredBase=base
 const url=q=>configuredBase+Object.entries(q.path||{}).reduce((s,[k,v])=>s.replace('{'+k+'}',encodeURIComponent(v)),q.url)
 const wire=async(input,init)=>{
  assert.match(input,/\/media\/upload-failures\/[a-f0-9]{32}$/);assert.equal(init.method,'PUT');assert.equal(init.redirect,'error');assert.equal(init.headers.authorization,'Bearer '+credential)
  const id=input.split('/').at(-1),body=JSON.parse(init.body);assert.deepEqual(Object.keys(body).sort(),body.handle?['at','code','handle','phase','sdk_code','version']:['at','code','phase','sdk_code','version']);assert.ok(!init.body.includes('SECRET'));requests.push({id,body,signal:init.signal})
  if(live){try{const response=await fetch(input,init);if(loseAck){await response.text();throw Error('synthetic lost metadata acknowledgement after real server response')}return response}finally{wireFinished=true}}
  let saved=remote.get(id);if(saved&&JSON.stringify(saved.input)!==JSON.stringify(body))return new Response('{}',{status:409})
  if(!saved){saved={input:body,result:{...body,operation:id,received_at:new Date(now).toISOString(),expires_at:new Date(now+retention).toISOString()}};remote.set(id,saved)}
  if(hold){hold=false;return new Promise(resolve=>held=()=>resolve(new Response(JSON.stringify(saved.result),{status:200})))}
  if(holdBody){holdBody=false;return{status:200,json:()=>new Promise(resolve=>held=()=>resolve(saved.result))}}
  if(lost){lost=false;throw Error('synthetic lost metadata acknowledgement')}
  return new Response(JSON.stringify(saved.result),{status:200})
 }
 function boot(){
  const hooks=new Set(),mounted=[],nativeCalls=[],props={botId,scope:'session-a',disabled:false,active:true,modelValue:[]};let exposed
  const vue={shallowReactive:v=>v,ref:v=>({value:v}),watch(){},onBeforeUnmount(){},onMounted:f=>mounted.push(f)}
  const api={sdkAuthQuery:()=>({token:credential}),sdkApiUrl:url,createApiFetch:(f=wire)=>f}
  const auth={'authSessionGeneration':()=>epoch,onAuthSessionCleared:f=>{hooks.add(f);return()=>hooks.delete(f)}}
  const imports={'./harmony-transfer-lifetime':lifetimeModule.exports,'vue':vue,'@/lib/api-client':api,'@/lib/auth-session':auth,'@/store/user':{useUserStore:()=>({userInfo:{id:owner}})}}
  function load(name,extra={},globals={}){const m={exports:{}},code=fs.readFileSync(path.join(root,'mobile/memoh',name),'utf8');vm.runInNewContext(ts.transpileModule(code,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText,{module:m,exports:m.exports,require:n=>{assert.ok(n in {...imports,...extra},n);return({...imports,...extra})[n]},localStorage:storage,crypto:crypto.webcrypto,TextEncoder,Uint8Array,Map,Set,Error,Date:Clock,AbortController,Response,DOMException,JSON,Number,String,...globals});return m.exports}
  const reporter=load('harmony-upload-failures.ts')
  const host={begoniaNative:{request:async raw=>{const q=JSON.parse(raw);nativeCalls.push(q.command);if(['upload.close','upload.pause','upload.observe.detach','upload.view.release'].includes(q.command))return '{"ok":true}';assert.equal(q.command,'upload.select');return JSON.stringify({ok:false,error:'VOICE_UNAVAILABLE',failure:{version:1,phase:'source-open',sdkCode:13900012,at:clock,uri:'SECRET_URI'}})}}}
  const bridge=load('harmony-resumable.ts',{'./harmony-upload-failures':reporter},{window:host})
  const source=fs.readFileSync(path.join(root,'mobile/memoh/HarmonyAttachments.vue'),'utf8').split('<script setup lang="ts">')[1].split('</script>')[0]
  const compiled=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText,m={exports:{}}
  vm.runInNewContext(compiled,{module:m,exports:m.exports,require:n=>n==='vue'?vue:n==='@felinic/ui'?{Button:{}}:n==='./harmony-transfer-lifetime'?lifetimeModule.exports:n==='./harmony-resumable'?bridge:n==='./harmony-upload-failures'?reporter:n==='@/lib/auth-session'?auth:undefined,defineProps:()=>props,defineEmits:()=>()=>{},defineExpose:v=>exposed=v,AbortController,Error,Map,JSON})
  return{reporter,bridge,exposed,mounted,nativeCalls,clear:()=>{epoch++;for(const f of hooks)f({reason:'logout'})}}
 }
 const wait=async f=>{for(let i=0;i<100;i++){if(f())return;await new Promise(r=>setTimeout(r,5))}assert.fail('metadata state did not settle')}
 if(live&&restoreOnly){
  const resumed=boot();await resumed.reporter.restoreNativeUploadFailures(botId);assert.equal(resumed.reporter.nativeUploadFailureQueue.size,1);const row=[...resumed.reporter.nativeUploadFailureQueue.values()][0];assert.equal(row.report,'reported');assert.equal(requests.length,1);assert.equal(resumed.nativeCalls.length,0)
  const response=await fetch(base+'/bots/'+botId+'/media/upload-failures',{headers:{authorization:'Bearer '+credential}});assert.equal(response.status,200);const records=await response.json();assert.equal(records.length,1);assert.equal(records[0].operation,row.operation)
  console.log(JSON.stringify({realHTTPServerReadback:true,independentProcessRestore:true,pid:process.pid,operation:row.operation,phase:row.failure.phase,sdkCode:row.failure.sdkCode,report:row.report,fileBytesSent:0,nativeCommands:0,nativeDevice:false}));return
 }
 const a=boot();for(const f of a.mounted)f();await a.exposed.select(false);assert.equal(typeof a.exposed.error.value,'object');const first=a.exposed.error.value.failure;assert.equal(first.phase,'source-open');assert.equal(first.sdkCode,13900012);assert.match(first.operation,/^[a-f0-9]{32}$/)
 await wait(()=>loseAck?wireFinished:a.reporter.nativeUploadFailureQueue.get(first.operation)?.report==='reported')
 if(live){
  const response=await fetch(base+'/bots/'+botId+'/media/upload-failures',{headers:{authorization:'Bearer '+credential}});assert.equal(response.status,200);const records=await response.json();assert.equal(records.length,1);assert.equal(records[0].operation,first.operation);assert.equal(records[0].phase,first.phase);assert.equal(records[0].sdk_code,first.sdkCode);assert.equal(requests.length,1);assert.deepEqual(a.nativeCalls,['upload.select'])
  const report=a.reporter.nativeUploadFailureQueue.get(first.operation).report;assert.equal(report,loseAck?'pending':'reported')
  console.log(JSON.stringify({actualComponentFailureState:true,actualBridgeConsumer:true,actualMetadataReporter:true,realHTTPServerReadback:true,pid:process.pid,operation:first.operation,phase:first.phase,sdkCode:first.sdkCode,report,fileBytesSent:0,nativeDevice:false}));return
 }
 assert.equal(remote.size,1);assert.equal(requests.length,1)
 const snapshot=JSON.stringify([...disk.values()]);for(const forbidden of ['SECRET','Bearer','example.test','owner-a'])assert.ok(!snapshot.includes(forbidden))
 const restarted=boot();await restarted.reporter.restoreNativeUploadFailures('bot-a');assert.equal(restarted.reporter.nativeUploadFailureQueue.get(first.operation).report,'reported');assert.equal(requests.length,1)
 const failure={version:1,phase:'source-verify',sdkCode:0,at:clock,code:'MEDIA_SHA_MISMATCH'}
 lost=true;await restarted.reporter.rememberNativeUploadFailure('bot-a',op('b'),failure);assert.equal(restarted.reporter.nativeUploadFailureQueue.get(op('b')).report,'pending');const originalReceipt=JSON.stringify(remote.get(op('b')).result)
 const recovered=boot();await recovered.reporter.restoreNativeUploadFailures('bot-a');assert.equal(recovered.reporter.nativeUploadFailureQueue.get(op('b')).report,'reported');assert.equal(JSON.stringify(remote.get(op('b')).result),originalReceipt);assert.equal(remote.size,2)
 remote.set(op('c'),{input:{different:true},result:{}});await recovered.reporter.rememberNativeUploadFailure('bot-a',op('c'),failure);assert.equal(recovered.reporter.nativeUploadFailureQueue.get(op('c')).report,'conflict');const count=requests.length;await recovered.reporter.restoreNativeUploadFailures('bot-a');assert.equal(requests.length,count)
 hold=true;const pending=recovered.reporter.rememberNativeUploadFailure('bot-a',op('d'),failure,op('e'));await wait(()=>!!held);const release=held;held=undefined;recovered.reporter.forgetNativeUploadFailures('bot-a',op('e'));assert.ok(requests.at(-1).signal.aborted);release();await pending;assert.equal(recovered.reporter.nativeUploadFailureQueue.has(op('d')),false)
 holdBody=true;const pendingBody=recovered.reporter.rememberNativeUploadFailure('bot-a',op('1'),failure,op('2'));await wait(()=>!!held);const releaseBody=held;held=undefined;recovered.reporter.forgetNativeUploadFailures('bot-a',op('2'));releaseBody();await pendingBody;assert.equal(recovered.reporter.nativeUploadFailureQueue.has(op('1')),false)
 hold=true;const late=recovered.reporter.rememberNativeUploadFailure('bot-a',op('f'),failure);await wait(()=>!!held);const previousDisk=new Map(disk);recovered.clear();owner='owner-b';credential='synthetic-b';assert.ok(requests.at(-1).signal.aborted);held();held=undefined;await late;assert.equal(recovered.reporter.nativeUploadFailureQueue.size,0);assert.equal(disk.size,0)
 for(const [k,v]of previousDisk)disk.set(k,v);const other=boot();const priorRequests=requests.length;await other.reporter.restoreNativeUploadFailures('bot-a');assert.equal(other.reporter.nativeUploadFailureQueue.size,0);assert.equal(requests.length,priorRequests)
 owner='owner-a';credential='synthetic-a';for(const [k,v]of previousDisk)disk.set(k,v);configuredBase='https://other.example.test/api';const foreignOrigin=boot();await foreignOrigin.reporter.restoreNativeUploadFailures('bot-a');assert.equal(foreignOrigin.reporter.nativeUploadFailureQueue.size,0);assert.equal(requests.length,priorRequests);assert.deepEqual([...disk],[...previousDisk]);configuredBase=base
 owner='owner-a';credential='synthetic-a';for(const [k,v]of previousDisk)disk.set(k,v);clock=now+retention+1;const expired=boot();await expired.reporter.restoreNativeUploadFailures('bot-a');assert.equal(expired.reporter.nativeUploadFailureQueue.size,0);assert.equal(requests.length,priorRequests)
 clock=now;const key='memoh:harmony:upload-failures:v1',known=JSON.parse(previousDisk.get(key))[0]
 for(const unknown of ['{"unrecognized":"SECRET_HISTORICAL"}',JSON.stringify([{...known,expires:'SECRET_INVALID_TIME'}]),JSON.stringify([{...known,failure:{...known.failure,version:2}}])]){
  disk.set(key,unknown);const preserved=boot();await preserved.reporter.restoreNativeUploadFailures('bot-a');assert.equal(disk.get(key),unknown);await preserved.reporter.rememberNativeUploadFailure('bot-a',op('0'),failure);assert.equal(disk.get(key),unknown);assert.equal(preserved.reporter.nativeUploadFailureQueue.get(op('0')).report,'reported')
 }
 assert.deepEqual(a.nativeCalls,['upload.select']);assert.equal(a.exposed.error.value.failure.operation,first.operation)
 console.log(JSON.stringify({actualAttachmentErrorState:true,actualNativeBridgeConsumer:true,actualMetadataReporter:true,persistentSameIdentityRestart:true,unknownAckSameOperation:true,conflictStopsRetry:true,consumePreventsLateResurrection:true,logoutAbortsAndClears:true,wrongOwnerOriginBinding:true,expiryMatchesUploadTTL:true,unknownDiskBytesPreserved:true,metadataOnlyRequests:true,noUploadRunOrOriginalBytes:true,newButtons:false,nativeDevice:false}))
}
