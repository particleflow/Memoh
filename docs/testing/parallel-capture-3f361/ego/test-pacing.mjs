// New pacing/stop differentials only. The 349 six-group/16-boundary suite is reused, not rerun.
import assert from 'node:assert/strict';
import vm from 'node:vm';
import {readFileSync} from 'node:fs';
import {execFileSync} from 'node:child_process';
const source=readFileSync(new URL('./capture.js',import.meta.url),'utf8');
const probe=readFileSync(new URL('./probe.js',import.meta.url),'utf8');
const oldProbe=execFileSync('git',['show','34921396db28d2105c7e21767dbcf7d3a41728de:docs/testing/parallel-capture-3f361/ego/probe.js'],{encoding:'utf8'});
const contract=JSON.parse(readFileSync(new URL('./contract-ego210.json',import.meta.url))).contract;
const base={apiRoot:'https://fixture.invalid/edge',bot:'source',contact:'contact',consumer:'fake Page with observed direct paths',contract,
  timeoutMs:1000,stopDrainMs:300,pollIntervalMs:100};
function harness(script=source) {
  const clock={now:0};class ClockDate extends Date {static now(){return clock.now;}}
  const hostTimer=(done,ms)=>{if(page.waitHook){const h=page.waitHook;page.waitHook=null;h();}
    if(page.badWait)throw new Error('fixture host timer failed');clock.now+=ms;queueMicrotask(done);};
  const realm=vm.createContext({URL,Date:ClockDate,Map,Set,Object,JSON,Error,Uint8Array,TextDecoder,encodeURIComponent,atob,AbortController,setTimeout:hostTimer,clearTimeout});
  vm.runInContext(script,realm);
  const page={reads:[],active:0,commands:[],queue:[],bodies:new Map(),GETs:[],waitHook:null,badWait:false,bodyGate:null,getGate:null,
    async cdp(method,params,options) {
      this.commands.push({method,params});assert.equal(options.timeout,1000);
      if(method==='Network.enable'||method==='Page.reload')return {};
      if(method==='Network.getResponseBody') {
        if(this.bodyGate) return this.bodyGate(params.requestId);
        return this.bodies.get(params.requestId)||{body:'{}',base64Encoded:false};
      }
      throw new Error('unexpected command');
    },
    async events() {
      assert.equal(this.active++,0);await Promise.resolve();this.reads.push(clock.now);clock.now++;
      const ready=this.queue.filter(x=>x.at<=clock.now);this.queue=this.queue.filter(x=>x.at>clock.now);this.active--;return ready.map(x=>x.event);
    },
    async evaluate(fn,arg) {
      if(arg?.delayMs!==undefined) {
        assert(arg.delayMs>0);if(this.waitHook){const h=this.waitHook;this.waitHook=null;h();}
        if(this.badWait)return {bad:true};
        // Run the actual callback in an isolated browser realm, using a virtual timer.
        const f=vm.runInNewContext('('+fn.toString()+')',{setTimeout:(done,ms)=>{clock.now+=ms;queueMicrotask(done);}});
        return await f(arg);
      }
      if(arg?.url){this.GETs.push(arg.url);if(this.getGate)return this.getGate(arg.url);throw new Error('unplanned GET');}
      return {probe:'ego-capture-shape',readyState:'complete'};
    },
    put(at,method,params){this.queue.push({at,event:{method,params,sessionId:'opaque-target'}});},
    pending(at=0,id='pending') {
      this.put(at,'Network.requestWillBeSent',{requestId:id,request:{method:'GET',url:base.apiRoot+'/bots/source/sessions/contact',headers:{}}});
      this.put(at,'Network.responseReceived',{requestId:id,response:{status:200}});
    }
  };
  return {clock,page,realm};
}
const results=[];
// Same artificial 1ms events() cost. Counts characterize source busy looping, not Ego Hz/capacity.
let redReads,greenReads;
for(const [name,script] of [['old',oldProbe],['new',probe]]) {
  const h=harness(script);await h.realm.probeEgoNetworkShape(h.page,{...base,durationMs:1000,reloadExistingPage:true});
  if(name==='old')redReads=h.page.reads.length;else greenReads=h.page.reads.length;
}
assert.equal(redReads,1000);assert(greenReads<=10);
results.push({case:'probe empty-read red/green',oldReads:redReads,newReads:greenReads,fixtureClockOnly:true});
{
  const h=harness();const r=await h.realm.createEgoCapture(h.page,base);await r.collectFor(1000);
  const s=r.summary();assert(s.stopped);assert.equal(s.stopReason,'observation_budget');
  assert(h.page.reads.length<=12);for(let i=1;i<h.page.reads.length;i++)assert(h.page.reads[i]-h.page.reads[i-1]>=base.pollIntervalMs);
  assert.equal(h.page.GETs.length,0);assert(h.page.commands.every(x=>x.method==='Network.enable'));
  results.push({case:'bounded passive collector, no busy loop or business operation',reads:s.reads,waits:s.waits});
}
{
  const h=harness();const r=await h.realm.createEgoCapture(h.page,base);await Promise.all([r.drain(),r.drain(),r.drain()]);
  for(let i=1;i<h.page.reads.length;i++)assert(h.page.reads[i]-h.page.reads[i-1]>=100);await r.stop();
  results.push({case:'concurrent callers remain serialized and paced'});
}
{
  const h=harness();h.page.pending();h.page.put(250,'Network.loadingFinished',{requestId:'pending'});
  const r=await h.realm.createEgoCapture(h.page,{...base,stopDrainMs:400});const a=await r.stop();const count=h.page.commands.length;await r.stop();
  assert.equal(h.page.commands.length,count);assert(a.some(x=>x.kind==='http_response_complete'));
  assert(!a.some(x=>x.kind==='response_unknown_on_stop'));assert.equal(r.summary().pendingResponses,0);
  results.push({case:'paced stop drains late completion and repeated stop is idempotent'});
}
{
  const h=harness();h.page.pending();const r=await h.realm.createEgoCapture(h.page,{...base,stopDrainMs:150});const out=await r.stop();
  assert(out.some(x=>x.kind==='response_unknown_on_stop'));assert.equal(r.summary().pendingResponses,1);
  assert(!h.page.commands.some(x=>/abort|cancel|disable/i.test(x.method)));assert(h.clock.now<=151);
  results.push({case:'unresolved response survives finite drain as unknown; no task cancellation'});
}
{
  const h=harness();const r=await h.realm.createEgoCapture(h.page,base);h.page.waitHook=()=>r.requestStop('writer_stop');
  await r.collectFor(10000);assert.equal(r.summary().stopReason,'writer_stop');assert(h.clock.now<10000);
  assert(!h.page.commands.some(x=>/abort|cancel|disable/i.test(x.method)));
  results.push({case:'requestStop interrupts collection after current await, not business execution'});
}
{
  const h=harness();const r=await h.realm.createEgoCapture(h.page,base);h.page.badWait=true;await r.drain();await r.stop();
  assert(r.summary().fault);assert(r.records().some(x=>x.kind==='observer_wait_unknown'));assert.equal(h.page.reads.length,1);
  results.push({case:'host timer failure fails closed without rapid-read fallback'});
}
{
  for(const interval of [undefined,0,-1,NaN,Infinity]) {
    const h=harness();await assert.rejects(h.realm.createEgoCapture(h.page,{...base,pollIntervalMs:interval}));assert.equal(h.page.commands.length,0);
  }
  results.push({case:'missing/non-finite pacing rejected before Network enable'});
}
{
  const h=harness();h.page.pending();const r=await h.realm.createEgoCapture(h.page,base);let release;
  h.page.bodyGate=()=>new Promise(resolve=>{release=resolve;});h.page.put(10,'Network.loadingFinished',{requestId:'pending'});
  const reading=r.drain();while(!release)await Promise.resolve();let stopped=false;const stopping=r.stop().then(()=>{stopped=true;});
  await Promise.resolve();assert.equal(stopped,false);release({body:'{}',base64Encoded:false});await reading;await stopping;
  assert(r.records().some(x=>x.kind==='http_response_complete'));assert(!r.records().some(x=>x.kind==='response_unknown_on_stop'));
  results.push({case:'stop awaits already-issued body command instead of discarding it'});
}
{
  const h=harness();h.page.pending();h.page.put(0,'Network.loadingFinished',{requestId:'pending'});
  h.page.put(0,'Network.webSocketCreated',{requestId:'socket',url:'wss://fixture.invalid/edge/bots/source/web/ws'});
  const r=await h.realm.createEgoCapture(h.page,base);await r.arm('A');
  h.page.put(h.clock.now,'Network.webSocketFrameSent',{requestId:'socket',response:{opcode:1,payloadData:JSON.stringify({type:'message',session_id:'contact',invocation_id:'intent-A'})}});
  let release;h.page.getGate=()=>new Promise(resolve=>{release=resolve;});const sampling=r.sample();while(!release)await Promise.resolve();
  const stopping=r.stop();release({status:200,body:{found:true,session_id:'contact',invocation_id:'intent-A',run_id:'a',turn_id:'turn-a',state:'completed'}});
  await sampling;await stopping;assert.equal(h.page.GETs.length,1);await assert.rejects(r.arm('B'));
  results.push({case:'stop awaits in-flight exact GET, prevents subsequent probes/arming'});
}
console.log(JSON.stringify({status:'PASS',scope:'new pacing/stop differentials only',oldEvidenceReused:'34921396d six groups / sixteen boundaries',
  paidCalls:0,productionRequests:0,browserLaunches:0,consumerReadback:'parent-reported turn 01a18c43-321a-764e-af5c-025dac3e5fbc; not repeated',results,
  fieldRemaining:['new pacing self-check','WS/POST mapping','failure/base64=true','retention/overflow/cross-turn guarantees','real parallel acceptance']},null,2));
