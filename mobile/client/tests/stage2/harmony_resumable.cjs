// Execute the actual ArkTS implementation with API boundary adapters, real
// filesystem and HTTP. Child termination is real process death; not ArkWeb/OS.
const fs=require('node:fs'),path=require('node:path'),os=require('node:os'),http=require('node:http'),crypto=require('node:crypto'),vm=require('node:vm'),assert=require('node:assert/strict'),{spawn}=require('node:child_process')
const root=path.resolve(__dirname,'../..'),ts=require(path.join(root,'mobile/.build/memoh-web/checkout/node_modules/typescript'))
const sha=b=>crypto.createHash('sha256').update(b).digest('hex')
if(process.argv[2]==='worker') worker().catch(e=>{console.log(JSON.stringify({error:e.message}));process.exitCode=2});else main().catch(e=>{console.error(e);process.exitCode=1})
async function worker(){
 const [dir,origin,action,handle='',token='alice',bot='bot-a',scope='session-a']=process.argv.slice(3),positions=new Map();let maxRead=0,maxWrite=0,maxBody=0,samples=0,onPatch
 const io={OpenMode:{READ_ONLY:fs.constants.O_RDONLY,WRITE_ONLY:fs.constants.O_WRONLY,READ_WRITE:fs.constants.O_RDWR,CREATE:fs.constants.O_CREAT,EXCL:fs.constants.O_EXCL,TRUNC:fs.constants.O_TRUNC,NOFOLLOW:fs.constants.O_NOFOLLOW},WhenceType:{SEEK_SET:0},
  accessSync:p=>fs.existsSync(p),mkdirSync:p=>fs.mkdirSync(p),openSync:(p,m)=>({fd:fs.openSync(p,m)}),closeSync:f=>{let fd=typeof f==='number'?f:f.fd;positions.delete(fd);fs.closeSync(fd)},
  statSync:p=>typeof p==='number'?fs.fstatSync(p):fs.statSync(p),lseek:(fd,at)=>positions.set(fd,at),readTextSync:p=>fs.readFileSync(p,'utf8'),listFileSync:p=>fs.readdirSync(p),renameSync:(a,b)=>fs.renameSync(a,b),unlinkSync:p=>fs.unlinkSync(p),fsyncSync:fd=>fs.fsyncSync(fd),
  read:async(fd,b,o)=>{maxRead=Math.max(maxRead,b.byteLength);let n=await new Promise((res,rej)=>fs.read(fd,new Uint8Array(b),0,o.length,positions.get(fd)||0,(e,n)=>e?rej(e):res(n)));positions.set(fd,(positions.get(fd)||0)+n);return n},
  write:async(fd,b,o)=>{maxWrite=Math.max(maxWrite,b.byteLength);let n=fs.writeSync(fd,new Uint8Array(b),0,o.length,positions.get(fd)||0);positions.set(fd,(positions.get(fd)||0)+n);return n},
  writeSync:(fd,b)=>fs.writeSync(fd,new Uint8Array(b))}
 const kitCrypto={cryptoFramework:{createMd:()=>{const h=crypto.createHash('sha256');return{update:async({data})=>{h.update(data)},digest:async()=>({data:new Uint8Array(h.digest())})}}}}
 class Request{constructor(url,method,headers,content){Object.assign(this,{url,method,headers,content})}}
 class MultipartForm{constructor(fields){this.fields=fields}}
 const rcp={Request,MultipartForm,createSession:()=>{let active,stream;return{cancel:()=>active?.destroy(Error('cancel')),close:()=>{active?.destroy();stream?.destroy()},fetch:r=>new Promise((resolve,reject)=>{
  assert.equal(r.configuration.transfer.autoRedirect,false)
  let content=r.content,headers={...r.headers},tail
  if(content instanceof MultipartForm){let p=content.fields.file;headers['content-type']='multipart/form-data; boundary=bounded';content=Buffer.from('--bounded\r\nContent-Disposition: form-data; name="file"; filename="file.bin"\r\nContent-Type: '+p.contentType+'\r\n\r\n');tail=Buffer.from('\r\n--bounded--\r\n');headers['content-length']=content.length+fs.statSync(p.contentOrPath).size+tail.length;stream=fs.createReadStream(p.contentOrPath,{highWaterMark:262144})}
  else if(content!==undefined){content=typeof content==='string'?Buffer.from(content):Buffer.from(content);headers['content-length']=content.length;maxBody=Math.max(maxBody,content.length)}
  if(r.method==='PATCH'&&onPatch){const once=onPatch;onPatch=undefined;once()}
  active=http.request(r.url,{method:r.method,headers},res=>{res.on('data',b=>{try{r.destination(b.buffer.slice(b.byteOffset,b.byteOffset+b.length))}catch(e){active.destroy(e)}});res.on('error',reject);res.on('end',()=>resolve({statusCode:res.statusCode,headers:res.headers}))});active.on('error',reject)
  if(stream){active.write(content);stream.pipe(active,{end:false});stream.on('end',()=>active.end(tail));stream.on('error',reject)}else active.end(content)
 })}}}
 const modules={'@kit.CoreFileKit':{fileIo:io,fileUri:{FileUri:class{constructor(uri){this.name=path.basename(uri)}}}},'@kit.CryptoArchitectureKit':kitCrypto,'@kit.ArkTS':{util:{TextEncoder:class{encodeInto(s){return new TextEncoder().encode(s)}},TextDecoder:class{decodeToString(b){return new TextDecoder().decode(b)}},generateRandomUUID:()=>crypto.randomUUID().replaceAll('-','')}},'@kit.RemoteCommunicationKit':{rcp},'@kit.ArkData':{uniformTypeDescriptor:{}},'./MemohClientConfig':{MEMOH_ORIGIN:origin},'./NativePhotoSelection':{NativePhotoSelection:class{}}}
 function load(name){const module={exports:{}},code=fs.readFileSync(path.join(root,'mobile/harmony/app/entry/src/main/ets/common/'+name+'.ets'),'utf8');vm.runInNewContext(ts.transpileModule(code,{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText,{module,exports:module.exports,require:n=>{assert.ok(n in modules,n);return modules[n]},ArrayBuffer,Uint8Array,Map,Set,Promise,Error,Date,JSON,Math,Number,String,setTimeout});return module.exports}
 modules['./NativeMediaFile']=load('NativeMediaFile');const {NativeResumableUpload}=load('NativeResumableUpload'),native=new NativeResumableUpload({filesDir:dir},()=>true)
 const input={operation:'a'.repeat(32),botId:bot,scope,authorization:'Bearer '+token,handle}
 global.gc?.();const baseline=process.memoryUsage(),peak={...baseline},timer=setInterval(()=>{samples++;let m=process.memoryUsage();for(let k in peak)peak[k]=Math.max(peak[k],m[k])},10)
 try{let result
  if(action==='stage')result=await native.select(input,path.join(dir,'source.bin'))
  else if(action==='list')result=await native.list(input)
  else if(action==='discard')result=await native.discard(input)
  else if(action==='abort'){onPatch=()=>setTimeout(()=>native.abort(input.operation),20);result=await native.upload(input)}
  else if(action==='close'){const promise=native.upload(input);setTimeout(()=>native.close(),80);result=await promise}
  else result=await native.upload(input)
  console.log(JSON.stringify({result,metrics:{baseline,peak,samples,maxRead,maxWrite,maxBody,pid:process.pid}}))
 }finally{clearInterval(timer);native.close()}
}
async function main(){
 const dir=fs.mkdtempSync(path.join(os.tmpdir(),'resumable-real-')),receipts=new Map(),events=[],mib=Number(process.argv[2]||200),size=mib*1024*1024
 const source=path.join(dir,'source.bin'),fd=fs.openSync(source,'w'),block=Buffer.alloc(262144,0xb7),h=crypto.createHash('sha256')
 for(let at=0;at<size;at+=block.length){let n=Math.min(block.length,size-at);fs.writeSync(fd,block,0,n);h.update(block.subarray(0,n))}fs.fsyncSync(fd);fs.closeSync(fd);const expected=h.digest('hex')
 let mode='normal',triggered=false,activeChild,created=0,patches=0,maxChunk=0,legacyPosts=0,firstOffset=null,quotaCalls=0
 const json=(res,status,value)=>{res.writeHead(status,{'content-type':'application/json'});res.end(JSON.stringify(value))}
 const server=http.createServer(async(req,res)=>{try{
  const u=new URL(req.url,'http://local'),token=req.headers.authorization
  if(u.pathname==='/test/native'){
   let bytes=[];for await(const b of req)bytes.push(b);const input=JSON.parse(Buffer.concat(bytes));assert.ok(['run','discard'].includes(input.action));
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
   if(!ready||mode.startsWith('old')){res.writeHead(404);return res.end()}
   const hh=crypto.createHash('sha256');for await(const b of fs.createReadStream(path.join(dir,ready.id),{highWaterMark:262144}))hh.update(b);
   assert.equal(hh.digest('hex'),expected);res.writeHead(200,{'content-length':size,etag:'"'+expected+'"','content-type':ready.mime});return res.end()
  }
  if(u.pathname===prefix&&req.method==='POST'){
   legacyPosts++;let prefixBuffer=Buffer.alloc(0),remaining=size,started=false,hh=crypto.createHash('sha256')
   for await(let chunk of req){if(!started){prefixBuffer=Buffer.concat([prefixBuffer,chunk]);let p=prefixBuffer.indexOf('\r\n\r\n');if(p<0)continue;chunk=prefixBuffer.subarray(p+4);prefixBuffer=Buffer.alloc(0);started=true}let n=Math.min(remaining,chunk.length);hh.update(chunk.subarray(0,n));remaining-=n}assert.equal(remaining,0);assert.equal(hh.digest('hex'),expected)
   if(mode==='oldlost'){res.destroy();return}return json(res,201,{content_hash:expected,size_bytes:size,mime:'application/octet-stream'})
  }
  if(req.method==='POST'&&u.pathname===prefix+'/uploads'){
   let b=[];for await(let c of req)b.push(c);let input=JSON.parse(Buffer.concat(b));assert.equal(input.size_bytes,size);assert.equal(input.sha256,expected)
   if(mode==='quota'){quotaCalls++;return json(res,429,{})}
   const id=crypto.randomUUID(),r={id,state:'uploading',offset:0,size_bytes:size,expires_at:new Date(Date.now()+86400000).toISOString(),mime:input.mime};receipts.set(id,r);fs.writeFileSync(path.join(dir,id),'');created++;events.push(['CREATE',id]);return json(res,201,r)
  }
  let [id,complete]=u.pathname.slice((prefix+'/uploads/').length).split('/'),r=receipts.get(id)
  if(!r)return json(res,404,{})
  if(req.method==='GET'){events.push(['GET',r.offset]);return json(res,200,r)}
  if(req.method==='DELETE'){r.state='canceled';return json(res,200,r)}
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
   if(r.offset!==size)return json(res,409,{})
   const hh=crypto.createHash('sha256');for await(let c of fs.createReadStream(path.join(dir,id),{highWaterMark:262144}))hh.update(c);assert.equal(hh.digest('hex'),expected);r.state='ready';r.content_hash=expected;events.push(['COMPLETE',r.offset]);
   if(mode==='complete-lost'&&!triggered){triggered=true;res.destroy();return}return json(res,200,r)
  }
  json(res,400,{})
 }catch(e){console.error(e);res.destroy(e)}})
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const origin='http://127.0.0.1:'+server.address().port+'/api'
 async function child(action,handle='',token='alice',bot='bot-a',scope='session-a'){
  return await new Promise((resolve,reject)=>{let out='',err='';const p=spawn(process.execPath,['--expose-gc',__filename,'worker',dir,origin,action,handle,token,bot,scope],{stdio:['ignore','pipe','pipe']});activeChild=p;p.stdout.on('data',c=>out+=c);p.stderr.on('data',c=>err+=c);p.on('error',reject);p.on('close',(code,signal)=>{if(signal==='SIGKILL')return resolve({killed:true});if(!out.trim())return reject(Error(err||'empty worker '+code));try{resolve(JSON.parse(out.trim().split('\n').at(-1)))}catch(e){reject(Error(out+err))}})})
 }
 let measurements=[],tests=[]
 async function stage(){let s=await child('stage');assert.ok(s.result,s.error);measurements.push({phase:'stage',...s.metrics});return s.result[0].handle}
 async function discard(handle){const r=await child('discard',handle);assert.equal(r.error,undefined);return r}
 try{
  let handle=await stage();mode='kill';const killed=await child('run',handle);assert.ok(killed.killed);let j=JSON.parse(fs.readFileSync(path.join(dir,'memoh-upload-journal-v1',handle+'.json')));assert.ok(j.offset<size);const durable=receipts.get(j.uploadId).offset
  mode='normal';firstOffset=null;let restored=await child('list');assert.equal(restored.result[0].handle,handle);let ready=await child('run',handle);assert.equal(ready.result.content_hash,expected);assert.equal(firstOffset,durable);measurements.push({phase:'resume-after-SIGKILL',...ready.metrics});tests.push('process-death-status-offset-ready')
  let before=patches;ready=await child('run',handle);assert.equal(ready.result.content_hash,expected);assert.equal(patches,before);tests.push('repeated-ready-no-PATCH')
  assert.deepEqual((await child('list','','bob')).result,[]);assert.deepEqual((await child('list','','alice','bot-a','session-b')).result,[])
  assert.equal((await child('run',handle,'bob')).error,'UPLOAD_IDENTITY');assert.deepEqual((await child('list','','alice','bot-b')).result,[]);assert.equal((await child('run',handle,'alice','bot-b')).error,'UPLOAD_IDENTITY');assert.equal((await child('run',handle,'alice','bot-a','session-b')).error,'UPLOAD_IDENTITY');assert.equal((await child('run',handle,'revoked')).error,'UPLOAD_HTTP_401');assert.equal((await child('run',handle,'forbidden')).error,'UPLOAD_HTTP_403');tests.push('account-bot-session-401-403-isolation');await discard(handle)
  // Every case uses the requested real on-disk size; memory samples are
  // retained separately for each independent worker process.
  for(const scenario of ['lost','409','429','complete-lost']){
   mode=scenario;triggered=false;handle=await stage();ready=await child('run',handle);assert.ok(ready.result,ready.error);assert.equal(ready.result.content_hash,expected);tests.push(scenario);await discard(handle)
  }
  mode='normal';handle=await stage();mode='kill';await child('run',handle);j=JSON.parse(fs.readFileSync(path.join(dir,'memoh-upload-journal-v1',handle+'.json')));receipts.delete(j.uploadId);mode='normal';let c=created;ready=await child('run',handle);assert.ok(ready.result,ready.error);assert.equal(created,c+1);tests.push('expired-recreate');await discard(handle)
  handle=await stage();const local=path.join(dir,'memoh-upload-journal-v1',handle+'.original'),f=fs.openSync(local,'r+');fs.writeSync(f,Buffer.from([1]),0,1,8);fs.closeSync(f);c=created;ready=await child('run',handle);assert.equal(ready.error,'MEDIA_SHA_MISMATCH');assert.equal(created,c);tests.push('same-size-source-mutation');await discard(handle)
  for(const scenario of ['old','limits404']){mode=scenario;handle=await stage();ready=await child('run',handle);assert.ok(ready.result,ready.error);tests.push(scenario+'-bounded-multipart');await discard(handle)}
  mode='oldlost';handle=await stage();await child('run',handle);before=legacyPosts;ready=await child('run',handle);assert.equal(ready.error,'UPLOAD_LEGACY_UNCERTAIN');assert.equal(legacyPosts,before);tests.push('old-lost-no-blind-repost');await discard(handle)
  mode='normal';handle=await stage();mode='kill';await child('run',handle);mode='normal';
  await new Promise((resolve,reject)=>{
   const e=spawn(process.execPath,[path.join(root,'tests/stage2/memoh_upload_preparation_checks.mjs')],{cwd:root,env:{...process.env,HARMONY_TEST_CONTEXT:'mobile/.build/memoh-web/official-context.tar',HARMONY_RESUMABLE_E2E:JSON.stringify({origin,handle,hash:expected,size})},stdio:['ignore','pipe','pipe']});let out='',err='';e.stdout.on('data',b=>out+=b);e.stderr.on('data',b=>err+=b);e.on('close',code=>{if(code)return reject(Error(err));const report=JSON.parse(out.trim());assert.ok(report.durableNativeDiskHTTPToChat);resolve()})
  });tests.push('actual-chat-store-resume-verified-head-single-reference');
  mode='normal';handle=await stage();mode='quota';ready=await child('run',handle);assert.equal(ready.error,'UPLOAD_HTTP_429');assert.equal(quotaCalls,4);tests.push('quota-bounded-four-attempts');mode='normal';await discard(handle);
  mode='normal';handle=await stage();mode='slow';ready=await child('abort',handle);assert.ok(ready.error);mode='normal';ready=await child('run',handle);assert.equal(ready.error,'UPLOAD_IDENTITY');assert.equal(fs.existsSync(path.join(dir,'memoh-upload-journal-v1',handle+'.original')),false);tests.push('cancel-during-PATCH-tombstone-cleanup-no-late-ready')
  const journals=fs.readdirSync(path.join(dir,'memoh-upload-journal-v1')).filter(n=>n.endsWith('.json')).map(n=>fs.readFileSync(path.join(dir,'memoh-upload-journal-v1',n),'utf8'));assert.ok(journals.every(x=>!x.includes('Bearer')&&!x.includes('authorization')&&!x.includes('token')))
  console.log(JSON.stringify({scope:'actual ArkTS via Node Kit adapters, independent processes, real disk and controlled HTTP; not Harmony native runtime',mib,size,maxChunk,created,patches,legacyPosts,tests,measurements,credentialFreeJournal:true}))
 }finally{server.closeAllConnections();server.close();fs.rmSync(dir,{recursive:true,force:true})}
}
