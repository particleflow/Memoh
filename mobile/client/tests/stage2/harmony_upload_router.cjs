// Actual native command dispatch and SharedChat's resource handler. Kit/ArkWeb
// boundaries are controlled here; disk/HTTP lives in harmony_resumable.cjs.
const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm'),assert=require('node:assert/strict')
const root=path.resolve(__dirname,'../..'),ts=require(process.env.HARMONY_TEST_TYPESCRIPT||path.join(root,'mobile/.build/memoh-web/checkout/node_modules/typescript'))
function load(code,imports={},globals={}){const m={exports:{}};vm.runInNewContext(ts.transpileModule(code,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText,{module:m,exports:m.exports,require:n=>{if(n==='./NativeTransferOwner')return load(file('common/NativeTransferOwner.ets'),imports);assert.ok(n in imports,n);return imports[n]},JSON,Error,ArrayBuffer,...globals});return m.exports}
const file=name=>fs.readFileSync(path.join(root,'mobile/harmony/app/entry/src/main/ets/'+name),'utf8')
const calls=[]
class Uploads {
 constructor(_context,auth){this.auth=auth}
 async select(q){assert.ok(this.auth());calls.push(['select',q]);return[{handle:'a'.repeat(32),scope:q.scope}]}
 async stageBegin(q,name,type,size){assert.ok(this.auth());calls.push(['stage.begin',q,name,type,size]);return{handle:'a'.repeat(32)}}
 async stageAppend(op,auth,offset,data){assert.ok(this.auth());calls.push(['stage.append',op,auth,offset,data]);return{offset:offset+1}}
 async stageFinish(op,auth){assert.ok(this.auth());calls.push(['stage.finish',op,auth]);return{handle:'a'.repeat(32),scope:'session:one'}}
 async list(q){assert.ok(this.auth());calls.push(['list',q]);return[]}
 async upload(q){assert.ok(this.auth());calls.push(['run',q]);return{content_hash:'b'.repeat(64),size_bytes:8,mime:'image/png'}}
 async consume(q){assert.ok(this.auth());calls.push(['consume',q])}
 async discard(q){assert.ok(this.auth());calls.push(['discard',q])}
 detachView(){} releasePreviews(){}
 pause(op){calls.push(['pause',op])} abort(op){calls.push(['abort',op])} close(){calls.push(['close'])}
 preview(handle){assert.ok(this.auth());assert.equal(handle,'a'.repeat(32));return{fd:42,mime:'image/png'}}
}
const config={MEMOH_ORIGIN:'',configureMemohOrigin:v=>{config.MEMOH_ORIGIN=v},resetMemohOrigin:()=>{config.MEMOH_ORIGIN=''}}
class Voice{close(){}prepare(){}}class Downloads{close(){}}
const {NativeClient}=load(file('common/NativeClient.ets'),{'./NativeVoice':{NativeVoice:Voice},'./NativeMediaDownload':{NativeMediaDownload:Downloads},'./NativeResumableUpload':{NativeResumableUpload:Uploads},'./MemohClientConfig':config})
let frame='begonia-client://app/index.html'
const client=new NativeClient({registerJavaScriptProxy(){},getLastJavascriptProxyCallingFrameUrl:()=>frame,getUrl:()=>frame},()=>{},async()=>{},{})
const request=(command,fields={})=>client.request(JSON.stringify({version:1,command,...fields})).then(JSON.parse)
class Response {setResponseCode(v){this.status=v}setReasonMessage(){}setResponseMimeType(v){this.mime=v}setResponseEncoding(){}setResponseHeader(v){this.headers=v}setResponseData(v){this.data=v}setResponseIsReady(){}}
const source=file('pages/SharedChat.ets'),start=source.indexOf('  private intercept('),end=source.indexOf('  private pageEnded(',start)
assert.ok(start>0&&end>start)
const {ResourceHost}=load('export class ResourceHost { active=true; chatReady=true; nativeClient: any; response(code:number){return {status:code}}\n'+source.slice(start,end)+'}',{}, {WebResourceResponse:Response,packagedPath:()=>'',memohRequest:()=>false,viewerRequest:()=>false})
;(async()=>{
 client.register();const q={operation:'c'.repeat(32),botId:'bot-a',scope:'session:one',authorization:'Bearer synthetic',photos:true,handle:'a'.repeat(32)}
 assert.equal((await request('upload.run',q)).ok,false)
 await request('client.configure',{apiOrigin:'https://example.test/api'});await request('client.state',{state:'ready'})
 for(const command of ['select','list','run','consume','discard'])assert.equal((await request('upload.'+command,q)).ok,true)
 assert.equal((await request('upload.stage.begin',{...q,name:'paste.png',type:'image/png',size:8})).data.handle,q.handle)
 assert.equal((await request('upload.stage.append',{operation:q.operation,authorization:q.authorization,offset:0,data:'YQ=='})).data.offset,1)
 assert.equal((await request('upload.stage.finish',{operation:q.operation,authorization:q.authorization})).data.handle,q.handle)
 assert.equal((await request('upload.stage.begin',{...q,name:'paste.png',type:'image/png',size:'8'})).error,'NATIVE_REQUEST_INVALID')
 assert.equal((await request('upload.stage.append',{operation:q.operation,offset:0,data:'YQ=='})).error,'NATIVE_REQUEST_INVALID')
 assert.equal((await request('upload.stage.finish',{operation:q.operation})).error,'NATIVE_REQUEST_INVALID')
 await request('upload.pause',{operation:q.operation});assert.ok(calls.some(x=>x[0]==='pause'))
 assert.equal((await request('upload.run',{...q,scope:undefined})).ok,false)
 frame='https://untrusted.example/';assert.equal((await request('upload.run',q)).error,'NATIVE_FRAME_DENIED');frame='begonia-client://app/index.html'
 const host=new ResourceHost();host.nativeClient=client
 const read=(url,method='GET',main=false)=>host.intercept({getRequestUrl:()=>url,getRequestMethod:()=>method,isMainFrame:()=>main})
 const preview=read('begonia-client://app/upload/'+q.handle);assert.equal(preview.status,200);assert.equal(preview.data,42);assert.equal(preview.mime,'image/png');assert.ok(preview.headers.some(h=>h.headerKey==='Cache-Control'&&h.headerValue==='no-store'))
 for(const url of ['begonia-client://app/upload/../private','begonia-client://app/upload/'+q.handle+'?token=x','file:///private','begonia-client://app/upload/%61'])assert.equal(read(url).status,403)
 assert.equal(read('begonia-client://app/upload/'+q.handle,'POST').status,403);assert.equal(read('begonia-client://app/upload/'+q.handle,'GET',true).status,403)
 host.chatReady=false;assert.equal(read('begonia-client://app/upload/'+q.handle).status,403)
 await request('client.state',{state:'login-page'});assert.equal((await request('upload.run',q)).ok,false);assert.equal((await request('upload.close')).ok,true)
 console.log(JSON.stringify({actualNativeRouter:true,stagingCommandsWithSameOperation:true,stagingArgumentValidation:true,actualSharedChatIntercept:true,frameAndReadyGates:true,scopeRequired:true,fdPreviewNoWebBytes:true,previewPathsRestricted:true,pauseAndDiscardDistinct:true,nativeDevice:false}))
})().catch(e=>{console.error(e);process.exitCode=1})
