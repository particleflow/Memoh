// Reproduce the exact reviewed WAV bytes through the actual current controller.
import { build } from '/workspace/Memoh/node_modules/.pnpm/node_modules/esbuild/lib/main.js'
import { chromium } from '/opt/codex/cua_node/lib/node_modules/playwright/index.mjs'
import { createRequire } from 'node:module'
import fs from 'node:fs'
import path from 'node:path'
import assert from 'node:assert/strict'
const root=process.cwd(), out=process.env.WAV_COMPAT_OUT
const parser=createRequire(fs.realpathSync(path.join(root,'apps/web/node_modules/markstream-vue/package.json'))).resolve('stream-markdown-parser')
const bytes=[...fs.readFileSync(path.join(root,'docs/testing/chat-read-aloud/streaming/wav-compat/fixture.wav'))]
const entry=`
import { ChatReadAloud } from './src/pages/home/composables/chat-read-aloud.ts';
const bytes=new Uint8Array(${JSON.stringify(bytes)}), result=window.probe={mime:'audio/wav',canPlayType:new Audio().canPlayType('audio/wav'),mseSupported:MediaSource.isTypeSupported('audio/wav'),requests:0,cancelled:false,playing:false,complete:false,urls:0,blobType:'',states:[]};let feed,player;
const controller=new ChatReadAloud({synthesize:async()=>{result.requests++;return {contentType:'audio/wav',stream:new ReadableStream({start(c){feed=c;c.enqueue(bytes.slice(0,4096));},cancel(){result.cancelled=true;}})};},createPlayer:()=>{player=new Audio();player.addEventListener('playing',()=>result.playing=true);return player;},createURL:source=>{result.urls++;result.blobType=source.type||'';return URL.createObjectURL(source);},revokeURL:url=>URL.revokeObjectURL(url),onState:state=>{result.status=state.status;result.states.push(state.status);if(state.error)result.error=String(state.error);}});
document.querySelector('#replay').onclick=()=>controller.replay({key:'wav',text:'controlled sine fixture'});
window.finish=()=>{try{feed.enqueue(bytes.slice(4096));feed.close();result.complete=true;}catch(error){result.finishError=String(error);}};
window.snapshot=()=>({...result,currentTime:player?.currentTime||0,src:player?.getAttribute('src')});
`
const compiled=await build({stdin:{contents:entry,resolveDir:path.join(root,'apps/web'),sourcefile:'wav-controller.ts'},bundle:true,write:false,format:'iife',platform:'browser',alias:{'@':path.join(root,'apps/web/src'),'markstream-vue':parser}})
const browser=await chromium.launch({executablePath:'/usr/bin/chromium',headless:true,args:['--no-sandbox']})
try{
 const page=await browser.newPage();await page.setContent('<button id="replay">existing replay fixture</button>');await page.addScriptTag({content:compiled.outputFiles[0].text});await page.click('#replay')
 await page.waitForFunction(()=>window.probe.status==='error'||window.probe.status==='loading')
 const beforeEOF=await page.evaluate(()=>window.snapshot());assert.equal(beforeEOF.requests,1);assert.equal(beforeEOF.playing,false)
 await page.evaluate(()=>window.finish());await page.waitForFunction(()=>window.probe.status==='error'||(window.probe.playing&&window.snapshot().currentTime>0.05))
 const observed=await page.evaluate(()=>window.snapshot())
 const report={beforeEOF,observed,browser:browser.version(),providerCalls:0,autoplayPolicyOverride:false,physicalAudible500msPassed:false,scope:'Exact v12 WAV fixture through actual current ChatReadAloud with one synthetic streaming response; native play promise unchanged. Not API, application, ArkWeb or acoustic evidence.'}
 fs.writeFileSync(out,JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify(report))
 if(process.env.WAV_EXPECT_RED==='1'){assert.equal(observed.status,'error');assert.equal(observed.cancelled,true);assert.match(observed.error,/unsupported for audio\/wav/)}
 else{assert.equal(beforeEOF.urls,0);assert.equal(observed.playing,true);assert.equal(observed.blobType,'audio/wav');assert.equal(observed.requests,1);assert.equal(observed.cancelled,false);assert(observed.currentTime>0)}
}finally{await browser.close()}
