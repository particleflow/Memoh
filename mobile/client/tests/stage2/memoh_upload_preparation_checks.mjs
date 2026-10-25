// Production adapter + actual pinned client store, with controlled SDK/XHR boundaries.
// No live API, model, NAS or native device is exercised.
import assert from 'node:assert/strict'
import fs from 'node:fs'
import { execFileSync } from 'node:child_process'
import { stripTypeScriptTypes } from 'node:module'
const sources = JSON.parse(execFileSync(process.env.PYTHON || 'python3', ['-B', '-c', `
import json,tarfile,os
with tarfile.open(os.environ.get('HARMONY_WEB_CONTEXT','mobile/.build/memoh-web/official-context.tar')) as t:
 print(json.dumps({p:t.extractfile('apps/web/src/store/chat/'+p+'.ts').read().decode() for p in ['send','view-registry','views']}))
`], { encoding: 'utf8' }))
function executable(source, names, scope) {
  let js = stripTypeScriptTypes(source, { mode: 'strip' })
  js = js.replace(/^import[\s\S]*?from ['"][^'"]+['"];?\r?\n/gm, '').replace(/\bexport /g, '')
  return new Function(...Object.keys(scope), js + '\nreturn {' + names.join(',') + '}')(...Object.values(scope))
}
const hash = async bytes => Buffer.from(await crypto.subtle.digest('SHA-256', bytes)).toString('hex')
const originals = new Map(), calls = []
const scopedCalls = [], nativeSaves = [], host = {}
const nativeCalls = [], nativeFileStages = new Map(), forgottenNativeFiles = []
let nativeFailure = false
const nativeFixture = {handle:'a'.repeat(32),name:'native.bin',type:'application/octet-stream',size:8,hash:'b'.repeat(64),resumable:true,scope:'session-a'}
let scopedStatus = 200
let limit = 209715200, holdUpload = false, activeXHR, readStatus, corrupt = false, badReceipt = false
class XHR {
  upload = {}; status = 201; statusText = 'Created'; responseURL = ''; headers = new Headers()
  open(method, url) { this.method = method; this.responseURL = url }
  setRequestHeader(name, value) { this.headers.set(name, value) }
  getAllResponseHeaders() { return 'content-type: application/json\r\n' }
  async send(body) {
    this.body = body
    assert.equal(this.method, 'POST')
    assert.equal(this.headers.get('authorization'), 'Bearer synthetic-test-only')
    assert.match(this.headers.get('content-type'), /^multipart\/form-data; boundary=/)
    const form = await new Response(body, { headers: this.headers }).formData()
    assert.deepEqual([...form.keys()], ['file'], 'media endpoint takes exactly one file and no workspace path')
    this.file = form.get('file'); this.bytes = await this.file.arrayBuffer()
    this.hash = await hash(this.bytes)
    this.bot = new URL(this.responseURL).pathname.split('/')[3]
    this.key = JSON.stringify([this.bot, this.hash])
    this.upload.onprogress({ loaded: body.size, total: body.size, lengthComputable: true })
    activeXHR = this
    if (!holdUpload) this.complete()
  }
  complete() {
    originals.set(this.key, this.bytes)
    this.response = new TextEncoder().encode(JSON.stringify({ content_hash: badReceipt ? '0'.repeat(64) : this.hash,
      size_bytes: this.bytes.byteLength, mime: this.file.type })).buffer
    this.onload()
  }
  abort() { this.onabort() }
}
const scope = {
  stageNativeFile: async (bot,scope,file,signal) => {
    signal?.throwIfAborted();const sha=await hash(await file.slice().arrayBuffer());
    const item={handle:sha.slice(0,32),hash:sha,size:file.size,name:file.name,type:file.type,scope,resumable:true};
    nativeFileStages.set(item.handle,item);return item;
  },
  forgetNativeFile: async (bot,scope,file,handle) => { forgottenNativeFiles.push({bot,scope,file,handle}) },
  nativeAttachmentCommand: async (command, bot, scope, fields, signal) => {
    signal?.throwIfAborted(); nativeCalls.push({command,bot,scope,handle:fields.handle})
    if (nativeFailure) throw new Error('controlled native failure')
    const real = process.env.HARMONY_RESUMABLE_E2E && JSON.parse(process.env.HARMONY_RESUMABLE_E2E)
    if (real) {
      const response = await fetch(real.origin.replace(/\/api$/, '')+'/test/native', {method:'POST',body:JSON.stringify({action:command==='upload.run'?'run':'consume',handle:fields.handle})})
      const reply = await response.json(); if (reply.error) throw new Error(reply.error); return reply.result
    }
    const selected=nativeFileStages.get(fields.handle)||nativeFixture
    return command==='upload.run' ? {content_hash:selected.hash,size_bytes:selected.size,mime:selected.type} : undefined
  },
  createApiFetch: value => value ?? (async (url, options) => {
    scopedCalls.push({ url, options })
    return new Response(scopedStatus === 200 ? file : null, { status: scopedStatus })
  }),
  sdkApiUrl: ({ url }) => 'https://synthetic.invalid/api' + url,
  sdkAuthQuery: () => ({ token: 'synthetic-test-only' }),
  normalizeDelegationPath: executable(fs.readFileSync('mobile/memoh/harmony-service-url.ts','utf8'),
    ['normalizeDelegationPath'], {}).normalizeDelegationPath,
  saveNativeOriginal: async (...args) => { nativeSaves.push(args) }, window: host,
  shallowReactive: value => value, toRaw: value => value, XMLHttpRequest: XHR,
  getBotsByBotIdContainerFsRead: () => { throw Error('workspace is not original storage') },
  getBotsByBotIdContainerFsDownload: () => { throw Error('workspace is not original storage') },
  getBotsByBotIdMediaLimits: async options => {
    options.signal?.throwIfAborted(); calls.push(['limits', options.path.bot_id])
    return { data: { max_upload_bytes: limit } }
  },
  getBotsByBotIdMediaByContentHash: async options => {
    options.signal?.throwIfAborted(); const { bot_id: bot, content_hash: sha } = options.path
    assert.match(sha, /^[a-f0-9]{64}$/); assert.equal(options.query, undefined)
    calls.push(['get',bot,sha]); const bytes = originals.get(JSON.stringify([bot,sha]))
    const status = readStatus ?? (bytes ? 200 : 404)
    const response = new Response(null,{status})
    if (options.throwOnError && status >= 400) throw Error('synthetic download refusal')
    return { data: corrupt && bytes ? new Uint8Array(bytes.byteLength).buffer : bytes, response }
  },
  postBotsByBotIdMedia: async options => {
    const bot = options.path.bot_id; calls.push(['post',bot]); assert.deepEqual(Object.keys(options.body),['file'])
    const body = new FormData(); body.append('file',options.body.file)
    const request = new Request('https://synthetic.invalid/api/bots/'+bot+'/media', { method:'POST', body,
      signal: options.signal, headers:{authorization:'Bearer synthetic-test-only'} })
    const response = await options.fetch(request)
    return { data:await response.json(),response }
  },
}
const adapter = executable(fs.readFileSync('mobile/memoh/harmony-files.ts','utf8'),
 ['prepareSubmission','prepareAttachment','UnknownWrite','verifyAttachmentReferences','attachmentForWire','originalBlob','saveOriginal'],scope)
