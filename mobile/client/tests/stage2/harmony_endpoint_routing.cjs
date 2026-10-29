// Actual native consumers, controlled Kit boundaries and real process/file/HTTP
// recovery. Synthetic no-authority bearer only; not device/TLS qualification.
const fs=require('node:fs'),path=require('node:path'),os=require('node:os'),http=require('node:http'),crypto=require('node:crypto'),vm=require('node:vm'),assert=require('node:assert/strict'),{spawn}=require('node:child_process')
const root=path.resolve(__dirname,'../..'),ts=require(path.join(root,'mobile/.build/memoh-web/checkout/node_modules/typescript')),base='mobile/harmony/app/entry/src/main/ets/common/'
const sha=b=>crypto.createHash('sha256').update(b).digest('hex'),plain=x=>JSON.parse(JSON.stringify(x))
function load(file,imports={}){const m={exports:{}};vm.runInNewContext(ts.transpileModule(fs.readFileSync(path.join(root,file),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText,{exports:m.exports,module:m,require:n=>{assert(n in imports,n);return imports[n]},URL,Error,JSON,Number,String,Array,Map,Set,Promise});return m.exports}
const shared=load('mobile/memoh/harmony-service-url.ts'),connection={getDefaultHttpProxy:async()=>({host:''}),getDefaultNet:async()=>({getAddressesByName:async()=>[]})}
const {NativeTransferNetwork}=load(base+'NativeTransferNetwork.ets',{'./ServiceUrl':shared,'@kit.ArkTS':{url:{URL:{parseURL:s=>new URL(s)}}},'@kit.NetworkKit':{connection}})
const logical='https://nas.example.invalid:18443',physical='http://127.0.0.1:18068',policy={version:2,endpoint:{logical_origin:logical,physical_origin:physical}}
let checks=0
async function units(){
 assert.throws(()=>new NativeTransferNetwork('http://nas.example.invalid/api').adopt({version:1,lan_addresses:['192.0.2.10']}),/NETWORK_POLICY_HTTPS/);checks++
 for(const method of ['GET','HEAD','POST','PATCH','DELETE']){
  const route=new NativeTransferNetwork(logical+'/prefix/api');route.adopt(plain(policy));const target=logical+'/prefix/api/bots/bot/media/uploads/receipt',content=new Uint8Array([0,255,42]).buffer,destination=()=>{},tracing={marker:'retained'}
  const request={url:new URL(target),method,headers:{authorization:'Bearer synthetic-no-authority',range:'bytes=262144-','Upload-Offset':'262144'},content,destination,configuration:{transfer:{autoRedirect:true},tracing}}
  let calls=0;const response=await route.fetch({fetch:async r=>{calls++;assert.equal(r,request);assert.equal(r.url.href,physical+'/prefix/api/bots/bot/media/uploads/receipt');assert.equal(r.content,content);assert.equal(r.destination,destination);assert.equal(r.configuration.tracing,tracing);assert.equal(r.configuration.security.remoteValidation,'system');assert.equal(r.configuration.transfer.autoRedirect,false);assert.equal(r.headers['Upload-Offset'],'262144');return{statusCode:503}}},target,method,()=>request)
  assert.equal(calls,1);assert.equal(response.statusCode,503);checks++
 }
 for(const endpoint of ['http://user:pass@host.invalid','http://host.invalid/path','http://host.invalid?','http://host.invalid#','http://host.invalid:0','http://host.invalid:65536','file:///tmp/data','//host.invalid','https://host.invalid/%61pi']){
  const route=new NativeTransferNetwork(logical+'/api');assert.throws(()=>route.adopt({version:2,endpoint:{logical_origin:logical,physical_origin:endpoint}}));checks++
 }
 for(const value of [{version:2},{version:2,endpoint:{...policy.endpoint,logical_origin:'https://other.invalid'}},{...policy,token:'forbidden'},{...policy,lan_addresses:null},{...policy,endpoint:{...policy.endpoint,path:'/api'}}]){assert.throws(()=>new NativeTransferNetwork(logical+'/api').adopt(value));checks++}
 const route=new NativeTransferNetwork(logical+'/api'),snapshot=plain(policy);route.adopt(snapshot);snapshot.endpoint.physical_origin='https://changed.invalid';let calls=0
 const target=logical+'/api/users/me',request=()=>({url:new URL(target),method:'GET',configuration:{}})
 for(const statusCode of [401,403,404,503]){await route.fetch({fetch:async r=>{calls++;assert.equal(r.url.origin,physical);return{statusCode}}},target,'GET',request);checks++}
 assert.equal(calls,4)
 await assert.rejects(route.fetch({fetch:async()=>{throw Error('dispatch forbidden')}},target,'PATCH',request),/AUTHORITY/);checks++
 await assert.rejects(route.fetch({fetch:async()=>{throw Error('dispatch forbidden')}},'https://other.invalid/api/users/me','GET',request),/AUTHORITY/);checks++
 await assert.rejects(route.fetch({fetch:async()=>{throw Error('dispatch forbidden')}},target,'GET',()=>({url:new URL('https://other.invalid/api/users/me'),method:'GET'})),/AUTHORITY/);checks++
 const cause=Object.assign(Error('controlled certificate or transport failure'),{code:1007900060});calls=0
 await assert.rejects(route.fetch({fetch:async()=>{calls++;throw cause}},target,'GET',request),e=>e===cause);assert.equal(calls,1);checks++
 let resolveProxy,active=true,dispatches=0;const oldProxy=connection.getDefaultHttpProxy
 connection.getDefaultHttpProxy=()=>new Promise(resolve=>{resolveProxy=resolve})
 const delayed=new NativeTransferNetwork(logical+'/api');delayed.adopt({version:2,lan_addresses:['192.0.2.10'],endpoint:{logical_origin:logical,physical_origin:'https://physical.example.invalid'}})
 const pending=delayed.fetch({fetch:async()=>{dispatches++;return{statusCode:200}}},target,'GET',()=>{if(!active)throw Error('UPLOAD_CANCELLED');return request()})
 const denied=assert.rejects(pending,/UPLOAD_CANCELLED/);active=false;resolveProxy({host:''});await denied;assert.equal(dispatches,0);connection.getDefaultHttpProxy=oldProxy;checks++
}

async function integration(){
 const tmp=fs.mkdtempSync(path.join(os.tmpdir(),'native-endpoint-')),up=path.join(tmp,'upload'),down=path.join(tmp,'download');fs.mkdirSync(up);fs.mkdirSync(down)
 const bytes=Buffer.alloc(8*1048576+33);for(let i=0;i<bytes.length;i++)bytes[i]=(i*31+i%251)%256;fs.writeFileSync(path.join(up,'source.bin'),bytes);const hash=sha(bytes),receiptId='12345678-1234-4234-8234-123456789abc'
 let logicalBase,physicalOrigin,receipt,active,killUpload=true,holdDownload=false,patchBytes=0,creates=0,completes=0,revoked=false,owner='same-user'
 const uploads=[],reads=[],identityRequests=[]
 const json=(res,code,value)=>{res.writeHead(code,{'content-type':'application/json'});res.end(JSON.stringify(value))}
 const serve=async(req,res)=>{try{
  assert.equal(req.headers.authorization,'Bearer synthetic-no-authority')
  const p=new URL(req.url,'http://local').pathname,media='/api/bots/bot-a/media'
  if(p===media+'/limits')return json(res,200,{max_upload_bytes:209715200,resumable_enabled:true,chunk_max_bytes:1048576})
  if(p===media+'/uploads'&&req.method==='POST'){
   creates++;let chunks=[];for await(const b of req)chunks.push(b);const input=JSON.parse(Buffer.concat(chunks));assert.equal(input.sha256,hash);assert.equal(input.size_bytes,bytes.length)
   receipt={id:receiptId,state:'uploading',offset:0,size_bytes:bytes.length,expires_at:new Date(Date.now()+86400000).toISOString(),mime:'application/octet-stream'};return json(res,201,receipt)
  }
  if(p===media+'/uploads/'+receiptId&&req.method==='GET')return json(res,200,receipt)
  if(p===media+'/uploads/'+receiptId&&req.method==='PATCH'){
   assert.equal(Number(req.headers['upload-offset']),receipt.offset);const chunks=[];for await(const b of req)chunks.push(b);const body=Buffer.concat(chunks);assert.equal(sha(body),req.headers['upload-sha256']);assert.ok(body.equals(bytes.subarray(receipt.offset,receipt.offset+body.length)));uploads.push(receipt.offset);receipt.offset+=body.length;patchBytes+=body.length
   if(killUpload){killUpload=false;active.kill('SIGKILL');res.destroy();return}return json(res,200,receipt)
  }
  if(p===media+'/uploads/'+receiptId+'/complete'){
   completes++;assert.equal(receipt.offset,bytes.length);receipt={...receipt,state:'ready',content_hash:hash};return json(res,200,receipt)
  }
  if(p===media+'/'+hash){
   if(req.method==='HEAD'){res.writeHead(200,{'content-length':bytes.length,etag:'"'+hash+'"','content-type':'application/octet-stream'});res.end();return}
   const offset=Number((req.headers.range||'bytes=0-').match(/bytes=(\d+)-/)[1]);reads.push(offset)
   res.writeHead(206,{'content-length':bytes.length-offset,'content-range':`bytes ${offset}-${bytes.length-1}/${bytes.length}`,etag:'"'+hash+'"','content-type':'application/octet-stream'})
   if(holdDownload){res.write(bytes.subarray(offset,offset+1048576));return}res.end(bytes.subarray(offset));return
  }
  throw Error('Unexpected physical path '+p)
 }catch(error){if(!res.headersSent)json(res,500,{});else res.destroy();console.error(error)}}
 const a=http.createServer(serve),b=http.createServer(serve),control=http.createServer((req,res)=>{
  assert.equal(req.url,'/api/users/me');identityRequests.push(req.url)
  if(revoked)return json(res,401,{})
  assert.equal(req.headers.authorization,'Bearer synthetic-no-authority');json(res,200,{id:owner,network_policy:{version:2,endpoint:{logical_origin:logicalBase.replace(/\/api$/,''),physical_origin:physicalOrigin}}})
 })
 const listen=s=>new Promise(resolve=>s.listen(0,'127.0.0.1',resolve));await listen(a);await listen(b);await listen(control);logicalBase='http://127.0.0.1:'+control.address().port+'/api';physicalOrigin='http://127.0.0.1:'+a.address().port
 function worker(script,args=[],env={}){
  const child=spawn(process.execPath,[path.join(root,'tests/stage2',script),...args],{env:{...process.env,...env},stdio:['ignore','pipe','pipe']});let out='',err='';child.stdout.on('data',x=>out+=x);child.stderr.on('data',x=>err+=x)
  const result=new Promise(resolve=>child.once('exit',(code,signal)=>resolve({code,signal,out,err})));return{child,result}
 }
 const upload=(action,handle='')=>worker('harmony_resumable.cjs',['worker',up,logicalBase,action,handle,'synthetic-no-authority','bot-a','session-a'])
 try{
  const staged=await upload('stage').result;assert.equal(staged.code,0,staged.err+staged.out);const handle=JSON.parse(staged.out.trim().split('\n').at(-1)).result[0].handle
  const first=upload('run',handle);active=first.child;const killed=await first.result;assert.equal(killed.signal,'SIGKILL');assert.equal(receipt.offset,262144)
  const journalPath=path.join(up,'memoh-upload-journal-v1',handle+'.json'),before=JSON.parse(fs.readFileSync(journalPath));assert.equal(before.uploadId,receiptId)
  physicalOrigin='http://127.0.0.1:'+b.address().port
  const resumed=await upload('run',handle).result;assert.equal(resumed.code,0,resumed.err+resumed.out);const after=JSON.parse(fs.readFileSync(journalPath));assert.equal(after.uploadId,receiptId);assert.equal(after.owner,before.owner);assert.equal(after.scope,before.scope);assert.equal(after.hash,hash);assert.equal(creates,1);assert.equal(completes,1);assert.equal(patchBytes,bytes.length);assert.equal(uploads[1],262144)
  const input={operation:'d'.repeat(32),botId:'bot-a',hash,size:bytes.length,name:'synthetic.bin',url:logicalBase+'/bots/bot-a/media/'+hash,authorization:'Bearer synthetic-no-authority'};fs.writeFileSync(path.join(down,'worker-input.json'),JSON.stringify(input))
  const download=()=>worker('harmony_native_download.cjs',[],{HARMONY_DOWNLOAD_TEST_ROOT:down,HARMONY_DOWNLOAD_TEST_ORIGIN:logicalBase})
  holdDownload=true;const firstDownload=download(),journalDir=path.join(down,'memoh-download-journal-v1');let checkpoint=0
  for(let i=0;i<500;i++){await new Promise(resolve=>setTimeout(resolve,10));if(fs.existsSync(journalDir)){const name=fs.readdirSync(journalDir).find(n=>n.endsWith('.json'));if(name){checkpoint=JSON.parse(fs.readFileSync(path.join(journalDir,name))).received;if(checkpoint>=262144)break}}}
  assert.ok(checkpoint>=262144);firstDownload.child.kill('SIGKILL');assert.equal((await firstDownload.result).signal,'SIGKILL')
  const savedName=fs.readdirSync(journalDir).find(n=>n.endsWith('.json'));checkpoint=JSON.parse(fs.readFileSync(path.join(journalDir,savedName))).received
  holdDownload=false;physicalOrigin='http://127.0.0.1:'+a.address().port;reads.length=0
  const completeDownload=await download().result;assert.equal(completeDownload.code,0,completeDownload.err+completeDownload.out);assert.equal(reads[0],checkpoint);assert.equal(sha(fs.readFileSync(path.join(down,'saved.bin'))),hash)
  owner='different-user';const mismatched=await upload('run',handle).result;assert.notEqual(mismatched.code,0);assert.equal(patchBytes,bytes.length);owner='same-user'
  revoked=true;const denied=await upload('run',handle).result;assert.notEqual(denied.code,0);assert.equal(creates,1);assert.equal(completes,1)
  return{bytes:bytes.length,uploadReceipt:receiptId,uploadProcessDeath:true,downloadProcessDeath:true,physicalPortChangedBothDirections:true,uploadFirstUncertainOffset:262144,downloadResumedOffset:checkpoint,confirmedUploadBytesNotRepeated:patchBytes===bytes.length,create:creates,complete:completes,sameLogicalOwnerAndScope:true,fullSavedHashVerified:true,differentOwnerDenied:true,revokedIdentityDeniedBeforePhysicalWrite:true,identityGETs:identityRequests.length}
 }finally{for(const s of[a,b,control]){s.closeAllConnections();s.close()}fs.rmSync(tmp,{recursive:true,force:true})}
}
;(async()=>{await units();const result=await integration();console.log(JSON.stringify({unitCases:checks,...result,scope:'Actual native source, Node Kit adapters and controlled real HTTP/process/disk; not device engine or TLS qualification',syntheticBearerOnly:true,newHAP:false}))})().catch(error=>{console.error(error);process.exitCode=1})
