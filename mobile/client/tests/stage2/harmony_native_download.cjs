// Actual native source with Node Kit boundaries, real disk and HTTP. Not device qualification.
const fs=require('node:fs'),path=require('node:path'),os=require('node:os'),http=require('node:http'),crypto=require('node:crypto'),vm=require('node:vm'),assert=require('node:assert/strict')
const root=path.resolve(__dirname,'../..'),ts=require(path.join(root,'mobile/.build/memoh-web/checkout/node_modules/typescript'))
const tmp=fs.mkdtempSync(path.join(os.tmpdir(),'harmony-download-')),positions=new Map()
let maxRead=0,maxWrite=0,maxChunk=0,readCalls=0,copyHook,shortWrite=false,diskFail=false,failCleanup=false
function read(fd,b,o={}){maxRead=Math.max(maxRead,b.byteLength);readCalls++;const at=positions.get(fd)||0;return new Promise((resolve,reject)=>fs.read(fd,new Uint8Array(b),0,o.length??b.byteLength,at,(e,n)=>{positions.set(fd,at+(n||0));copyHook?.();e?reject(e):resolve(n)}))}
function write(fd,b,o={}){maxWrite=Math.max(maxWrite,b.byteLength);if(diskFail)throw Error('ENOSPC');const at=positions.get(fd)||0,len=Math.min(o.length??b.byteLength,shortWrite?8191:Infinity),n=fs.writeSync(fd,new Uint8Array(b),0,len,at);positions.set(fd,at+n);return n}
const io={OpenMode:{READ_ONLY:0,WRITE_ONLY:1,READ_WRITE:2,CREATE:64,TRUNC:512,NOFOLLOW:131072},WhenceType:{SEEK_SET:0},
 openSync:(p,m)=>{const fd=fs.openSync(p.startsWith('file:')?new URL(p):p,m);positions.set(fd,0);return{fd}},
 open:async(p,m)=>io.openSync(p,m),closeSync:f=>{const fd=typeof f==='number'?f:f.fd;positions.delete(fd);fs.closeSync(fd)},
 statSync:f=>typeof f==='number'?fs.fstatSync(f):fs.statSync(f),lseek:(fd,at)=>positions.set(fd,at),read,write:async(...a)=>write(...a),writeSync:write,
 fsyncSync:fd=>fs.fsyncSync(fd),truncateSync:(fd,n)=>{if(failCleanup)throw Error('EIO');fs.ftruncateSync(fd,n)},accessSync:p=>fs.existsSync(p),mkdirSync:p=>fs.mkdirSync(p),unlinkSync:p=>fs.unlinkSync(p)}
