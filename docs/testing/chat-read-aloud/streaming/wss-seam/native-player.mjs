// Only new adapter -> real HTTP -> real generated SDK -> existing player seam.
// Loopback WSS/HTTP are owned by the Go test. No cloud calls or autoplay flags.
import { build } from '/workspace/Memoh/node_modules/.pnpm/node_modules/esbuild/lib/main.js'
import { chromium } from '/opt/codex/cua_node/lib/node_modules/playwright/index.mjs'
import { createRequire } from 'node:module'
import fs from 'node:fs'
import path from 'node:path'
import assert from 'node:assert/strict'
const root=process.cwd(), origin=process.env.SEAM_URL, out=process.env.SEAM_OUT
assert.equal(new URL(origin).hostname,'127.0.0.1')
const parser=createRequire(fs.realpathSync(path.join(root,'apps/web/node_modules/markstream-vue/package.json'))).resolve('stream-markdown-parser')
const entry=`
import { createApp,h,ref } from 'vue';
import { client } from '@memohai/sdk/client';
import { useChatReadAloud } from './src/pages/home/composables/useChatReadAloud.ts';
const probe=window.probe={requests:[],playCalls:0,playingEvents:0,rejections:[],nonzero:false};
const nativeFetch=window.fetch.bind(window);let graph,analyser,source;
client.setConfig({baseUrl:location.origin,fetch:async(...args)=>{
 const request=args[0],url=typeof request==='string'?request:request.url;
 if(new URL(url,location.href).origin!==location.origin)throw Error('non-loopback request');
 if(!url.includes('/tts/playback'))return nativeFetch(...args);
 const trace={firstBody:false,eof:false,bytes:0,sha256:null,error:null};probe.requests.push(trace);
 const response=await nativeFetch(...args),reader=response.body.getReader(),audit=[];
 const stream=new ReadableStream({async pull(feed){try{const value=await reader.read();if(value.done){trace.eof=true;const all=new Uint8Array(trace.bytes);let offset=0;for(const chunk of audit){all.set(chunk,offset);offset+=chunk.length;}trace.sha256=[...new Uint8Array(await crypto.subtle.digest('SHA-256',all))].map(x=>x.toString(16).padStart(2,'0')).join('');feed.close();return;}trace.firstBody=true;trace.bytes+=value.value.length;audit.push(value.value.slice());feed.enqueue(value.value);}catch(error){trace.error=error.name;feed.error(error);}},cancel(reason){return reader.cancel(reason);}});
 return new Response(stream,{status:response.status,headers:response.headers});
}});
const NativeAudio=window.Audio;window.Audio=function(){const player=new NativeAudio();window.player=player;
 if(graph){source=graph.createMediaElementSource(player);source.connect(analyser);analyser.connect(graph.destination);}
 const play=player.play.bind(player);player.play=()=>{probe.playCalls++;const result=play();result.catch(error=>probe.rejections.push(error.name));return result;};player.addEventListener('playing',()=>probe.playingEvents++);return player;};
const draft=ref(''),message=name=>({id:name,turnId:name,runtimeRunId:name,role:'assistant',timestamp:'',streaming:false,messages:[{id:0,type:'text',content:name}]}),messages=ref([message('complete'),message('cancel')]);let speech;
function sample(){if(analyser){const values=new Float32Array(analyser.fftSize);analyser.getFloatTimeDomainData(values);if(values.some(value=>Math.abs(value)>0.0001))probe.nonzero=true;}requestAnimationFrame(sample);}sample();
createApp({setup(){speech=useChatReadAloud({botId:ref('bot'),scope:ref('fixture'),enabled:ref(true),loading:ref(false),draft,messages});return()=>h('div',[
 ...messages.value.map(msg=>h('button',{id:msg.id,onClick:()=>{if(!graph){graph=new AudioContext();analyser=graph.createAnalyser();}graph.resume();probe.nonzero=false;speech.replay(msg);}},msg.id)),
 h('input',{id:'draft',onInput:event=>{draft.value=event.target.value;}}),
 h('div',{id:'bubble',onClickCapture:()=>speech.stop()},'existing interruption')
]);}}).mount('#root');
window.snapshot=()=>({requests:probe.requests,playCalls:probe.playCalls,playingEvents:probe.playingEvents,rejections:probe.rejections,nonzero:probe.nonzero,state:speech.state.value,paused:window.player?.paused,src:window.player?.getAttribute('src')});
`
const bundle=await build({stdin:{contents:entry,resolveDir:path.join(root,'apps/web'),sourcefile:'wss-seam.ts'},bundle:true,write:false,format:'iife',platform:'browser',alias:{'@':path.join(root,'apps/web/src'),'@memohai/sdk/client':path.join(root,'packages/sdk/src/client.gen.ts'),'@memohai/sdk':path.join(root,'packages/sdk/src/index.ts'),'markstream-vue':parser}})
const browser=await chromium.launch({executablePath:'/usr/bin/chromium',headless:true,args:['--no-sandbox']})
try{
 const page=await browser.newPage();const unexpected=[];page.on('request',r=>{if(new URL(r.url()).origin!==origin)unexpected.push(r.url())})
 await page.goto(origin);const cdp=await page.context().newCDPSession(page)
 const injected=await cdp.send('Runtime.evaluate',{expression:bundle.outputFiles[0].text,userGesture:false});if(injected.exceptionDetails)throw Error(JSON.stringify(injected.exceptionDetails))
 const snapshot=async()=>{const result=await cdp.send('Runtime.evaluate',{expression:'JSON.stringify(window.snapshot())',returnByValue:true,userGesture:false});return JSON.parse(result.result.value)}
 const status=async name=>fetch(origin+'/seam/status?case='+name).then(r=>r.json())
 const release=async name=>fetch(origin+'/seam/release?case='+name,{method:'POST'})
 async function until(check){for(let i=0;i<160;i++){const value=await snapshot();if(check(value))return value;await new Promise(resolve=>setTimeout(resolve,25))}throw Error('unobserved seam condition '+JSON.stringify(await snapshot()))}
 await page.click('#complete')
 const completeBeforeEOF=await until(s=>s.requests[0]?.firstBody&&s.state.status==='playing'&&s.nonzero)
 const upstreamBeforeEOF=await status('complete')
 assert.equal(upstreamBeforeEOF.released,false);assert.equal(upstreamBeforeEOF.upstreamFinished,false);assert.equal(completeBeforeEOF.requests[0].eof,false)
 await release('complete')
 const completed=await until(s=>s.state.status==='idle'&&s.requests[0]?.sha256)
 assert.equal(completed.requests[0].sha256,process.env.SEAM_MP3_SHA256)
 assert.equal(completed.requests[0].eof,true)
 await page.click('#cancel')
 const cancelBeforeEOF=await until(s=>s.requests.length===2&&s.requests[1].firstBody&&s.state.status==='playing'&&s.nonzero)
 const cancelUpstreamBeforeEOF=await status('cancel');assert.equal(cancelUpstreamBeforeEOF.released,false);assert.equal(cancelBeforeEOF.requests[1].eof,false)
 await page.fill('#draft','typing interrupts')
 const interrupted=await until(s=>s.state.status==='idle'&&s.paused&&s.src==='')
 let canceledStatus;for(let i=0;i<100;i++){canceledStatus=await status('cancel');if(canceledStatus.handlerCanceled)break;await new Promise(resolve=>setTimeout(resolve,20))}
 assert.equal(canceledStatus.handlerCanceled,true)
 await release('cancel');await new Promise(resolve=>setTimeout(resolve,350))
 const afterLateFrames=await snapshot(),lateStatus=await status('cancel')
 assert.equal(afterLateFrames.playCalls,interrupted.playCalls);assert.equal(afterLateFrames.playingEvents,interrupted.playingEvents);assert.equal(afterLateFrames.state.status,'idle');assert.equal(afterLateFrames.src,'');assert.equal(afterLateFrames.paused,true);assert(lateStatus.lateFramesAttempted>0)
 assert.equal(afterLateFrames.requests.length,2);assert.deepEqual(unexpected,[])
 const report={completeBeforeEOF,upstreamBeforeEOF,completed,cancelBeforeEOF,cancelUpstreamBeforeEOF,interrupted,canceledStatus,afterLateFrames,lateStatus,browser:browser.version(),sdkStubbed:false,providerCalls:0,autoplayPolicyOverride:false,physicalAudible500msPassed:false,decodedGraphIsNotAcousticEvidence:true,transportAudit:'fetch forwards each chunk immediately; copies retained only for completion SHA256, never prebuffered before the player',scope:'Loopback TLS WSS + real Alibaba SDK/service/handler + generated client + existing composable/controller/native MSE. Synthetic identity/settings/voice/audio. Not ArkWeb or public deployment.'}
 fs.writeFileSync(path.join(out,'native-player.json'),JSON.stringify(report,null,2)+'\n')
 console.log(JSON.stringify({firstClientBodyBeforeUpstreamEOF:true,nonzeroDecodedGraphBeforeUpstreamEOF:true,completeByteHashMatches:true,handlerCanceled:true,noPlaybackAfterLateFrames:true,requests:2,realProviderCalls:0,physicalAudible500msPassed:false}))
}finally{await browser.close()}
