// Actual ArkTS socket bridge and the real overlay shim, driven through the
// real NativeClient command dispatch and the real garnet CustomEvent channel
// (the native push script is evaluated inside the shim realm). Kit/webSocket
// boundaries are controlled; no OS netstack, TLS or phone.
const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm'),assert=require('node:assert/strict')
const root=path.resolve(__dirname,'../..'),ts=require(process.env.HARMONY_TEST_TYPESCRIPT||path.join(root,'mobile/.build/memoh-web/checkout/node_modules/typescript'))
const common='mobile/harmony/app/entry/src/main/ets/common/'
const connects=[],sends=[],closed=[],diagnostics=[],sockets=[]
let openRefusal=false
const webSocket={createWebSocket(){
 const socket={handlers:{},
  on(type,cb){this.handlers[type]=cb},off(type){delete this.handlers[type]},
  connect(url,options){connects.push({url,options});
   return openRefusal?Promise.reject(Object.assign(Error('controlled connect failure'),{code:1001})):Promise.resolve(true)},
  send(data){sends.push(data);return Promise.resolve(true)},
  close(){closed.push(socket);socket.handlers.close(null,{code:1000,reason:''});return Promise.resolve(true)}}
 sockets.push(socket);return socket}}
const config={MEMOH_ORIGIN:'https://nas.example/api',resetMemohOrigin(){},configureMemohOrigin(){}}
function load(file,imports={}){const m={exports:{}}
 vm.runInNewContext(ts.transpileModule(fs.readFileSync(path.join(root,file),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText,
  {module:m,exports:m.exports,require:n=>{if(n==='./NativeTransferOwner')return load(common+'NativeTransferOwner.ets',imports);assert.ok(n in imports,n);return imports[n]},console,URL});return m.exports}
const diagnosticsModule={ClientDiagnostics:{event:(area,component,phase,detail)=>diagnostics.push({area,component,phase,detail})}}
const policy=load(common+'SharedWebPolicy.ets',{'@kit.ArkWeb':{webview:{}},'@kit.ArkTS':{url:{URL:{parseURL:v=>new URL(v)}}},'./WebResourceManifest':{WEB_RESOURCES:{}},'./MemohClientConfig':config})
const chatSocketModule=load(common+'NativeChatSocket.ets',{'@kit.NetworkKit':{webSocket},'@kit.ArkTS':{util:{Base64Helper:class{
 encodeToStringSync(bytes){return Buffer.from(bytes).toString('base64')}
 decodeSync(text){return new Uint8Array(Buffer.from(text,'base64'))}}}},'./MemohClientConfig':config,'./SharedWebPolicy':policy,'./ClientDiagnostics':diagnosticsModule})
class Voice{close(){}prepare(){}}class Media{close(){}detachView(){}releasePreviews(){}}
const {NativeClient}=load(common+'NativeClient.ets',{'./NativeVoice':{NativeVoice:Voice},'./NativeMediaDownload':{NativeMediaDownload:Media},'./NativeResumableUpload':{NativeResumableUpload:Media},'./MemohClientConfig':config,'./NativeChatSocket':chatSocketModule,'./NetReport':{NetReport:{report(){},flush(){}}},'./ClientDiagnostics':diagnosticsModule})
// The shim realm carries the DOM constructors the overlay uses.
class RealmEvent{constructor(type,init){this.type=type;Object.assign(this,init||{})}}
class RealmCustomEvent extends RealmEvent{constructor(type,init){super(type,init);this.detail=init&&init.detail}}
class RealmCloseEvent extends RealmEvent{constructor(type,init){super(type,init);this.code=(init&&init.code)||0;this.reason=(init&&init.reason)||''}}
class RealmMessageEvent extends RealmEvent{constructor(type,init){super(type,init);this.data=init&&init.data}}
class RealmDOMException extends Error{constructor(message,name){super(message);this.name=name}}
class RealmEventTarget{constructor(){this.realmListeners=new Map()}
 addEventListener(type,listener){const all=this.realmListeners.get(type)||[];all.push(listener);this.realmListeners.set(type,all)}
 dispatchEvent(event){for(const listener of this.realmListeners.get(event.type)||[])listener.call(this,event);return true}}
const windowRealm={listeners:new Map(),addEventListener(type,listener){const all=this.listeners.get(type)||[];all.push(listener);this.listeners.set(type,all)},dispatchEvent(event){for(const listener of this.listeners.get(event.type)||[])listener.call(this,event)}}
const realm={module:{exports:{}},Event:RealmEvent,CustomEvent:RealmCustomEvent,CloseEvent:RealmCloseEvent,MessageEvent:RealmMessageEvent,DOMException:RealmDOMException,EventTarget:RealmEventTarget,
 btoa:text=>Buffer.from(text,'binary').toString('base64'),atob:text=>Buffer.from(text,'base64').toString('binary'),
 URL,Math,Date,JSON,Promise,Map,console,window:windowRealm,ArrayBuffer,Uint8Array,Blob}
realm.exports=realm.module.exports
const frame='begonia-client://app/index.html'
const controller={registerJavaScriptProxy(){},deleteJavaScriptRegister(){},getUrl:()=>frame,getLastJavascriptProxyCallingFrameUrl:()=>frame,
 runJavaScript:async()=>{return ''}}
// The native push script really executes in the shim realm: the whole
// runJavaScript CustomEvent channel is exercised, not simulated.
function realmEval(script){vm.runInNewContext(script,realm)}
controller.runJavaScript=async script=>{realmEval(script);return ''}
const client=new NativeClient(controller,()=>{},async()=>{},{},()=>{},authorization=>authorization==='Bearer token-x')
client.register()
windowRealm.begoniaNative={request:raw=>client.request(raw)}
vm.runInNewContext(ts.transpileModule(fs.readFileSync(path.join(root,'mobile/memoh/harmony-ws.ts'),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText,realm)
const shim=realm.module.exports
const settle=()=>new Promise(resolve=>setImmediate(resolve))
const wait=async predicate=>{for(let i=0;i<100&&!predicate();i++)await settle();assert.ok(predicate(),'condition not reached')}
const url='wss://nas.example/api/bots/bot-a/web/ws?token=token-x'
let checks=0
async function check(name,fn){await fn();checks++;console.log('ok -',name)}
;(async()=>{
 await check('without the native bridge nothing is installed',async()=>{
  const bareWindow={listeners:new Map(),addEventListener(){},dispatchEvent(){}}
  const bareRealm={...realm,module:{exports:{}},window:bareWindow}
  bareRealm.exports=bareRealm.module.exports
  vm.runInNewContext(ts.transpileModule(fs.readFileSync(path.join(root,'mobile/memoh/harmony-ws.ts'),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText,bareRealm)
  bareRealm.module.exports.installNativeWebSocket()
  assert.equal(bareWindow.WebSocket,undefined)
 })
 shim.installNativeWebSocket()
 const NativeWebSocket=windowRealm.WebSocket
 assert.ok(NativeWebSocket)
 await check('open rides the native bridge with the session bearer',async()=>{
  const probe=new NativeWebSocket(url)
  assert.equal(probe.readyState,NativeWebSocket.CONNECTING)
  await wait(()=>connects.length===1)
  assert.equal(connects[0].url,url)
  assert.equal(JSON.stringify(connects[0].options.header),JSON.stringify({Authorization:'Bearer token-x'}))
  sockets[0].handlers.open(null,{})
  await wait(()=>probe.readyState===NativeWebSocket.OPEN)
 })
 const socket=new NativeWebSocket(url)
 await wait(()=>connects.length===2)
 sockets[1].handlers.open(null,{})
 await wait(()=>socket.readyState===NativeWebSocket.OPEN)
 await check('text and binary frames round-trip in both directions',async()=>{
  const received=[]
  socket.onmessage=event=>received.push(event.data)
  const tricky='quote " backslash \\ newline\nunicode ✓结束'
  sockets[1].handlers.message(null,tricky)
  sockets[1].handlers.message(null,new Uint8Array([0,1,2,250,255]).buffer)
  await wait(()=>received.length===2)
  assert.equal(received[0],tricky)
  assert.ok(received[1] instanceof ArrayBuffer)
  assert.deepEqual([...new Uint8Array(received[1])],[0,1,2,250,255])
  socket.send('plain wire frame')
  socket.send(new Uint8Array([9,8,7]))
  await wait(()=>sends.length===2)
  assert.equal(sends[0],'plain wire frame')
  assert.ok(sends[1] instanceof ArrayBuffer)
  assert.deepEqual([...new Uint8Array(sends[1])],[9,8,7])
 })
 await check('server close maps code and state',async()=>{
  let closeEvent=null
  socket.onclose=event=>{closeEvent=event}
  sockets[1].handlers.close(null,{code:1000,reason:''})
  await wait(()=>socket.readyState===NativeWebSocket.CLOSED)
  assert.equal(closeEvent&&closeEvent.code,1000)
 })
 await check('rejected session surfaces error then close 1006',async()=>{
  openRefusal=true
  const refused=new NativeWebSocket('wss://nas.example/api/bots/bot-a/web/ws?token=other')
  const seen=[]
  refused.onerror=()=>seen.push('error')
  refused.onclose=event=>seen.push('close:'+event.code)
  await wait(()=>refused.readyState===NativeWebSocket.CLOSED)
  assert.deepEqual(seen,['error','close:1006'])
  assert.ok(diagnostics.some(entry=>entry.component==='chat-socket'&&entry.phase==='open-rejected'))
  openRefusal=false
 })
 await check('sending before open is refused like the browser',async()=>{
  const pending=new NativeWebSocket(url)
  assert.throws(()=>pending.send('early'),/WebSocket is not open/)
 })
 await check('pagehide closes every live native socket',async()=>{
  const live=new NativeWebSocket(url)
  const native=sockets[sockets.length-1]
  native.handlers.open(null,{})
  await wait(()=>live.readyState===NativeWebSocket.OPEN)
  const before=closed.length
  windowRealm.dispatchEvent({type:'pagehide'})
  await wait(()=>closed.length>before)
 })
 console.log(JSON.stringify({actualArkTsBridge:true,actualOverlayShim:true,realCommandDispatch:true,realPushChannel:true,nativeDevice:false,checks}))
})().catch(error=>{console.error(error);process.exitCode=1})
