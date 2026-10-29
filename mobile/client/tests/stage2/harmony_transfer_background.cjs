// Real production controller; controlled platform promises, no Harmony device claim.
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path'), vm = require('node:vm')
const root = path.resolve(__dirname, '../..'), ts = require(process.env.HARMONY_TEST_TYPESCRIPT || path.join(root, 'mobile/.build/memoh-web/checkout/node_modules/typescript'))
const source = fs.readFileSync(path.join(root, 'mobile/harmony/app/entry/src/main/ets/common/NativeTransferBackground.ets'), 'utf8')
const deferred = () => { let resolve, reject; const promise = new Promise((a,b) => { resolve=a; reject=b }); return { promise, resolve, reject } }
const tick = async () => { for(let i=0;i<20;i++) await Promise.resolve() }
const diagnostics = [], systemCalls = []
let platformCancel, cancelError
const sdk = {
 '@kit.AbilityKit': { wantAgent: { OperationType: { START_ABILITY: 1 }, WantAgentFlags: { UPDATE_PRESENT_FLAG: 4 }, async getWantAgent(info) { systemCalls.push(['want', info]);return 'target' } } },
 '@kit.BackgroundTasksKit': { backgroundTaskManager: { async startBackgroundRunning(...args) { systemCalls.push(['start', ...args]);return { notificationId:9,slotType:4,contentType:5,continuousTaskId:7 } }, async stopBackgroundRunning(...args) { systemCalls.push(['stop',...args]);platformCancel?.({id:args[1]??7,reason:0}) }, on(_name,fn) { platformCancel=fn }, off(_name,fn) { assert.equal(fn,platformCancel);platformCancel=undefined } } },
 '@kit.NotificationKit': { notificationManager: { ContentType: { NOTIFICATION_CONTENT_SYSTEM_LIVE_VIEW: 5 }, async publish(value) { systemCalls.push(['publish',value]) }, async cancel(id) { systemCalls.push(['cancel',id]);if(cancelError)throw cancelError } } },
 './ClientDiagnostics': { ClientDiagnostics: { failure(...args) { diagnostics.push(args) } } }
}
const mod={exports:{}}
vm.runInNewContext(ts.transpileModule(source,{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText,{module:mod,exports:mod.exports,require:n=>{ assert.ok(n in sdk,n);return sdk[n] },Promise,Map,Set,Number,Math,JSON,Error})
const Background=mod.exports.NativeTransferBackground
function fixture() {
 const calls=[], stops=[], gates={}, lease={notificationId:9,slotType:4,contentType:5,continuousTaskId:7};let cancel, off=0
 const platform={ async start(){ calls.push(['start']);return gates.start?await gates.start.promise:lease },async publish(l,p){calls.push(['publish',p]);if(gates.publish)await gates.publish.promise}, async stop(l,c){calls.push(['stop',l.continuousTaskId,c]);if(gates.stop)await gates.stop.promise},onCancel(fn){cancel=fn},offCancel(fn){assert.equal(fn,cancel);off++} }
 const b=new Background({},r=>stops.push(r),platform)
 return {b,calls,stops,gates,lease,cancel:(id=7)=>cancel({id,reason:0}),off:()=>off,count:name=>calls.filter(x=>x[0]===name).length}
}
async function main(){const cases=[]
 {
 const f=fixture();f.gates.start=deferred();assert.ok(f.b.begin('u'));await tick();f.b.progress('u',1,10);f.b.finish('u');f.gates.start.resolve(f.lease);await f.b.settled();assert.equal(f.count('stop'),1);assert.equal(f.count('publish'),0);assert.deepEqual(f.stops,[]);cases.push('last transfer ends during pending start: late grant released')
 }
 {
 const f=fixture();f.b.begin('u');f.b.begin('d');f.b.progress('u',2,10);await f.b.settled();assert.equal(f.count('publish'),0);f.b.progress('d',18,30);await f.b.settled();assert.deepEqual(f.calls.find(x=>x[0]==='publish'),['publish',50]);f.b.progress('d',18,30);for(let i=0;i<100;i++)await tick();assert.equal(f.count('publish'),1,'frozen progress must not manufacture heartbeat');assert.equal(f.count('start'),1);f.b.finish('u');await f.b.settled();assert.equal(f.count('stop'),0);f.b.finish('d');await f.b.settled();assert.equal(f.count('stop'),1);cases.push('shared lease, durable byte weighted progress, frozen progress has no heartbeat')
 }
 {
 const f=fixture();f.b.begin('u');f.b.progress('u',1,10);await f.b.settled();f.cancel(99);assert.equal(f.stops.length,0);f.cancel();f.cancel();await f.b.settled();assert.deepEqual(f.stops,['background-system-canceled']);assert.deepEqual(f.calls.at(-1),['stop',7,true]);assert.equal(f.count('start'),1);assert.ok(f.b.begin('explicit retry'));await f.b.settled();assert.equal(f.count('start'),2);f.b.destroy();await f.b.settled();assert.equal(f.off(),1);cases.push('matched system revoke stops once, no automatic reacquisition, explicit retry permitted')
 }
 {
 const f=fixture();f.gates.start=deferred();f.b.begin('u');await tick();f.cancel();f.gates.start.resolve(f.lease);await f.b.settled();assert.deepEqual(f.stops,['background-system-canceled']);assert.deepEqual(f.calls.at(-1),['stop',7,true]);assert.equal(f.count('publish'),0);cases.push('system cancellation arrives before start result')
 }
 {
 const f=fixture();f.gates.start=deferred();f.b.begin('u');await tick();f.b.reset();f.b.progress('u',10,10);f.gates.start.resolve(f.lease);await f.b.settled();assert.equal(f.count('publish'),0);assert.equal(f.count('stop'),1);assert.equal(f.count('start'),1);cases.push('logout during pending start leaves no grant or late notification')
 }
 {
 const f=fixture();f.b.begin('u');await f.b.settled();f.gates.publish=deferred();f.b.progress('u',1,10);await tick();f.cancel();assert.equal(f.count('stop'),0,'must wait for pending publish before cleanup');assert.equal(f.stops.length,1);f.b.begin('new');f.gates.publish.resolve();await f.b.settled();assert.deepEqual(f.calls.map(x=>x[0]),['start','publish','stop','start']);f.b.destroy();await f.b.settled();cases.push('in-flight notification settles before cancellation cleanup and next acquisition')
 }
 {
 const f=fixture();f.gates.start=deferred();f.b.begin('u');await tick();f.gates.start.reject(Error('grant denied'));await f.b.settled();assert.deepEqual(f.stops,['background-request-failed']);assert.equal(f.count('start'),1);assert.equal(f.count('stop'),0);assert.equal(f.b.begin('retry before outcome reconciliation'),false);cases.push('request failure stops I/O; uncertain acquisition forbids further grants in this owner')
 }
 {
 const f=fixture();f.b.begin('u');await f.b.settled();f.gates.publish=deferred();f.b.progress('u',1,10);await tick();f.gates.publish.reject(Error('notification denied'));await f.b.settled();assert.deepEqual(f.stops,['background-notification-failed']);assert.equal(f.count('stop'),1);cases.push('notification failure stops I/O and releases grant')
 }
 {
 const f=fixture();f.b.begin('u');await f.b.settled();f.gates.stop=deferred();f.b.finish('u');f.b.begin('new');await tick();f.gates.stop.reject(Error('uncertain service outcome'));await f.b.settled();assert.deepEqual(f.stops,['background-cleanup-unknown']);assert.equal(f.b.begin('later'),false);assert.equal(f.count('start'),1);cases.push('uncertain release blocks new grants and stops I/O')
 }
 {
 const f=fixture();f.gates.start=deferred();f.b.begin('u');await tick();f.b.destroy();f.b.destroy();f.gates.start.resolve(f.lease);await f.b.settled();assert.equal(f.off(),1);assert.equal(f.count('stop'),1);assert.equal(f.b.begin('later'),false);cases.push('ability destruction unsubscribes and cleans pending acquisition')
 }
 {
 const context={abilityInfo:{bundleName:'com.memoh.client',name:'EntryAbility'}}, b=new Background(context,()=>assert.fail('unexpected stop I/O'));b.begin('u');b.progress('u',5,10);await b.settled();const start=systemCalls.find(x=>x[0]==='start'),request=systemCalls.find(x=>x[0]==='publish')[1];assert.equal(start[1],context);assert.deepEqual(Array.from(start[2]),['dataTransfer']);assert.equal(start[3],'target');assert.equal(request.id,9);assert.equal(request.slotType,4);assert.equal(request.content.systemLiveView.typeCode,8);assert.equal(request.template.data.progressValue,50);b.finish('u');cancelError={code:1600007};await b.settled();assert.equal(b.snapshot(),'idle');assert.equal(systemCalls.find(x=>x[0]==='stop')[2],7);assert.ok(systemCalls.some(x=>x[0]==='cancel'&&x[1]===9));b.destroy();await b.settled();assert.equal(platformCancel,undefined);cases.push('real SDK adapter arguments and exact nonexistent-notification error handling')
 }
 {
 const f=fixture();f.gates.start=deferred();f.b.begin('old');await tick();f.b.finish('old');f.b.begin('new');f.gates.start.resolve(f.lease);await f.b.settled();assert.deepEqual(f.calls.map(x=>x[0]),['start','stop','start']);f.b.progress('old',9,10);await f.b.settled();assert.equal(f.count('publish'),0);f.b.destroy();await f.b.settled();cases.push('new operation during old pending acquisition waits for old grant cleanup')
 }
 {
 const stop=[];const b=new Background({},r=>stop.push(r),{onCancel(){throw Error('not authorized')},offCancel(){assert.fail('not subscribed')},start(){assert.fail('subscription failed')},publish(){assert.fail()},stop(){assert.fail()}});assert.equal(b.begin('u'),false);b.destroy();await b.settled();assert.deepEqual(stop,[]);cases.push('subscription failure blocks acquisition without changing device settings')
 }
 {
 const b=new Background({abilityInfo:{bundleName:'com.memoh.client',name:'EntryAbility'}},()=>{});cancelError={code:1600003};b.begin('u');await b.settled();b.finish('u');await b.settled();assert.equal(b.snapshot(),'cleanup-unknown');assert.equal(b.begin('new'),false);cancelError=undefined;b.destroy();await b.settled();cases.push('notification cleanup service failure remains uncertain and blocks new acquisition')
 }
 {
 const f=fixture();f.gates.start=deferred();f.b.begin('old');await tick();f.b.finish('old');f.b.begin('new');f.gates.start.reject(Error('late unknown acquisition'));await f.b.settled();assert.equal(f.count('start'),1);assert.deepEqual(f.stops,['background-request-failed']);assert.equal(f.b.begin('again'),false);cases.push('late acquisition rejection also blocks a newer queued grant')
 }
 console.log(JSON.stringify({cases,passed:cases.length,realController:true,controlledSDK:true,noTimer:true,nativeDevice:false}))
}
main().catch(e=>{console.error(e);process.exitCode=1})