const { prepareSubmission, UnknownWrite } = adapter
const file = new File(['UPLOAD_SYNTHETIC\nsecond line'], 'example.txt', {type:'text/plain'})
const selected = [file], first = prepareSubmission('bot-a',selected); selected.length=0
const c = new AbortController(), a = first.prepareAttachments(c.signal), b=first.prepareAttachments(c.signal)
assert.equal(a,b); const ready=await a
assert.equal(ready[0].metadata.harmony_transfer,'ready');assert.equal(calls.filter(x=>x[0]==='post').length,1)
assert.equal(first.attachments[0].metadata.harmony_upload_wire_loaded,activeXHR.body.size)
assert.ok(activeXHR.body.size>file.size)
assert.equal(await hash(await (await adapter.originalBlob(ready[0])).arrayBuffer()),ready[0].content_hash)
const scopedBot = '10000000-0000-4000-8000-000000000001'
const scopedPath = `/bots/${scopedBot}/sessions/20000000-0000-4000-8000-000000000002/delegations/30000000-0000-4000-8000-000000000003/attachments/${ready[0].content_hash}`
const delegated = { ...ready[0], bot_id: scopedBot, metadata: { bot_id: scopedBot, delegation_url: scopedPath } }
const ordinaryReads = calls.filter(x => x[0] === 'get').length
assert.equal(await hash(await (await adapter.originalBlob(delegated)).arrayBuffer()), ready[0].content_hash)
assert.equal(scopedCalls[0].url, 'https://synthetic.invalid/api' + scopedPath)
assert.equal(scopedCalls[0].options.headers.authorization, 'Bearer synthetic-test-only')
assert.equal(scopedCalls[0].options.redirect, 'error')
for (const status of [401, 403, 302]) {
  scopedStatus = status
  await assert.rejects(adapter.originalBlob(delegated))
}
scopedStatus = 200
for (const invalid of [null, '', 'https://other.invalid' + scopedPath, scopedPath + '?token=invalid']) {
  const before = scopedCalls.length
  await assert.rejects(adapter.originalBlob({ ...delegated, metadata: { ...delegated.metadata, delegation_url: invalid } }))
  assert.equal(scopedCalls.length, before)
}
host.begoniaNative = {}
await adapter.saveOriginal(delegated)
assert.equal(nativeSaves.length, 1)
assert.deepEqual(nativeSaves[0], [scopedBot, ready[0].content_hash, file.size, file.name, undefined, scopedPath])
delete host.begoniaNative
assert.equal(calls.filter(x => x[0] === 'get').length, ordinaryReads, 'delegated denial never falls back to ordinary Bot/hash')
await prepareSubmission('bot-a',[new File([file],'renamed.bin',{type:file.type})]).prepareAttachments(c.signal)
assert.equal(calls.filter(x=>x[0]==='post').length,1,'same Bot/hash reuses original across filenames')
await prepareSubmission('bot-b',[file]).prepareAttachments(c.signal)
assert.equal(calls.filter(x=>x[0]==='post').length,2,'different Bot must not reuse an authorization result')
for(const invalid of [0,-1,NaN,Infinity,1.5,Number.MAX_SAFE_INTEGER+1,1]) {
 limit=invalid; const count=calls.length
 await assert.rejects(prepareSubmission('bot-a',[file]).prepareAttachments(c.signal))
 assert.deepEqual(calls.slice(count).map(x=>x[0]),['limits'])
}
limit=209715200
await assert.rejects(prepareSubmission('bot-a',[new File([],'empty.txt')]).prepareAttachments(c.signal))
for(const status of [401,403,500,206]) {
 readStatus=status;const count=calls.filter(x=>x[0]==='post').length
 await assert.rejects(prepareSubmission('bot-a',[file]).prepareAttachments(c.signal))
 assert.equal(calls.filter(x=>x[0]==='post').length,count)
}
readStatus=undefined;corrupt=true
await assert.rejects(prepareSubmission('bot-a',[file]).prepareAttachments(c.signal),/完整性/)
corrupt=false
await assert.rejects(adapter.verifyAttachmentReferences('bot-b',ready,c.signal),/其他 Bot/)
const sent=[],views=new Map(); let ensured=0
const targetA={botId:'bot-a',sessionId:'session-a',viewId:'a'},targetB={botId:'bot-b',sessionId:'session-b',viewId:'b'}
function view(target) {
 const key=JSON.stringify([target.botId,target.sessionId,target.viewId])
 if(!views.has(key))views.set(key,{attachmentPreparation:{value:null},transcript:{loadingMessages:{value:false}}})
 return views.get(key)
}
const transcript={createOptimisticUserTurn:(text,attachments)=>({role:'user',text,attachments}),
 createOptimisticAssistantTurn:()=>({role:'assistant',messages:[]}),appendToView(){},messages:[]}