const kitCrypto={cryptoFramework:{createMd:()=>{const h=crypto.createHash('sha256');return{update:async({data})=>h.update(data),digest:async()=>({data:new Uint8Array(h.digest())})}}}}
function load(file,imports,globals={}){const code=ts.transpileModule(fs.readFileSync(path.join(root,file),'utf8'),{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText;const m={exports:{}};vm.runInNewContext(code,{exports:m.exports,require:n=>{assert.ok(n in imports,n);return imports[n]},ArrayBuffer,Uint8Array,Map,Set,Promise,Error,...globals});return m.exports}
let mode='normal',total=0,wantHash='',source='',requests=[],pickerCalls=0,pickMode='save',resolvePicker,pickReached
const destination=path.join(tmp,'saved.bin'),sourceSmall=path.join(tmp,'small.bin')
function fileHash(p){const h=crypto.createHash('sha256'),f=fs.openSync(p,'r'),b=Buffer.alloc(262144);try{for(let n;(n=fs.readSync(f,b,0,b.length,null))>0;)h.update(b.subarray(0,n));return h.digest('hex')}finally{fs.closeSync(f)}}
const server=http.createServer((req,res)=>{
 requests.push(req.headers.range);assert.equal(req.headers.authorization,'Bearer synthetic-test-only');assert.equal(req.headers['if-range'],'"'+wantHash+'"')
 assert.equal(req.url,'/api/bots/bot-a/media/'+wantHash);const start=Number(/^bytes=(\d+)-$/.exec(req.headers.range)[1]);
 let status=(mode==='ignore-range'||(mode==='resume-ignored'&&start>0))?200:206,begin=status===200?0:start
 const headers={'content-length':total-begin,'etag':'"'+wantHash+'"','content-type':'application/octet-stream'}
 if(status===206)headers['content-range']='bytes '+begin+'-'+(total-1)+'/'+total
 if(mode==='wrong-range')headers['content-range']='bytes 1-'+(total-1)+'/'+total
 if(mode==='wrong-etag')headers.etag='"'+'0'.repeat(64)+'"'
 if(mode==='unauthorized')status=401
 if(mode==='redirect'){status=302;headers.location='http://127.0.0.1:1/leak';}
 res.writeHead(status,headers)
 if(mode==='hold'){res.flushHeaders();return}
 if(['resume','resume-ignored'].includes(mode)&&start===0){const fd=fs.openSync(source,'r'),b=Buffer.alloc(262144);fs.readSync(fd,b,0,b.length,0);fs.closeSync(fd);res.write(b);setTimeout(()=>res.destroy(),30);return}
 const stream=fs.createReadStream(source,{start:begin,highWaterMark:65536});res.on('close',()=>stream.destroy());stream.on('error',()=>res.destroy());stream.pipe(res)
})
;(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const origin='http://127.0.0.1:'+server.address().port+'/api'
 class Request{constructor(url,method,headers){Object.assign(this,{url,method,headers})}}
 const rcp={Request,createSession:()=>{let active;return{cancel(){active?.destroy(Error('cancelled'))},close(){active?.destroy()},fetch(q){assert.equal(q.configuration.transfer.autoRedirect,false);return new Promise((resolve,reject)=>{
  active=http.request(q.url,{method:q.method,headers:q.headers},res=>{
   const events=q.configuration.tracing.httpEventsHandler;events.onStatusCodeReceive(res.statusCode);events.onHeaderReceive(res.headers)
   res.on('error',reject);res.on('aborted',()=>reject(Error('interrupted')))
   res.on('data',b=>{maxChunk=Math.max(maxChunk,b.length);try{q.destination(b.buffer.slice(b.byteOffset,b.byteOffset+b.length))}catch(e){active.destroy(e);reject(e)}})
   res.on('end',()=>resolve({statusCode:res.statusCode,headers:res.headers}))
  });active.on('error',reject);active.end()
 })}}}}
 const picker={DocumentSaveOptions:class{},DocumentViewPicker:class{async save(options){pickerCalls++;assert.ok(options.newFileNames[0]&&!options.newFileNames[0].includes('/'));pickReached?.();if(pickMode==='hold')return new Promise(r=>resolvePicker=r);if(pickMode==='cancel')return[];fs.closeSync(fs.openSync(destination,'w'));return[new URL('file://'+destination).href]}}}
 const base='mobile/harmony/app/entry/src/main/ets/common/',shared=load(base+'NativeMediaFile.ets',{'@kit.CoreFileKit':{fileIo:io},'@kit.CryptoArchitectureKit':kitCrypto})
 const {NativeMediaDownload}=load(base+'NativeMediaDownload.ets',{'@kit.CoreFileKit':{fileIo:io,picker},'@kit.RemoteCommunicationKit':{rcp},'./MemohClientConfig':{MEMOH_ORIGIN:origin},'./NativeMediaFile':shared,'./ServiceUrl':load('mobile/memoh/harmony-service-url.ts',{}, {URL})})
 let authorized=true;const service=new NativeMediaDownload({cacheDir:tmp},()=>authorized)
 const mib=Number(process.argv[2]||200);total=mib*1048576;source=path.join(tmp,'original.bin')
 const fd=fs.openSync(source,'w'),b=Buffer.alloc(262144);for(let at=0;at<total;at+=b.length){b.fill((at/b.length)%251);fs.writeSync(fd,b,0,Math.min(b.length,total-at))}fs.fsyncSync(fd);fs.closeSync(fd);wantHash=fileHash(source)
 let seq=0;const input=()=>({operation:(++seq).toString(16).padStart(32,'0'),botId:'bot-a',hash:wantHash,size:total,name:'test.bin',url:origin+'/bots/bot-a/media/'+wantHash,authorization:'Bearer synthetic-test-only'})
 const cacheEmpty=()=>assert.deepEqual(fs.readdirSync(path.join(tmp,'memoh-original-downloads')),[])
 global.gc?.();const baseline=process.memoryUsage(),peak={...baseline};let samples=0
 const measure=()=>{samples++;const m=process.memoryUsage();for(const k of Object.keys(m))peak[k]=Math.max(peak[k],m[k])},timer=setInterval(measure,10),started=Date.now()
 const q=input(),ready=await service.fetch(q);assert.equal(ready.hash,wantHash);assert.equal(pickerCalls,0);assert.equal(await service.save(q.operation,q.botId),true);measure();clearInterval(timer)
 assert.equal(fs.statSync(destination).size,total);assert.equal(fileHash(destination),wantHash);cacheEmpty();const milliseconds=Date.now()-started,largeRequests=[...requests]
 // All remaining checks use real 1 MiB originals and the same production class.
 fs.writeFileSync(sourceSmall,Buffer.alloc(1048576,83));source=sourceSmall;total=1048576;wantHash=fileHash(source)
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
 shortWrite=true;const sw=input();await service.fetch(sw);await service.save(sw.operation,sw.botId);shortWrite=false;assert.equal(fileHash(destination),wantHash);cacheEmpty()
 diskFail=true;await assert.rejects(service.fetch(input()),/ENOSPC/);diskFail=false;cacheEmpty()
 const corrupt=input();await service.fetch(corrupt);const cache=path.join(tmp,'memoh-original-downloads',corrupt.operation+'.part'),corruptFD=fs.openSync(cache,'r+');fs.writeSync(corruptFD,Buffer.from([7]),0,1,0);fs.closeSync(corruptFD);await assert.rejects(service.save(corrupt.operation,corrupt.botId),/SHA_MISMATCH/);assert.equal(fs.statSync(destination).size,0);cacheEmpty()
 assert.ok(maxRead<=262144&&maxWrite<=262144);assert.equal(positions.size,0)
 console.log(JSON.stringify({scope:'Actual native source with Node FS/crypto/RCP/picker adapters; real disk and controlled local HTTP, not Harmony device runtime',mib,bytes:mib*1048576,baseline,peak,samples,milliseconds,maxRead,maxWrite,maxNetworkCallback:maxChunk,largeRequests,readCalls,fullSavedHashVerified:true,noFileBytesInWebView:true,rangeResume:true,rangeIgnoredAtZeroAccepted:true,rangeIgnoredOnResumeRejected:true,invalidHeadersRejected:true,hashMismatchRejected:true,crossBotRejected:true,wrongEndpointRejected:true,cancelDuringNetwork:true,cancelBeforeStart:true,cancelDuringCopy:true,authWhilePickerRejected:true,pickerDismissal:true,shortWritesHandled:true,diskFailureCleanup:true,cleanupIOFailureSurfacedWithNoDescriptorLeak:true,cacheMutationRejected:true,allDescriptorsClosed:true,partialTargetsClearedWhenFilesystemAllows:true}))
 service.close()
})().catch(e=>{console.error(e);process.exitCode=1}).finally(()=>{server.closeAllConnections();server.close();fs.rmSync(tmp,{recursive:true,force:true})})
