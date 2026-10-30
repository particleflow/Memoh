// Execute the actual ArkTS implementation with API boundary adapters, real
// filesystem and HTTP. Child termination is real process death; not ArkWeb/OS.
const fs=require('node:fs'),path=require('node:path'),os=require('node:os'),http=require('node:http'),crypto=require('node:crypto'),vm=require('node:vm'),assert=require('node:assert/strict'),{spawn}=require('node:child_process')
const root=path.resolve(__dirname,'../..'),ts=require(process.env.HARMONY_TEST_TYPESCRIPT||path.join(root,'mobile/.build/memoh-web/checkout/node_modules/typescript'))
const networkPolicyTest=process.env.HARMONY_NETWORK_POLICY_TEST==='1'
const networkPolicy={version:1,lan_addresses:['192.0.2.10','2001:db8::10']}
const sha=b=>crypto.createHash('sha256').update(b).digest('hex')
const diagnostics=[]
if(process.argv[2]==='worker') worker().catch(e=>{console.log(JSON.stringify({error:e.message,...(process.env.HARMONY_UPLOAD_STAGING_FAILURE_ONLY==='1'?{diagnostics}:{})}));process.exitCode=2});else main().catch(e=>{console.error(e);process.exitCode=1})
async function worker(){
 const [dir,origin,action,handle='',token='alice',bot='bot-a',scope='session-a']=process.argv.slice(3),positions=new Map();let maxRead=0,maxWrite=0,maxBody=0,samples=0,networkHints=[],onPatch,onJournalSave,onWrite,onRead,sourceMutated=false
 const io={OpenMode:{READ_ONLY:fs.constants.O_RDONLY,WRITE_ONLY:fs.constants.O_WRONLY,READ_WRITE:fs.constants.O_RDWR,CREATE:fs.constants.O_CREAT,EXCL:fs.constants.O_EXCL,TRUNC:fs.constants.O_TRUNC,NOFOLLOW:fs.constants.O_NOFOLLOW},WhenceType:{SEEK_SET:0},
  accessSync:p=>fs.existsSync(p),mkdirSync:p=>fs.mkdirSync(p),openSync:(p,m)=>{if(action.endsWith('-source-open-fail')&&p===path.join(dir,'source.bin'))throw Object.assign(Error('synthetic denied SECRET_URI'),{code:13900012});return{fd:fs.openSync(p,m)}},closeSync:f=>{let fd=typeof f==='number'?f:f.fd;positions.delete(fd);fs.closeSync(fd)},
  statSync:p=>typeof p==='number'?fs.fstatSync(p):fs.statSync(p),lseek:(fd,at)=>{if(action.endsWith('-source-verify-fail')&&!sourceMutated){const source=fs.openSync(path.join(dir,'source.bin'),'r+');try{fs.writeSync(source,Buffer.from([0x4f]),0,1,0)}finally{fs.closeSync(source)}sourceMutated=true}positions.set(fd,at)},readTextSync:p=>fs.readFileSync(p,'utf8'),listFileSync:p=>fs.readdirSync(p),renameSync:(a,b)=>{if(action.endsWith('-rename-fail'))throw Object.assign(Error('synthetic journal rename failure'),{code:13900020});fs.renameSync(a,b);const hook=onJournalSave;onJournalSave=undefined;hook?.(b)},unlinkSync:p=>{if(action==='bridge-cleanup-source-verify-fail'&&p.endsWith('.original'))throw Object.assign(Error('synthetic cleanup SECRET_PATH'),{code:13900020});fs.unlinkSync(p)},fsyncSync:fd=>{if(action.endsWith('-dir-sync-fail')&&fs.fstatSync(fd).isDirectory())throw Object.assign(Error('synthetic directory sync failure'),{code:13900020});fs.fsyncSync(fd)},
  read:async(fd,b,o={})=>{const hook=onRead;onRead=undefined;hook?.();maxRead=Math.max(maxRead,b.byteLength);const at=o.offset??positions.get(fd)??0;let n=await new Promise((res,rej)=>fs.read(fd,new Uint8Array(b),0,o.length??b.byteLength,at,(e,n)=>e?rej(e):res(n)));positions.set(fd,at+n);return n},
  write:async(fd,b,o={})=>{const hook=onWrite;onWrite=undefined;hook?.();maxWrite=Math.max(maxWrite,b.byteLength);const at=o.offset??positions.get(fd)??0;let n=fs.writeSync(fd,new Uint8Array(b),0,o.length??b.byteLength,at);positions.set(fd,at+n);return n},
  writeSync:(fd,b)=>fs.writeSync(fd,new Uint8Array(b))}
 const kitCrypto={cryptoFramework:{createMd:()=>{const h=crypto.createHash('sha256');return{update:async({data})=>{h.update(data)},digest:async()=>({data:new Uint8Array(h.digest())})}},createRandom:()=>({generateRandomSync:len=>({data:new Uint8Array(crypto.randomBytes(len))})})}}
 class Request{constructor(url,method,headers,content){Object.assign(this,{url,method,headers,content})}}
 class MultipartForm{constructor(fields){this.fields=fields}}
 const rcp={Request,MultipartForm,createSession:()=>{let active,stream;return{cancel:()=>active?.destroy(Error('cancel')),close:()=>{active?.destroy();stream?.destroy()},fetch:r=>new Promise((resolve,reject)=>{
  assert.equal(r.configuration.transfer.autoRedirect,false)
  if(action.endsWith('-network-fail')&&r.url.endsWith('/limits'))return reject(Object.assign(Error('synthetic failed SECRET_ADDRESS'),{code:1007900007}))
  let content=r.content,headers={...r.headers},tail
  let wireURL=r.url;
  if(networkPolicyTest){const u=new URL(r.url),ip=r.configuration.dns?.dnsRules[0]?.ipAddresses[0];assert.equal(u.protocol,'https:');assert.equal(u.hostname,'nas.example');assert.equal(r.configuration.security.remoteValidation,'system');assert.equal(r.configuration.dns.dnsRules[0].host,u.hostname);assert.equal(r.configuration.dns.dnsRules[0].port,Number(u.port));networkHints.push([r.method,ip]);if(r.method==='GET'&&u.pathname==='/api/users/me'&&ip!=='2001:db8::20'){if(ip==='192.0.2.10'){const stale=Buffer.from('stale partial metadata');r.destination(stale.buffer.slice(stale.byteOffset,stale.byteOffset+stale.length))}return reject(Object.assign(Error('synthetic metadata path loss'),{code:ip==='192.0.2.10'?1007900056:1007900007}))}assert.equal(ip,'2001:db8::20');wireURL='http://127.0.0.1:'+u.port+u.pathname;headers.host=u.host}
  if(content instanceof MultipartForm){let p=content.fields.file;headers['content-type']='multipart/form-data; boundary=bounded';content=Buffer.from('--bounded\r\nContent-Disposition: form-data; name="file"; filename="file.bin"\r\nContent-Type: '+p.contentType+'\r\n\r\n');tail=Buffer.from('\r\n--bounded--\r\n');headers['content-length']=content.length+fs.statSync(p.contentOrPath).size+tail.length;stream=fs.createReadStream(p.contentOrPath,{highWaterMark:262144})}
  else if(content!==undefined){content=typeof content==='string'?Buffer.from(content):Buffer.from(content);headers['content-length']=content.length;maxBody=Math.max(maxBody,content.length)}
  if(r.method==='PATCH'&&onPatch){const once=onPatch;onPatch=undefined;once()}
  active=http.request(wireURL,{method:r.method,headers},res=>{res.on('data',b=>{try{r.destination(b.buffer.slice(b.byteOffset,b.byteOffset+b.length))}catch(e){active.destroy(e)}});res.on('error',reject);res.on('end',()=>resolve({statusCode:res.statusCode,headers:action==='run-array-header'&&typeof res.headers['retry-after']==='string'?{...res.headers,'retry-after':[res.headers['retry-after']]}:res.headers}))});active.on('error',reject)
  if(stream){active.write(content);stream.pipe(active,{end:false});stream.on('end',()=>active.end(tail));stream.on('error',reject)}else active.end(content)
 })}}}
 const modules={'@kit.CoreFileKit':{fileIo:io,fileUri:{FileUri:class{constructor(uri){this.name=path.basename(uri)}}}},'@kit.CryptoArchitectureKit':kitCrypto,'@kit.ArkTS':{url:{URL:{parseURL:value=>new URL(value)}},util:{TextEncoder:class{encodeInto(s){return new TextEncoder().encode(s)}},TextDecoder:class{decodeToString(b){return new TextDecoder().decode(b)}},Base64Helper:class{decodeSync(s){return new Uint8Array(Buffer.from(s,'base64'))}},generateRandomUUID:()=>crypto.randomUUID()}},'@kit.RemoteCommunicationKit':{rcp},'@kit.ArkData':{uniformTypeDescriptor:{getUniformDataTypeByFilenameExtension:()=>''}},'./MemohClientConfig':{MEMOH_ORIGIN:origin,MEMOH_NETWORK_POLICY:networkPolicyTest?networkPolicy:undefined},'./NativePhotoSelection':{NativePhotoSelection:class{async select(){return[path.join(dir,'source.bin')]}}}}
 function load(name){const module={exports:{}},code=fs.readFileSync(path.join(root,'mobile/harmony/app/entry/src/main/ets/common/'+name+'.ets'),'utf8');vm.runInNewContext(ts.transpileModule(code,{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText,{module,exports:module.exports,require:n=>{assert.ok(n in modules,n);return modules[n]},ArrayBuffer,Uint8Array,Map,Set,Promise,Error,Date,JSON,Math,Number,String,URL,setTimeout});return module.exports}
 modules['@kit.PerformanceAnalysisKit']={hilog:{info(){},error(){}}};const diagnosticModule=load('ClientDiagnostics');diagnosticModule.ClientDiagnostics.event=(area,component,phase,detail)=>diagnostics.push({area,component,phase,detail});modules['./ClientDiagnostics']=diagnosticModule;
 modules['@kit.NetworkKit']={connection:{getDefaultHttpProxy:async()=>{assert(networkPolicyTest);return{host:''}},getDefaultNet:async()=>{assert(networkPolicyTest);return{getAddressesByName:async host=>{assert.equal(host,'nas.example');return[{family:2,address:'2001:db8::20'}]}}}}};
 const serviceModule={exports:{}};vm.runInNewContext(ts.transpileModule(fs.readFileSync(path.join(root,'mobile/memoh/harmony-service-url.ts'),'utf8'),{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText,{exports:serviceModule.exports,require:()=>{throw Error('unexpected validator import')},URL,Error,JSON,Number,Array});modules['./ServiceUrl']=serviceModule.exports;
 modules['./NativeTransferNetwork']=load('NativeTransferNetwork');
 modules['./NativeMediaFile']=load('NativeMediaFile');const {NativeResumableUpload}=load('NativeResumableUpload'),native=new NativeResumableUpload({filesDir:dir},()=>true)
 const input={operation:'a'.repeat(32),botId:bot,scope,authorization:'Bearer '+token,handle}
 global.gc?.();const baseline=process.memoryUsage(),peak={...baseline},timer=setInterval(()=>{samples++;let m=process.memoryUsage();for(let k in peak)peak[k]=Math.max(peak[k],m[k])},10)
 try{let result
  if(action==='expired-scope-source-open-fail'){
   let failure;try{await native.select(input,path.join(dir,'source.bin'))}catch(error){failure=error}
   assert.equal(failure.uploadFailure.phase,'source-open');native.close();assert.equal(failure.uploadFailure,undefined);result={expiredFailureHidden:true}
  }
  else if(action.startsWith('bridge-')){
   modules['./NativeResumableUpload']={NativeResumableUpload};modules['./NativeVoice']={NativeVoice:class{close(){}prepare(){}}};modules['./NativeMediaDownload']={NativeMediaDownload:class{close(){}}}
   modules['./NativeTransferOwner']=load('NativeTransferOwner');
   const {NativeClient}=load('NativeClient'),frame='begonia-client://app/index.html',client=new NativeClient({registerJavaScriptProxy(){},getLastJavascriptProxyCallingFrameUrl:()=>frame,getUrl:()=>frame},()=>{},async()=>{},{filesDir:dir})
   client.register();assert.equal(JSON.parse(await client.request(JSON.stringify({version:1,command:'client.state',state:'ready'}))).ok,true)
   result=JSON.parse(await client.request(JSON.stringify({version:1,command:'upload.select',...input})))
  }
  else if(['stage','stage-dir-sync-fail','stage-rename-fail','stage-source-open-fail','stage-source-verify-fail','stage-network-fail'].includes(action))result=await native.select(input,path.join(dir,'source.bin'))
  else if(['copy-dir-sync-fail','copy-rename-fail'].includes(action)){
   const bytes=fs.readFileSync(path.join(dir,'source.bin'));await native.stageBegin(input,'synthetic.bin','application/octet-stream',bytes.length)
   for(let at=0;at<bytes.length;at+=65536)await native.stageAppend(input.operation,input.authorization,at,bytes.subarray(at,at+65536).toString('base64'))
   result=await native.stageFinish(input.operation,input.authorization)
  }
  else if(action==='stage-close'){onJournalSave=()=>native.close();result=await native.select(input,path.join(dir,'source.bin'))}
  else if(action.startsWith('copy-')){
   const source=path.join(dir,'source.bin'),size=fs.statSync(source).size
   const staged=await native.stageBegin(input,'paste-or-recording.bin','application/octet-stream',size)
   const snapshot=path.join(dir,'memoh-upload-journal-v1',staged.handle+'.original'),journal=snapshot.replace(/\.original$/,'.json')
   assert.ok(fs.existsSync(snapshot));assert.equal(fs.existsSync(journal),false)
   if(action==='copy-close-before-chunk'){native.close();assert.equal(fs.existsSync(snapshot),false);result={cleared:true}}
   else if(action==='copy-wrong-operation'||action==='copy-wrong-auth'){
    await assert.rejects(native.stageAppend(action==='copy-wrong-operation'?'b'.repeat(32):input.operation,action==='copy-wrong-auth'?'Bearer bob':input.authorization,0,'YQ=='),/UPLOAD_IDENTITY/)
    assert.equal(fs.existsSync(snapshot),false);result={isolated:true}
   }else if(action==='copy-bad-offset'||action==='copy-oversized'){
    await assert.rejects(native.stageAppend(input.operation,input.authorization,action==='copy-bad-offset'?1:0,Buffer.alloc(action==='copy-oversized'?65537:1).toString('base64')),/UPLOAD_REQUEST/)
    assert.equal(fs.existsSync(snapshot),false);result={bounded:true}
   }else{
    const sourceFd=fs.openSync(source,'r'),buffer=Buffer.alloc(65536);let offset=0
    try{while(offset<size){const n=fs.readSync(sourceFd,buffer,0,Math.min(buffer.length,size-offset),offset)
     if(action==='copy-close-during-write')onWrite=()=>native.close()
     const chunk=native.stageAppend(input.operation,input.authorization,offset,buffer.subarray(0,n).toString('base64'))
     if(action==='copy-close-during-write'){await assert.rejects(chunk,/UPLOAD_CANCELLED/);assert.equal(fs.existsSync(snapshot),false);break}
     offset+=n;assert.equal((await chunk).offset,offset)
    }}finally{fs.closeSync(sourceFd)}
    if(action==='copy-close-during-write')result={cleared:true}
    else{
     if(action==='copy-close-during-finish')onRead=()=>native.close()
     if(action==='copy-close-after-save')onJournalSave=()=>native.close()
     const finish=native.stageFinish(input.operation,input.authorization)
     if(action==='copy-close-during-finish'||action==='copy-close-after-save'){
      await assert.rejects(finish,/UPLOAD_CANCELLED/);assert.equal(fs.existsSync(journal),action==='copy-close-after-save');assert.equal(fs.existsSync(snapshot),action==='copy-close-after-save');result={handle:staged.handle,committed:fs.existsSync(journal)}
     }else{
      result=await finish;assert.equal(result.handle,staged.handle);assert.equal(result.scope,scope);assert.equal(result.size,size)
      const digest=crypto.createHash('sha256');for await(const b of fs.createReadStream(snapshot,{highWaterMark:262144}))digest.update(b);assert.equal(result.hash,digest.digest('hex'))
      await assert.rejects(native.stageAppend('b'.repeat(32),'Bearer bob',0,'YQ=='),/UPLOAD_IDENTITY/);native.close();assert.ok(fs.existsSync(journal));assert.ok(fs.existsSync(snapshot))
     }
    }
   }
  }
  else if(action==='create-before-send-kill'){onJournalSave=p=>{assert.equal(JSON.parse(fs.readFileSync(p)).state,'create-unknown');process.kill(process.pid,'SIGKILL')};result=await native.upload(input)}
  else if(action==='retry-pause'){const hook=p=>{if(JSON.parse(fs.readFileSync(p)).retryAt)native.close();else onJournalSave=hook};onJournalSave=hook;result=await native.upload(input)}
  else if(action==='list')result=await native.list(input)
  else if(action==='list-drafts')result=await native.list({...input,recoverDrafts:true})
  else if(action==='consume')result=await native.consume(input)
  else if(action==='discard')result=await native.discard(input)
  else if(action==='abort'){onPatch=()=>setTimeout(()=>native.abort(input.operation),20);result=await native.upload(input)}
  else if(action==='pause'){onPatch=()=>setTimeout(()=>native.pause(input.operation),20);result=await native.upload(input)}
  else if(action==='close'){const promise=native.upload(input);setTimeout(()=>native.close(),80);result=await promise}
  else result=await native.upload(input)
  console.log(JSON.stringify({result,metrics:{baseline,peak,samples,maxRead,maxWrite,maxBody,networkHints,pid:process.pid}}))
 }finally{clearInterval(timer);native.close()}
}
async function main(){
 const dir=fs.mkdtempSync(path.join(os.tmpdir(),'resumable-real-')),receipts=new Map(),events=[],mib=Number(process.argv[2]||200),size=mib*1024*1024
 const source=path.join(dir,'source.bin'),fd=fs.openSync(source,'w'),block=Buffer.alloc(262144,0xb7),h=crypto.createHash('sha256')
 for(let at=0;at<size;at+=block.length){let n=Math.min(block.length,size-at);fs.writeSync(fd,block,0,n);h.update(block.subarray(0,n))}fs.fsyncSync(fd);fs.closeSync(fd);const expected=h.digest('hex')
 let mode='normal',triggered=false,activeChild,created=0,patches=0,maxChunk=0,legacyPosts=0,firstOffset=null,quotaCalls=0,createPosts=0,completePosts=0
 const json=(res,status,value)=>{res.writeHead(status,{'content-type':'application/json'});res.end(JSON.stringify(value))}
 const server=http.createServer(async(req,res)=>{try{
  const u=new URL(req.url,'http://local'),token=req.headers.authorization
  if(u.pathname==='/test/native'){
   let bytes=[];for await(const b of req)bytes.push(b);const input=JSON.parse(Buffer.concat(bytes));assert.ok(['run','consume'].includes(input.action));
   const result=await child(input.action,input.handle);if(result.metrics)measurements.push({phase:'actual-chat-prepare',...result.metrics});return json(res,200,result)
  }
  if(token==='Bearer revoked')return json(res,401,{})
  if(u.pathname==='/api/users/me')return json(res,200,{id:token==='Bearer bob'?'bob':'alice'})
  if(token==='Bearer forbidden')return json(res,403,{})
  const prefix='/api/bots/bot-a/media'
  if(!u.pathname.startsWith(prefix))return json(res,404,{})
  if(u.pathname===prefix+'/limits')return mode==='limits404'?json(res,404,{}):json(res,200,{max_upload_bytes:209715200,resumable_enabled:!['old','oldlost'].includes(mode),chunk_max_bytes:8388608})
  if(req.method==='HEAD'){
   const ready=[...receipts.values()].find(r=>r.state==='ready'&&r.content_hash===u.pathname.slice(prefix.length+1));
   if(!ready||mode.startsWith('old')||mode==='original-missing'){res.writeHead(404);return res.end()}
   const hh=crypto.createHash('sha256');for await(const b of fs.createReadStream(path.join(dir,ready.id),{highWaterMark:262144}))hh.update(b);
   assert.equal(hh.digest('hex'),expected);res.writeHead(200,{'content-length':size,etag:'"'+expected+'"','content-type':ready.mime});return res.end()
  }
  if(u.pathname===prefix&&req.method==='POST'){
   legacyPosts++;let prefixBuffer=Buffer.alloc(0),remaining=size,started=false,hh=crypto.createHash('sha256')
   for await(let chunk of req){if(!started){prefixBuffer=Buffer.concat([prefixBuffer,chunk]);let p=prefixBuffer.indexOf('\r\n\r\n');if(p<0)continue;chunk=prefixBuffer.subarray(p+4);prefixBuffer=Buffer.alloc(0);started=true}let n=Math.min(remaining,chunk.length);hh.update(chunk.subarray(0,n));remaining-=n}assert.equal(remaining,0);assert.equal(hh.digest('hex'),expected)
   if(mode==='oldlost'){res.destroy();return}return json(res,201,{content_hash:expected,size_bytes:size,mime:'application/octet-stream'})
  }
  if(req.method==='POST'&&u.pathname===prefix+'/uploads'){
   createPosts++;if(mode==='create-lost-before'){req.resume();res.destroy();return}
   let b=[];for await(let c of req)b.push(c);let input=JSON.parse(Buffer.concat(b));assert.equal(input.size_bytes,size);assert.equal(input.sha256,expected)
   if(mode==='quota'){quotaCalls++;return json(res,429,{})}
   const id=crypto.randomUUID(),r={id,state:'uploading',offset:0,size_bytes:size,expires_at:new Date(Date.now()+86400000).toISOString(),mime:input.mime};receipts.set(id,r);fs.writeFileSync(path.join(dir,id),'');created++;events.push(['CREATE',id]);if(mode==='create-lost-after'){res.destroy();return}if(mode==='create-kill-after'){activeChild.kill('SIGKILL');res.destroy();return}return json(res,201,r)
  }
  let [id,complete]=u.pathname.slice((prefix+'/uploads/').length).split('/'),r=receipts.get(id)
  if(!r){events.push(['MISSING',id]);return json(res,404,{})}
  if(req.method==='GET'){events.push(['GET',r.offset,id]);return json(res,200,r)}
  if(req.method==='DELETE'){events.push(['DELETE',id]);if(r.state==='ready')return json(res,409,{});r.state='canceled';return json(res,200,r)}
  if(req.method==='PATCH'){
   patches++;if(firstOffset===null)firstOffset=Number(req.headers['upload-offset'])
   if(mode==='429'&&!triggered){triggered=true;req.resume();return json(res,429,{})}
   if(mode==='409'&&!triggered){triggered=true;req.resume();return json(res,409,{})}
   if(Number(req.headers['upload-offset'])!==r.offset){req.resume();return json(res,409,{})}
   let bytes=[];for await(let c of req)bytes.push(c);bytes=Buffer.concat(bytes);assert.equal(bytes.length,Number(req.headers['upload-length']));assert.equal(sha(bytes),req.headers['upload-sha256']);assert.equal(req.headers['content-type'],'application/octet-stream');assert.ok(bytes.length<=262144);maxChunk=Math.max(maxChunk,bytes.length)
   const out=fs.openSync(path.join(dir,id),'r+');fs.writeSync(out,bytes,0,bytes.length,r.offset);fs.fsyncSync(out);fs.closeSync(out);r.offset+=bytes.length;events.push(['PATCH',r.offset]);fs.writeFileSync(path.join(dir,id+'.receipt'),JSON.stringify(r))
   if(mode==='kill'&&r.offset>=1048576){activeChild.kill('SIGKILL');res.destroy();return}
   if(mode==='lost'&&!triggered){triggered=true;res.destroy();return}
   if(mode==='slow')return setTimeout(()=>json(res,200,r),120)
   return json(res,200,r)
  }
  if(req.method==='POST'&&complete==='complete'){
   completePosts++;if(mode==='complete-lost-before'){res.destroy();return}
   if(r.offset!==size)return json(res,409,{})
   const hh=crypto.createHash('sha256');for await(let c of fs.createReadStream(path.join(dir,id),{highWaterMark:262144}))hh.update(c);assert.equal(hh.digest('hex'),expected);r.state='ready';r.content_hash=expected;events.push(['COMPLETE',r.offset]);
   if(mode==='complete-lost'&&!triggered){triggered=true;res.destroy();return}return json(res,200,r)
  }
  json(res,400,{})
 }catch(e){console.error(e);res.destroy(e)}})
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const origin=(networkPolicyTest?'https://nas.example:':'http://127.0.0.1:')+server.address().port+'/api'
 async function child(action,handle='',token='alice',bot='bot-a',scope='session-a'){
  return await new Promise((resolve,reject)=>{let out='',err='';const p=spawn(process.execPath,['--expose-gc',__filename,'worker',dir,origin,action,handle,token,bot,scope],{stdio:['ignore','pipe','pipe']});activeChild=p;p.stdout.on('data',c=>out+=c);p.stderr.on('data',c=>err+=c);p.on('error',reject);p.on('close',(code,signal)=>{if(signal==='SIGKILL')return resolve({killed:true});if(!out.trim())return reject(Error(err||'empty worker '+code));try{resolve(JSON.parse(out.trim().split('\n').at(-1)))}catch(e){reject(Error(out+err))}})})
 }
 let measurements=[],tests=[]
 async function stage(){let s=await child('stage');assert.ok(s.result,s.error);assert.match(s.result[0].handle,/^[a-f0-9]{32}$/);measurements.push({phase:'stage',...s.metrics});return s.result[0].handle}
 async function discard(handle){const r=await child('discard',handle);assert.equal(r.error,undefined);return r}
 try{
  if(process.env.HARMONY_UPLOAD_FAILURE_ENVELOPE_ONLY==='1'){
   const reports=[]
   for(const [action,phase,sdkCode,code]of [['bridge-source-open-fail','source-open',13900012,'VOICE_UNAVAILABLE'],['bridge-source-verify-fail','source-verify',0,'MEDIA_SHA_MISMATCH'],['bridge-network-fail','limits-read',1007900007,'VOICE_UNAVAILABLE'],['bridge-cleanup-source-verify-fail','source-verify',0,'MEDIA_SHA_MISMATCH']]){
    const before=Date.now(),reply=(await child(action)).result;assert.ok(reply);assert.equal(reply.ok,false);assert.equal(reply.error,code);assert.equal(reply.failure.version,1);assert.equal(reply.failure.phase,phase);assert.equal(reply.failure.sdkCode,sdkCode);assert.ok(reply.failure.at>=before&&reply.failure.at<=Date.now())
    assert.deepEqual(Object.keys(reply.failure).sort(),['at','phase','sdkCode','version']);assert.ok(!JSON.stringify(reply).includes('SECRET'));assert.ok(!JSON.stringify(reply).includes('Bearer'));reports.push({phase,sdkCode,code})
    if(action.endsWith('-source-verify-fail')){const original=fs.openSync(source,'r+');try{fs.writeSync(original,block,0,1,0)}finally{fs.closeSync(original)}}
   }
   assert.equal((await child('expired-scope-source-open-fail')).result.expiredFailureHidden,true)
   assert.equal(createPosts,0);assert.equal(completePosts,0);assert.equal(patches,0);assert.equal(legacyPosts,0)
   console.log(JSON.stringify({actualNativeRouter:true,actualNativeUploader:true,realDisk:true,syntheticKitFailures:true,size,reports,expiredScopeFailureHidden:true,createPosts,completePosts,patches,newEndpoint:false,nativeDevice:false}));return
  }
  if(process.env.HARMONY_UPLOAD_STAGING_FAILURE_ONLY==='1'){
   const prior=await stage(),journalDir=path.join(dir,'memoh-upload-journal-v1'),priorJournal=fs.readFileSync(path.join(journalDir,prior+'.json')),priorSnapshot=fs.readFileSync(path.join(journalDir,prior+'.original'))
   for(const entry of ['stage','copy'])for(const failure of ['rename-fail','dir-sync-fail']){
    const before=new Set(fs.readdirSync(journalDir)),reply=await child(entry+'-'+failure)
    assert.equal(reply.error,'synthetic '+(failure==='rename-fail'?'journal rename':'directory sync')+' failure')
    assert.deepEqual(reply.diagnostics,[{area:'SYSTEM',component:'attachment-stage',phase:failure==='rename-fail'?'journal-publish':'journal-dir-sync',detail:'/code=13900020/reason=SDK_ERROR'}])
    const added=fs.readdirSync(journalDir).filter(name=>!before.has(name))
    if(failure==='rename-fail')assert.deepEqual(added,[],'unpublished snapshot must be reclaimed')
    else{
     const journalName=added.find(name=>name.endsWith('.json'));assert.ok(journalName,'published journal must survive directory sync failure')
     const handle=journalName.slice(0,-5),saved=fs.readFileSync(path.join(journalDir,journalName)),j=JSON.parse(saved),snapshot=path.join(journalDir,handle+'.original')
     assert.equal(j.hash,expected);assert.equal(j.size,size);assert.equal(j.state,'staged');assert.equal(j.uploadId,'');assert.equal(sha(fs.readFileSync(snapshot)),expected)
     // A different process reads the same committed snapshot, not the picker URI.
     fs.renameSync(source,source+'.grant-withdrawn')
     try{
      const restored=await child('list',handle);assert.equal(restored.result[0].handle,handle);assert.equal(restored.result[0].hash,expected)
      assert.deepEqual((await child('list',handle,'bob')).result,[]);assert.equal((await child('list',handle,'revoked')).error,'UPLOAD_HTTP_401')
      assert.deepEqual(fs.readFileSync(path.join(journalDir,journalName)),saved);assert.equal(sha(fs.readFileSync(snapshot)),expected)
     }finally{fs.renameSync(source+'.grant-withdrawn',source)}
     await discard(handle)
    }
    assert.deepEqual(fs.readFileSync(path.join(journalDir,prior+'.json')),priorJournal);assert.deepEqual(fs.readFileSync(path.join(journalDir,prior+'.original')),priorSnapshot)
    tests.push(entry+'-'+failure)
   }
   for(const [action,phase,code,reason]of [['stage-source-open-fail','source-open',13900012,'SDK_ERROR'],['stage-source-verify-fail','source-verify',0,'MEDIA_SHA_MISMATCH'],['stage-network-fail','limits-read',1007900007,'SDK_ERROR']]){
    const before=fs.readdirSync(journalDir),reply=await child(action)
    assert.ok(reply.error);assert.deepEqual(reply.diagnostics,[{area:'SYSTEM',component:'attachment-stage',phase,detail:'/code='+code+'/reason='+reason}]);assert.ok(!JSON.stringify(reply.diagnostics).includes('SECRET'))
    assert.deepEqual(fs.readdirSync(journalDir),before);assert.deepEqual(fs.readFileSync(path.join(journalDir,prior+'.json')),priorJournal);assert.deepEqual(fs.readFileSync(path.join(journalDir,prior+'.original')),priorSnapshot)
    if(action==='stage-source-verify-fail'){assert.equal(reply.error,'MEDIA_SHA_MISMATCH');const original=fs.openSync(source,'r+');try{fs.writeSync(original,block,0,1,0)}finally{fs.closeSync(original)}}
    tests.push(action)
   }
   assert.equal(createPosts,0);assert.equal(completePosts,0);assert.equal(patches,0);assert.equal(legacyPosts,0)
   await discard(prior)
   console.log(JSON.stringify({actualArkTS:true,realDisk:true,independentRestoreProcesses:true,size,sha256:expected,tests,priorQueuePreserved:true,withdrawnPickerSourceNotReopened:true,revokedAndWrongOwnerDenied:true,createPosts,completePosts,patches,nativeDevice:false}));return
  }
  if(process.env.HARMONY_UPLOAD_SCOPE_ONLY){
   const one=await stage(),two=await stage();const found=await child('list',one);assert.equal(found.result.length,1);assert.equal(found.result[0].handle,one)
   assert.deepEqual((await child('list',one,'bob')).result,[]);assert.deepEqual((await child('list',one,'alice','bot-b')).result,[]);assert.deepEqual((await child('list',one,'alice','bot-a','other-session')).result,[])
   await discard(one);await discard(two);console.log(JSON.stringify({exactHandleReauthorization:true,ownerBotSessionIsolation:true,realDisk:true,size,nativeDevice:false}));return
  }
  // Recovery matrix: create has no idempotency lookup. Unknown outcomes retain
  // the same immutable snapshot; only the original known receipt may be read.
  const journalPath=handle=>path.join(dir,'memoh-upload-journal-v1',handle+'.json')
  const journal=handle=>JSON.parse(fs.readFileSync(journalPath(handle)))
  if(process.env.HARMONY_UPLOAD_HEAD_ONLY){
   const handle=await stage();assert.ok((await child('run',handle)).result);const saved=journal(handle),posts=createPosts,completes=completePosts,eventCount=events.length
   mode='original-missing';assert.equal((await child('run',handle)).error,'UPLOAD_ORIGINAL_UNAVAILABLE');assert.deepEqual(journal(handle),saved)
   assert.equal(createPosts,posts);assert.equal(completePosts,completes);assert.equal(events.length,eventCount);assert.ok(fs.existsSync(journalPath(handle).replace(/\.json$/,'.original')))
   console.log(JSON.stringify({readyOriginalHEAD404NoStaleRef:true,noCreateOrReceiptFallback:true,localSnapshotRetained:true,realDisk:true,size,nativeDevice:false}));return
  }
  for(const scenario of ['create-before-send-kill','create-lost-before','create-lost-after','create-kill-after']){
   mode='normal';const handle=await stage(),posts=createPosts,accepted=created;mode=scenario
   const first=await child(scenario==='create-before-send-kill'?scenario:'run',handle)
   if(scenario.includes('kill'))assert.ok(first.killed);else assert.equal(first.error,'UPLOAD_CREATE_UNCERTAIN')
   const saved=journal(handle);assert.equal(saved.state,'create-unknown');assert.equal(saved.uploadId,'');assert.equal(saved.offset,0)
   assert.equal(createPosts,posts+(scenario==='create-before-send-kill'?0:1));assert.equal(created,accepted+(['create-lost-after','create-kill-after'].includes(scenario)?1:0))
   const stablePosts=createPosts;mode='normal';assert.equal((await child('run',handle)).error,'UPLOAD_CREATE_UNCERTAIN');assert.deepEqual(journal(handle),saved);assert.equal(createPosts,stablePosts)
   const legacyBefore=legacyPosts;mode='old';assert.equal((await child('run',handle)).error,'UPLOAD_CREATE_UNCERTAIN');assert.equal(legacyPosts,legacyBefore);mode='normal'
   assert.ok(fs.existsSync(journalPath(handle).replace(/\.json$/,'.original')));await discard(handle);tests.push(scenario+'-restart-never-reposts')
  }
  mode='normal';let missing=await stage();mode='kill';assert.ok((await child('run',missing)).killed)
  const original=journal(missing),originalPosts=createPosts;receipts.delete(original.uploadId);mode='normal'
  for(let retry=0;retry<2;retry++){assert.equal((await child('run',missing)).error,'UPLOAD_RECEIPT_UNAVAILABLE');assert.deepEqual(journal(missing),original);assert.equal(createPosts,originalPosts)}
  await discard(missing);tests.push('original-receipt-404-retains-id-offset-expiry-no-create')
  mode='normal';let pending=await stage();mode='complete-lost-before';const completes=completePosts,posts=createPosts
  assert.equal((await child('run',pending)).error,'UPLOAD_COMPLETE_UNCERTAIN');const pendingJournal=journal(pending)
  assert.equal(pendingJournal.completePending,true);assert.equal(pendingJournal.offset,size);assert.equal(completePosts,completes+1);assert.equal(createPosts,posts+1)
  mode='normal';assert.equal((await child('run',pending)).error,'UPLOAD_COMPLETE_UNCERTAIN');assert.equal(completePosts,completes+1);assert.equal(createPosts,posts+1)
  const remote=receipts.get(pendingJournal.uploadId);remote.state='ready';remote.content_hash=expected
  const verified=await child('run',pending);assert.equal(verified.result.content_hash,expected);assert.equal(completePosts,completes+1);assert.equal(journal(pending).completePending,false)
  const deletes=events.filter(x=>x[0]==='DELETE').length;await child('consume',pending);assert.equal(events.filter(x=>x[0]==='DELETE').length,deletes);tests.push('complete-unknown-get-only-until-ready-consume-local-only')
  mode='normal';const missingOriginal=await stage();assert.ok((await child('run',missingOriginal)).result)
  const readyJournal=journal(missingOriginal),createBeforeMissing=createPosts,completeBeforeMissing=completePosts,eventsBeforeMissing=events.length
  mode='original-missing';assert.equal((await child('run',missingOriginal)).error,'UPLOAD_ORIGINAL_UNAVAILABLE');assert.deepEqual(journal(missingOriginal),readyJournal)
  assert.equal(createPosts,createBeforeMissing);assert.equal(completePosts,completeBeforeMissing);assert.equal(events.length,eventsBeforeMissing)
  mode='normal';await discard(missingOriginal);tests.push('ready-original-HEAD404-no-stale-reference-or-recreate')
  for(const scenario of ['lost','complete-lost']){
   mode='normal';const handle=await stage(),posts=createPosts,eventStart=events.length;mode=scenario;triggered=false
   const result=await child('run',handle);assert.equal(result.result.content_hash,expected);const id=journal(handle).uploadId
   assert.equal(createPosts,posts+1);assert.ok(events.slice(eventStart).some(e=>e[0]==='GET'&&e[2]===id));await discard(handle);tests.push(scenario+'-original-id-GET-reconciliation')
  }
  mode='normal';const quotaHandle=await stage();mode='quota';const quotaBefore=quotaCalls
  assert.equal((await child('run',quotaHandle)).error,'UPLOAD_HTTP_429');assert.equal(quotaCalls,quotaBefore+1)
  assert.equal((await child('run',quotaHandle)).error,'UPLOAD_CREATE_UNCERTAIN');assert.equal(quotaCalls,quotaBefore+1);mode='normal';await discard(quotaHandle);tests.push('create-429-no-automatic-POST-retry')
  for(const scenario of ['copy-normal','copy-close-before-chunk','copy-wrong-operation','copy-wrong-auth','copy-bad-offset','copy-oversized','copy-close-during-write','copy-close-during-finish','copy-close-after-save']){
   const posts=createPosts;mode='normal';const result=await child(scenario);assert.ok(result.result,result.error);assert.equal(createPosts,posts)
   assert.ok(result.metrics.maxWrite<=65536);assert.ok(result.metrics.maxRead<=262144)
   if(scenario==='copy-normal'){
    const local=result.result;assert.equal(local.hash,expected);const ready=await child('run',local.handle);assert.equal(ready.result.content_hash,expected);await discard(local.handle)
   }else if(result.result.committed)await discard(result.result.handle)
   tests.push(scenario);measurements.push({phase:scenario,...result.metrics})
  }
  if(process.env.HARMONY_UPLOAD_MATRIX_ONLY){console.log(JSON.stringify({scope:'actual ArkTS via Node Kit adapters, real disk/HTTP and process death; not Harmony native runtime',mib,size,maxChunk,createPosts,completePosts,tests,measurements}));return}
  let handle=await stage();mode='kill';const killed=await child('run',handle);assert.ok(killed.killed);let j=JSON.parse(fs.readFileSync(path.join(dir,'memoh-upload-journal-v1',handle+'.json')));assert.ok(j.offset<size);const durable=receipts.get(j.uploadId).offset
  mode='normal';firstOffset=null;let restored=await child('list');assert.equal(restored.result[0].handle,handle);const originalUpload=j.uploadId,createdBefore=created;let ready=await child('run',handle);assert.equal(ready.result.content_hash,expected);assert.equal(firstOffset,durable);assert.equal(created,createdBefore);assert.equal(JSON.parse(fs.readFileSync(path.join(dir,'memoh-upload-journal-v1',handle+'.json'))).uploadId,originalUpload);measurements.push({phase:'resume-after-SIGKILL',...ready.metrics});tests.push('process-death-status-offset-ready-same-upload-id')
  let before=patches;ready=await child('run',handle);assert.equal(ready.result.content_hash,expected);assert.equal(patches,before);tests.push('repeated-ready-no-PATCH')
  assert.deepEqual((await child('list','','bob')).result,[]);assert.deepEqual((await child('list','','alice','bot-a','session-b')).result,[])
  assert.equal((await child('run',handle,'bob')).error,'UPLOAD_IDENTITY');assert.deepEqual((await child('list','','alice','bot-b')).result,[]);assert.equal((await child('run',handle,'alice','bot-b')).error,'UPLOAD_IDENTITY');assert.equal((await child('run',handle,'alice','bot-a','session-b')).error,'UPLOAD_IDENTITY');assert.equal((await child('run',handle,'revoked')).error,'UPLOAD_HTTP_401');assert.equal((await child('run',handle,'forbidden')).error,'UPLOAD_HTTP_403');tests.push('account-bot-session-401-403-isolation');await discard(handle)
  // Every case uses the requested real on-disk size; memory samples are
  // retained separately for each independent worker process.
  for(const scenario of ['lost','409','429','complete-lost']){
   mode=scenario;triggered=false;handle=await stage();ready=await child('run',handle);assert.ok(ready.result,ready.error);assert.equal(ready.result.content_hash,expected);tests.push(scenario);await discard(handle)
  }
  mode='normal';handle=await stage();mode='kill';await child('run',handle);j=JSON.parse(fs.readFileSync(path.join(dir,'memoh-upload-journal-v1',handle+'.json')));receipts.delete(j.uploadId);mode='normal';let c=created;ready=await child('run',handle);assert.equal(ready.error,'UPLOAD_RECEIPT_UNAVAILABLE');assert.equal(created,c);assert.deepEqual(JSON.parse(fs.readFileSync(path.join(dir,'memoh-upload-journal-v1',handle+'.json'))),j);tests.push('expired-receipt-retained-no-recreate');await discard(handle)
  handle=await stage();const local=path.join(dir,'memoh-upload-journal-v1',handle+'.original'),f=fs.openSync(local,'r+');fs.writeSync(f,Buffer.from([1]),0,1,8);fs.closeSync(f);c=created;ready=await child('run',handle);assert.equal(ready.error,'MEDIA_SHA_MISMATCH');assert.equal(created,c);tests.push('same-size-source-mutation');await discard(handle)
  for(const scenario of ['old','limits404']){mode=scenario;handle=await stage();ready=await child('run',handle);assert.ok(ready.result,ready.error);tests.push(scenario+'-bounded-multipart');await discard(handle)}
  mode='oldlost';handle=await stage();await child('run',handle);before=legacyPosts;ready=await child('run',handle);assert.equal(ready.error,'UPLOAD_LEGACY_UNCERTAIN');assert.equal(legacyPosts,before);tests.push('old-lost-no-blind-repost');await discard(handle)
  mode='normal';handle=await stage();mode='kill';await child('run',handle);mode='normal';
  const deletesBefore=events.filter(x=>x[0]==='DELETE').length
  await new Promise((resolve,reject)=>{
   const e=spawn(process.execPath,[path.join(root,'tests/stage2/memoh_upload_preparation_checks.mjs')],{cwd:root,env:{...process.env,HARMONY_RESUMABLE_E2E:JSON.stringify({origin,handle,hash:expected,size})},stdio:['ignore','pipe','pipe']});let out='',err='';e.stdout.on('data',b=>out+=b);e.stderr.on('data',b=>err+=b);e.on('close',code=>{if(code)return reject(Error(err+out));const report=JSON.parse(out.trim());assert.ok(report.durableNativeDiskHTTPToChat);resolve()})
  });tests.push('actual-chat-store-resume-verified-head-single-reference');
  assert.equal(events.filter(x=>x[0]==='DELETE').length,deletesBefore);assert.equal(fs.existsSync(path.join(dir,'memoh-upload-journal-v1',handle+'.original')),false);tests.push('confirmed-chat-consume-local-only')
  mode='normal';handle=await stage();mode='slow';ready=await child('pause',handle);assert.ok(ready.error);j=JSON.parse(fs.readFileSync(path.join(dir,'memoh-upload-journal-v1',handle+'.json')));assert.notEqual(j.state,'canceled');assert.ok(fs.existsSync(path.join(dir,'memoh-upload-journal-v1',handle+'.original')));mode='normal';ready=await child('run',handle);assert.equal(ready.result.content_hash,expected);tests.push('lifecycle-pause-retains-original-and-resumes');await discard(handle)
  const priorNames=new Set(fs.readdirSync(path.join(dir,'memoh-upload-journal-v1')));ready=await child('stage-close');assert.equal(ready.error,'UPLOAD_CANCELLED');const saved=fs.readdirSync(path.join(dir,'memoh-upload-journal-v1')).find(n=>n.endsWith('.json')&&!priorNames.has(n));assert.ok(saved);await discard(saved.slice(0,-5));tests.push('close-after-atomic-stage-keeps-committed-snapshot')
  const backlog=[];for(let i=0;i<5;i++)backlog.push(await stage());assert.ok((await child('list')).result.length>=5);for(const id of backlog)await discard(id);tests.push('no-four-item-global-backlog-cap')
  ready=await child('stage','','alice','bot-a','draft:old');const draft=ready.result[0];restored=await child('list-drafts','','alice','bot-a','draft:new');assert.equal(restored.result.find(x=>x.handle===draft.handle).scope,'draft:old');assert.deepEqual((await child('list-drafts','','bob','bot-a','draft:new')).result,[]);assert.deepEqual((await child('list-drafts','','alice','bot-b','draft:new')).result,[]);await child('discard',draft.handle,'alice','bot-a','draft:old');tests.push('explicit-old-draft-scope-preserved-owner-bot-isolated')
  mode='normal';handle=await stage();mode='quota';const quotaAt=quotaCalls;ready=await child('run',handle);assert.equal(ready.error,'UPLOAD_HTTP_429');assert.equal(quotaCalls,quotaAt+1);assert.equal((await child('run',handle)).error,'UPLOAD_CREATE_UNCERTAIN');assert.equal(quotaCalls,quotaAt+1);tests.push('quota-create-once-no-repost');mode='normal';await discard(handle);
  mode='normal';handle=await stage();mode='slow';ready=await child('abort',handle);assert.ok(ready.error);mode='normal';ready=await child('run',handle);assert.equal(ready.error,'UPLOAD_IDENTITY');assert.equal(fs.existsSync(path.join(dir,'memoh-upload-journal-v1',handle+'.original')),false);tests.push('cancel-during-PATCH-tombstone-cleanup-no-late-ready')
  const journals=fs.readdirSync(path.join(dir,'memoh-upload-journal-v1')).filter(n=>n.endsWith('.json')).map(n=>fs.readFileSync(path.join(dir,'memoh-upload-journal-v1',n),'utf8'));assert.ok(journals.every(x=>!x.includes('Bearer')&&!x.includes('authorization')&&!x.includes('token')))
  console.log(JSON.stringify({scope:'actual ArkTS via Node Kit adapters, independent processes, real disk and controlled HTTP; not Harmony native runtime',mib,size,maxChunk,created,patches,legacyPosts,tests,measurements,credentialFreeJournal:true}))
 }finally{server.closeAllConnections();server.close();fs.rmSync(dir,{recursive:true,force:true})}
}
