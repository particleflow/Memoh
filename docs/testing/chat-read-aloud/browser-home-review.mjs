// Candidate contact/task navigation review with synthetic server/audio fixtures only.
// Observation uses raw CDP userGesture:false; native play() promises are returned unchanged.
// Run with READ_ALOUD_ORIGIN pointing to the current Vite app. No provider call.
import { chromium } from '/opt/codex/cua_node/lib/node_modules/playwright/index.mjs'
import fs from 'node:fs'
import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { spawn } from 'node:child_process'
import { WebSocketServer } from '/workspace/Memoh/node_modules/.pnpm/node_modules/ws/wrapper.mjs'
const origin = process.env.READ_ALOUD_ORIGIN || 'http://127.0.0.1:18485'
const isolatedURL = new URL(origin)
assert(isolatedURL.protocol === 'http:' && ['127.0.0.1', 'localhost'].includes(isolatedURL.hostname) && isolatedURL.port && isolatedURL.pathname === '/' && !isolatedURL.username && !isolatedURL.password && !isolatedURL.search && !isolatedURL.hash, 'refusal runner is restricted to an explicit loopback HTTP port')
const out = process.env.READ_ALOUD_EVIDENCE || '/tmp/readaloud-home-review/browser-red'
fs.mkdirSync(out, { recursive: true })
const botId = '10000000-0000-4000-8000-000000000001', sessionId = '20000000-0000-4000-8000-000000000001'
const bot = { id: botId, name: 'read-aloud-synthetic', display_name: '朗读交互模拟验证', status: 'ready', current_user_permissions: ['manage'], chat_model_id: 'synthetic-chat', runtime_type: 'native' }
const session = { id: sessionId, bot_id: botId, title: '仅合成音频 · 无模型调用', type: 'chat', runtime_type: 'native', channel_type: 'local', created_at: '2026-10-28T10:00:00Z', updated_at: '2026-10-28T10:00:00Z' }
// A playable, quiet 10-second PCM WAV. This verifies media events, not TTS quality.
function wav() {
  const rate = 8000, frames = rate * 10, data = Buffer.alloc(44 + frames * 2)
  data.write('RIFF', 0); data.writeUInt32LE(data.length - 8, 4); data.write('WAVEfmt ', 8)
  data.writeUInt32LE(16, 16); data.writeUInt16LE(1, 20); data.writeUInt16LE(1, 22)
  data.writeUInt32LE(rate, 24); data.writeUInt32LE(rate * 2, 28); data.writeUInt16LE(2, 32); data.writeUInt16LE(16, 34)
  data.write('data', 36); data.writeUInt32LE(frames * 2, 40)
  for (let i = 0; i < frames; i++) data.writeInt16LE(Math.round(Math.sin(i * Math.PI / 20) * 50), 44 + i * 2)
  return data
}

