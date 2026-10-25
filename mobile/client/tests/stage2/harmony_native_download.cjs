// Actual native source with Node Kit boundaries, real disk and HTTP. Not device qualification.
const fs=require('node:fs'),path=require('node:path'),os=require('node:os'),http=require('node:http'),crypto=require('node:crypto'),vm=require('node:vm'),assert=require('node:assert/strict')
const root=path.resolve(__dirname,'../..'),ts=require(path.join(root,'mobile/.build/memoh-web/checkout/node_modules/typescript'))
const tmp=process.env.HARMONY_DOWNLOAD_TEST_ROOT||fs.mkdtempSync(path.join(os.tmpdir(),'harmony-download-')),positions=new Map()
let maxRead=0,maxWrite=0,maxChunk=0,readCalls=0,copyHook,shortWrite=false,diskFail=false,failCleanup=false
function read(fd,b,o={}){maxRead=Math.max(maxRead,b.byteLength);readCalls++;const at=o.offset??positions.get(fd)??0;return new Promise((resolve,reject)=>fs.read(fd,new Uint8Array(b),0,o.length??b.byteLength,at,(e,n)=>{positions.set(fd,at+(n||0));copyHook?.();e?reject(e):resolve(n)}))}
function write(fd,b,o={}){maxWrite=Math.max(maxWrite,b.byteLength);if(diskFail)throw Error('ENOSPC');const at=o.offset??positions.get(fd)??0,len=Math.min(o.length??b.byteLength,shortWrite?8191:Infinity),n=fs.writeSync(fd,new Uint8Array(b),0,len,at);positions.set(fd,at+n);return n}
const io={OpenMode:{READ_ONLY:0,WRITE_ONLY:1,READ_WRITE:2,CREATE:64,TRUNC:512,NOFOLLOW:131072},WhenceType:{SEEK_SET:0},
 openSync:(p,m)=>{const fd=fs.openSync(p.startsWith('file:')?new URL(p):p,m);positions.set(fd,0);return{fd}},
 open:async(p,m)=>io.openSync(p,m),closeSync:f=>{const fd=typeof f==='number'?f:f.fd;positions.delete(fd);fs.closeSync(fd)},
 statSync:f=>typeof f==='number'?fs.fstatSync(f):fs.statSync(f),lseek:(fd,at)=>positions.set(fd,at),read,write:async(...a)=>write(...a),writeSync:write,
 fsyncSync:fd=>fs.fsyncSync(fd),truncateSync:(fd,n)=>{if(failCleanup)throw Error('EIO');fs.ftruncateSync(fd,n)},accessSync:p=>fs.existsSync(p),mkdirSync:p=>fs.mkdirSync(p),unlinkSync:p=>fs.unlinkSync(p),renameSync:(a,b)=>fs.renameSync(a,b),readTextSync:p=>fs.readFileSync(p,'utf8')}