const deps=new Proxy({currentBotId:{value:'bot-a'},sessionId:{value:'session-a'},focusedChatViewId:{value:'a'},
 normalizeTarget:t=>t,chatView:view,transcriptForTarget:()=>transcript,isWebSlashInput:()=>false,isExternalAgentTarget:()=>false,
 isChatViewStreaming:t=>!!view(t).attachmentPreparation.value,
 ensureChatViewSession:async t=>{++ensured;return t},ensureWebSocket:()=>true,
 trackAssistantStream:async()=>{},sendWebSocketMessage:(bot,message)=>{sent.push({bot,message});return true},
 createdSessionIdForInvocation:()=>'',sendFailedMessage:()=> 'failed',draftWorkdirIdFor:()=>'',
 firstSend:{begin(){},admit(){},reveal(){},finish(){},isRevealed:()=>false},watchFirstSendConfirmation:()=>()=>{},
},{get:(o,k)=>k in o?o[k]:()=>undefined})
let invocation=0
const { createChatSend }=executable(sources.send,['createChatSend'],{
 parseMemohError:()=>undefined,resolveApiErrorMessage:e=>e.message,
 cloneRequestedSkills:v=>[...v],normalizeRequestedSkills:v=>v??[],requestedSkillRequestsForWire:()=>[],
 createInvocationId:()=> 'synthetic-'+ ++invocation,hasUserAttachments:v=>!!v?.length,
 attachmentForWire:adapter.attachmentForWire,verifyAttachmentReferences:adapter.verifyAttachmentReferences,
})
const sender=createChatSend(deps)
let preparing
const blocked=sender.sendMessage('draft',first.attachments,{target:targetA,prepareAttachments:signal=>new Promise((resolve,reject)=>{
 preparing=signal;signal.addEventListener('abort',()=>reject(new DOMException('cancel','AbortError')),{once:true})
})})
assert.ok(preparing);assert.equal(ensured,0,'no session creation before readiness')
assert.equal((await sender.sendMessage('independent',undefined,{target:targetB})).ok,true)
view(targetA).attachmentPreparation.value.abort(); const stopped=await blocked
assert.equal(stopped.stage,'startup');assert.equal(stopped.restoreInput,'draft');assert.equal(sent.length,1)
assert.equal(view(targetA).attachmentPreparation.value,null)
const success=await sender.sendMessage('batch',first.attachments,{target:targetA,prepareAttachments:first.prepareAttachments})
assert.equal(success.ok,true);assert.equal(sent.length,2)
assert.deepEqual(Object.keys(sent[1].message.attachments[0]).sort(),['content_hash','mime','name','size','type'])
await first.prepareAttachments(c.signal);assert.equal(sent.length,2,'repeated ready never dispatches')
const missing={type:'file',content_hash:'f'.repeat(64)}
const rejected=await sender.sendMessage('explicit refs',[...ready,missing],{target:targetA})
assert.equal(rejected.stage,'startup');assert.equal(sent.length,2,'every explicitly supplied reference must verify')
const videoReady = await prepareSubmission('bot-a', [new File(['valid video fixture'], 'ok.mp4', { type: 'video/mp4' })]).prepareAttachments(c.signal)
assert.equal(videoReady[0].type, 'video')
// Mixed file batch including video, then a corrupt receipt: no model request.
badReceipt=true
const bad=prepareSubmission('bot-a',[file,new File(['video'],'v.mp4',{type:'video/mp4'})])
const failed=await sender.sendMessage('mixed',bad.attachments,{target:targetA,prepareAttachments:bad.prepareAttachments})
assert.equal(failed.stage,'startup');assert.equal(sent.length,2);badReceipt=false
// Cancellation after write began is unknown. An explicit retry only reconciles.
holdUpload=true
const pendingFile=new File(['pending upload'],'pending.txt',{type:'text/plain'})
const pendingBatch=prepareSubmission('bot-a',[pendingFile]), previous=activeXHR
const pending=sender.sendMessage('keep after cancel',pendingBatch.attachments,{target:targetA,prepareAttachments:pendingBatch.prepareAttachments})
while(activeXHR===previous)await new Promise(r=>setImmediate(r))
activeXHR.abort();const cancelled=await pending
assert.equal(cancelled.stage,'startup');assert.equal(cancelled.restoreInput,'keep after cancel');assert.equal(sent.length,2)
assert.equal(pendingBatch.attachments[0].metadata.harmony_transfer,'unknown')
const postCount=calls.filter(x=>x[0]==='post').length
await assert.rejects(prepareSubmission('bot-a',[pendingFile]).prepareAttachments(c.signal),e=>e instanceof UnknownWrite)
assert.equal(calls.filter(x=>x[0]==='post').length,postCount)
originals.set(activeXHR.key,activeXHR.bytes);holdUpload=false
await prepareSubmission('bot-a',[pendingFile]).prepareAttachments(c.signal)
assert.equal(calls.filter(x=>x[0]==='post').length,postCount,'confirmed lost receipt recovers without retransmission')
const result={targetCommit:JSON.parse(fs.readFileSync('config/memoh_web.lock.json','utf8')).commit,productionAdapter:true,productionStore:true,
 limitsFailClosed:true,singleMultipartFile:true,hashReceiptAndReadback:true,botIsolation:true,
 unknownWriteReconciliation:true,cancelledModelMessages:0,duplicateReadyMessages:0,explicitReferencesGated:true,
 independentText:true,videoType:true,delegatedReadAndSaveScope:true,scopedDenialNoFallback:true,networkRequests:0,nativeDevice:false}
