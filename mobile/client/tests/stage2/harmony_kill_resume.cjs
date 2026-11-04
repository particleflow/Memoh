// TF-4 L1a level-2 gate: killing the app pauses durable uploads; a restarted
// signed-in app reconciles the existing journals through the existing single
// task engine and stops at ready. Real ArkTS plus the real web reconciliation
// modules run behind Node kit adapters with real disk/HTTP and real SIGKILL;
// this is still not the Harmony OS runtime or a device.
const fs=require('node:fs'),path=require('node:path'),os=require('node:os'),http=require('node:http'),crypto=require('node:crypto'),vm=require('node:vm'),assert=require('node:assert/strict'),{spawn}=require('node:child_process')
const root=path.resolve(__dirname,'../..'),ts=require(process.env.HARMONY_TEST_TYPESCRIPT||path.join(root,'mobile/.build/memoh-web/checkout/node_modules/typescript'))
const sha=b=>crypto.createHash('sha256').update(b).digest('hex')
const mib=Number(process.env.HARMONY_KILL_RESUME_MIB||12),size=mib*1024*1024
if(process.argv[2]==='worker')worker().catch(e=>{console.log(JSON.stringify({error:String(e&&e.message||e)}));process.exitCode=2});else main().catch(e=>{console.error(e);process.exitCode=1})
async function worker(){
 const [dir,origin,action,handle='',token='alice',bot='bot-a',scope='session-a']=process.argv.slice(3),commands=[]
 const io={OpenMode:{READ_ONLY:fs.constants.O_RDONLY,WRITE_ONLY:fs.constants.O_WRONLY,READ_WRITE:fs.constants.O_RDWR,CREATE:fs.constants.O_CREAT,TRUNC:fs.constants.O_TRUNC,NOFOLLOW:fs.constants.O_NOFOLLOW},WhenceType:{SEEK_SET:0},
  accessSync:p=>fs.existsSync(p),mkdirSync:p=>fs.mkdirSync(p),openSync:(p,m)=>({fd:fs.openSync(p,m)}),closeSync:f=>fs.closeSync(typeof f==='number'?f:f.fd),
  statSync:p=>typeof p==='number'?fs.fstatSync(p):fs.statSync(p),lseek:()=>{},readTextSync:p=>fs.readFileSync(p,'utf8'),listFileSync:p=>fs.readdirSync(p),renameSync:(a,b)=>fs.renameSync(a,b),unlinkSync:p=>fs.unlinkSync(p),fsyncSync:fd=>{const n=typeof fd==='number'?fd:fd.fd;if(!fs.fstatSync(n).isDirectory())fs.fsyncSync(n)},
  read:async(fd,b,o={})=>new Promise((res,rej)=>fs.read(fd,new Uint8Array(b),0,o.length??b.byteLength,o.offset??0,(e,n)=>e?rej(e):res(n))),
  write:async(fd,b,o={})=>new Promise((res,rej)=>fs.write(fd,new Uint8Array(b),0,o.length??b.byteLength,o.offset??0,(e,n)=>e?rej(e):res(n))),writeSync:(fd,b)=>fs.writeSync(fd,new Uint8Array(b))}
 const kitCrypto={cryptoFramework:{createMd:()=>{const h=crypto.createHash('sha256');return{update:async({data})=>h.update(data),digest:async()=>({data:new Uint8Array(h.digest())})}},createRandom:()=>({generateRandomSync:len=>({data:new Uint8Array(crypto.randomBytes(len))})})}}
 const realDigest=kitCrypto.cryptoFramework.createMd;let hashBytes=0
 kitCrypto.cryptoFramework.createMd=()=>{const md=realDigest();return{update:async({data})=>{hashBytes+=data.byteLength;return md.update({data})},digest:()=>md.digest()}}
 class Request{constructor(url,method,headers,content){Object.assign(this,{url,method,headers,content})}}
 class MultipartForm{constructor(fields){this.fields=fields}}
 const rcp={Request,MultipartForm,createSession:()=>{let active;return{cancel:()=>active?.destroy(Error('cancel')),close:()=>active?.destroy(),fetch:r=>new Promise((resolve,reject)=>{
  assert.equal(r.configuration.transfer.autoRedirect,false)
  let content=r.content,headers={...r.headers},tail
  if(content instanceof MultipartForm){const p=content.fields.file;headers['content-type']='multipart/form-data; boundary=bounded';content=Buffer.from('--bounded\r\nContent-Disposition: form-data; name="file"; filename="file.bin"\r\nContent-Type: '+p.contentType+'\r\n\r\n');tail=Buffer.from('\r\n--bounded--\r\n');headers['content-length']=content.length+fs.statSync(p.contentOrPath).size+tail.length}
  else if(content!==undefined){content=typeof content==='string'?Buffer.from(content):Buffer.from(content);headers['content-length']=content.length}
  active=http.request(r.url,{method:r.method,headers},res=>{res.on('data',b=>{try{r.destination(b.buffer.slice(b.byteOffset,b.byteOffset+b.length))}catch(e){active.destroy(e)}});res.on('error',reject);res.on('end',()=>resolve({statusCode:res.statusCode,headers:res.headers}))});active.on('error',reject)
  if(tail){const stream=fs.createReadStream(r.content.fields.file.contentOrPath);active.write(content);stream.pipe(active,{end:false});stream.on('end',()=>active.end(tail))}else active.end(content)
 })}}}
 const modules={'@kit.CoreFileKit':{fileIo:io,fileUri:{FileUri:class{constructor(uri){this.name=path.basename(uri)}}}},'@kit.CryptoArchitectureKit':kitCrypto,'@kit.ArkTS':{url:{URL:{parseURL:value=>new URL(value)}},util:{TextEncoder:class{encodeInto(s){return new TextEncoder().encode(s)}},TextDecoder:class{decodeToString(b){return new TextDecoder().decode(b)}},Base64Helper:class{decodeSync(s){return new Uint8Array(Buffer.from(s,'base64'))}},generateRandomUUID:()=>crypto.randomUUID()}},'@kit.RemoteCommunicationKit':{rcp},'@kit.ArkData':{uniformTypeDescriptor:{getUniformDataTypeByFilenameExtension:()=>''}},'./MemohClientConfig':{MEMOH_ORIGIN:origin},'./NativePhotoSelection':{NativePhotoSelection:class{async select(){return[path.join(dir,'source.bin')]}}}}
 function load(name){const module={exports:{}},code=fs.readFileSync(path.join(root,'mobile/harmony/app/entry/src/main/ets/common/'+name+'.ets'),'utf8');vm.runInNewContext(ts.transpileModule(code,{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText,{module,exports:module.exports,require:n=>{assert.ok(n in modules,n);return modules[n]},ArrayBuffer,Uint8Array,Map,Set,Promise,Error,Date,JSON,Math,Number,String,URL,setTimeout});return module.exports}
 modules['@kit.PerformanceAnalysisKit']={hilog:{info(){},error(){}}};const diagnosticModule=load('ClientDiagnostics');diagnosticModule.ClientDiagnostics.event=()=>{};modules['./ClientDiagnostics']=diagnosticModule
 modules['@kit.NetworkKit']={connection:{getDefaultHttpProxy:async()=>{throw Error('network policy path not under test')},getDefaultNet:async()=>{throw Error('network policy path not under test')}}}
 const serviceModule={exports:{}};vm.runInNewContext(ts.transpileModule(fs.readFileSync(path.join(root,'mobile/memoh/harmony-service-url.ts'),'utf8'),{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText,{exports:serviceModule.exports,require:()=>{throw Error('unexpected validator import')},URL,Error,JSON,Number,Array});modules['./ServiceUrl']=serviceModule.exports
 modules['./NativeTransferNetwork']=load('NativeTransferNetwork')
 modules['./NativeMediaFile']=load('NativeMediaFile')
 const {NativeResumableUpload}=load('NativeResumableUpload'),native=new NativeResumableUpload({filesDir:dir},()=>true)
 const input={operation:'a'.repeat(32),botId:bot,scope,authorization:'Bearer '+token,handle}
 const journalPath=h=>path.join(dir,'memoh-upload-journal-v1',h+'.json')
 const journals=()=>fs.readdirSync(path.join(dir,'memoh-upload-journal-v1')).filter(n=>n.endsWith('.json')).map(n=>JSON.parse(fs.readFileSync(path.join(dir,'memoh-upload-journal-v1',n))))
 function web(client){
  const lifetime={exports:{}};vm.runInNewContext(ts.transpileModule(fs.readFileSync(path.join(root,'mobile/memoh/harmony-transfer-lifetime.ts'),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText,{exports:lifetime.exports})
  const listeners=new Set(),authSession={authSessionGeneration:()=>0,onAuthSessionCleared:f=>{listeners.add(f);return()=>listeners.delete(f)}}
  const apiClient={sdkAuthQuery:()=>({token:'alice'}),sdkApiUrl:({url})=>origin.replace(/\/+$/,'')+url,createApiFetch:f=>(async(...a)=>f(...a))}
  const resumableImports={'./harmony-transfer-lifetime':lifetime.exports,'./harmony-upload-failures':{rememberNativeUploadFailure:async()=>{},forgetNativeUploadFailures:()=>{}},'@/lib/api-client':apiClient,'@/lib/auth-session':authSession}
  const webWindow={begoniaNative:{request:raw=>client.request(raw)}}
  const resumable={exports:{}};vm.runInNewContext(ts.transpileModule(fs.readFileSync(path.join(root,'mobile/memoh/harmony-resumable.ts'),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText,{module:resumable,exports:resumable.exports,require:n=>resumableImports[n],window:webWindow,crypto:crypto.webcrypto,Response,DOMException,Error,JSON,File,Uint8Array,btoa,Map,Set,Promise,Date,AbortController})
  const reconcileImports={vue:{shallowReactive:v=>v},'./harmony-resumable':resumable.exports,'@/lib/auth-session':authSession,'@/lib/api-client':apiClient}
  const reconcile={exports:{}};vm.runInNewContext(ts.transpileModule(fs.readFileSync(path.join(root,'mobile/memoh/harmony-reconcile.ts'),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText,{module:reconcile,exports:reconcile.exports,require:n=>reconcileImports[n],Map,Set,Promise,Error,Date,JSON,AbortController})
  return{reconcile:reconcile.exports}
 }
 function bridgeStack(){
  modules['./NativeResumableUpload']={NativeResumableUpload}
  modules['./NativeVoice']={NativeVoice:class{close(){}prepare(){}}}
  modules['./NativeMediaDownload']={NativeMediaDownload:class{close(){}}}
  modules['./NativeChatSocket']={NativeChatSocket:class{open(){throw new Error('NATIVE_WS_REJECTED')}send(){}close(){}closeAll(){}}}
  modules['./NetReport']={NetReport:{report(){},flush(){}}}
  modules['./NativeTransferOwner']=load('NativeTransferOwner')
  const {NativeClient}=load('NativeClient'),frame='begonia-client://app/index.html',client=new NativeClient({registerJavaScriptProxy(){},getLastJavascriptProxyCallingFrameUrl:()=>frame,getUrl:()=>frame},()=>{},async()=>{},{filesDir:dir})
  client.register()
  const real=client.request.bind(client)
  client.request=async raw=>{const at=commands.length;commands.push({command:JSON.parse(raw).command});const reply=await real(raw);commands[at].reply=JSON.parse(reply);return reply}
  return{client,web:web(client)}
 }
 let result
 if(action==='stage')result=await native.select(input,path.join(dir,'source.bin'))
 else if(action==='run')result=await native.upload(input)
 else if(action==='discard')await native.discard(input)
 else if(action==='consume')await native.consume(input)
 else if(action==='enumerate'){hashBytes=0;result={census:await native.enumerate({operation:'b'.repeat(32),authorization:'Bearer '+token}),hashBytes}}
 else if(action==='enumerate-busy'){
  const run=native.upload(input)
  await new Promise(r=>setTimeout(r,30))
  let busy;try{await native.enumerate({operation:'b'.repeat(32),authorization:'Bearer '+token})}catch(e){busy=e.message}
  result=await run
  hashBytes=0;const census=await native.enumerate({operation:'c'.repeat(32),authorization:'Bearer '+token})
  result={busy,census:census.map(x=>x.state),ready:result.content_hash}
 }
 else if(action==='abort'){
  const watch=setInterval(()=>{try{const j=JSON.parse(fs.readFileSync(journalPath(handle)));if(j.uploadId&&j.offset>0)native.abort(input.operation)}catch{}},5)
  try{result=await native.upload(input)}catch(e){result={aborted:e.message}}finally{clearInterval(watch)}
 }
 else if(action==='reconcile'||action==='reconcile-discard'){
  const stack=bridgeStack()
  assert.equal(JSON.parse(await stack.client.request(JSON.stringify({version:1,command:'client.state',state:'ready',authorization:'Bearer alice'}))).ok,true)
  stack.web.reconcile.ensureNativeUploadReconciliation()
  let discard
  if(action==='reconcile-discard'){
   const deadline=Date.now()+8000
   while(![...stack.web.reconcile.nativeUploadRecovery.values()].some(i=>i.status==='resuming')){if(Date.now()>deadline)throw Error('recovery never resumed');await new Promise(r=>setTimeout(r,5))}
   await new Promise(r=>setTimeout(r,50))
   const item=[...stack.web.reconcile.nativeUploadRecovery.values()].find(i=>i.status==='resuming')
   discard={statusAtDiscard:item.status,name:item.name}
   try{await stack.web.reconcile.discardNativeRecoveryItem(item);discard.ok=true}catch(e){discard.ok=false;discard.error=String(e&&e.message||e)}
  }
  await stack.web.reconcile.nativeUploadReconciliationSettled()
  result={commands,discard,recovery:[...stack.web.reconcile.nativeUploadRecovery.entries()].map(([h,item])=>({handle:h,...item})),journals:journals()}
 }
 else throw Error('unknown action '+action)
 console.log(JSON.stringify({result,commands}))
}
async function main(){
 const dir=fs.mkdtempSync(path.join(os.tmpdir(),'kill-resume-')),receipts=new Map(),events=[],strays=[]
 let mode='normal',activeChild,created=0,createPosts=0,completePosts=0,deletes=0,firstRestartOffset=null,slow=0
 const source=path.join(dir,'source.bin'),fd=fs.openSync(source,'w'),block=Buffer.alloc(262144,0xb7),h=crypto.createHash('sha256')
 for(let at=0;at<size;at+=block.length){const n=Math.min(block.length,size-at);fs.writeSync(fd,block,0,n);h.update(block.subarray(0,n))}fs.fsyncSync(fd);fs.closeSync(fd);const expected=h.digest('hex')
 const json=(res,status,value)=>{res.writeHead(status,{'content-type':'application/json'});res.end(JSON.stringify(value))}
 const server=http.createServer(async(req,res)=>{try{
  const u=new URL(req.url,'http://local'),token=req.headers.authorization
  if(u.pathname==='/api/users/me')return json(res,200,{id:token==='Bearer bob'?'bob':'alice'})
  const media=/^\/api\/bots\/([A-Za-z0-9_-]+)\/media(.*)$/.exec(u.pathname)
  if(!media){strays.push(req.method+' '+u.pathname);return json(res,404,{})}
  const suffix=media[2]
  if(suffix==='/limits')return json(res,200,{max_upload_bytes:209715200,resumable_enabled:true,chunk_max_bytes:8388608})
  if(req.method==='POST'&&suffix==='/uploads'){
   createPosts++
   if(mode==='create-lost-after'){req.resume();const r={id:crypto.randomUUID(),state:'uploading',offset:0,size_bytes:size,expires_at:new Date(Date.now()+86400000).toISOString(),mime:'application/octet-stream'};receipts.set(r.id,r);res.destroy();return}
   let b=[];for await(const c of req)b.push(c);const body=JSON.parse(Buffer.concat(b));assert.equal(body.size_bytes,size);assert.equal(body.sha256,expected)
   const r={id:crypto.randomUUID(),state:'uploading',offset:0,size_bytes:size,expires_at:new Date(Date.now()+86400000).toISOString(),mime:body.mime};receipts.set(r.id,r);fs.writeFileSync(path.join(dir,r.id),'');created++;events.push(['CREATE',r.id]);return json(res,201,r)
  }
  const upload=/^\/uploads\/([a-f0-9-]+)(\/complete)?$/.exec(suffix||'')
  if(upload){let r=receipts.get(upload[1])
   if(r&&Date.parse(r.expires_at)<Date.now()){receipts.delete(upload[1]);r=undefined}
   if(!r){events.push(['MISSING',upload[1]]);return json(res,404,{})}
   if(req.method==='GET'){events.push(['GET',r.offset]);return json(res,200,r)}
   if(req.method==='DELETE'){deletes++;events.push(['DELETE',r.id]);if(r.state==='ready')return json(res,409,{});r.state='canceled';return json(res,200,r)}
   if(req.method==='PATCH'){
    if(Number(req.headers['upload-offset'])!==r.offset){req.resume();return json(res,409,{})}
    if(firstRestartOffset===null&&r.offset>0)firstRestartOffset=Number(req.headers['upload-offset'])
    let bytes=[];for await(const c of req)bytes.push(c);bytes=Buffer.concat(bytes);assert.equal(bytes.length,Number(req.headers['upload-length']));assert.equal(sha(bytes),req.headers['upload-sha256'])
    const out=fs.openSync(path.join(dir,r.id),'r+');fs.writeSync(out,bytes,0,bytes.length,r.offset);fs.closeSync(out);r.offset+=bytes.length;events.push(['PATCH',r.offset])
    if(mode==='kill'&&r.offset>=1048576){activeChild.kill('SIGKILL');res.destroy();return}
    return slow?setTimeout(()=>json(res,200,r),slow):json(res,200,r)
   }
   if(req.method==='POST'&&upload[2]==='/complete'){
    completePosts++;if(r.offset!==size)return json(res,409,{})
    const hh=crypto.createHash('sha256');for await(const c of fs.createReadStream(path.join(dir,r.id),{highWaterMark:262144}))hh.update(c);if(hh.digest('hex')!==expected)return json(res,400,{})
    r.state='ready';r.content_hash=expected;events.push(['COMPLETE',r.offset]);return json(res,200,r)
   }
  }
  strays.push(req.method+' '+u.pathname);return json(res,404,{})
 }catch(e){console.error(e);res.destroy(e)}})
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const origin='http://127.0.0.1:'+server.address().port+'/api'
 async function child(action,handle='',token='alice',bot='bot-a',scope='session-a'){
  return await new Promise((resolve,reject)=>{let out='',err='';const p=spawn(process.execPath,[__filename,'worker',dir,origin,action,handle,token,bot,scope],{stdio:['ignore','pipe','pipe']});activeChild=p;p.stdout.on('data',c=>out+=c);p.stderr.on('data',c=>err+=c);p.on('error',reject);p.on('close',(code,signal)=>{if(signal==='SIGKILL')return resolve({killed:true});if(!out.trim())return reject(Error(err||'empty worker '+code));try{resolve(JSON.parse(out.trim().split('\n').at(-1)))}catch(e){reject(Error(out+err))}})})
 }
 const journals=()=>fs.readdirSync(path.join(dir,'memoh-upload-journal-v1')).filter(n=>n.endsWith('.json')).map(n=>JSON.parse(fs.readFileSync(path.join(dir,'memoh-upload-journal-v1',n))))
 const journalOf=handle=>journals().find(j=>j.handle===handle)
 const residue=()=>fs.readdirSync(path.join(dir,'memoh-upload-journal-v1'))
 const stage=async(token='alice',bot='bot-a',scope='session-a')=>{const s=await child('stage','',token,bot,scope);assert.ok(s.result&&s.result[0],s.error);return s.result[0].handle}
 const verdict={}
 try{
  // Native census semantics: owner-scoped, cross-bot, journal-only (no
  // snapshot hashing), canceled tombstones hidden, single-task BUSY kept.
  {
   const one=await stage(),two=await stage('alice','bot-b','session-b'),three=await stage('bob')
   const aborted=await stage();mode='slow';slow=400;const stop=await child('abort',aborted);slow=0;mode='normal'
   assert.equal(stop.result.aborted,'UPLOAD_CANCELLED');assert.equal(journalOf(aborted).state,'canceled')
   const census=await child('enumerate','','alice')
   assert.equal(census.error,undefined,census.error)
   assert.deepEqual(census.result.census.map(x=>[x.handle,x.bot,x.scope,x.state]).sort(),[[one,'bot-a','session-a','staged'],[two,'bot-b','session-b','staged']].sort())
   assert.ok(census.result.hashBytes<4096,'enumerate must not hash snapshots')
   const busy=await child('enumerate-busy',one)
   assert.equal(busy.result.busy,'UPLOAD_BUSY');assert.deepEqual(busy.result.census.sort(),['ready','staged']);assert.equal(busy.result.ready,expected)
   await child('discard',one);await child('discard',two,'alice','bot-b','session-b');await child('discard',three,'bob');await child('discard',aborted)
   assert.deepEqual(residue(),[])
   verdict.nativeCensus={ownerScoped:true,crossBot:true,noSnapshotHash:census.result.hashBytes,canceledHidden:true,busy:busy.result.busy}
  }
  // A: process death mid upload; restart plus sign-in reconciles to ready.
  {
   createPosts=0;created=0;completePosts=0;deletes=0;events.length=0;firstRestartOffset=null;strays.length=0
   const handle=await stage();mode='kill';assert.ok((await child('run',handle)).killed);mode='normal'
   const saved=journalOf(handle);assert.equal(saved.state,'uploading');assert.ok(saved.uploadId);assert.equal(saved.offset,0)
   assert.equal(receipts.get(saved.uploadId).offset,8388608)
   const report=await child('reconcile')
   assert.equal(report.error,undefined,report.error)
   assert.deepEqual(report.commands.map(c=>c.command),['client.state','upload.enumerate','upload.run'])
   assert.equal(report.commands[1].reply.ok,true)
   assert.equal(report.commands[1].reply.data.length,1)
   assert.equal(report.commands[1].reply.data[0].state,'uploading');assert.equal(report.commands[1].reply.data[0].handle,handle)
   assert.equal(report.commands[2].reply.ok,true);assert.equal(report.commands[2].reply.data.content_hash,expected)
   assert.equal(firstRestartOffset,8388608,'resume continues from the durable server offset')
   assert.equal(createPosts,1);assert.equal(created,1);assert.equal(completePosts,1)
   const ready=receipts.get(saved.uploadId);assert.equal(ready.state,'ready');assert.equal(ready.offset,size)
   assert.equal(sha(fs.readFileSync(path.join(dir,ready.id))),expected)
   const final=journalOf(handle);assert.equal(final.state,'ready');assert.equal(final.uploadId,saved.uploadId);assert.equal(final.offset,size)
   assert.deepEqual(residue().sort(),[handle+'.json',handle+'.original'].sort())
   assert.deepEqual(report.result.recovery,[])
   assert.deepEqual(strays,[])
   verdict.aKillResume={resumedFrom:firstRestartOffset,createPosts,completePosts,hashMatch:true,singleOriginal:created===1,zeroStagingResidue:residue().every(n=>n.startsWith(handle)),noAutoSend:strays.length===0}
   await child('consume',handle)
  }
  // B: discard while the restarted app is resuming the killed upload.
  {
   createPosts=0;completePosts=0;deletes=0;strays.length=0
   const handle=await stage();mode='kill';assert.ok((await child('run',handle)).killed);mode='normal';slow=400
   const saved=journalOf(handle);const report=await child('reconcile-discard');slow=0
   assert.equal(report.error,undefined,report.error)
   assert.equal(report.result.discard.statusAtDiscard,'resuming');assert.equal(report.result.discard.ok,true)
   const issued=report.commands.map(c=>c.command)
   assert.ok(issued.includes('upload.pause'));assert.ok(issued.includes('upload.discard'))
   assert.ok(deletes>=1);assert.equal(completePosts,0);assert.equal(receipts.get(saved.uploadId).state,'canceled')
   assert.deepEqual(residue(),[],'journal and snapshot fully removed')
   assert.deepEqual(report.result.recovery,[])
   assert.deepEqual(strays,[])
   verdict.bDiscardDuringRecovery={discardDuringResume:true,serverCanceled:true,residue:0}
  }
  // C: expired receipt presents failure without rebuilding or deleting.
  {
   createPosts=0;completePosts=0;strays.length=0
   const handle=await stage();mode='kill';assert.ok((await child('run',handle)).killed);mode='normal'
   const saved=journalOf(handle);receipts.get(saved.uploadId).expires_at=new Date(Date.now()-1000).toISOString()
   const report=await child('reconcile')
   assert.equal(report.error,undefined,report.error)
   assert.equal(report.commands[1].reply.data[0].state,'uploading')
   assert.equal(report.commands[2].reply.ok,false);assert.equal(report.commands[2].reply.error,'UPLOAD_RECEIPT_UNAVAILABLE')
   assert.equal(report.result.recovery.length,1);assert.equal(report.result.recovery[0].status,'failed')
   assert.match(report.result.recovery[0].reason,/回执/)
   assert.equal(createPosts,1,'no second create after the expired receipt');assert.equal(completePosts,0)
   assert.deepEqual(journalOf(handle),saved,'journal retains id, offset and expiry')
   assert.ok(fs.existsSync(path.join(dir,'memoh-upload-journal-v1',handle+'.original')))
   assert.deepEqual(strays,[])
   verdict.cExpiredReceipt={rebuilt:createPosts===1,falseSuccess:completePosts===0,snapshotRetained:true,presented:report.result.recovery[0].status}
   await child('discard',handle)
  }
  // D: a lost create reply stays fenced; restart never reposts create.
  {
   createPosts=0;strays.length=0
   const handle=await stage();mode='create-lost-after';const first=await child('run',handle);mode='normal'
   assert.equal(first.error,'UPLOAD_CREATE_UNCERTAIN')
   const saved=journalOf(handle);assert.equal(saved.state,'create-unknown');assert.equal(saved.uploadId,'')
   const report=await child('reconcile')
   assert.equal(report.error,undefined,report.error)
   assert.deepEqual(report.commands.map(c=>c.command),['client.state','upload.enumerate'])
   assert.equal(report.result.recovery.length,1);assert.equal(report.result.recovery[0].status,'fenced')
   assert.equal(createPosts,1,'no second create after restart')
   assert.deepEqual(journalOf(handle),saved)
   assert.ok(fs.existsSync(path.join(dir,'memoh-upload-journal-v1',handle+'.original')))
   assert.deepEqual(strays,[])
   verdict.dCreateUnknownFence={createPosts,fenced:report.result.recovery[0].status,snapshotRetained:true}
   await child('discard',handle)
  }
  // E: the recovery ride is the existing single-task upload.run path; the
  // untouched long-task/background gates stay covered by their own stage2 runs.
  console.log(JSON.stringify({scope:'real ArkTS + real web reconciliation modules behind Node kit adapters, real disk/HTTP and SIGKILL; not the Harmony runtime',mib,size,sha256:expected,...verdict,
   eBackgroundRegression:{resumeRidesExistingUploadRun:true,longTaskGates:'harmony_transfer_background.cjs+harmony_background_owner.cjs',nativeDevice:false}}))
 }finally{
  server.close()
  try{fs.rmSync(dir,{recursive:true,force:true})}catch{}
 }
}
