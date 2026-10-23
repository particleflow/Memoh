// Current production main/worker lifecycle at controlled SDK boundaries. Not native ASR evidence.
const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm'),assert=require('node:assert/strict');
const {Worker}=require('node:worker_threads');
const root=path.resolve(__dirname,'../../..');
const ts=require(process.env.HARMONY_TEST_TYPESCRIPT || path.join(root,'validation/harmony/.build/commandline-tools/command-line-tools/sdk/default/openharmony/ets/build-tools/ets-loader/node_modules/typescript'));
const base=path.join(root,'mobile/harmony/app/entry/src/main/ets');
const compile=file=>ts.transpileModule(fs.readFileSync(file,'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2020}}).outputText;
function load(file,imports={},globals={}){const module={exports:{}};vm.runInNewContext(compile(file),{exports:module.exports,module,require:n=>{assert(n in imports,'unexpected import '+n);return imports[n];},setTimeout,clearTimeout,Promise,Error,Date,Uint8Array,Map,...globals},{filename:file});return module.exports;}
const protocol=load(path.join(base,'common/VoiceTextProtocol.ets'));
const diagnostics=[],threads=new Set(),sdkState=new Int32Array(new SharedArrayBuffer(12));
const workerCode=compile(path.join(base,'workers/VoiceRecognition.ets'));
// SDK cancel deliberately blocks only this real background thread. Atomic count rejects overlapping engines.
const workerBootstrap=`
const {parentPort,workerData}=require('node:worker_threads'),vm=require('node:vm');
const active=new Int32Array(workerData.state);let receive;
const port={postMessage:v=>parentPort.postMessage(v),close:()=>parentPort.close()};
Object.defineProperty(port,'onmessage',{set:f=>receive=f});parentPort.on('message',v=>receive({data:v}));
const sdk={createEngine:async()=>{if(Atomics.add(active,0,1)!==0){Atomics.add(active,0,-1);throw Object.assign(Error('private SDK overlap'),{code:123});}Atomics.add(active,1,1);return{
setListener(f){this.listener=f;},isBusy(){return !!this.busy;},startListening(p){if(this.busy)throw Error('start before cancel completed');this.busy=true;this.id=p.sessionId;this.listener.onStart(this.id,'private SDK message');},
writeAudio(id,b){if(b[0]===99)throw Object.assign(Error('private path /secret/audio'),{code:1002200010});this.listener.onResult(id,{result:b[0]===42?'真实文字':'',isFinal:false,isLast:false});},
finish(id){setTimeout(()=>{if(this.id===id)this.busy=false;this.listener.onComplete(id,'private completion');},40);},
cancel(){Atomics.add(active,2,1);const at=Date.now();while(Date.now()-at<180){}this.busy=false;},shutdown(){Atomics.sub(active,0,1);}
};}};
const m={exports:{}};vm.runInNewContext(workerData.code,{exports:m.exports,module:m,require:n=>{if(n==='@kit.CoreSpeechKit')return{speechRecognizer:sdk};if(n==='@kit.ArkTS')return{worker:{workerPort:port}};if(n==='../common/VoiceTextProtocol')return workerData.protocol;throw Error(n);},Date,Promise,Error,Uint8Array,setTimeout,clearTimeout});
`;
class ThreadWorker{constructor(){this.native=new Worker(workerBootstrap,{eval:true,workerData:{code:workerCode,protocol,state:sdkState.buffer}});threads.add(this);this.native.on('message',v=>this.onmessage?.({data:v}));this.native.on('error',e=>this.onAllErrors?.(e));this.native.on('exit',n=>{threads.delete(this);this.onexit?.(n);});}postMessage(v){this.native.postMessage(v);}terminate(){return this.native.terminate();}}
const ClientDiagnostics={event:(area,component,phase,detail)=>diagnostics.push({area,component,phase,detail}),failure:(_a,_c,phase,e)=>diagnostics.push({phase,detail:/^VOICE_TEXT_[A-Z0-9_]+$/.test(e.message)?e.message:'SDK_ERROR'})};
const main=load(path.join(base,'common/VoiceText.ets'),{'@kit.ArkTS':{worker:{ThreadWorker}},'./ClientDiagnostics':{ClientDiagnostics},'./VoiceTextProtocol':protocol});
const wait=async pred=>{const at=Date.now();while(!pred()){assert(Date.now()-at<4000,'controlled worker deadline');await new Promise(r=>setTimeout(r,5));}};
(async()=>{
 const service=new main.VoiceText(),id=n=>'dictation-'+String(n).padStart(32,'0');
 await service.prepare();await service.start(id(1),'zh-CN',35000);
 const pcm=new Uint8Array(1280);pcm[0]=42;service.write(id(1),pcm);await wait(()=>service.snapshot(id(1))==='真实文字');
 const pending=service.finish(id(1)).then(()=>assert.fail('cancelled finish resolved'),e=>e.message);
 await wait(()=>diagnostics.some(d=>d.phase==='finish-requested'));
 const at=performance.now();service.cancel(id(1));assert(performance.now()-at<50,'SDK cancel blocked main thread');
 const timerAt=performance.now();await new Promise(r=>setTimeout(r,20));assert(performance.now()-timerAt<100,'SDK busy cancel blocked main event loop');
 const next=service.start(id(2),'zh-CN',35000);assert.equal(await pending,'VOICE_TEXT_CANCELLED');assert.equal(await next,id(2));
 assert.equal(Atomics.load(sdkState,0),1,'cached SDK engine missing');assert.equal(Atomics.load(sdkState,1),1,'cancel recreated the engine');service.cancel(id(1));service.write(id(2),pcm);await wait(()=>service.snapshot(id(2))==='真实文字');
 assert.equal(await service.finish(id(2)),'真实文字');await service.prepare();
 await service.start(id(3),'zh-CN',20000);assert.equal(Atomics.load(sdkState,2),1,'completed recognition was needlessly cancelled');service.write(id(3),new Uint8Array(640));assert.equal(await service.finish(id(3)),'','silence became text');
 await service.start(id(4),'zh-CN',20000);assert.throws(()=>service.write(id(4),new Uint8Array(1)),/VOICE_TEXT_AUDIO_INVALID/);
 const bad=new Uint8Array(640);bad[0]=99;service.write(id(4),bad);await wait(()=>diagnostics.some(d=>d.phase==='write-failed'&&d.detail.includes('1002200010')));
 assert.throws(()=>service.snapshot(id(4)),/VOICE_TEXT_FAILED/);service.cancel(id(4));
 // Cancel while the next start is queued behind SDK cancellation: no stale session or partial survives.
 const cancelled=service.start(id(5),'zh-CN',20000).then(()=>assert.fail('cancelled waiting start resolved'),e=>e.message);service.cancel(id(5));assert.equal(await cancelled,'VOICE_TEXT_CANCELLED');
 await service.start(id(6),'zh-CN',20000);service.close();assert.equal(service.snapshot(id(6)),'');assert.throws(()=>service.write(id(6),pcm));
 service.close();await wait(()=>threads.size===0);assert.equal(Atomics.load(sdkState,0),0);assert.equal(Atomics.load(sdkState,1),1,'normal finish/cancel must reuse the warmed engine');
 assert(!JSON.stringify(diagnostics).includes('真实文字'));assert(!JSON.stringify(diagnostics).includes('/secret/'));

 console.log('PASS actual production main/background worker: synchronous SDK cancellation stays off the main loop and precedes the next start; six sessions reuse one warmed engine; stale finish/cancel preserve next identity; PCM/partial/final/silence/error/close; shutdown only at lifecycle end. Controlled boundary, not native ASR speed.');
})().catch(async e=>{console.error(e.stack);process.exitCode=1;await Promise.all([...threads].map(t=>t.terminate()));});
