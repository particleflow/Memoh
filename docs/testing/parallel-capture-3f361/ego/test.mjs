// Controlled contract fixtures only. No browser, endpoint, model, app or DB.
import assert from 'node:assert/strict';
import vm from 'node:vm';
import {readFileSync} from 'node:fs';
const context = vm.createContext({URL, Date, Map, Set, Object, JSON, Error, Uint8Array, TextDecoder,
  encodeURIComponent, atob, AbortController, setTimeout, clearTimeout});
vm.runInContext(readFileSync(new URL('./capture.js', import.meta.url), 'utf8'), context);
vm.runInContext(readFileSync(new URL('./probe.js', import.meta.url), 'utf8'), context);
const cfgBase={apiRoot:'https://fixture.invalid/edge',bot:'source',contact:'contact',timeoutMs:1000,stopDrainMs:50,consumer:'fixture-only'};
const prefix='/edge/bots/source/sessions/contact';
const view={delegation_id:'delegation',source_session_id:'contact',target_bot_id:'expert',target_session_id:'expert-session',target_run_id:'worker'};
const secret='fixture-sensitive-credential';
class PageFixture {
  constructor(nested=false) {this.nested=nested;this.batches=[];this.bodies=new Map();this.commands=[];this.fetches=[];this.reads=0;this.activeReads=0;this.worker='running';this.wrongWorker=false;this.denied=false;this.finished=new Set();}
  wrap(v) {return this.nested?{result:v}:v;}
  event(method,params) {return this.nested?{event:{method,params}}:{method,params};}
  push(...events) {this.batches.push(events);}
  async events() {
    assert.equal(this.activeReads++,0,'destructive events reads must be serialized');
    await Promise.resolve(); const events=this.batches.shift()||[];this.reads++;
    for(const e of events) {const p=this.nested?e.event:e;if(p.method==='Network.loadingFinished')this.finished.add(p.params.requestId);}
    this.activeReads--;return events;
  }
  async cdp(method,params,options) {
    assert.equal(options.timeout,1000);this.commands.push({method,params});
    if(method==='Network.enable') {assert.equal(JSON.stringify(params),'{}');return this.wrap({});}
    if(method==='Page.reload') {this.seed();return this.wrap({});}
    if(method==='Network.getResponseBody') {
      assert(this.finished.has(params.requestId),'body read before loadingFinished');
      const b=this.bodies.get(params.requestId);if(b instanceof Error)throw b;
      if(typeof b==='function')return this.wrap(await b());
      assert(b,'fixture body missing');return this.wrap(b);
    }
    if(method==='Network.getRequestPostData')return this.wrap({postData:JSON.stringify({invocation_id:'B-request',text:'same text'})});
    throw new Error('unexpected CDP command');
  }
  async evaluate(fn,arg) {
    // Isolated browser realm catches accidental capture of Node/module variables.
    const fetch=async(url,options)=>{
      assert.equal(options.method,'GET');this.fetches.push(url);
      const u=new URL(url);let body,status=200;
      if(u.pathname.includes('/invocations/')) {
        const inv=decodeURIComponent(u.pathname.split('/').at(-1));
        if(inv==='A-request'&&this.denied){status=403;body={code:'denied'};}
        else body={found:true,session_id:'contact',invocation_id:inv,run_id:inv==='A-request'?'a-run':'b-run',turn_id:inv==='A-request'?'a-turn':'b-turn',state:'completed'};
      } else if(u.pathname.endsWith('/messages')) body={items:[
        {turn_id:'old-turn',messages:[{output:{...view,delegation_id:'wrong-old'}}]},
        {turn_id:'a-turn',role:'assistant',text:'Accepted'},
        {turn_id:'b-turn',role:'assistant',text:'A substantive answer',token:secret,link:'https://private.invalid/a'}]};
      else if(u.pathname.endsWith('/delegations/delegation'))body={...view,target_run_id:this.wrongWorker?'wrong':view.target_run_id,target_run_state:this.worker};
      else throw new Error('unplanned GET');
      return {status,text:async()=>JSON.stringify(body)};
    };
    const f=vm.runInNewContext('('+fn.toString()+')',{fetch,AbortController,setTimeout,clearTimeout,Uint8Array,TextDecoder,atob,document:{readyState:'complete'}});
    return this.wrap(await f(arg));
  }
  seed() {
    this.bodies.set('boot',{body:'{"ok":true}',base64Encoded:false});
    this.push(this.event('Network.requestWillBeSent',{requestId:'boot',request:{url:cfgBase.apiRoot+'/bots/source/sessions/contact',method:'GET',headers:{Authorization:'Bearer '+secret}}}),
      this.event('Network.responseReceived',{requestId:'boot',response:{status:200}}),
      this.event('Network.loadingFinished',{requestId:'boot'}),
      this.event('Network.webSocketCreated',{requestId:'socket',url:'wss://fixture.invalid/edge/bots/source/web/ws?token='+secret}));
  }
}
function cfg(nested=false) {return {...cfgBase,contract:{evidence:'controlled fixture, not actual Ego',
  methodPath:nested?['event','method']:['method'],paramsPath:nested?['event','params']:['params'],
  cdpResultPath:nested?['result']:[],evaluateResultPath:nested?['result']:[]}};}