const real = process.env.HARMONY_RESUMABLE_E2E && JSON.parse(process.env.HARMONY_RESUMABLE_E2E)
if (real) Object.assign(nativeFixture,{handle:real.handle,hash:real.hash,size:real.size})
const webReadsBefore=calls.length, postsBefore=sent.length
const nativeBatch=prepareSubmission('bot-a',[],undefined,{scope:'session-a',files:[nativeFixture]})
await nativeBatch.consumeNative();assert.equal(nativeCalls.length,0,'not ready is never consumed')
nativeFailure=true
const nativeFailed=await sender.sendMessage('retain original',nativeBatch.attachments,{target:targetA,prepareAttachments:nativeBatch.prepareAttachments})
assert.equal(nativeFailed.stage,'startup');assert.equal(sent.length,postsBefore);assert.equal(nativeBatch.nativeFiles[0].handle,nativeFixture.handle)
await nativeBatch.consumeNative();assert.equal(nativeCalls.filter(x=>x.command==='upload.consume').length,0)
nativeFailure=false
const retryBatch=prepareSubmission('bot-a',[],undefined,{scope:'session-a',files:[nativeFixture]})
const nativeSent=await sender.sendMessage('native original',retryBatch.attachments,{target:targetA,prepareAttachments:retryBatch.prepareAttachments})
assert.equal(nativeSent.messageSent,true);assert.equal(sent.length,postsBefore+1)
assert.equal(calls.length,webReadsBefore,'native originals never enter Web byte reads, multipart or limits fallback')
assert.equal(nativeCalls.filter(x=>x.command==='upload.consume').length,0,'upload ready alone does not consume')
assert.deepEqual(Object.keys(sent.at(-1).message.attachments[0]).sort(),['content_hash','mime','name','size','type'])
assert.equal(sent.at(-1).message.attachments[0].content_hash,nativeFixture.hash)
await retryBatch.consumeNative();assert.equal(nativeCalls.filter(x=>x.command==='upload.consume').length,1)
result.nativeHandlePreparation=true;result.nativeFailedSendKeepsOriginal=true;result.nativeConsumeAfterConfirmedSend=true
if(real){result.durableNativeDiskHTTPToChat=true;result.networkRequests='controlled loopback native adapter'}
else {
  const pasted=new File(['pasted text'], 'pasted-text.txt', {type:'text/plain'})
  const audio=new File(['synthetic recorded wave'], '录音-voice-test.wav', {type:'audio/wav'})
  const dropped=new File(['dropped bytes'], 'dropped.bin',{type:'application/octet-stream'})
  host.begoniaNative={};const webBeforeMixed=calls.length
  for(const f of [pasted,dropped,audio])f.arrayBuffer=()=>{throw Error('native uploads must not read whole File')}
  const mixed=prepareSubmission('bot-a',[pasted,dropped],{capture:{handle:'voice-test',durationMs:0,peaks:[],state:'complete',text:'',speechError:''},recording:Promise.resolve({file:audio,durationMs:123,peaks:[0.2]})},{scope:'session-a',files:[nativeFixture]})
  const readyMixed=await mixed.prepareAttachments(new AbortController().signal)
  assert.equal(readyMixed.length,4);assert.deepEqual(readyMixed.map(x=>x.type),['file','file','audio','file'])
  assert.equal(readyMixed[2].metadata.voice_duration_ms,123);assert.equal(mixed.files[0],pasted);assert.equal(mixed.files[1],dropped);assert.equal(mixed.files[2],audio)
  assert.equal(calls.length,webBeforeMixed,'paste/drop/recorded Files must not use Web multipart or byte reads')
  await mixed.consumeNative();assert.equal(forgottenNativeFiles.length,3);delete host.begoniaNative
  result.pasteDropAndVoiceUseSameNativeReceipt=true;result.nativeFileUploadForbidsWholeFileRead=true
}
console.log(JSON.stringify(result))
