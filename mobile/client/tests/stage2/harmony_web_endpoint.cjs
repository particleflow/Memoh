// Actual ETS adapter, controlled ArkWeb/RCP boundaries, real HTTP byte streams.
// Synthetic credentials only. Does not qualify ArkWeb, RCP wire TLS or a phone.
const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm'),http=require('node:http'),assert=require('node:assert/strict'),crypto=require('node:crypto')
const {once}=require('node:events')
const root=path.resolve(__dirname,'../..'),common='mobile/harmony/app/entry/src/main/ets/common/'
const ts=require(path.join(root,'mobile/.build/memoh-web/checkout/node_modules/typescript'))
function load(file,imports={}){const m={exports:{}};vm.runInNewContext(ts.transpileModule(fs.readFileSync(path.join(root,file),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText,{exports:m.exports,module:m,require:n=>{assert(n in imports,n);return imports[n]},URL,ArrayBuffer,Uint8Array,Map,Set,Error,JSON,Number,String,Array,Promise});return m.exports}
const ark={url:{URL:{parseURL:s=>new URL(s)}},util:{TextDecoder:class{decodeToString(b){return new TextDecoder().decode(b)}}}}
const shared=load('mobile/memoh/harmony-service-url.ts'),origin=load(common+'MemohClientConfig.ets',{'./ServiceUrl':shared})
const policy=load(common+'SharedWebPolicy.ets',{'@kit.ArkWeb':{},'@kit.ArkTS':ark,'./WebResourceManifest':{WEB_RESOURCES:{}},'./MemohClientConfig':origin})
const connection={getDefaultHttpProxy:async()=>({host:''})}
const network=load(common+'NativeTransferNetwork.ets',{'./ServiceUrl':shared,'@kit.ArkTS':ark,'@kit.NetworkKit':{connection}})
const logical='https://logical.fixture.invalid:18443',base=logical+'/prefix/api',token='Bearer synthetic-no-authority',tokenB='Bearer synthetic-no-authority-b'
const hash=b=>crypto.createHash('sha256').update(b).digest('hex'),buffer=b=>b.buffer.slice(b.byteOffset,b.byteOffset+b.byteLength)
const bytes=Buffer.alloc(3*1048576+39);for(let i=0;i<bytes.length;i++)bytes[i]=(i*31+i%251)%256
const multipartWire=Buffer.concat([Buffer.from('--existing-boundary\r\nContent-Disposition: form-data; name="file"; filename="fixture.bin"\r\nContent-Type: application/octet-stream\r\n\r\n'),bytes,Buffer.from('\r\n--existing-boundary--\r\n')])
let checks=0,physical,controlOrigin,profileStatus=200,profilePolicy,owner='owner-a',delayBody,failTransport=false,records=[],reads=[]
const server=http.createServer(async(req,res)=>{try{
 const url=new URL(req.url,'http://local'),chunks=[];for await(const b of req)chunks.push(b)
 const body=Buffer.concat(chunks);records.push({path:req.url,method:req.method,size:body.length,hash:hash(body),authorization:req.headers.authorization,range:req.headers.range,offset:req.headers['upload-offset'],type:req.headers['content-type']})
 const status=Number(url.searchParams.get('status')||200)
 if(url.searchParams.has('disconnect')){res.destroy();return}
 if(status===302){res.writeHead(status,{location:'http://foreign.fixture.invalid/leak'});res.end();return}
 res.writeHead(status,{'content-type':'application/octet-stream','content-length':bytes.length,etag:'"'+hash(bytes)+'"'})
 for(let i=0;i<bytes.length;i+=32768){if(!res.write(bytes.subarray(i,i+32768)))await new Promise(resolve=>res.once('drain',resolve))}res.end()
 }catch{res.destroy()}})
const control=http.createServer((req,res)=>{assert.equal(req.url,'/prefix/api/users/me');res.writeHead(profileStatus,{'content-type':'application/json'});res.end(JSON.stringify({id:owner,network_policy:profilePolicy}))})
class SchemeHandler{onRequestStart(f){this.start=f}onRequestStop(f){this.stop=f}}
class SchemeResponse{constructor(){this.headers={}}setUrl(v){this.url=v}setStatus(v){this.status=v}setHeaderByName(k,v){this.headers[k]=v}setMimeType(v){this.mime=v}}
class Request{constructor(url,method,headers){this.url=new URL(url);this.method=method;this.headers=headers;this.configuration={}}}
const sessions=[]
class Session{
 constructor(){this.pending=new Set();sessions.push(this)}
 cancel(){for(const request of this.pending)request.destroy(Error('controlled cancellation'))}
 close(){this.cancel()}
 async fetch(request){
  if(failTransport)throw Object.assign(Error('controlled certificate failure'),{code:1007900060})
  const target=new URL(request.url),isControl=target.origin===logical
  const actual=isControl?controlOrigin+target.pathname:target.href
  assert.equal(request.configuration.security.remoteValidation,'system');assert.equal(request.configuration.transfer.autoRedirect,false)
  const events=request.configuration.tracing?.httpEventsHandler
  return new Promise((resolve,reject)=>{
   const req=http.request(actual,{method:request.method,headers:request.headers},async res=>{try{
    events?.onStatusCodeReceive?.(res.statusCode);events?.onHeaderReceive?.(res.headers)
    for await(const chunk of res)await request.destination(buffer(chunk))
    resolve({statusCode:res.statusCode,headers:res.headers})
   }catch(e){res.destroy();reject(e)}})
   this.pending.add(req);req.once('error',reject);req.once('close',()=>this.pending.delete(req))
   void(async()=>{try{
    if(request.content){while(true){const chunk=await request.content(131072);reads.push(chunk.byteLength);if(!chunk.byteLength)break;if(!req.write(Buffer.from(chunk)))await once(req,'drain')}}
    req.end()
   }catch(e){req.destroy(e);reject(e)}})()
  })
 }
}
const rcp={Request,createSession:()=>new Session()}
const {NativeWebNetwork}=load(common+'NativeWebNetwork.ets',{'@kit.ArkWeb':{webview:{WebSchemeHandler:SchemeHandler,WebSchemeHandlerResponse:SchemeResponse}},'@kit.RemoteCommunicationKit':{rcp},'@kit.ArkTS':ark,'./MemohClientConfig':origin,'./SharedWebPolicy':policy,'./NativeTransferNetwork':network})
let alive=true
const bridge=new NativeWebNetwork(()=>alive),handlers={};bridge.attach({setWebSchemeHandler:(scheme,handler)=>{handlers[scheme]=handler}})
function source(target,options={}){
 let position=0
 const body=options.body
 return{getRequestUrl:()=>target,getRequestMethod:()=>options.method||'GET',isMainFrame:()=>!!options.main,getFrameUrl:()=>options.frame||policy.WEB_START,
 getHeader:()=>[...(options.noAuth?[]:[{headerKey:'Authorization',headerValue:options.auth||token}]),{headerKey:'Range',headerValue:'bytes=262144-'},{headerKey:'Upload-Offset',headerValue:'262144'},{headerKey:'Content-Type',headerValue:'multipart/form-data; boundary=existing-boundary'}],
 getHttpBodyStream:()=>body?{initialize:async()=>{if(delayBody)await delayBody},read:async max=>{const chunk=body.subarray(position,position+max);position+=chunk.length;return buffer(chunk)}}:null}
}
function call(target,options={}){
 const request=source(target,options),parts=[];let response,error,finish
 const done=new Promise(resolve=>{finish=resolve})
 const sink={didReceiveResponse:r=>{response=r},didReceiveResponseBody:b=>parts.push(Buffer.from(b)),didFail:e=>{error=e;finish()},didFinish:()=>finish()}
 const accepted=handlers.https.start(request,sink)
 if(!accepted)finish()
 return{request,accepted,done:done.then(()=>({response,error,bytes:Buffer.concat(parts)}))}
}
async function profile(){const callResult=call(base+'/users/me');assert(callResult.accepted);const result=await callResult.done;assert.equal(result.error,undefined);assert.equal(result.response.status,profileStatus);return result}
async function check(name,fn){await fn();checks++;}
const listen=s=>new Promise(resolve=>s.listen(0,'127.0.0.1',resolve))
;(async()=>{await listen(server);await listen(control);physical='http://127.0.0.1:'+server.address().port;controlOrigin='http://127.0.0.1:'+control.address().port
 origin.configureMemohOrigin(base);profilePolicy={version:2,endpoint:{logical_origin:logical,physical_origin:physical}}
 const target=base+'/bots/original-bot/container/fs/download?path=old%20name'
 try{
 await check('unbound request keeps original engine',async()=>assert.equal(call(target).accepted,false))
 await check('original profile bytes and logical response URL',async()=>{const result=await profile();assert.equal(result.response.url,base+'/users/me');assert.equal(JSON.parse(result.bytes).id,owner)})
 await check('actual bounded multipart upload with original headers',async()=>{const x=call(base+'/bots/original-bot/container/fs/upload',{method:'POST',body:multipartWire});const result=await x.done;assert.equal(result.error,undefined);assert.equal(hash(result.bytes),hash(bytes));const record=records.at(-1);assert.equal(record.hash,hash(multipartWire));assert.equal(record.size,multipartWire.length);assert.equal(record.type,'multipart/form-data; boundary=existing-boundary');assert.equal(record.offset,'262144');assert(Math.max(...reads)<=65536)})
 await check('range and query retained, response URL remains logical',async()=>{const result=await call(target+'&status=206').done;assert.equal(result.response.status,206);assert.equal(result.response.url,target+'&status=206');assert.equal(hash(result.bytes),hash(bytes));assert.equal(records.at(-1).range,'bytes=262144-');assert.equal(records.at(-1).path,'/prefix/api/bots/original-bot/container/fs/download?path=old%20name&status=206')})
 for(const status of [401,403,404,503])await check('HTTP returned once '+status,async()=>{const count=records.length,result=await call(target+'&status='+status).done;assert.equal(result.response.status,status);assert.equal(records.length,count+1)})
 await check('redirect refused without repeat/follow',async()=>{const count=records.length,result=await call(target+'&status=302').done;assert.equal(result.error,-2);assert.equal(records.length,count+1)})
 await check('unknown PATCH not repeated',async()=>{const count=records.length,result=await call(base+'/bots/original-bot/media/uploads/original-receipt?disconnect=1',{method:'PATCH',body:bytes}).done;assert.equal(result.error,-2);assert.equal(records.length,count+1);assert.equal(records.at(-1).hash,hash(bytes))})
 await check('foreign, physical and encoded escape remain refused',async()=>{for(const url of [physical+'/prefix/api/bots/original-bot/media/hash','https://foreign.invalid/prefix/api/bots/original-bot/media/hash',base+'/bots/original-bot/media/%2e%2e/hash']){assert.equal(policy.memohRequest(url),false);assert.equal(call(url).accepted,false)}})
 await check('frame, main document, unrelated control and changed token not mapped',async()=>{for(const [url,options] of [[target,{frame:'https://foreign.invalid'}],[target,{main:true}],[base+'/bots/original-bot/messages',{}],[target,{auth:tokenB}]])assert.equal(call(url,options).accepted,false)})
 await check('existing query-token resource uses same credential without adding a bearer header',async()=>{const result=await call(target+'&token=synthetic-no-authority',{noAuth:true}).done;assert.equal(result.error,undefined);assert.equal(hash(result.bytes),hash(bytes));assert.equal(records.at(-1).authorization,undefined);assert.equal(call(target+'&token=unknown',{noAuth:true}).accepted,false);assert.equal(call(target+'&token=synthetic-no-authority&token=synthetic-no-authority',{noAuth:true}).accepted,false);assert.equal(call(target+'&token=unknown').accepted,false)})
 await check('delegated attachment keeps scope and status with no bot/hash fallback',async()=>{const scope=base+'/bots/bot-a/sessions/session-b/delegations/delegation-c/attachments/'+'a'.repeat(64)+'?status=403';const count=records.length,result=await call(scope).done;assert.equal(result.response.status,403);assert.equal(records.length,count+1);assert(records.at(-1).path.includes('/session-b/delegations/delegation-c/attachments/'))})
 await check('lifecycle reset during body initialization sends zero bytes',async()=>{let release;delayBody=new Promise(resolve=>release=resolve);const count=records.length,x=call(target,{method:'POST',body:bytes});bridge.reset();release();const result=await x.done;delayBody=undefined;assert.equal(result.error,-3);await new Promise(resolve=>setImmediate(resolve));assert.equal(records.length,count);assert.equal(call(target).accepted,false);await profile()})
 await check('ArkWeb request stop during initialization sends zero bytes',async()=>{let release;delayBody=new Promise(resolve=>release=resolve);const count=records.length,x=call(target,{method:'POST',body:bytes});handlers.https.stop(x.request);release();delayBody=undefined;await new Promise(resolve=>setImmediate(resolve));assert.equal(records.length,count)})
 await check('owner switch cancels pending old credential',async()=>{let release;delayBody=new Promise(resolve=>release=resolve);const count=records.length,x=call(target,{method:'POST',body:bytes});owner='owner-b';await call(base+'/users/me',{auth:tokenB}).done;release();delayBody=undefined;assert.equal((await x.done).error,-3);assert.equal(records.length,count);assert.equal(call(target).accepted,false);const result=await call(target,{auth:tokenB}).done;assert.equal(result.error,undefined);assert.equal(records.at(-1).authorization,tokenB);await profile()})
 await check('revocation cancels pending file, retains original 401 profile',async()=>{let release;delayBody=new Promise(resolve=>release=resolve);const count=records.length,x=call(target,{method:'POST',body:bytes});profileStatus=401;await profile();release();delayBody=undefined;assert.equal((await x.done).error,-3);assert.equal(records.length,count);assert.equal(call(target).accepted,false);profileStatus=200;await profile()})
 await check('owner changed under same bearer cancels old pending body',async()=>{let release;delayBody=new Promise(resolve=>release=resolve);const count=records.length,x=call(target,{method:'POST',body:bytes});owner='owner-c';await profile();release();delayBody=undefined;assert.equal((await x.done).error,-3);assert.equal(records.length,count)})
 await check('invalid mapped authority never adopted',async()=>{profilePolicy={version:2,endpoint:{logical_origin:'https://different.invalid',physical_origin:physical}};await profile();assert.equal(call(target).accepted,false);profilePolicy=undefined;await profile();assert.equal(call(target).accepted,false)})
 await check('certificate/transport error has no HTTP fallback/retry',async()=>{profilePolicy={version:2,endpoint:{logical_origin:logical,physical_origin:physical}};await profile();failTransport=true;const count=records.length;assert.equal((await call(target).done).error,-2);assert.equal(records.length,count);failTransport=false})
 await check('legacy HTTP logical service is unchanged',async()=>{origin.resetMemohOrigin();origin.configureMemohOrigin('http://legacy.fixture.invalid/api');assert.equal(call('http://legacy.fixture.invalid/api/users/me').accepted,false);origin.resetMemohOrigin();origin.configureMemohOrigin(base)})
 await check('changed configured API and detached document send zero bytes',async()=>{const count=records.length;alive=false;assert.equal(call(target).accepted,false);alive=true;let release;delayBody=new Promise(resolve=>release=resolve);const x=call(target,{method:'POST',body:bytes});origin.resetMemohOrigin();origin.configureMemohOrigin('https://different.invalid/api');release();delayBody=undefined;assert.equal((await x.done).error,-2);assert.equal(records.length,count)})
 console.log(JSON.stringify({passed:true,checks,streamBytes:bytes.length,sha256:hash(bytes),uploadWireBytes:multipartWire.length,uploadWireSHA256:hash(multipartWire),maxUploadRead:Math.max(...reads),logicalOriginPreserved:true,writeReplay:0,scope:'Actual ETS with controlled ArkWeb/RCP Kit boundaries and loopback HTTP; not actual ArkWeb/RCP engine, TLS, throughput or phone acceptance'}))
 }finally{bridge.reset();server.closeAllConnections();control.closeAllConnections();await new Promise(resolve=>server.close(resolve));await new Promise(resolve=>control.close(resolve))}
})().catch(error=>{console.error(error);process.exitCode=1})