const cases=[];
for(const nested of [false,true]) {
  const page=new PageFixture(nested);page.seed();const r=await context.createEgoCapture(page,cfg(nested));
  page.push(page.event('Network.webSocketFrameSent',{requestId:'socket',response:{opcode:1,payloadData:JSON.stringify({type:'message',session_id:'contact',invocation_id:'pre-arm-old'})}}));
  await r.arm('A');page.push(page.event('Network.webSocketFrameSent',{requestId:'socket',response:{opcode:1,payloadData:JSON.stringify({type:'message',session_id:'contact',invocation_id:'A-request',text:'A'})}}),
    page.event('Network.webSocketFrameReceived',{requestId:'socket',response:{opcode:1,payloadData:JSON.stringify({run_id:'a-run',messages:[{tool_call_id:'tool-original',output:view}]})}}));
  await r.drain();assert.equal(r.records().find(x=>x.kind==='role_request').requestInvocation,'A-request');await r.arm('B');page.push(page.event('Network.requestWillBeSent',{requestId:'queue',request:{url:cfgBase.apiRoot+'/bots/source/sessions/contact/follow-up-queue',method:'POST',hasPostData:true}}),
    page.event('Network.responseReceived',{requestId:'queue',response:{status:202}}));
  await r.drain();assert(!r.records().some(x=>x.kind==='queue_mapping'));
  page.bodies.set('unrelated',{body:'{"code":"conflict"}',base64Encoded:false});
  page.push(page.event('Network.requestWillBeSent',{requestId:'unrelated',request:{url:cfgBase.apiRoot+'/bots/source/sessions/contact/follow-up-queue',method:'POST',postData:'{"invocation_id":"other","text":"same text"}'}}),
    page.event('Network.responseReceived',{requestId:'unrelated',response:{status:409}}),page.event('Network.loadingFinished',{requestId:'unrelated'}));
  await r.drain();assert(!r.records().some(x=>x.kind==='queue_mapping'));
  page.bodies.set('queue',{body:Buffer.from('{"item_id":"queue-item","enqueued_during_run_id":"a-run"}').toString('base64'),base64Encoded:true});
  page.push(page.event('Network.loadingFinished',{requestId:'queue'}));await Promise.all([r.drain(),r.drain()]);
  const mapping=r.records().find(x=>x.kind==='queue_mapping');assert.equal(mapping.admittedInvocation,'follow-up:queue-item');assert.equal(mapping.requestInvocation,'B-request');
  await r.sample();let rows=r.records();const overlap=rows.find(x=>x.kind==='overlap_candidate');
  assert(overlap.bCompletedSeq<overlap.persistedHistorySeq&&overlap.persistedHistorySeq<overlap.subsequentWorkerSeq);
  assert.equal(overlap.workerRun,'worker');assert(!rows.some(x=>x.delegationId==='wrong-old'));
  page.wrongWorker=true;await r.sample();assert(r.records().some(x=>x.kind==='worker_identity_mismatch'));
  page.wrongWorker=false;page.worker='completed';await r.sample();assert.equal(r.records().filter(x=>x.kind==='overlap_candidate').length,1);
  page.denied=true;await r.sample();const before=page.fetches.filter(x=>x.endsWith('/A-request')).length;await r.sample();assert.equal(page.fetches.filter(x=>x.endsWith('/A-request')).length,before);
  // Completion arrives in a later read/clear batch while stop is draining.
  page.bodies.set('late',{body:'{"late":true}',base64Encoded:false});
  page.push(page.event('Network.requestWillBeSent',{requestId:'late',request:{url:cfgBase.apiRoot+'/bots/source/sessions/contact/queue',method:'GET'}}),page.event('Network.responseReceived',{requestId:'late',response:{status:200}}));
  await r.drain();page.push();page.push(page.event('Network.loadingFinished',{requestId:'late'}));rows=await r.stop();
  assert(rows.some(x=>x.kind==='http_response_complete'&&x.requestId==='late'));
  const text=JSON.stringify(rows);for(const forbidden of [secret,'Authorization','fixture.invalid','private.invalid'])assert(!text.includes(forbidden));
  assert(!page.commands.some(x=>x.method==='Network.disable'));assert(page.fetches.every(x=>typeof x==='string'));
  cases.push(nested?'explicit nested event/CDP/evaluate fixture':'explicit direct event/CDP/evaluate fixture');
}
// Events arriving while the body RPC is pending remain in the next clear-buffer batch.
{
  const p=new PageFixture();p.seed();const r=await context.createEgoCapture(p,cfg());await r.arm('B');
  let release;const pendingBody=new Promise(resolve=>{release=resolve;});
  p.bodies.set('async',()=>pendingBody);
  p.push(p.event('Network.requestWillBeSent',{requestId:'async',request:{url:cfgBase.apiRoot+'/bots/source/sessions/contact/follow-up-queue',method:'POST',postData:'{"invocation_id":"async-B"}'}}),
    p.event('Network.responseReceived',{requestId:'async',response:{status:202}}),p.event('Network.loadingFinished',{requestId:'async'}));
  const draining=r.drain();
  while(!p.commands.some(x=>x.method==='Network.getResponseBody'&&x.params.requestId==='async'))await Promise.resolve();
  p.push(p.event('Network.requestWillBeSent',{requestId:'failure',request:{url:cfgBase.apiRoot+'/bots/source/sessions/contact/queue',method:'GET'}}),
    p.event('Network.loadingFailed',{requestId:'failure',canceled:true}));
  assert(!r.records().some(x=>x.kind==='queue_mapping'));
  release({body:'{"item_id":"async-item"}',base64Encoded:false});await draining;await r.drain();
  const out=await r.stop();assert(out.some(x=>x.kind==='queue_mapping'&&x.itemId==='async-item'));assert(out.some(x=>x.kind==='request_failed_unknown'&&x.requestId==='failure'));
  cases.push('events arriving during awaited body RPC survive into next batch');
}

