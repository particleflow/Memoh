// New diagnostic only: an injected page failure, not a real Ego/navigation reproduction.
import assert from 'node:assert/strict';
import vm from 'node:vm';
import {execFileSync} from 'node:child_process';
const revisions=['4120cdb1d24d3c948ca30a1d116658b606bb5aaf','890640078b9c52b26edcb3efec0ec851985ec90b'];
const path='docs/testing/parallel-capture-3f361/ego/capture.js';
const sources=revisions.map(r=>execFileSync('git',['show',r+':'+path],{encoding:'utf8'}));
assert.equal(sources[0],sources[1]);
const cases=[];
for (const navigationReported of [false,true]) {
  const context=vm.createContext({URL,Date:{now:()=>1000},Map,Set,Object,JSON,Error,Uint8Array,TextDecoder,encodeURIComponent});
  vm.runInContext(sources[0],context);
  let reads=0,evaluates=0;const methods=[];
  const page={async cdp(method){methods.push(method);return {};},async events(){reads++;return [];},
    async evaluate(fn,arg){evaluates++;assert.equal(arg.delayMs,250);
      // Same API-visible error is possible with or without recorded navigation evidence.
      const error=new Error('fixture page evaluation timeout');error.name='PageEvaluationTimeoutError';error.code='EGO_PAGE_EVALUATION_TIMED_OUT';throw error;}};
  const rec=await context.createEgoCapture(page,{apiRoot:'https://fixture.invalid',bot:'source',contact:'C',consumer:'fixture only',timeoutMs:10000,stopDrainMs:2000,pollIntervalMs:250,
    contract:{evidence:'fixture direct envelope',methodPath:['method'],paramsPath:['params'],cdpResultPath:[],evaluateResultPath:[]}});
  if(navigationReported)await page.cdp('Page.reload',{});
  await rec.drain();
  assert.equal(reads,1);assert.equal(evaluates,1);assert.equal(rec.summary().fault,true);
  assert(rec.records().some(x=>x.kind==='observer_wait_unknown'&&x.errorCode==='EGO_PAGE_EVALUATION_TIMED_OUT'));
  await rec.stop();assert.equal(rec.summary().stopped,true);assert.equal(reads,1);assert.equal(evaluates,1);
  assert.equal(rec.summary().scopedGetObserved,false);assert.equal(rec.summary().sourceSocketCreated,false);
  assert(methods.every(x=>['Network.enable','Page.reload'].includes(x)));
  cases.push({navigationReported,firstEmptyReadThenPageWaitFails:true,noSecondReadOrBusinessAction:true,
    localCatchAndStop:'works only if Ego returns the rejection into the still-running collector execution'});
}
console.log(JSON.stringify({status:'PASS',scope:'new injected page-wait failure diagnostics only',sameSourceAtBothRevisions:true,cases,
  notProven:['actual navigation/context destruction','Ego host execution survival/cancellation','supported host timer primitive'],
  liveRequests:0,modelCalls:0,oldSuitesRerun:false},null,2));
