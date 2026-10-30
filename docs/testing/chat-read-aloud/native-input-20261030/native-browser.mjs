// Actual current SDK/composable/controller + native Chromium Audio/MSE.
// The server/audio are local nonvoice fixtures. Never a provider/quality claim.
import { createRequire } from 'node:module'
import { build } from '/workspace/Memoh/node_modules/.pnpm/esbuild@0.27.2/node_modules/esbuild/lib/main.js'
import { chromium } from '/opt/codex/cua_node/lib/node_modules/playwright/index.mjs'
import { createServer } from 'node:http'
import { createHash } from 'node:crypto'
import fs from 'node:fs'
import path from 'node:path'
import assert from 'node:assert/strict'
const root=process.cwd(),out=process.env.SPEECH_EVIDENCE;assert(out);fs.mkdirSync(out,{recursive:true})
const slow=process.env.SPEECH_UNDERRUN==='1',bytes=fs.readFileSync(new URL('./continuous-fixture.mp3',import.meta.url)),tasks=new Map(),calls=[]
let phase='timing'
const entry=`import {createApp,h,ref} from 'vue';import {client} from '@memohai/sdk/client';import {useChatReadAloud} from './src/pages/home/composables/useChatReadAloud.ts';
client.setConfig({baseUrl:location.origin});const a=window.audit={events:[],plays:[],samples:[],inputs:[]},graph=new AudioContext(),analyser=graph.createAnalyser();analyser.connect(graph.destination);analyser.fftSize=512;
const OriginalAudio=window.Audio;window.Audio=function(){const p=new OriginalAudio();if(location.search!=='?policy')graph.createMediaElementSource(p).connect(analyser);a.players??=[];a.players.push(p);for(const type of ['playing','pause','waiting','ended','error','emptied'])p.addEventListener(type,e=>a.events.push({type,ms:performance.now(),time:p.currentTime,trusted:e.isTrusted}));const play=p.play.bind(p);p.play=()=>{const row={ms:performance.now(),src:p.src,active:navigator.userActivation.isActive,ever:navigator.userActivation.hasBeenActive,outcome:'pending'};a.plays.push(row);const promise=play();promise.then(()=>row.outcome='fulfilled',e=>{row.outcome='rejected';row.name=e.name;row.domException=e instanceof DOMException});return promise};return p};
const values=new Float32Array(analyser.fftSize);function sample(){analyser.getFloatTimeDomainData(values);a.samples.push({ms:performance.now(),rms:Math.sqrt(values.reduce((n,v)=>n+v*v,0)/values.length)});requestAnimationFrame(sample)}sample();
const messages=ref([]),draft=ref('');let speech;const reply=(text,streaming=true)=>({id:'row',turnId:'turn',runtimeRunId:'run',role:'assistant',timestamp:'',streaming,messages:[{id:0,type:'text',content:text}]});window.emit=()=>{a.trigger=performance.now();messages.value=[reply('First sentence. More')];setTimeout(()=>messages.value=[reply('First sentence. Second sentence.')],200);setTimeout(()=>messages.value=[reply('First sentence. Second sentence.',false)],400)};
createApp({setup(){speech=useChatReadAloud({botId:ref('fixture'),scope:ref('fixture-scope'),nativeInput:true,enabled:ref(true),loading:ref(false),draft,messages});return()=>h('main',[h('button',{id:'send',onClick:()=>{void graph.resume();window.emit()}},'Send local fixture'),h('button',{id:'replay',onClick:()=>{void graph.resume();speech.replay(messages.value[0])}},'Continue'),h('input',{id:'input',onInput:e=>{a.inputs.push({ms:performance.now(),trusted:e.isTrusted});draft.value=e.target.value}}),h('div',{id:'bubble',onClick:()=>speech.stop()},messages.value[0]?.messages[0].content)])}}).mount('#app');window.snapshot=()=>({...a,state:speech.state.value,players:a.players?.map(p=>({src:p.src,paused:p.paused,time:p.currentTime}))??[]});`
const parser=createRequire(fs.realpathSync('apps/web/node_modules/markstream-vue/package.json')).resolve('stream-markdown-parser')
const compiled=await build({stdin:{contents:entry,resolveDir:path.join(root,'apps/web'),sourcefile:'native-speech-review.ts'},bundle:true,write:false,format:'iife',platform:'browser',alias:{'@':path.join(root,'apps/web/src'),'@memohai/sdk/client':path.join(root,'packages/sdk/src/client.gen.ts'),'@memohai/sdk':path.join(root,'packages/sdk/src/index.ts'),'markstream-vue':parser}})
const server=createServer((request,response)=>{
 if(request.url.endsWith('/tts/playback/config')){response.writeHead(200,{'Content-Type':'application/json'});response.end(JSON.stringify({identity:'native-fixture-config',input_content_type:'audio/mpeg'}));return}
 if(request.url.endsWith('/tts/playback'))throw Error('native capability fell back to another synthesis')
 if(request.url.endsWith('/tts/playback/input')){
  let body='';request.on('data',chunk=>body+=chunk);request.on('end',()=>{
   const frame=JSON.parse(body),record={phase,method:request.method,ms:performance.now(),...frame};calls.push(record)
   if(request.method==='POST'){
    assert.equal(frame.sequence,0);assert.equal(frame.offset,0);assert(!tasks.has(frame.id));const task={response,sequence:1,offset:Array.from(frame.text).length,sent:false,final:false,tailScheduled:false,phase};tasks.set(frame.id,task)
    response.writeHead(200,{'Content-Type':'audio/mpeg','Cache-Control':'no-store','X-Speech-Playback-Identity':'native-fixture-config'});response.flushHeaders()
    setTimeout(()=>{if(!response.destroyed)response.write(bytes.subarray(0,Math.floor(bytes.length*.6)))},250)
    response.on('close',()=>tasks.delete(frame.id))
   }else{
    const task=tasks.get(frame.id);if(!task){response.writeHead(404);response.end();return}
    assert.equal(frame.sequence,task.sequence);assert.equal(frame.offset,task.offset);assert(!task.final);task.sequence++;task.offset+=Array.from(frame.text).length;task.final=frame.final
    if(frame.text&&!task.tailScheduled){task.tailScheduled=true;setTimeout(()=>{if(!task.response.destroyed){task.response.write(bytes.subarray(Math.floor(bytes.length*.6)));task.sent=true;if(task.final)task.response.end()}},slow&&task.phase==='timing'?2200:250)}
    const ack=()=>{response.writeHead(204);response.end()};if(task.phase==='cancel'&&frame.text)setTimeout(ack,600);else ack()
    if(task.final&&task.sent)task.response.end()
   }
  });return
 }
 if(request.url==='/fixture.js'){response.writeHead(200,{'Content-Type':'text/javascript'});response.end(compiled.outputFiles[0].text);return}
 response.writeHead(200,{'Content-Type':'text/html'});response.end('<div id="app"></div><script src="/fixture.js"></script>')
})
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const origin='http://127.0.0.1:'+server.address().port
const browser=await chromium.launch({headless:true,executablePath:'/usr/bin/chromium',args:['--no-sandbox','--autoplay-policy=document-user-activation-required','--disable-features=PreloadMediaEngagementData,MediaEngagementBypassAutoplayPolicies']})
const context=await browser.newContext(),errors=[]
async function scenario(suffix=''){
 const page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));const cdp=await context.newCDPSession(page)
 const read=async expression=>{const result=await cdp.send('Runtime.evaluate',{expression,userGesture:false,returnByValue:true,awaitPromise:true});if(result.exceptionDetails)throw Error(JSON.stringify(result.exceptionDetails));return result.result.value}
 const poll=async(expression,label)=>{const deadline=Date.now()+15000;while(Date.now()<deadline){const result=await read(expression);if(result)return result;await new Promise(resolve=>setTimeout(resolve,25))}throw Error('Timed out '+label)}
 await page.goto(origin+suffix);return{page,read,poll}
}
const report={browser:browser.version(),origin,fixtureSHA256:createHash('sha256').update(bytes).digest('hex'),providerCalls:0,physicalAudibleMeasurement:false,profile:slow?'intentional-underrun':'normal',sourceHashes:Object.fromEntries(['chat-read-aloud.ts','useChatReadAloud.ts','speech-replay-cache.ts','speech-audio-stream.ts','speech-render-completion.ts'].map(n=>[n,createHash('sha256').update(fs.readFileSync('apps/web/src/pages/home/composables/'+n)).digest('hex')]))}
try{
 let current=await scenario();await current.page.locator('#send').click();await current.poll('window.snapshot().events.some(e=>e.type==="ended")&&window.snapshot().state.status==="idle"','native one-task end')
 const data=await current.read('window.snapshot()'),nonzero=data.samples.filter(s=>s.rms>.001),gaps=[]
 for(let i=1;i<nonzero.length;i++){const gap=nonzero[i].ms-nonzero[i-1].ms;if(gap>50)gaps.push(gap)}
 report.timing={firstNonzeroMS:nonzero[0].ms-data.trigger,gapsMS:gaps,samplerMaxPollGapMS:Math.max(...data.samples.slice(1).map((v,i)=>v.ms-data.samples[i].ms)),underruns:data.events.filter((e,i)=>e.type==='waiting'&&e.time>0&&data.events.slice(0,i).some(p=>p.type==='playing')).length,plays:data.plays,events:data.events,state:data.state,calls:calls.filter(c=>c.phase==='timing')}
 assert.equal(report.timing.calls.filter(c=>c.method==='POST').length,1);assert.equal(report.timing.calls.filter(c=>c.method==='PUT').length,2);assert.equal(report.timing.calls.map(c=>c.text).join(''),'First sentence. Second sentence.');assert.equal(new Set(data.plays.map(p=>p.src)).size,1)
 if(slow){assert(report.timing.underruns>0);assert(gaps.length>0)}else{assert.equal(report.timing.underruns,0);assert.equal(gaps.length,0)}await current.page.close()
 if(!slow){
 phase='cancel';current=await scenario();await current.page.locator('#send').click();await current.poll('window.snapshot().events.some(e=>e.type==="playing")','native playing before cancel')
 const deadline=Date.now()+10000;while(!calls.some(c=>c.phase==='cancel'&&c.method==='PUT'&&c.text)&&Date.now()<deadline)await new Promise(resolve=>setTimeout(resolve,20));assert(calls.some(c=>c.phase==='cancel'&&c.method==='PUT'&&c.text));await current.page.locator('#input').pressSequentially('x');await current.poll('window.snapshot().state.status==="idle"&&window.snapshot().players.every(p=>p.paused)','typed cancel')
 await new Promise(resolve=>setTimeout(resolve,900));report.cancel={snapshot:await current.read('window.snapshot()'),calls:calls.filter(c=>c.phase==='cancel')};assert.equal(report.cancel.snapshot.state.status,'idle');assert.equal(report.cancel.calls.filter(c=>c.method==='POST').length,1);assert(!report.cancel.calls.some(c=>c.final));await current.page.close()
 phase='policy';current=await scenario('?policy');await current.read('window.emit()');await current.poll('window.snapshot().state.status==="blocked"','real policy refusal');const before=await current.read('window.snapshot()');assert.equal(before.plays[0].name,'NotAllowedError');assert.equal(before.plays[0].domException,true);assert.equal(before.plays[0].ever,false)
 await current.page.locator('#replay').click();await current.poll('window.snapshot().state.status==="playing"','trusted continue');const resumed=await current.read('window.snapshot()');assert.equal(resumed.plays[0].src,resumed.plays[1].src);await current.page.locator('#bubble').click();await current.poll('window.snapshot().state.status==="idle"&&window.snapshot().players.every(p=>p.paused)','message click cancel');await new Promise(resolve=>setTimeout(resolve,900));const stopped=await current.read('window.snapshot()');assert.equal(stopped.state.status,'idle');report.policy={before,resumed,stopped,calls:calls.filter(c=>c.phase==='policy')};assert.equal(report.policy.calls.filter(c=>c.method==='POST').length,1);await current.page.close()
 }else report.reusedCancellationAndPolicyFrom='native-normal/result.json';
 assert.deepEqual(errors,[]);report.errors=errors;fs.writeFileSync(out+'/result.json',JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify({timing:report.timing.firstNonzeroMS,gaps:report.timing.gapsMS,underruns:report.timing.underruns,cancel:!slow?true:'reused',policy:!slow?true:'reused',providerCalls:0}))
}catch(error){report.failure=String(error);report.errors=errors;fs.writeFileSync(out+'/failure.json',JSON.stringify(report,null,2)+'\n');throw error}
finally{await browser.close();for(const task of tasks.values())task.response.destroy();await new Promise(resolve=>server.close(resolve))}