// Unknown envelope is not guessed; the consumed batch and fault are retained.
{
  const p=new PageFixture();p.seed();const r=await context.createEgoCapture(p,cfg());p.push({unknownWrapper:{method:'Network.loadingFinished',params:{requestId:'missing'}}});await r.drain();
  await assert.rejects(r.arm('A'));const out=await r.stop();assert(out.some(x=>x.kind==='event_envelope_contract_mismatch'));
  cases.push('unknown envelope fails closed without fallback');
}
// Missing contract never enables Network; expired bodies and stop gaps remain unknown.
{
  const p=new PageFixture();await assert.rejects(context.createEgoCapture(p,{...cfgBase,contract:null}));assert.equal(p.commands.length,0);
  p.seed();const r=await context.createEgoCapture(p,cfg());await r.arm('B');p.bodies.set('expired',new Error('body no longer available'));
  p.push(p.event('Network.requestWillBeSent',{requestId:'expired',request:{url:cfgBase.apiRoot+'/bots/source/sessions/contact/follow-up-queue',method:'POST',postData:'{"invocation_id":"lost"}'}}),
    p.event('Network.responseReceived',{requestId:'expired',response:{status:202}}),p.event('Network.loadingFinished',{requestId:'expired'}),
    p.event('Network.requestWillBeSent',{requestId:'unfinished',request:{url:cfgBase.apiRoot+'/bots/source/sessions/contact/queue',method:'GET'}}));
  await r.drain();const out=await r.stop();assert(out.some(x=>x.kind==='response_body_unknown'));assert(out.some(x=>x.kind==='response_unknown_on_stop'));assert(!out.some(x=>x.kind==='queue_mapping'));
  cases.push('no contract no enable; expired response and unfinished stop are unknown');
}
{
  const p=new PageFixture(true);const out=await context.probeEgoNetworkShape(p,{...cfgBase,durationMs:100,reloadExistingPage:true});
  assert.equal(out.reloads,1);assert.equal(out.bodyObservation.candidates[0].resultPath[0],'result');
  assert.equal(out.examples['Network.requestWillBeSent'].methodPath.join('.'),'event.method');
  const text=JSON.stringify(out);for(const secretValue of [secret,'Authorization','fixture.invalid'])assert(!text.includes(secretValue));
  cases.push('one shape probe reports paths/types only, no credentials/content');
}
console.log(JSON.stringify({status:'PASS',consumer:'controlled fake Page, NOT real Ego',paidCalls:0,browserLaunches:0,productionRequests:0,
  cases,covered:['pre-arm buffers cannot bind old sends','destructive buffers serialized','loadingFinished gates body read','delayed202 versus earlier409','postData fetch','base64 decode in isolated evaluate',
  'no invocation tool frame retained','exact queue/run/turn relation','B history then worker-running ordering','old task excluded','changed worker rejected','403 no retry',
  'stop drains late completion','credential/address redaction','no disable/subscribe/on/off','unknown shape/body and missing contract'],fieldAcceptance:'PENDING one Ego shape readback'},null,2));
