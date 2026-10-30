// Actual owned ETS through the retained Kit boundary worker; real bytes, HTTP,
// journal and child-process death. Synthetic credentials; not phone/TLS evidence.
const fs=require('node:fs'),path=require('node:path'),os=require('node:os'),http=require('node:http'),crypto=require('node:crypto'),assert=require('node:assert/strict'),{spawn}=require('node:child_process');
const root=path.resolve(__dirname,'../../..'),worker=path.join(root,'mobile/client/tests/stage2/harmony_resumable.cjs');
const sha=b=>crypto.createHash('sha256').update(b).digest('hex');
async function one(name,faults,kind='committed',prefix='/api'){
 const dir=fs.mkdtempSync(path.join(os.tmpdir(),'harmony-recovery-boundary-')),bytes=Buffer.alloc((faults+1)*262144+17,0x7d),hash=sha(bytes),events=[];
 fs.writeFileSync(path.join(dir,'source.bin'),bytes);
 let receipt,received=Buffer.alloc(0),created=0,completed=0,failed=0,active,firstFailureAt=0,firstCooldownReadAt=0,advertisedDue=0,childCount=0;
 const json=(res,status,data,headers={})=>{res.writeHead(status,{'content-type':'application/json',...headers});res.end(JSON.stringify(data))};
 const server=http.createServer(async(req,res)=>{try{
  assert(['Bearer synthetic','Bearer other-owner'].includes(req.headers.authorization));const p=new URL(req.url,'http://fixture').pathname;
  if(p===prefix+'/users/me')return json(res,200,{id:req.headers.authorization==='Bearer synthetic'?'synthetic-owner':'other-owner'});
  if(p===prefix+'/bots/bot-a/media/limits')return json(res,200,{max_upload_bytes:bytes.length,resumable_enabled:true,chunk_max_bytes:262144});
  const body=Buffer.concat(await Array.fromAsync(req));
  if(p===prefix+'/bots/bot-a/media/uploads'&&req.method==='POST'){
   created++;assert.equal(JSON.parse(body).sha256,hash);receipt={id:crypto.randomUUID(),state:'uploading',size_bytes:bytes.length,offset:0,mime:'application/octet-stream',expires_at:new Date(Date.now()+(['expiry','long-expiry'].includes(kind)?1600:86400000)).toISOString()};return json(res,201,receipt);
  }
  assert(receipt&&p.includes(receipt.id));
  if(req.method==='GET'){
   if(firstFailureAt&&!firstCooldownReadAt)firstCooldownReadAt=Date.now();events.push({kind:'GET',at:Date.now(),offset:receipt.offset});
   return json(res,200,receipt);
  }
  if(req.method==='PATCH'){
   const at=Number(req.headers['upload-offset']);assert.equal(at,received.length);assert(body.equals(bytes.subarray(at,at+body.length)));assert.equal(sha(body),req.headers['upload-sha256']);events.push({kind:'PATCH',at:Date.now(),offset:at});
   const pending=failed<faults;
   if(pending&&['cooldown','array','date','long','long-expiry','restart','expiry','pause','forbidden'].includes(kind)){
    failed++;firstFailureAt ||= Date.now();
    if(kind==='forbidden')return json(res,403,{});
    const date=new Date(Math.ceil(Date.now()/1000)*1000+2000).toUTCString();if(kind==='date')advertisedDue=Date.parse(date);
    return json(res,['cooldown','array'].includes(kind)?429:503,{},kind==='restart'?{'Retry-After':'2'}:['cooldown','array'].includes(kind)?{'Retry-After':'1'}:kind==='date'?{'Retry-After':date}:['long','long-expiry'].includes(kind)?{'Retry-After':'7200'}:{});
   }
   received=Buffer.concat([received,body]);receipt.offset=received.length;
   if(pending){failed++;if(kind==='lost'){res.destroy();return}return json(res,kind==='conflict'?409:503,{})}
   return json(res,200,receipt);
  }
  if(p.endsWith('/complete')){completed++;assert(received.equals(bytes));receipt={...receipt,state:'ready',content_hash:hash};return json(res,200,receipt)}
  throw Error('unexpected fixture route');
 }catch(e){res.destroy(e)}});
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const origin='http://127.0.0.1:'+server.address().port+prefix;
 function start(action,handle='',token='synthetic',bot='bot-a',scope='session-a'){
  childCount++;const child=spawn(process.execPath,[worker,'worker',dir,origin,action,handle,token,bot,scope],{stdio:['ignore','pipe','pipe']});active=child;
  let out='',err='';child.stdout.on('data',b=>out+=b);child.stderr.on('data',b=>err+=b);
  const timer=setTimeout(()=>child.kill('SIGKILL'),20000);
  const done=new Promise((resolve,reject)=>{child.on('error',reject);child.on('exit',(code,signal)=>{clearTimeout(timer);if(signal)return resolve({code,signal});try{resolve({code,...JSON.parse(out.trim().split('\n').at(-1))})}catch(e){reject(Error(err+out))}})});return{child,done};
 }
 async function waitUntil(predicate){const end=Date.now()+10000;while(!predicate()){if(Date.now()>end)throw Error('fixture wait timeout');await new Promise(r=>setTimeout(r,10))}}
 try{
  const stage=await start('stage').done;assert.equal(stage.code,0);const handle=stage.result[0].handle,journal=path.join(dir,'memoh-upload-journal-v1',handle+'.json');
  let run;
  if(kind==='restart'){
   const first=start('run',handle);await waitUntil(()=>JSON.parse(fs.readFileSync(journal)).retryAt>0);
   const checkpoint=JSON.parse(fs.readFileSync(journal));assert(checkpoint.retryAt>=firstFailureAt+2000);first.child.kill('SIGKILL');assert.equal((await first.done).signal,'SIGKILL');
   const before=events.length;run=await start('run',handle).done;assert(events.slice(before).some(e=>e.kind==='GET'&&e.at>=checkpoint.retryAt));assert(firstCooldownReadAt>=checkpoint.retryAt);
  }else run=await start(['pause','long'].includes(kind)?'retry-pause':kind==='array'?'run-array-header':'run',handle).done;
  if(['expiry','long-expiry','pause','long','forbidden'].includes(kind)){
   assert.equal(run.code,2);assert.equal(run.error,kind==='forbidden'?'UPLOAD_HTTP_403':['pause','long'].includes(kind)?'UPLOAD_CANCELLED':'UPLOAD_RECEIPT_UNAVAILABLE');
   assert.equal(completed,0);const j=JSON.parse(fs.readFileSync(journal));assert.equal(j.uploadId,receipt.id);assert(fs.existsSync(journal.replace('.json','.original')));assert.equal(j.state,'uploading');
   if(kind==='long'){assert(j.retryAt>=firstFailureAt+7200000);assert.equal(firstCooldownReadAt,0)}
   if(kind==='forbidden')assert.equal(events.filter(e=>e.kind==='PATCH').length,1);
   if(kind==='long-expiry'){assert.equal(firstCooldownReadAt,0);assert.equal(events.filter(e=>e.kind==='PATCH').length,1)}
   if(kind==='expiry'){const writes=events.filter(e=>e.kind==='PATCH');assert(writes.length<faults);for(let i=1;i<writes.length;i++)assert(writes[i].at-writes[i-1].at>=200)}
   return{name,pendingPreserved:true,error:run.error,creates:created,completes:completed,patches:events.filter(e=>e.kind==='PATCH').length};
  }
  assert.equal(run.code,0,JSON.stringify(run));assert.equal(run.result.content_hash,hash);assert(received.equals(bytes));assert.equal(created,1);assert.equal(completed,1);
  if(['cooldown','array'].includes(kind))assert(firstCooldownReadAt>=firstFailureAt+1000);
  if(kind==='date')assert(firstCooldownReadAt>=advertisedDue);
  if(['committed','lost','conflict'].includes(kind)){const offsets=events.filter(e=>e.kind==='PATCH').map(e=>e.offset);assert.equal(new Set(offsets).size,offsets.length)}
  // A new process and different identity cannot take the retained original.
  const wrong=await start('run',handle,'other-owner').done;assert.equal(wrong.error,'UPLOAD_IDENTITY');
  return{name,bytes:bytes.length,hashVerified:true,creates:created,completes:completed,recoveredFaults:failed,identityRejected:true,processes:childCount,headersCooldownRespected:['cooldown','array','date','restart'].includes(kind)};
 }finally{active?.kill();server.closeAllConnections();await new Promise(r=>server.close(r));fs.rmSync(dir,{recursive:true,force:true})}
}
(async()=>{
 const cases=process.argv.includes('--red')?[['four-committed',4]]:[['three-committed',3],['four-committed',4],['eight-committed',8],['four-lost-replies',4,'lost'],['four-conflicts',4,'conflict'],['retry-after',1,'cooldown'],['retry-after-rcp-single-array',1,'array'],['retry-after-http-date',1,'date'],['long-cooldown-cancel',1,'long'],['retry-after-restart',1,'restart'],['expiry-no-busy-loop',99,'expiry'],['retry-after-beyond-expiry',1,'long-expiry'],['cancel-cooldown',1,'pause'],['403-not-retried',1,'forbidden'],['root-api-real-bytes',4,'committed',''],['safe-prefix-real-bytes',4,'committed','/memoh/v1']];
 const index=process.argv.indexOf('--case'),selected=index<0?cases:cases.filter(row=>(process.argv[index+1]??'').split(',').includes(row[0]));assert(selected.length);
 const results=[];for(const args of selected)results.push(await one(...args));console.log(JSON.stringify({cases:results,device:false,TLS:false},null,2));
})().catch(e=>{console.error(e);process.exitCode=1});