const args = ['--no-sandbox', '--autoplay-policy=document-user-activation-required', '--disable-features=PreloadMediaEngagementData,MediaEngagementBypassAutoplayPolicies']
const browser = await chromium.launch({ headless: true, executablePath: '/usr/bin/chromium', args })
const proof = { source: '28ac9393c7e20db34b804ad3ece63680eecded8c + Home visibility candidate', syntheticServerAndWav: true, realProviderCalls: 0, playbackRequests: [], writes: [], pageErrors: [], checks: [], observations: [] }
let debugSession, viteProcess
const fixtureHTTP = createServer((request, response) => { response.writeHead(503); response.end('Unexpected fixture HTTP request') })
const fixtureSockets = new WebSocketServer({ server: fixtureHTTP })
await new Promise((resolve, reject) => { fixtureHTTP.once('error', reject); fixtureHTTP.listen(0, '127.0.0.1', resolve) })
try {
  proof.browser = await browser.version()
  const browserSession = await browser.newBrowserCDPSession()
  const command = await browserSession.send('Browser.getBrowserCommandLine')
  proof.policyArguments = command.arguments.filter(value => /autoplay|disable-features|mute-audio/.test(value))
  const context = await browser.newContext({ viewport: { width: 1280, height: 960 }, colorScheme: 'light' })
  await context.addInitScript(() => {
    localStorage.setItem('token', 'synthetic-only'); localStorage.setItem('language', 'zh')
    const originalPlay = HTMLMediaElement.prototype.play
    window.refusalAudit = { originalPlaySource: Function.prototype.toString.call(originalPlay), players: [], plays: [], mediaEvents: [], inputEvents: [] }
    // Observation only: invoke the native method and return its exact Promise.
    HTMLMediaElement.prototype.play = function (...params) {
      const audit = window.refusalAudit
      if (!audit.players.includes(this)) {
        audit.players.push(this)
        for (const type of ['playing', 'pause', 'waiting', 'ended', 'error', 'abort', 'emptied']) {
          this.addEventListener(type, event => audit.mediaEvents.push({ type, trusted: event.isTrusted, time: this.currentTime, paused: this.paused }))
        }
      }
      const record = { player: audit.players.indexOf(this), src: this.src, active: navigator.userActivation.isActive, hasBeenActive: navigator.userActivation.hasBeenActive, muted: this.muted, volume: this.volume, outcome: 'pending' }
      audit.plays.push(record)
      const nativePromise = Reflect.apply(originalPlay, this, params)
      nativePromise.then(() => { record.outcome = 'fulfilled' }, error => {
        record.outcome = 'rejected'; record.name = error.name; record.message = error.message; record.isDOMException = error instanceof DOMException
      })
      return nativePromise
    }
    for (const type of ['click', 'input', 'contextmenu']) document.addEventListener(type, event => {
      window.refusalAudit.inputEvents.push({ type, trusted: event.isTrusted, tag: event.target.tagName, active: navigator.userActivation.isActive, hasBeenActive: navigator.userActivation.hasBeenActive })
    }, true)
  })
  const page = await context.newPage()
  const cdp = await context.newCDPSession(page); debugSession = cdp
  const read = async expression => {
    const result = await cdp.send('Runtime.evaluate', { expression, userGesture: false, returnByValue: true, awaitPromise: true })
    if (result.exceptionDetails) throw new Error(result.exceptionDetails.text + JSON.stringify(result.exceptionDetails.exception))
    return result.result.value
  }
  const poll = async (expression, label) => {
    const deadline = Date.now() + 60000
    while (Date.now() < deadline) {
      const value = await read(expression)
      if (value) return value
      await new Promise(resolve => setTimeout(resolve, 100))
    }
    throw new Error(`Timed out: ${label}`)
  }
  const capture = async name => {
    const shot = await cdp.send('Page.captureScreenshot', { format: 'png', captureBeyondViewport: false })
    fs.writeFileSync(`${out}/${name}.png`, Buffer.from(shot.data, 'base64'))
  }
  const observe = async label => {
    const value = await read(`({ active:navigator.userActivation.isActive, hasBeenActive:navigator.userActivation.hasBeenActive, bubbles:[...document.querySelectorAll('[data-speech-state]')].map(el=>({state:el.dataset.speechState,visibility:getComputedStyle(el).visibility,animation:getComputedStyle(el,'::after').animationName,angle:getComputedStyle(el,'::after').getPropertyValue('--chat-speech-angle')})), players:window.refusalAudit.players.map(el=>({paused:el.paused,time:el.currentTime,src:el.getAttribute('src'),duration:Number.isFinite(el.duration)?el.duration:null})), plays:window.refusalAudit.plays.map(entry=>({...entry})), inputs:window.refusalAudit.inputEvents.map(entry=>({...entry})), nativePlay:window.refusalAudit.originalPlaySource, mediaEvents:window.refusalAudit.mediaEvents.map(entry=>({...entry})) })`)
    proof.observations.push({ label, requestCount: proof.playbackRequests.length, ...value }); return value
  }
  const pointer = async (elementExpression, which = 'left') => {
    const point = await read(`(()=>{const el=${elementExpression};if(!el)throw Error('missing pointer target');el.scrollIntoView({block:'center',behavior:'instant'});const r=el.getBoundingClientRect();return{x:r.x+r.width/2,y:r.y+Math.min(r.height/2,20)}})()`)
    const buttons = which === 'left' ? 1 : 2
    await cdp.send('Input.dispatchMouseEvent', { type: 'mousePressed', ...point, button: which, buttons, clickCount: 1 })
    await cdp.send('Input.dispatchMouseEvent', { type: 'mouseReleased', ...point, button: which, buttons: 0, clickCount: 1 })
  }
  page.on('pageerror', error => proof.pageErrors.push(error.message))

  const expertBot = '10000000-0000-4000-8000-000000000002'
  const bots = [bot,{...bot,id:expertBot,name:'read-aloud-expert',display_name:'合成专家 Bot'}]
  const mainFor = id => id===botId?sessionId:'20000000-0000-4000-8000-000000000003'
  const taskFor = id => id===botId?'20000000-0000-4000-8000-000000000002':'20000000-0000-4000-8000-000000000004'
  const sessionFor = (id,bid) => ({...session,id,bot_id:bid})
  const recordFor = id => `30000000-0000-4000-8000-${String(id).padStart(12,'0')}`
  let socket, seq=0, busy=true, run=1, currentSession=taskFor(botId), currentBot=botId
  let destinationBot=botId,destinationSession=taskFor(botId),hold=false,release,mode='ok'
  function snapshot() {return {type:'runtime_snapshot',session_id:currentSession,epoch:'home-admission',seq:++seq,snapshot:{bot_id:currentBot,session_id:currentSession,epoch:'home-admission',seq,current_run_view:{run_id:`result-run-${run}`,turn_id:`result-turn-${run}`,generation:'1',status:busy?'running':'completed',started_at:'2026-10-28T10:00:00Z',updated_at:'2026-10-28T10:00:00Z',messages:[{id:0,type:'text',content:`Home 边界合成回复第${run}段。`}]},updated_at:'2026-10-28T10:00:00Z'}}}
  fixtureSockets.on('connection',(ws,request)=>{socket=ws;currentBot=request.url.split('/')[2]||currentBot;ws.on('message',message=>{const data=JSON.parse(String(message));if(data.type==='runtime_subscribe'){currentSession=data.session_id||currentSession;ws.send(JSON.stringify(snapshot()))}})})
  viteProcess=spawn(process.execPath,['node_modules/vite/bin/vite.js','--host','127.0.0.1','--port',new URL(origin).port,'--strictPort','--config','/workspace/memoh-readaloud-home-review/apps/web/vite.read-aloud-review.config.ts'],{cwd:'/workspace/memoh-readaloud-home-review/apps/web',env:{...process.env,MEMOH_WEB_PROXY_TARGET:`http://127.0.0.1:${fixtureHTTP.address().port}`},stdio:['ignore','pipe','pipe']})
  viteProcess.stdout.on('data',data=>fs.appendFileSync(`${out}/vite.log`,data));viteProcess.stderr.on('data',data=>fs.appendFileSync(`${out}/vite.log`,data))
  for(let attempt=0;attempt<150;attempt++){if(viteProcess.exitCode!==null)throw Error(`Vite exited ${viteProcess.exitCode}`);let ready=false;try{ready=(await fetch(origin)).ok}catch{}if(ready)break;await new Promise(resolve=>setTimeout(resolve,200))}
  await page.route('https://fonts.googleapis.com/**',route=>route.fulfill({contentType:'text/css',body:''}))
  await page.route('**/api/**',async route=>{
    const req=route.request(),pathname=new URL(req.url()).pathname
    if(!pathname.startsWith('/api/'))return route.continue()
    const path=pathname.slice(4),requestBot=path.split('/')[2]
    if(path.endsWith('/sessions/contact')){const contact=sessionFor(mainFor(requestBot),requestBot);return route.fulfill({json:req.method()==='POST'?contact:{session:contact,candidates:[]}}).catch(()=>{})}
    if(path.endsWith('/tts/playback')){proof.playbackRequests.push({...req.postDataJSON(),bot:requestBot});return route.fulfill({contentType:'audio/wav',body:wav()})}
    if(req.method()!=='GET'){proof.writes.push({path,method:req.method()});return route.fulfill({json:{}})}
    let body={items:[]}
    if(path==='/users/me')body={id:'11111111-1111-4111-8111-111111111111',username:'synthetic',role:'admin',metadata:{onboarding_completed:true}}
    else if(path==='/bots')body={items:bots}
    else if(/^\/bots\/[^/]+$/.test(path))body=bots.find(entry=>entry.id===requestBot||entry.name===requestBot)
    else if(path.endsWith('/sessions'))body={items:[sessionFor(mainFor(requestBot),requestBot),sessionFor(taskFor(requestBot),requestBot)]}
    else if(/^\/bots\/[^/]+\/sessions\/[^/]+$/.test(path))body=sessionFor(path.split('/')[4],requestBot)
    else if(path.endsWith('/messages'))body={items:busy?[]:[{id:'persisted-result-'+run,role:'assistant',turn_id:'result-turn-'+run,timestamp:'2026-10-28T10:00:00Z',messages:[{id:0,type:'text',content:`Home 边界合成回复第${run}段。`}]}]}
    else if(path.includes('/tasks/native_agent/')){
      const id=Number(path.slice(-12)),bid=destinationBot,sid=destinationSession
      if(hold)await new Promise(resolve=>{release=resolve})
      if(mode==='denied'||mode==='expired')return route.fulfill({status:mode==='denied'?403:404,json:{code:mode==='denied'?'http.forbidden':'http.not_found'}})
      body={task_type:'native_agent',task_id:'bg_original',record_id:recordFor(id),name:'合成任务',state:'completed',created_at:'2026-10-28T10:00:00Z',destination:{bot_id:bid,session_id:sid,run_id:mode==='wrong-run'?'another-run':`result-run-${id}`,turn_id:`result-turn-${id}`}}
    }
    else if(path.endsWith('/events'))return route.fulfill({contentType:'text/event-stream',body:'event: activity_ready\ndata: {"type":"activity_ready","cache_invalidation":true}\n\n'})
    else if(path.endsWith('/queue'))body={follow_up:[],steer:[],steer_supported:true}
    else if(path==='/models')body=[{id:'synthetic-chat',name:'Synthetic',model_id:'synthetic',type:'chat',client_type:'openai-completions'}]
    else if(path==='/users/me/runtimes'||path==='/providers')body=[]
    else if(path.includes('/settings'))body={chat_model_id:'synthetic-chat',tts_model_id:'existing-configured-tts'}
    return route.fulfill({json:body})
  })
  const globals="document.querySelector('#app').__vue_app__.config.globalProperties"
  const taskURL=(id,bid)=>`/bot/${bots.find(entry=>entry.id===bid).name}?${new URLSearchParams({session:taskFor(bid),task_bot:botId,task_type:'native_agent',task_id:'bg_original',task_record:recordFor(id),run:`result-run-${id}`})}`
  const navigate=path=>read(`${globals}.$router.push(${JSON.stringify(path)})`)
  const navigation=()=>read(`(()=>{const g=${globals},c=g.$pinia._s.get('chat'),p=g.$pinia._s.get('workspace-tabs').api?.activePanel;return {url:location.pathname+location.search,bot:c.currentBotId,session:c.sessionId,panel:p?.id,anchor:p?.params?.taskAnchor?.runId,contactView:p?.params?.contactView,workspaceMasked:[...document.querySelectorAll('[data-speech-state]')].some(el=>!!el.closest('.invisible')),visibleBubbles:[...document.querySelectorAll('[data-speech-state]')].filter(el=>el.checkVisibility()&&getComputedStyle(el).visibility==='visible').length,body:document.body.innerText}})()`)
  const silent=observation=>{assert(observation.players.every(player=>player.paused),'Home admission mask must immediately stop old native audio');assert(observation.bubbles.every(item=>item.state!=='playing'&&item.animation==='none'),'Home admission mask must stop all bubble frames')}
  async function liveReply(id){
    const target=await navigation();currentBot=target.bot;currentSession=target.session
    const count=proof.playbackRequests.length;run=id;busy=true;socket.send(JSON.stringify(snapshot()))
    await poll(`document.body.textContent.includes('Home 边界合成回复第${id}段。')`,'fresh reply observed for next boundary')
    busy=false;socket.send(JSON.stringify(snapshot()))
    await poll("document.querySelector('[data-speech-state=playing]') && window.refusalAudit.players.some(player=>!player.paused&&player.currentTime>0.1)",'native playback baseline')
    assert.equal(proof.playbackRequests.length,count+1)
    assert.equal(proof.playbackRequests.at(-1).text,`Home 边界合成回复第${id}段。`)
  }
  async function pending(id,bid,label){
    const before=await navigation(),count=proof.playbackRequests.length
    destinationBot=bid;destinationSession=taskFor(bid);hold=true;release=undefined;mode='ok'
    await navigate(taskURL(id,bid))
    for(let attempt=0;attempt<80&&!release;attempt++)await new Promise(resolve=>setTimeout(resolve,100))
    assert.equal(typeof release,'function','real Home task lookup reached the withheld fixture')
    await poll("[...document.querySelectorAll('[data-speech-state]')].some(el=>!!el.closest('.invisible'))",'Home URL admission masks workspace before pane parameters change')
    const state=await observe(label+'-pending'),nav=await navigation()
    proof.navigation??=[];proof.navigation.push({label,before,pending:nav})
    assert.equal(nav.anchor,before.anchor,'old pane must not receive the new anchor before Home authorization')
    assert.equal(nav.bot,before.bot,'old Bot remains selected during Home authorization')
    assert.equal(nav.visibleBubbles,0,'Home mask must actually hide all bubble contents');assert(state.bubbles.every(b=>b.visibility==='hidden'));await capture(label+'-pending');silent(state);assert.equal(proof.playbackRequests.length,count)
    return {count,release:()=>{hold=false;release()},before}
  }
  await page.goto(origin+taskURL(1,botId))
  await poll("document.body.textContent.includes('Home 边界合成回复第1段。') && [...document.querySelectorAll('[data-speech-state]')].some(el=>el.checkVisibility()&&getComputedStyle(el).visibility==='visible')",'old authorized pane mounted')
  await pointer("document.querySelector('textarea')")
  await liveReply(2)
  const same=await pending(2,botId,'same-session-new-run');same.release()
  await poll(`(()=>{const p=${globals}.$pinia._s.get('workspace-tabs').api?.activePanel;return p?.params?.taskAnchor?.runId==='result-run-2'&&[...document.querySelectorAll('[data-speech-state]')].some(el=>el.checkVisibility()&&getComputedStyle(el).visibility==='visible')})()`,'new run admitted')
  silent(await observe('same-session-admitted'));assert.equal(proof.playbackRequests.length,same.count)
  proof.checks.push('same Bot and session: Home pending stops native media before new anchor; admission never restores old queue')
  await liveReply(3)
  const cross=await pending(4,expertBot,'cross-bot');run=4;busy=false;cross.release()
  await poll(`(()=>{const g=${globals},c=g.$pinia._s.get('chat'),p=g.$pinia._s.get('workspace-tabs').api?.activePanel;return c.currentBotId==='${expertBot}'&&p?.params?.taskAnchor?.runId==='result-run-4'&&[...document.querySelectorAll('[data-speech-state]')].some(el=>el.checkVisibility()&&getComputedStyle(el).visibility==='visible')})()`,'expert history admitted')
  silent(await observe('cross-bot-admitted'));assert.equal(proof.playbackRequests.length,cross.count)
  proof.checks.push('cross Bot and session: old Bot/pane stops before destination admission and does not resume')
  let nextRun=5
  for(const failure of ['denied','expired','wrong-run']){
    await liveReply(nextRun++)
    const request=await pending(nextRun++,expertBot,failure);mode=failure;request.release()
    await poll("document.querySelector('.invisible') && !document.body.innerText.includes('加载中')",failure+' Home error mask')
    silent(await observe(failure+'-error'));assert.equal(proof.playbackRequests.length,request.count)
    await capture(failure+'-error')
    await navigate('/bot/read-aloud-expert')
    await poll(`(()=>{const p=${globals}.$pinia._s.get('workspace-tabs').api?.activePanel;return p?.params?.contactView===true&&!p?.params?.taskAnchor&&[...document.querySelectorAll('[data-speech-state]')].some(el=>el.checkVisibility()&&getComputedStyle(el).visibility==='visible')})()`,'primary contact restored')
    silent(await observe(failure+'-return-contact'));assert.equal(proof.playbackRequests.length,request.count)
    proof.checks.push(failure+': Home error remains silent; actual primary contact return never restores old audio or queue')
    mode='ok'
  }
  await liveReply(nextRun++)
  const stale=await pending(nextRun++,expertBot,'stale-response')
  await navigate('/bot/read-aloud-expert')
  await poll(`(()=>{const p=${globals}.$pinia._s.get('workspace-tabs').api?.activePanel;return p?.params?.contactView===true&&!p?.params?.taskAnchor&&[...document.querySelectorAll('[data-speech-state]')].some(el=>el.checkVisibility()&&getComputedStyle(el).visibility==='visible')})()`,'leave pending authorization for contact')
  stale.release();await new Promise(resolve=>setTimeout(resolve,250))
  const returned=await navigation();assert.equal(returned.contactView,true);assert.equal(returned.anchor,undefined)
  silent(await observe('late-response-after-contact'));assert.equal(proof.playbackRequests.length,stale.count)
  await liveReply(nextRun)
  await capture('primary-contact-new-reply');await observe('primary-contact-new-reply')
  proof.checks.push('late URL admission cannot repoint the primary contact or restart old audio; its new live reply plays normally')
  assert.deepEqual(proof.pageErrors,[])
  fs.writeFileSync(`${out}/result.json`,JSON.stringify(proof,null,2)+'\n');console.log(JSON.stringify(proof,null,2))
} catch(error){
  if(debugSession){const result=await debugSession.send('Runtime.evaluate',{expression:'document.body.innerText',userGesture:false,returnByValue:true}).catch(()=>({}));proof.failureBody=result.result?.value;const shot=await debugSession.send('Page.captureScreenshot',{format:'png'}).catch(()=>({}));if(shot.data)fs.writeFileSync(`${out}/failure.png`,Buffer.from(shot.data,'base64'))}
  fs.writeFileSync(`${out}/failure.json`,JSON.stringify(proof,null,2)+'\n');throw error
} finally {
  await browser.close();for(const client of fixtureSockets.clients)client.terminate()
  await new Promise(resolve=>fixtureSockets.close(resolve));await new Promise(resolve=>fixtureHTTP.close(resolve))
  if(viteProcess&&viteProcess.exitCode===null){viteProcess.kill('SIGTERM');await new Promise(resolve=>viteProcess.once('exit',resolve))}
}
