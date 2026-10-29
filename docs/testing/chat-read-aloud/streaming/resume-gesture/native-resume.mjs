// Targeted regression only: real browser NotAllowedError -> original replay
// gesture -> SAME native audio source. In-memory page and existing sine fixture;
// no application server, provider call, autoplay override or audible-timing claim.
import { build } from '/workspace/Memoh/node_modules/.pnpm/node_modules/esbuild/lib/main.js'
import { chromium } from '/opt/codex/cua_node/lib/node_modules/playwright/index.mjs'
import { createRequire } from 'node:module'
import fs from 'node:fs'
import path from 'node:path'
import assert from 'node:assert/strict'
const root = process.cwd(), out = path.join(root, 'docs/testing/chat-read-aloud/streaming/resume-gesture/evidence')
const bytes = [...fs.readFileSync('/tmp/nas-speech-native/sine.mp3')]
const parser = createRequire(fs.realpathSync(path.join(root, 'apps/web/node_modules/markstream-vue/package.json'))).resolve('stream-markdown-parser')
const entry = `
import { createApp, h, ref } from 'vue';
import { useChatReadAloud } from './src/pages/home/composables/useChatReadAloud.ts';
const probe = window.probe = { calls: 0, playCalls: [], rejections: [], states: [], urls: [] };
const NativeAudio = window.Audio, nativeURL = URL.createObjectURL.bind(URL);
window.Audio = function(...args) { const audio = new NativeAudio(...args); window.nativePlayer=audio; const play=audio.play.bind(audio); audio.play=function(){ probe.playCalls.push({ active:navigator.userActivation.isActive }); const promise=play(); promise.then(()=>{},error=>probe.rejections.push(error.name)); return promise; }; return audio; };
URL.createObjectURL = source => { const url=nativeURL(source);probe.urls.push(url);return url; };
window.synthesizeFixture=async ({signal}) => { probe.calls++;let timer,index=0;return { data:new ReadableStream({start(feed){function next(){if(signal.aborted)return;if(index>=window.fixtureBytes.length){feed.close();return;}const end=Math.min(index+4096,window.fixtureBytes.length);feed.enqueue(new Uint8Array(window.fixtureBytes.slice(index,end)));index=end;timer=setTimeout(next,60);}next();},cancel(){clearTimeout(timer);}}),response:{headers:new Headers({'Content-Type':'audio/mpeg'})} }; };
const msg={id:'m',turnId:'turn',runtimeRunId:'run',role:'assistant',timestamp:'',streaming:true,messages:[{id:0,type:'text',content:'本地手势恢复反例。'}]};
const messages=ref([msg]),showMenu=ref(false);let speech;
createApp({setup(){speech=useChatReadAloud({botId:ref('synthetic-only'),scope:ref('scope'),enabled:ref(true),loading:ref(false),draft:ref(''),messages});return()=>h('div',[
 h('div',{id:'bubble',onContextmenu:event=>{event.preventDefault();showMenu.value=true;},onClickCapture:()=>speech.stop()},'message'),
 showMenu.value?h('button',{id:'existing-menu-replay',onClick:()=>{speech.replay(messages.value[0]);showMenu.value=false;}},'existing replay'):null,
]);}}).mount('#root');
window.snapshot=()=>({calls:probe.calls,playCalls:probe.playCalls,rejections:probe.rejections,urls:probe.urls,state:speech.state.value,src:window.nativePlayer?.getAttribute('src'),paused:window.nativePlayer?.paused,activation:navigator.userActivation.hasBeenActive});
`
const bundle = await build({ stdin:{contents:entry,resolveDir:path.join(root,'apps/web'),sourcefile:'resume-regression.ts'},bundle:true,write:false,format:'iife',platform:'browser',alias:{'@':path.join(root,'apps/web/src'),'markstream-vue':parser},plugins:[{name:'synthetic-sdk-only',setup(build){build.onResolve({filter:/^@memohai\/sdk$/},()=>({path:'sdk-fixture',namespace:'synthetic'}));build.onLoad({filter:/.*/,namespace:'synthetic'},()=>({contents:'export const postBotsByBotIdTtsPlayback = options => window.synthesizeFixture(options);',loader:'js'}));}}] })
const browser=await chromium.launch({executablePath:'/usr/bin/chromium',headless:true,args:['--no-sandbox']})
try {
 const page=await browser.newPage();let requests=0;page.on('request',()=>requests++)
 const cdp=await page.context().newCDPSession(page)
 await cdp.send('Runtime.evaluate',{expression:`document.body.innerHTML='<div id="root"></div>';window.fixtureBytes=${JSON.stringify(bytes)};`,userGesture:false})
 await cdp.send('Runtime.evaluate',{expression:bundle.outputFiles[0].text,userGesture:false})
 const snapshot=async()=>{const r=await cdp.send('Runtime.evaluate',{expression:'JSON.stringify(window.snapshot())',returnByValue:true,userGesture:false});return JSON.parse(r.result.value)}
 async function until(test){for(let i=0;i<60;i++){const value=await snapshot();if(test(value))return value;await new Promise(resolve=>setTimeout(resolve,50))}throw new Error('native policy/state was not observed: '+JSON.stringify(await snapshot()))}
 const before=await until(value=>value.state.status==='blocked')
 assert(before.rejections.includes('NotAllowedError'));assert.equal(before.activation,false);assert.equal(before.calls,1)
 await page.click('#bubble',{button:'right'});await page.click('#existing-menu-replay')
 const resumed=await until(value=>value.state.status==='playing')
 assert.equal(resumed.calls,1);assert.equal(resumed.src,before.src);assert.equal(resumed.urls.length,1);assert.equal(resumed.playCalls.length,2);assert.equal(resumed.playCalls[1].active,true)
 await page.click('#bubble')
 const interrupted=await until(value=>value.state.status==='idle')
 assert.equal(interrupted.src,'');assert.equal(interrupted.calls,1)
 const report={before,resumed,interrupted,networkRequests:requests,providerCalls:0,browser:browser.version(),nativePlayPromiseUnchanged:true,autoplayPolicyOverride:false,physicalAudible500msPassed:false,scope:'Isolated trusted native mouse gesture plus real composable/controller/MSE; actual library menu timing covered separately by controlled regression cases. Not app/HAP or ArkWeb evidence.'}
 fs.writeFileSync(path.join(out,'native-resume.json'),JSON.stringify(report,null,2)+'\n')
 console.log(JSON.stringify({blockedBeforeGesture:before.state.status,sameURL:resumed.src===before.src,apiCalls:resumed.calls,playCalls:resumed.playCalls.length,trustedActiveOnResume:resumed.playCalls[1].active,interrupted:interrupted.state.status,networkRequests:requests,physicalAudible500msPassed:false}))
} finally {await browser.close()}
