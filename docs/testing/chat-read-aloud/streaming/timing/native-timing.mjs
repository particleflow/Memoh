// New objective timing measurement only. No app/model/SDK retry or autoplay flag.
import { build } from '/workspace/Memoh/node_modules/.pnpm/node_modules/esbuild/lib/main.js'
import { chromium } from '/opt/codex/cua_node/lib/node_modules/playwright/index.mjs'
import { createRequire } from 'node:module'
import fs from 'node:fs'
import path from 'node:path'
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { arm, take, clear } from './device-sampling.mjs'
const root=process.cwd(),origin=process.env.TIMING_URL,out=process.env.TIMING_OUT
assert.equal(new URL(origin).hostname,'127.0.0.1')
const parser=createRequire(fs.realpathSync(path.join(root,'apps/web/node_modules/markstream-vue/package.json'))).resolve('stream-markdown-parser')
const entry=`
import { createApp,h,ref } from 'vue';
import { client } from '@memohai/sdk/client';
import { useChatReadAloud } from './src/pages/home/composables/useChatReadAloud.ts';
client.setConfig({baseUrl:location.origin});sessionStorage.setItem('memoh.speechTiming','1');
const graph=new AudioContext(),analyser=graph.createAnalyser();analyser.connect(graph.destination);
const probe=window.probe={graph:{},lastZero:{},rafGaps:[],clicks:{},errors:[]};let previousPoll=performance.now();
const NativeAudio=window.Audio;window.Audio=function(){const player=new NativeAudio();graph.createMediaElementSource(player).connect(analyser);return player;};
function sample(){const now=performance.now();probe.rafGaps.push(now-previousPoll);if(probe.rafGaps.length>500)probe.rafGaps.shift();previousPoll=now;
 const request=window.__memohSpeechTiming?.snapshot().findLast(record=>record.events.some(event=>event.event==='request_start'));
 if(request&&!probe.graph[request.id]){const values=new Float32Array(analyser.fftSize);analyser.getFloatTimeDomainData(values);const nonzero=values.some(value=>Math.abs(value)>0.0001);if(!nonzero)probe.lastZero[request.id]=now-request.startMS;
 if(nonzero&&request.events.some(event=>event.event==='native_playing'))probe.graph[request.id]={firstNonzeroMS:now-request.startMS,previousZeroMS:probe.lastZero[request.id]??null};}
 requestAnimationFrame(sample);}sample();
const msg=(name,streaming=false)=>({id:name,turnId:name,runtimeRunId:name,role:'assistant',timestamp:'',streaming,messages:[{id:0,type:'text',content:streaming?'':name}]}),messages=ref([msg('fast'),msg('slow'),msg('wav')]);let speech;
createApp({setup(){speech=useChatReadAloud({botId:ref('bot'),scope:ref('fixture'),enabled:ref(true),loading:ref(false),draft:ref(''),messages});return()=>h('div',[
 ...messages.value.filter(message=>!message.streaming).map(message=>h('button',{id:message.id,onClick:()=>{probe.clicks[message.id]=performance.now();void graph.resume();speech.replay(message);}},message.id)),
 h('button',{id:'automatic',onClick:()=>{probe.clicks.auto=performance.now();void graph.resume();messages.value.push(msg('auto',true));setTimeout(()=>{messages.value.at(-1).messages[0].content='auto';},50);}})
]);}}).mount('#root');
window.snapshot=()=>({records:window.__memohSpeechTiming?.snapshot()??[],graph:probe.graph,state:speech.state.value,clockResolutionMS:(()=>{let a=performance.now(),b=a;for(let i=0;i<1000&&b===a;i++)b=performance.now();return b-a;})(),contextBaseLatencyMS:graph.baseLatency*1000,contextOutputLatencyMS:graph.outputLatency*1000,maxObservedRAFPollGapMS:Math.max(...probe.rafGaps),clicks:probe.clicks});
`
const bundle=await build({stdin:{contents:entry,resolveDir:path.join(root,'apps/web'),sourcefile:'timed-fixture.ts'},bundle:true,write:false,format:'iife',platform:'browser',alias:{'@':path.join(root,'apps/web/src'),'@memohai/sdk/client':path.join(root,'packages/sdk/src/client.gen.ts'),'@memohai/sdk':path.join(root,'packages/sdk/src/index.ts'),'markstream-vue':parser}})
const browser=await chromium.launch({executablePath:'/usr/bin/chromium',headless:true,args:['--no-sandbox']})
try{
 const page=await browser.newPage();const unexpected=[];page.on('request',r=>{if(new URL(r.url()).origin!==origin)unexpected.push('non-loopback')})
 page.on('pageerror',error=>console.error(error.message));await page.goto(origin);await page.addScriptTag({content:bundle.outputFiles[0].text})
 const results=[]
 for(const name of ['fast','slow','wav','auto']){
  await page.click(name==='auto'?'#automatic':'#'+name)
  await page.waitForFunction(count=>{const s=window.snapshot(),requests=s.records.filter(r=>r.events.some(e=>e.event==='request_start'));return requests.length===count&&s.graph[requests.at(-1).id]&&requests.at(-1).events.some(e=>e.event==='body_eof')&&s.state.status==='idle';},results.length+1,{timeout:6000})
  const s=await page.evaluate(()=>window.snapshot()),request=s.records.filter(r=>r.events.some(e=>e.event==='request_start')).at(-1),operation=s.records.find(r=>r.id===request.operationId)
  const event=key=>request.events.find(e=>e.event===key)?.ms??null
  const graph=s.graph[request.id],requestMS=event('request_start'),playing=event('native_playing'),firstBytes=event('client_first_bytes'),firstServer=request.events.find(e=>e.event==='response_headers')?.serverFirstAudioMS??null
  assert.notEqual(firstServer,null);assert(firstBytes>=requestMS);assert(playing>=firstBytes)
  if(name==='wav'){assert.equal(event('decoder_append_start'),null);assert(playing>=event('body_eof'));assert.equal(request.events.find(e=>e.event==='source_branch').branch,'blob')}
  else{assert(event('decoder_append_done')<event('body_eof'));assert(playing<event('body_eof'))}
  const row={profile:name,traceId:request.id,operationId:request.operationId,triggerToOperationMS:request.startMS-s.clicks[name],firstTextObservedMS:operation.events.find(e=>e.event==='text_observed')?.ms??null,firstTextChunkMS:event('text_chunk_ready'),requestStartMS:requestMS,serverAdapterFirstAudioMS:firstServer,clientFirstBytesMS:firstBytes,firstAppendStartMS:event('decoder_append_start'),firstAppendDoneMS:event('decoder_append_done'),nativePlayingMS:playing,firstNonzeroGraphMS:graph.firstNonzeroMS,previousZeroGraphMS:graph.previousZeroMS,eofMS:event('body_eof'),requestToPlayingMS:playing-requestMS,requestToNonzeroGraphMS:graph.firstNonzeroMS-requestMS,nonAcousticGraphMarginTo500MS:500-(graph.firstNonzeroMS-requestMS),contextBaseLatencyMS:s.contextBaseLatencyMS,contextOutputLatencyMS:s.contextOutputLatencyMS,maxObservedRAFPollGapMS:s.maxObservedRAFPollGapMS,branch:request.events.find(e=>e.event==='source_branch').branch,record:request,operation}
  results.push(row);console.log(JSON.stringify({...row,record:undefined,operation:undefined}))
 }
 assert(results[0].requestToNonzeroGraphMS<500);assert(results[1].requestToNonzeroGraphMS>500);assert(results[2].requestToNonzeroGraphMS>500);assert.deepEqual(unexpected,[])
 const measuredSourceHashes=Object.fromEntries(['apps/web/src/pages/home/composables/chat-read-aloud.ts','apps/web/src/pages/home/composables/useChatReadAloud.ts','apps/web/src/pages/home/composables/speech-audio-stream.ts','apps/web/src/pages/home/composables/speech-timing.ts'].map(file=>[file,createHash('sha256').update(fs.readFileSync(path.join(root,file))).digest('hex')]))
 await page.evaluate(clear);const armed=JSON.parse(await page.evaluate(arm));assert.equal(armed.armed,true);assert.deepEqual(JSON.parse(await page.evaluate(take)),[]);await page.evaluate(clear)
 const report={measuredSourceHashes,fixedDeviceSamplingScriptsChecked:true,results,browser:browser.version(),source:'Controlled provider-channel first-byte/EOF delays + actual HTTP handler/generated SDK/composable/controller/MSE or Blob/native Audio',plannedFixtureDelays:{fast:{firstMS:70,eofMS:450},slow:{firstMS:620,eofMS:900},wav:{firstMS:40,eofMS:780},auto:{firstMS:70,eofMS:450}},providerCalls:0,autoplayPolicyOverride:false,physicalAudibleMeasurement:false,acoustic500msStatus:'UNMEASURED',notice:'Playing and nonzero WebAudio graph are software observations, not speaker output. Server durations are on an independent monotonic clock; IDs correlate them, epochs are never subtracted.'}
 fs.writeFileSync(path.join(out,'native-timing.json'),JSON.stringify(report,null,2)+'\n')
}finally{await browser.close()}
