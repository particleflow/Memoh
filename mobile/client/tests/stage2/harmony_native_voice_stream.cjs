// Real native transfer source, filesystem and multipart HTTP; Kit network/crypto
// boundaries use Node equivalents. This is NOT Harmony RCP/ArkWeb runtime proof.
const fs=require('node:fs'),path=require('node:path'),os=require('node:os'),http=require('node:http'),crypto=require('node:crypto'),vm=require('node:vm'),assert=require('node:assert/strict')
const root=path.resolve(__dirname,'../..')
const ts=require(process.env.HARMONY_TEST_TYPESCRIPT || path.join(root,'mobile/.build/memoh-web/checkout/node_modules/typescript'))
const tmp=fs.mkdtempSync(path.join(os.tmpdir(),'native-voice-next-'))
function load(relative,imports,globals={}){
 const s=fs.readFileSync(path.join(root,relative),'utf8'),module={exports:{}}
 const js=ts.transpileModule(s,{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText
 vm.runInNewContext(js,{module,exports:module.exports,require:n=>{assert.ok(n in imports,n);return imports[n]},ArrayBuffer,Uint8Array,DataView,Map,Set,Promise,Error,Date,JSON,Math,Number,String,File,Blob,URL,AbortController,DOMException,setTimeout,clearTimeout,...globals})
 return module.exports
}
let maxRead=0,activeReads=0,maxActiveReads=0,readBytes=0,posts=0,sessions=0
const positions=new Map()
const io={WhenceType:{SEEK_SET:0},lseek:(fd,at)=>positions.set(fd,at),OpenMode:{READ_ONLY:fs.constants.O_RDONLY},openSync:p=>({fd:fs.openSync(p,'r')}),statSync:x=>fs.statSync(typeof x==='number'?'/proc/self/fd/'+x:x),
 closeSync:f=>fs.closeSync(typeof f==='number'?f:f.fd),unlinkSync:p=>fs.unlinkSync(p),
 read:async(fd,b,opt)=>{maxRead=Math.max(maxRead,b.byteLength);maxActiveReads=Math.max(maxActiveReads,++activeReads);try{assert.equal(opt.offset,undefined);const at=positions.get(fd)||0;return await new Promise((res,rej)=>fs.read(fd,new Uint8Array(b),0,opt.length??b.byteLength,at,(e,n)=>{positions.set(fd,at+(n||0));readBytes+=n??0;e?rej(e):res(n)}))}finally{activeReads--}},
 readSync:(fd,b,o)=>fs.readSync(fd,new Uint8Array(b),0,o.length??b.byteLength,o.offset),
 writeSync:(fd,b,o)=>fs.writeSync(fd,new Uint8Array(b),0,b.byteLength,o.offset),fsyncSync:fd=>fs.fsyncSync(fd),accessSync:p=>fs.existsSync(p)}
const kitCrypto={cryptoFramework:{createMd:()=>{const h=crypto.createHash('sha256');return{update:async({data})=>{h.update(data)},digest:async()=>({data:new Uint8Array(h.digest())})}}}}
let total=0,wantHash='',storedHash='',storedSize=0,oversize=false,delayResponse=false,releaseReply,mode='normal'
const server=http.createServer(async(req,res)=>{
 posts++;let pre=Buffer.alloc(0),reading=false,left=total,tail=Buffer.alloc(0);const h=crypto.createHash('sha256'),fd=fs.openSync(path.join(tmp,'received.wav'),'w')
 try{for await(const chunk of req){let data=chunk;if(!reading){pre=Buffer.concat([pre,data]);const at=pre.indexOf('\r\n\r\n');if(at<0){assert.ok(pre.length<4096);continue}assert.match(pre.subarray(0,at).toString(),/name="file"/);data=pre.subarray(at+4);pre=Buffer.alloc(0);reading=true}
  const n=Math.min(left,data.length);if(n){const bytes=data.subarray(0,n);fs.writeSync(fd,bytes);h.update(bytes);left-=n}if(n<data.length)tail=Buffer.concat([tail,data.subarray(n)]);assert.ok(tail.length<512)
 }fs.fsyncSync(fd);storedSize=fs.fstatSync(fd).size;storedHash=h.digest('hex');assert.equal(left,0);assert.equal(storedHash,wantHash);assert.match(tail.toString(),/^\r\n--voice-test--\r\n$/)
 if(delayResponse)await new Promise(r=>releaseReply=r)
 res.writeHead(201,{'content-type':'application/json'});res.end(oversize?'x'.repeat(65537):JSON.stringify({content_hash:storedHash,size_bytes:storedSize,mime:'audio/wav'}))
 }catch(e){res.destroy(e)}finally{fs.closeSync(fd)}
})
;(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const origin='http://127.0.0.1:'+server.address().port+'/api'
 class MultipartForm{constructor(fields){this.fields=fields}}
 class Request{constructor(url,method,headers,content){Object.assign(this,{url,method,headers,content})}}
 const rcp={MultipartForm,Request,createSession:()=>{sessions++;let active,stream,pendingReject;return{
  cancel(){pendingReject?.(Error('cancelled'));active?.destroy(Error('cancelled'));stream?.destroy()},close(){active?.destroy();stream?.destroy()},
  fetch(request){if(mode==='hold')return new Promise((_resolve,reject)=>pendingReject=reject);if(mode==='oversize')return Promise.resolve().then(()=>{request.destination(new ArrayBuffer(65537));throw Error('receipt limit missing')});assert.equal(request.configuration.transfer.autoRedirect,false);assert.deepEqual(Object.keys(request.content.fields),['file']);const part=request.content.fields.file;assert.equal(typeof part.contentOrPath,'string');assert.equal(request.headers.authorization,'Bearer synthetic-test-only')
   const header=Buffer.from('--voice-test\r\nContent-Disposition: form-data; name="file"; filename="voice.wav"\r\nContent-Type: audio/wav\r\n\r\n'),end=Buffer.from('\r\n--voice-test--\r\n')
   return new Promise((resolve,reject)=>{active=http.request(request.url,{method:request.method,headers:{...request.headers,'content-type':'multipart/form-data; boundary=voice-test','content-length':header.length+fs.statSync(part.contentOrPath).size+end.length}},response=>{
    response.on('data',chunk=>{try{request.destination(chunk.buffer.slice(chunk.byteOffset,chunk.byteOffset+chunk.length))}catch(e){active.destroy(e)}});response.on('error',reject);response.on('end',()=>resolve({statusCode:response.statusCode}))
   });active.on('error',reject);active.write(header);stream=fs.createReadStream(part.contentOrPath,{highWaterMark:65536});stream.on('error',reject);stream.pipe(active,{end:false});stream.on('end',()=>active.end(end))})
  }
 }}}
 const base='mobile/harmony/app/entry/src/main/ets/common/'
 const {NativeVoiceTransfer}=load(base+'NativeVoiceTransfer.ets',{'@kit.CoreFileKit':{fileIo:io},'@kit.CryptoArchitectureKit':kitCrypto,'@kit.ArkTS':{util:{TextDecoder:class{decodeToString(bytes){return new TextDecoder().decode(bytes)}}}},'@kit.RemoteCommunicationKit':{rcp},'./MemohClientConfig':{MEMOH_ORIGIN:origin}})
 const mib=Number(process.argv[2]||200);total=mib*1024*1024
 const file=path.join(tmp,'original.wav'),fd=fs.openSync(file,'w'),buffer=Buffer.alloc(256*1024,137),wav=Buffer.alloc(44)
 wav.write('RIFF');wav.writeUInt32LE(total-8,4);wav.write('WAVEfmt ',8);wav.writeUInt32LE(16,16);wav.writeUInt16LE(1,20);wav.writeUInt16LE(1,22);wav.writeUInt32LE(16000,24);wav.writeUInt32LE(32000,28);wav.writeUInt16LE(2,32);wav.writeUInt16LE(16,34);wav.write('data',36);wav.writeUInt32LE(total-44,40)
 const hash=crypto.createHash('sha256');fs.writeSync(fd,wav);hash.update(wav)
 for(let at=44;at<total;){const n=Math.min(buffer.length,total-at);fs.writeSync(fd,buffer,0,n);hash.update(buffer.subarray(0,n));at+=n}fs.fsyncSync(fd);fs.closeSync(fd);wantHash=hash.digest('hex')
 let authorized=true;const handle='a'.repeat(32),op='b'.repeat(32)
 const transfer=new NativeVoiceTransfer(h=>{assert.equal(h,handle);return file},()=>authorized)
 global.gc?.();const baseline=process.memoryUsage();let peak={...baseline},samples=0
 const measure=()=>{samples++;const m=process.memoryUsage();for(const k of Object.keys(m))peak[k]=Math.max(peak[k],m[k])}
 const timer=setInterval(measure,10)
 const start=Date.now(),info=await transfer.describe(handle,'bot-a',op);assert.equal(info.hash,wantHash);assert.equal(info.size,total)
 const reply=await transfer.upload(handle,'bot-a',op,origin+'/bots/bot-a/media','Bearer synthetic-test-only');assert.equal(reply.status,201);assert.equal(JSON.parse(reply.body).content_hash,wantHash);assert.equal(storedSize,total)
 measure();clearInterval(timer);const milliseconds=Date.now()-start, transferHashReadBytes=readBytes
 // The current frontend finalizer carries only metadata and makes zero read calls.
 const commands=[];const front=load('mobile/memoh/harmony-voice.ts',{}, {window:{begoniaNative:{request:async raw=>{const q=JSON.parse(raw);commands.push(q.command);assert.equal(q.command,'voice.stop');return JSON.stringify({ok:true,data:{handle,state:'complete',durationMs:(total-44)/32,peaks:[1],text:'',speechError:'',bytes:total,audible:true}})}}}})
 const recording=await front.finishRecording(handle);assert.equal(recording.file,undefined);assert.equal(recording.original.size,total);assert.ok(JSON.stringify(recording).length<1024);assert.deepEqual(commands,['voice.stop'])
 await assert.rejects(transfer.describe(handle,'bot-b','c'.repeat(32)),/VOICE_BOT_MISMATCH/)
 await assert.rejects(transfer.upload(handle,'bot-a','d'.repeat(32),origin+'/wrong','Bearer synthetic-test-only'),/VOICE_REQUEST_INVALID/)
 transfer.abort(handle,'e'.repeat(32));await assert.rejects(transfer.describe(handle,'bot-a','e'.repeat(32)),/VOICE_CANCELLED/)
 const other=new NativeVoiceTransfer(()=>file,()=>authorized);const pending=other.describe(handle,'bot-a','f'.repeat(32));other.close();await assert.rejects(pending,/VOICE_CANCELLED/)
 mode='hold';const blocked=transfer.upload(handle,'bot-a','3'.repeat(32),origin+'/bots/bot-a/media','Bearer synthetic-test-only');transfer.abort(handle,'3'.repeat(32));await assert.rejects(blocked,/cancelled/)
 mode='oversize';await assert.rejects(transfer.upload(handle,'bot-a','4'.repeat(32),origin+'/bots/bot-a/media','Bearer synthetic-test-only'),/VOICE_RECEIPT_TOO_LARGE/)
 authorized=false;await assert.rejects(transfer.upload(handle,'bot-a','9'.repeat(32),origin+'/bots/bot-a/media','Bearer synthetic-test-only'),/VOICE_CANCELLED/)
 assert.equal(posts,1)
 console.log(JSON.stringify({scope:'Actual ArkTS transfer and frontend source under Node Kit adapters; real disk WAV and local HTTP; not Harmony RCP/ArkWeb device runtime',mib,total,maxRead,maxActiveReads,transferHashReadBytes,readBytesIncludingCancellationProbe:readBytes,posts,storedSize,storedHash,wholeFileInFrontend:false,bridgeCommands:commands,bridgeDescriptorBytes:JSON.stringify(recording).length,baseline,peak,samples,milliseconds,crossBotRefused:true,wrongEndpointRefused:true,cancelBeforeReadRefused:true,cancelDuringHashRefused:true,authRevokedRefused:true,cancelDuringUploadRefused:true,oversizedReceiptRefused:true}))
 transfer.close();other.close()
})().catch(e=>{console.error(e);process.exitCode=1}).finally(()=>{server.closeAllConnections();server.close();fs.rmSync(tmp,{recursive:true,force:true})})
