// Actual owned ArkTS modules, real UTF8 files, API-boundary adapters only.
const fs=require('node:fs'),path=require('node:path'),os=require('node:os'),vm=require('node:vm'),crypto=require('node:crypto'),assert=require('node:assert/strict')
const root=path.resolve(__dirname,'../..'),ts=require(path.join(root,'mobile/.build/memoh-web/checkout/node_modules/typescript'))
const dir=fs.mkdtempSync(path.join(os.tmpdir(),'harmony-diagnostic-')),file=path.join(dir,'client-diagnostics.jsonl'),scope='a'.repeat(64),other='b'.repeat(64),document='c'.repeat(32)
let short=0,failed=false,failSync=false
const io={OpenMode:{CREATE:fs.constants.O_CREAT,READ_WRITE:fs.constants.O_RDWR,APPEND:fs.constants.O_APPEND},
 accessSync:p=>fs.existsSync(p),statSync:p=>typeof p==='number'?fs.fstatSync(p):fs.statSync(p),readTextSync:p=>fs.readFileSync(p,'utf8'),
 openSync:(p,m)=>({fd:fs.openSync(p,m)}),closeSync:file=>fs.closeSync(typeof file==='number'?file:file.fd),renameSync:(a,b)=>fs.renameSync(a,b),unlinkSync:p=>fs.unlinkSync(p),truncateSync:(fd,n)=>fs.ftruncateSync(fd,n),
 readSync:(fd,b,o)=>fs.readSync(fd,Buffer.from(b),0,o.length,o.offset),
 writeSync:(fd,b)=>{if(failed)throw Error('controlled crash after partial write');const bytes=Buffer.from(b);if(short){const n=short;short=0;failed=true;return fs.writeSync(fd,bytes.subarray(0,n))}return fs.writeSync(fd,bytes)},
 fsyncSync:fd=>{if(failSync){failSync=false;throw Error('controlled fsync failure')}fs.fsyncSync(fd)}}
function load(name,extra={}){
 const m={exports:{}},native=path.join(root,'mobile/harmony/app/entry/src/main/ets/common',name+'.ets')
 const output=ts.transpileModule(fs.readFileSync(native,'utf8'),{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText
 const req=id=>{if(id==='@kit.CoreFileKit')return{fileIo:io};if(id==='@kit.ArkTS')return{util:{TextEncoder:class{encodeInto(s){return new Uint8Array(Buffer.from(s))}}}};if(id==='@kit.PerformanceAnalysisKit')return{hilog:{info(){},error(){}}};if(id==='@kit.CryptoArchitectureKit')return{cryptoFramework:{createMd(){const hash=crypto.createHash('sha256');return{async update(x){hash.update(x.data)},async digest(){return{data:new Uint8Array(hash.digest())}}}}}};if(extra[id])return extra[id];return{}}
 vm.runInNewContext(output,{require:req,module:m,exports:m.exports,Uint8Array,ArrayBuffer,Map,Set,Date,JSON,Number,Error},{filename:native});return m.exports
}
const batch=seq=>{const body=JSON.stringify({schema:1,document,timeUnit:'relative_monotonic_ms',events:[{seq,t:seq,kind:'load',g:1,request:1,loaded:seq}]});return{id:crypto.createHash('sha256').update(body).digest('hex'),document,through:seq,body}}
async function main(){
 let D=load('ClientDiagnostics').ClientDiagnostics;D.bind({filesDir:dir});D.event('TEST','local','utf8','本地秘密不进入回传')
 short=17;await assert.rejects(D.put(scope,batch(1)));failed=false
 assert.equal(D.capturedThrough(scope,document),0);assert.equal(D.pending(scope).length,0)
 await D.put(scope,batch(1));assert.equal(D.capturedThrough(scope,document),1);assert.equal(D.pending(scope).length,1)
 assert.ok(fs.readFileSync(file,'utf8').includes('本地秘密'));assert.ok(!JSON.stringify(D.pending(scope)).includes('秘密'))
 failSync=true;await assert.rejects(D.put(scope,batch(2)));assert.equal(D.capturedThrough(scope,document),2)
 await D.put(scope,batch(2));assert.equal(D.pending(scope).length,2)
 D=load('ClientDiagnostics').ClientDiagnostics;D.bind({filesDir:dir});assert.equal(D.pending(scope).length,2)
 const one=batch(1);assert.throws(()=>D.ack(scope,one.id,one.id,1));assert.throws(()=>D.ack(other,one.id,one.id,Buffer.byteLength(one.body)))
 short=12;assert.throws(()=>D.ack(scope,one.id,one.id,Buffer.byteLength(one.body)));failed=false;assert.equal(D.pending(scope).length,2)
 D.ack(scope,one.id,one.id,Buffer.byteLength(one.body));await D.put(scope,one);assert.equal(D.pending(scope).length,1)
 D.event('TEST','rotation','fill','x'.repeat(1048577));D.event('TEST','rotation','rotate','local only')
 assert.ok(fs.existsSync(file+'.previous'));assert.equal(D.pending(scope).length,1)
 const two=batch(2);D.ack(scope,two.id,two.id,Buffer.byteLength(two.body));assert.equal(D.pending(scope).length,0)
 D=load('ClientDiagnostics').ClientDiagnostics;D.bind({filesDir:dir});assert.equal(D.pending(scope).length,0);assert.equal(D.capturedThrough(scope,document),2)
 const bad=batch(3);bad.body=JSON.stringify({...JSON.parse(bad.body),filename:'must never persist'});bad.id=crypto.createHash('sha256').update(bad.body).digest('hex');await assert.rejects(D.put(scope,bad))
 let frame='begonia-client://app/index.html'
 class Stub{close(){}prepare(){}}
 const {NativeClient}=load('NativeClient',{'./ClientDiagnostics':{ClientDiagnostics:D},'./NativeVoice':{NativeVoice:Stub},'./NativeMediaDownload':{NativeMediaDownload:Stub},'./NativeResumableUpload':{NativeResumableUpload:Stub},'./MemohClientConfig':{MEMOH_ORIGIN:'https://example.test/api',resetMemohOrigin(){}}})
 const controller={registerJavaScriptProxy(){},getLastJavascriptProxyCallingFrameUrl:()=>frame,getUrl:()=> 'begonia-client://app/index.html',runJavaScript:async()=>'',deleteJavaScriptRegister(){}}
 const client=new NativeClient(controller,()=>{},async()=>{},{});client.register()
 const call=async value=>JSON.parse(await client.request(JSON.stringify({version:1,...value})))
 assert.equal((await call({command:'diagnostic.pending',scope})).ok,false)
 await call({command:'client.state',state:'ready'});assert.deepEqual((await call({command:'diagnostic.pending',scope})).data,[])
 assert.equal((await call({command:'diagnostic.pending',scope,filename:'forbidden'})).ok,false)
 frame='https://example.test/';assert.equal((await call({command:'diagnostic.pending',scope})).error,'NATIVE_FRAME_DENIED');client.close()
 console.log(JSON.stringify({actualNativeJournal:true,realUTF8Files:true,tornPutDoesNotAdvanceCursor:true,strictFsyncFailure:true,restartSameJournal:true,tornAckRetainsPending:true,ackHashAndSizeRequired:true,otherScopeCannotAck:true,rotationAcrossOriginalTwoFiles:true,retainedAckSuppressesOldPut:true,noRawJournalExport:true,actualReadyAndFrameGuard:true,device:false}))
}
main().catch(error=>{console.error(error);process.exitCode=1})