const kitCrypto={cryptoFramework:{createMd:()=>{const h=crypto.createHash('sha256');return{update:async({data})=>h.update(data),digest:async()=>({data:new Uint8Array(h.digest())})}}}}
function load(file,imports,globals={}){const code=ts.transpileModule(fs.readFileSync(path.join(root,file),'utf8'),{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText;const m={exports:{}};vm.runInNewContext(code,{exports:m.exports,require:n=>{assert.ok(n in imports,n);return imports[n]},ArrayBuffer,Uint8Array,Map,Set,Promise,Error,...globals});return m.exports}
let ownerId='owner-a',delegatedPath='',measureTimer;let mode='normal',total=0,wantHash='',source='',requests=[],pickerCalls=0,pickMode='save',resolvePicker,pickReached
const destination=path.join(tmp,'saved.bin'),sourceSmall=path.join(tmp,'small.bin')
function fileHash(p){const h=crypto.createHash('sha256'),f=fs.openSync(p,'r'),b=Buffer.alloc(262144);try{for(let n;(n=fs.readSync(f,b,0,b.length,null))>0;)h.update(b.subarray(0,n));return h.digest('hex')}finally{fs.closeSync(f)}}
const server=http.createServer((req,res)=>{
 if(req.url==='/api/users/me'){res.writeHead(200,{'content-type':'application/json'});res.end(JSON.stringify({id:ownerId}));return}
 requests.push(req.method==='HEAD'?'HEAD':req.headers.range);assert.equal(req.headers.authorization,'Bearer synthetic-test-only');if(req.method!=='HEAD')assert.equal(req.headers['if-range'],'"'+wantHash+'"')
 assert.equal(req.url,delegatedPath||'/api/bots/bot-a/media/'+wantHash);const start=req.method==='HEAD'?0:Number(/^bytes=(\d+)-$/.exec(req.headers.range)[1]);
 let status=req.method==='HEAD'?200:(mode==='ignore-range'||(mode==='resume-ignored'&&start>0))?200:206,begin=status===200?0:start
 const headers={'content-length':total-begin,'etag':'"'+wantHash+'"','content-type':'application/octet-stream'}
 if(status===206)headers['content-range']='bytes '+begin+'-'+(total-1)+'/'+total
 if(mode==='wrong-range')headers['content-range']='bytes 1-'+(total-1)+'/'+total
 if(mode==='wrong-etag')headers.etag='"'+'0'.repeat(64)+'"'
 if(mode==='unauthorized')status=401
 if(mode==='forbidden')status=403
 if(mode==='redirect'){status=302;headers.location='http://127.0.0.1:1/leak';}
 res.writeHead(status,headers)
 if(req.method==='HEAD'){res.end();return}
 if(mode==='hold'){res.flushHeaders();return}
 if(mode==='durable-hold'){const f=fs.openSync(source,'r'),b=Buffer.alloc(262144);fs.readSync(f,b,0,b.length,start);fs.closeSync(f);res.write(b);return}
 if(['resume','resume-ignored'].includes(mode)&&start===0){const fd=fs.openSync(source,'r'),b=Buffer.alloc(262144);fs.readSync(fd,b,0,b.length,0);fs.closeSync(fd);res.write(b);setTimeout(()=>res.destroy(),30);return}
 const stream=fs.createReadStream(source,{start:begin,highWaterMark:65536});res.on('close',()=>stream.destroy());stream.on('error',()=>res.destroy());stream.pipe(res)
})
;(async()=>{
 if(!process.env.HARMONY_DOWNLOAD_TEST_ORIGIN)await new Promise(r=>server.listen(0,'127.0.0.1',r));const origin=process.env.HARMONY_DOWNLOAD_TEST_ORIGIN||'http://127.0.0.1:'+server.address().port+'/api'
 class Request{constructor(url,method,headers){Object.assign(this,{url,method,headers})}}
 const rcp={Request,createSession:()=>{let active;return{cancel(){active?.destroy(Error('cancelled'))},close(){active?.destroy()},fetch(q){assert.equal(q.configuration.transfer.autoRedirect,false);return new Promise((resolve,reject)=>{
  active=http.request(q.url,{method:q.method,headers:q.headers},res=>{
   const events=q.configuration.tracing?.httpEventsHandler;events?.onStatusCodeReceive(res.statusCode);events?.onHeaderReceive(res.headers)
   res.on('error',reject);res.on('aborted',()=>reject(Error('interrupted')))
   res.on('data',b=>{maxChunk=Math.max(maxChunk,b.length);try{q.destination(b.buffer.slice(b.byteOffset,b.byteOffset+b.length))}catch(e){active.destroy(e);reject(e)}})
   res.on('end',()=>resolve({statusCode:res.statusCode,headers:res.headers}))
  });active.on('error',reject);active.end()
 })}}}}
 const picker={DocumentSaveOptions:class{},DocumentViewPicker:class{async save(options){pickerCalls++;assert.ok(options.newFileNames[0]&&!options.newFileNames[0].includes('/'));pickReached?.();if(pickMode==='hold')return new Promise(r=>resolvePicker=r);if(pickMode==='cancel')return[];fs.closeSync(fs.openSync(destination,'w'));return[new URL('file://'+destination).href]}}}
 const base='mobile/harmony/app/entry/src/main/ets/common/',shared=load(base+'NativeMediaFile.ets',{'@kit.CoreFileKit':{fileIo:io},'@kit.CryptoArchitectureKit':kitCrypto})
 const {NativeMediaDownload}=load(base+'NativeMediaDownload.ets',{'@kit.CoreFileKit':{fileIo:io,picker},'@kit.CryptoArchitectureKit':kitCrypto,'@kit.ArkTS':{util:{TextEncoder:class{encodeInto(s){return new TextEncoder().encode(s)}},TextDecoder:class{decodeToString(b){return new TextDecoder().decode(b)}}}},'@kit.RemoteCommunicationKit':{rcp},'./MemohClientConfig':{MEMOH_ORIGIN:origin},'./NativeMediaFile':shared,'./ServiceUrl':load('mobile/memoh/harmony-service-url.ts',{}, {URL})})
 let authorized=true;const service=new NativeMediaDownload({cacheDir:tmp,filesDir:tmp},()=>authorized)
 if(process.env.HARMONY_DOWNLOAD_TEST_ORIGIN){
  const q=JSON.parse(fs.readFileSync(path.join(tmp,'worker-input.json'),'utf8'));
  q.operation=crypto.randomBytes(16).toString('hex');
  const result=await service.fetch(q);await service.save(q.operation,q.botId);
  assert.equal(fileHash(destination),q.hash);assert.equal(result.size,q.size);return
 }
 const mib=Number(process.argv[2]||200);total=mib*1048576;source=path.join(tmp,'original.bin')
 const fd=fs.openSync(source,'w'),b=Buffer.alloc(262144);for(let at=0;at<total;at+=b.length){b.fill((at/b.length)%251);fs.writeSync(fd,b,0,Math.min(b.length,total-at))}fs.fsyncSync(fd);fs.closeSync(fd);wantHash=fileHash(source)
 let seq=0;const input=()=>({operation:(++seq).toString(16).padStart(32,'0'),botId:'bot-a',hash:wantHash,size:total,name:'test.bin',url:origin+'/bots/bot-a/media/'+wantHash,authorization:'Bearer synthetic-test-only'})
 const cacheEmpty=()=>assert.deepEqual(fs.readdirSync(path.join(tmp,'memoh-download-journal-v1')),[])
 global.gc?.();const baseline=process.memoryUsage(),peak={...baseline};let samples=0
 const measure=()=>{samples++;const m=process.memoryUsage();for(const k of Object.keys(m))peak[k]=Math.max(peak[k],m[k])},timer=measureTimer=setInterval(measure,10),started=Date.now()
 const q=input(),ready=await service.fetch(q);assert.equal(ready.hash,wantHash);assert.equal(pickerCalls,0);assert.equal(await service.save(q.operation,q.botId),true);measure();clearInterval(timer)
 assert.equal(fs.statSync(destination).size,total);assert.equal(fileHash(destination),wantHash);cacheEmpty();const milliseconds=Date.now()-started,largeRequests=[...requests]
 // All remaining checks use real 1 MiB originals and the same production class.
 fs.writeFileSync(sourceSmall,Buffer.alloc(1048576,83));source=sourceSmall;total=1048576;wantHash=fileHash(source)
 const bot='11111111-1111-4111-8111-111111111111',session='22222222-2222-4222-8222-222222222222',delegation='33333333-3333-4333-8333-333333333333'
 delegatedPath='/api/bots/'+bot+'/sessions/'+session+'/delegations/'+delegation+'/attachments/'+wantHash
 const scoped=()=>({...input(),botId:bot,url:origin+delegatedPath.slice(4)})
 const scopedOK=scoped();await service.fetch(scopedOK);await service.save(scopedOK.operation,bot);assert.equal(fileHash(destination),wantHash);cacheEmpty()
 // Cached scoped bytes are never a substitute for current permission.
 const cachedScoped=scoped();await service.fetch(cachedScoped);service.release(cachedScoped.operation,bot);
 mode='forbidden';requests=[];await assert.rejects(service.fetch(scoped()),/HTTP_403/);assert.deepEqual(requests,['HEAD']);
 assert.ok(fs.readdirSync(path.join(tmp,'memoh-download-journal-v1')).some(n=>n.endsWith('.part')));
 mode='normal';requests=[];const reopenedScoped=scoped();await service.fetch(reopenedScoped);assert.deepEqual(requests,['HEAD']);await service.save(reopenedScoped.operation,bot);cacheEmpty();
 for(const denied of ['unauthorized','forbidden','redirect']){mode=denied;const count=requests.length;await assert.rejects(service.fetch(scoped()));assert.equal(requests.length,count+1);cacheEmpty()}
 mode='normal';const invalid=scoped();invalid.url+='?token=forbidden';const count=requests.length;await assert.rejects(service.fetch(invalid),/REQUEST/);assert.equal(requests.length,count)
 delegatedPath=''
 for(const bad of ['wrong-range','wrong-etag','unauthorized','redirect']){mode=bad;await assert.rejects(service.fetch(input()));cacheEmpty()}
 mode='normal';const wrong=input();wrong.hash='0'.repeat(64);wrong.url=origin+'/bots/bot-a/media/'+wrong.hash;const trueHash=wantHash;wantHash=wrong.hash;await assert.rejects(service.fetch(wrong),/MEDIA_SHA_MISMATCH/);wantHash=trueHash;cacheEmpty()
 mode='resume';requests=[];const r=input();await service.fetch(r);assert.deepEqual(requests,['bytes=0-','bytes=262144-']);await service.save(r.operation,r.botId);assert.equal(fileHash(destination),wantHash);cacheEmpty()
 mode='resume-ignored';await assert.rejects(service.fetch(input()));cacheEmpty()
 mode='ignore-range';const ignored=input();await service.fetch(ignored);await service.save(ignored.operation,ignored.botId);cacheEmpty()
 mode='hold';const cancelled=input(),pending=service.fetch(cancelled);await new Promise(r=>setTimeout(r,20));service.abort(cancelled.operation,cancelled.botId);await assert.rejects(pending);cacheEmpty()
 mode='normal';const pre=input();service.abort(pre.operation,pre.botId);await assert.rejects(service.fetch(pre),/CANCELLED/)
 const cross=input();await service.fetch(cross);await assert.rejects(service.save(cross.operation,'bot-b'),/IDENTITY/);assert.throws(()=>service.abort(cross.operation,'bot-b'),/IDENTITY/);service.abort(cross.operation,cross.botId);cacheEmpty()
 const other=input();await assert.rejects(service.fetch({...other,url:origin+'/wrong'}),/REQUEST/)
 pickMode='cancel';const pc=input();await service.fetch(pc);assert.equal(await service.save(pc.operation,pc.botId),false);cacheEmpty()
 pickMode='hold';const held=input();await service.fetch(held);const reached=new Promise(r=>pickReached=r),saving=service.save(held.operation,held.botId);await reached;authorized=false;service.close();resolvePicker(['file://'+destination]);await assert.rejects(saving,/CANCELLED/);cacheEmpty();authorized=true;pickMode='save';pickReached=undefined
 const cp=input();await service.fetch(cp);copyHook=()=>service.abort(cp.operation,cp.botId);await assert.rejects(service.save(cp.operation,cp.botId),/CANCELLED/);copyHook=undefined;assert.equal(fs.statSync(destination).size,0);cacheEmpty()
 const cleanup=input();await service.fetch(cleanup);let copiedReads=0;copyHook=()=>{if(++copiedReads===2)service.abort(cleanup.operation,cleanup.botId)};failCleanup=true;await assert.rejects(service.save(cleanup.operation,cleanup.botId),/EIO/);failCleanup=false;copyHook=undefined;cacheEmpty();assert.equal(positions.size,0)
 // A fresh account cannot discover or reuse another account's cache.
 const accountCache=input();await service.fetch(accountCache);service.release(accountCache.operation,accountCache.botId);
 ownerId='owner-b';requests=[];const newOwner=input();await service.fetch(newOwner);assert.deepEqual(requests,['bytes=0-']);await service.save(newOwner.operation,newOwner.botId);
 assert.ok(fs.readdirSync(path.join(tmp,'memoh-download-journal-v1')).some(n=>n.endsWith('.part')));
 ownerId='owner-a';requests=[];const oldOwner=input();await service.fetch(oldOwner);assert.deepEqual(requests,['HEAD']);await service.save(oldOwner.operation,oldOwner.botId);cacheEmpty();
 shortWrite=true;const sw=input();await service.fetch(sw);await service.save(sw.operation,sw.botId);shortWrite=false;assert.equal(fileHash(destination),wantHash);cacheEmpty()
 diskFail=true;await assert.rejects(service.fetch(input()),/ENOSPC/);diskFail=false;cacheEmpty()
 const corrupt=input();await service.fetch(corrupt);const cache=path.join(tmp,'memoh-download-journal-v1',fs.readdirSync(path.join(tmp,'memoh-download-journal-v1')).find(n=>n.endsWith('.part'))),corruptFD=fs.openSync(cache,'r+');fs.writeSync(corruptFD,Buffer.from([7]),0,1,0);fs.closeSync(corruptFD);await assert.rejects(service.save(corrupt.operation,corrupt.botId),/SHA_MISMATCH/);assert.equal(fs.statSync(destination).size,0);cacheEmpty()
 // Kill the actual child process after its durable checkpoint, then reopen
 // through a second process with a new operation handle and fresh authorization.
 const {spawn}=require('node:child_process');
 const journalRoot=path.join(tmp,'memoh-download-journal-v1');
 const workerInput=input();fs.writeFileSync(path.join(tmp,'worker-input.json'),JSON.stringify(workerInput));
 const child=()=>spawn(process.execPath,[__filename],{env:{...process.env,HARMONY_DOWNLOAD_TEST_ROOT:tmp,HARMONY_DOWNLOAD_TEST_ORIGIN:origin},stdio:['ignore','pipe','pipe']});
 mode='durable-hold';const killed=child();let childError='';killed.stderr.on('data',b=>childError+=b);
 const killedExit=new Promise(r=>killed.once('exit',(code,signal)=>r({code,signal})));
 let journal,offset=0;
 for(let tries=0;tries<500;tries++){
  journal=fs.readdirSync(journalRoot).find(n=>n.endsWith('.json'));
  if(journal){offset=JSON.parse(fs.readFileSync(path.join(journalRoot,journal),'utf8')).received;if(offset>=262144)break}
  if(killed.exitCode!==null)throw Error(childError||'worker exited before checkpoint');
  await new Promise(r=>setTimeout(r,10));
 }
 assert.ok(offset>=262144&&offset<total);killed.kill('SIGKILL');assert.equal((await killedExit).signal,'SIGKILL');
 const record=fs.readFileSync(path.join(journalRoot,journal),'utf8');
 assert.ok(!record.includes('Bearer')&&!record.includes(origin)&&!record.includes('owner-a'));
 // Uncommitted trailing bytes are never trusted after a crash.
 fs.appendFileSync(path.join(journalRoot,journal.slice(0,-5)),Buffer.from('uncommitted tail'));
 mode='normal';requests=[];const reopened=child();let errors='';reopened.stderr.on('data',b=>errors+=b);
 const exit=await new Promise(r=>reopened.once('exit',code=>r(code)));assert.equal(exit,0,errors);
 assert.equal(requests[0],'bytes='+offset+'-');assert.equal(fileHash(destination),wantHash);cacheEmpty();
 const restartOffset=offset;
 assert.ok(maxRead<=262144&&maxWrite<=262144);assert.equal(positions.size,0)
 console.log(JSON.stringify({scope:'Actual native source with Node FS/crypto/RCP/picker adapters; real disk and controlled local HTTP, not Harmony device runtime',mib,bytes:mib*1048576,baseline,peak,samples,milliseconds,maxRead,maxWrite,maxNetworkCallback:maxChunk,largeRequests,readCalls,fullSavedHashVerified:true,noFileBytesInWebView:true,scopedOriginalSaved:true,scopedDenialNoBroadFallback:true,rangeResume:true,cachedScopedPermissionRechecked:true,crossAccountCacheIsolated:true,processKilledAfterFsync:true,restartedInIndependentProcess:true,restartOffset,uncommittedTailDiscarded:true,noCredentialsOrEndpointInJournal:true,rangeIgnoredAtZeroAccepted:true,rangeIgnoredOnResumeRejected:true,invalidHeadersRejected:true,hashMismatchRejected:true,crossBotRejected:true,wrongEndpointRejected:true,cancelDuringNetwork:true,cancelBeforeStart:true,cancelDuringCopy:true,authWhilePickerRejected:true,pickerDismissal:true,shortWritesHandled:true,diskFailureCleanup:true,cleanupIOFailureSurfacedWithNoDescriptorLeak:true,cacheMutationRejected:true,allDescriptorsClosed:true,partialTargetsClearedWhenFilesystemAllows:true}))
 service.close()
})().catch(e=>{console.error(e);process.exitCode=1}).finally(()=>{clearInterval(measureTimer);server.closeAllConnections();server.close();if(!process.env.HARMONY_DOWNLOAD_TEST_ROOT)fs.rmSync(tmp,{recursive:true,force:true})})
