// Narrow host/page timer seam tests. No live browser, model, network or DB.
import assert from 'node:assert/strict';
import vm from 'node:vm';
import {readFileSync} from 'node:fs';
import {execFileSync} from 'node:child_process';
const path='docs/testing/parallel-capture-3f361/ego/capture.js';
const current=readFileSync(new URL('./capture.js',import.meta.url),'utf8');
const before=execFileSync('git',['show','890640078b9c52b26edcb3efec0ec851985ec90b:'+path],{encoding:'utf8'});
const contract=JSON.parse(readFileSync(new URL('./contract-ego210.json',import.meta.url))).contract;
const cfg={apiRoot:'https://fixture.invalid',bot:'source',contact:'C',consumer:'fake host/Page',contract,
  timeoutMs:10000,pollIntervalMs:250,stopDrainMs:600};
function harness(source=current,{timer=true}={}) {
  const clock={now:0},metrics={hostWaits:[],pageEvaluations:0,reads:[],commands:[]};
  let onWait=null,timerError=false;const queue=[];
  const globals={URL,Date:{now:()=>clock.now},Map,Set,Object,JSON,Error,Uint8Array,TextDecoder,encodeURIComponent};
  if(timer)globals.setTimeout=(resolve,ms)=>{metrics.hostWaits.push(ms);if(timerError)throw new Error('fixture host timer error');
    if(onWait){const fn=onWait;onWait=null;fn();}clock.now+=ms;queueMicrotask(resolve);};
  const realm=vm.createContext(globals);vm.runInContext(source,realm);
  const page={
    async cdp(method,params){metrics.commands.push(method);if(method==='Network.getResponseBody')return {body:'{}',base64Encoded:false};return {};},
    async events(){metrics.reads.push(clock.now);const due=queue.filter(x=>x.at<=clock.now);for(const x of due)queue.splice(queue.indexOf(x),1);return due.map(x=>x.event);},
    async evaluate(){metrics.pageEvaluations++;const e=new Error('fixture Page unavailable during navigation');e.name='PageEvaluationTimeoutError';e.code='EGO_PAGE_EVALUATION_TIMED_OUT';throw e;}
  };
  const put=(at,method,params)=>queue.push({at,event:{method,params,sessionId:'fixture-page'}});
  const pending=()=>{put(0,'Network.requestWillBeSent',{requestId:'pending',request:{url:cfg.apiRoot+'/bots/source/sessions/C',method:'GET'}});put(0,'Network.responseReceived',{requestId:'pending',response:{status:200}});};
  return {clock,metrics,page,put,pending,create:()=>realm.createEgoCapture(page,cfg),onWait:fn=>{onWait=fn;},failTimer:()=>{timerError=true;}};
}
const results=[];
{
  const old=harness(before);const a=await old.create();await old.page.cdp('Page.reload',{});await a.drain();
  assert.equal(a.summary().fault,true);assert.equal(old.metrics.pageEvaluations,1);assert.equal(old.metrics.reads.length,1);await a.stop();
  const next=harness();const b=await next.create();await next.page.cdp('Page.reload',{});await b.collectFor(1000);
  assert.equal(b.summary().fault,false);assert.equal(b.summary().stopped,true);assert.equal(next.metrics.pageEvaluations,0);
  assert(next.metrics.hostWaits.length>0);assert(next.metrics.reads.length>1);assert.equal(b.summary().pacingRealm,'node-host');
  results.push({case:'same unavailable Page: old pacing fails, host pacing progresses',oldPageEvaluations:1,newPageEvaluations:0,fixtureOnly:true});
}
{
  const h=harness();const r=await h.create();await Promise.all([r.drain(),r.drain(),r.drain()]);await r.stop();
  for(let i=1;i<h.metrics.reads.length;i++)assert(h.metrics.reads[i]-h.metrics.reads[i-1]>=250);
  assert.equal(h.metrics.pageEvaluations,0);results.push({case:'concurrent drain callers retain serialized minimum interval'});
}
{
  const h=harness();h.pending();h.put(500,'Network.loadingFinished',{requestId:'pending'});
  const r=await h.create();const records=await r.stop();assert(records.some(x=>x.kind==='http_response_complete'));
  assert.equal(r.summary().pendingResponses,0);assert.equal(h.metrics.pageEvaluations,0);
  const n=h.metrics.commands.length;await r.stop();assert.equal(h.metrics.commands.length,n);
  results.push({case:'final drain captures delayed body using host timer; repeated stop idempotent'});
}
{
  const h=harness();h.pending();const r=await h.create();h.onWait(()=>r.requestStop('writer_stop'));
  const records=await r.collectFor(10000);assert.equal(r.summary().stopReason,'writer_stop');assert(h.clock.now<10000);
  assert(records.some(x=>x.kind==='response_unknown_on_stop'));assert.equal(r.summary().pendingResponses,1);
  assert(!h.metrics.commands.some(x=>/cancel|abort|disable/i.test(x)));assert.equal(h.metrics.pageEvaluations,0);
  results.push({case:'stop during host wait preserves pending response unknown without business cancellation'});
}
{
  const h=harness(current,{timer:false});await assert.rejects(h.create(),/Node host timer required/);
  assert.equal(h.metrics.commands.length,0);assert.equal(h.metrics.reads.length,0);
  results.push({case:'missing host timer rejects before Network enable; no Page fallback'});
}
{
  const h=harness();const r=await h.create();h.failTimer();await r.drain();await r.stop();
  assert.equal(r.summary().fault,true);assert(r.records().some(x=>x.kind==='observer_wait_unknown'));
  assert.equal(h.metrics.reads.length,1);assert.equal(h.metrics.pageEvaluations,0);
  results.push({case:'host timer failure remains unknown and does not fall back or spin'});
}
{
  const h=harness();h.pending();h.put(0,'Network.loadingFinished',{requestId:'pending'});
  h.put(0,'Network.webSocketCreated',{requestId:'socket',url:'wss://fixture.invalid/bots/source/web/ws'});
  const r=await h.create();await r.arm('A');h.put(h.clock.now,'Network.webSocketFrameSent',{requestId:'socket',response:{opcode:1,payloadData:JSON.stringify({type:'message',session_id:'C',invocation_id:'original-A'})}});
  await r.sample();await r.stop();assert.equal(h.metrics.pageEvaluations,1);
  assert(r.records().some(x=>x.kind==='probe_end'&&x.errorCode==='EGO_PAGE_EVALUATION_TIMED_OUT'));
  assert(r.records().some(x=>x.kind==='probe_blocked'));assert(!r.records().some(x=>x.kind==='overlap_candidate'));
  results.push({case:'actual Page data GET failure remains explicit; host wait fix does not fake business success'});
}
console.log(JSON.stringify({status:'PASS',scope:'seven new host/page timer seam checks only',results,
  oldSuitesRerun:false,liveRequests:0,paidCalls:0,sourcePattern:'a7665a65d1244ab4a41497e2b2b48bd8ec37dc6b capture-initial-history.mjs host setTimeout',
  fieldEvidence:'parent reports a766 host wait executed without Page timer timeout; modified collector not field-executed'},null,2));
