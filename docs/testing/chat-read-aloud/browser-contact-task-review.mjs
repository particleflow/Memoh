// Candidate contact/task navigation review with synthetic server/audio fixtures only.
// Observation uses raw CDP userGesture:false; native play() promises are returned unchanged.
// Run with READ_ALOUD_ORIGIN pointing to the current Vite app. No provider call.
import { chromium } from '/opt/codex/cua_node/lib/node_modules/playwright/index.mjs'
import fs from 'node:fs'
import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { spawn } from 'node:child_process'
import { WebSocketServer } from '/workspace/Memoh/node_modules/.pnpm/node_modules/ws/wrapper.mjs'
const origin = process.env.READ_ALOUD_ORIGIN || 'http://127.0.0.1:18483'
const isolatedURL = new URL(origin)
assert(isolatedURL.protocol === 'http:' && ['127.0.0.1', 'localhost'].includes(isolatedURL.hostname) && isolatedURL.port && isolatedURL.pathname === '/' && !isolatedURL.username && !isolatedURL.password && !isolatedURL.search && !isolatedURL.hash, 'refusal runner is restricted to an explicit loopback HTTP port')
const out = process.env.READ_ALOUD_EVIDENCE || '/tmp/memoh-readaloud-review/red'
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
const proof = { source: '2edfd88dafec2291df6eee9343a7a2bd494da24a', syntheticServerAndWav: true, realProviderCalls: 0, playbackRequests: [], writes: [], pageErrors: [], checks: [], observations: [] }
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
    const value = await read(`({ active:navigator.userActivation.isActive, hasBeenActive:navigator.userActivation.hasBeenActive, bubbles:[...document.querySelectorAll('[data-speech-state]')].map(el=>({state:el.dataset.speechState,animation:getComputedStyle(el,'::after').animationName,angle:getComputedStyle(el,'::after').getPropertyValue('--chat-speech-angle')})), players:window.refusalAudit.players.map(el=>({paused:el.paused,time:el.currentTime,src:el.getAttribute('src'),duration:Number.isFinite(el.duration)?el.duration:null})), plays:window.refusalAudit.plays.map(entry=>({...entry})), inputs:window.refusalAudit.inputEvents.map(entry=>({...entry})), nativePlay:window.refusalAudit.originalPlaySource, mediaEvents:window.refusalAudit.mediaEvents.map(entry=>({...entry})) })`)
    proof.observations.push({ label, requestCount: proof.playbackRequests.length, ...value }); return value
  }
  const pointer = async (elementExpression, which = 'left') => {
    const point = await read(`(()=>{const el=${elementExpression};if(!el)throw Error('missing pointer target');el.scrollIntoView({block:'center',behavior:'instant'});const r=el.getBoundingClientRect();return{x:r.x+r.width/2,y:r.y+Math.min(r.height/2,20)}})()`)
    const buttons = which === 'left' ? 1 : 2
    await cdp.send('Input.dispatchMouseEvent', { type: 'mousePressed', ...point, button: which, buttons, clickCount: 1 })
    await cdp.send('Input.dispatchMouseEvent', { type: 'mouseReleased', ...point, button: which, buttons: 0, clickCount: 1 })
  }
  page.on('pageerror', error => proof.pageErrors.push(error.message))

  let socket, seq = 0, busy = true, run = 1, holdContact = false, releaseContact
  const mainSession = sessionId
  const taskSession = '20000000-0000-4000-8000-000000000002'
  const destination = {bot_id:botId,session_id:taskSession}
  const task = {task_type:'native_agent',task_id:'bg_original',record_id:'30000000-0000-4000-8000-000000000001',name:'合成后台任务',state:'running',created_at:'2026-10-28T10:00:00Z',destination}
  let currentSession = taskSession
  function snapshot() {
    return {type:'runtime_snapshot',session_id:currentSession,epoch:'new-contact-review',seq:++seq,snapshot:{bot_id:botId,session_id:currentSession,epoch:'new-contact-review',seq,current_run_view:{run_id:`result-run-${run}`,turn_id:`result-turn-${run}`,generation:'1',status:busy?'running':'completed',started_at:'2026-10-28T10:00:00Z',updated_at:'2026-10-28T10:00:00Z',messages:[{id:0,type:'text',content:`主管 Bot 转述后台任务结果第${run}段。`}]},updated_at:'2026-10-28T10:00:00Z'}}
  }
  fixtureSockets.on('connection', ws => {socket=ws;ws.on('message',message=>{const data=JSON.parse(String(message));if(data.type==='runtime_subscribe'){currentSession=data.session_id || currentSession;ws.send(JSON.stringify(snapshot()))}})})
  viteProcess = spawn(process.execPath, ['node_modules/vite/bin/vite.js','--host','127.0.0.1','--port',new URL(origin).port,'--strictPort','--config','/workspace/memoh-readaloud-review-2edf/apps/web/vite.read-aloud-review.config.ts'],{cwd:'/workspace/memoh-readaloud-review-2edf/apps/web',env:{...process.env,MEMOH_WEB_PROXY_TARGET:`http://127.0.0.1:${fixtureHTTP.address().port}`},stdio:['ignore','pipe','pipe']})
  viteProcess.stdout.on('data',data=>fs.appendFileSync(`${out}/vite.log`,data));viteProcess.stderr.on('data',data=>fs.appendFileSync(`${out}/vite.log`,data))
  for(let attempt=0;attempt<150;attempt++){if(viteProcess.exitCode!==null)throw Error(`Vite exited ${viteProcess.exitCode}`);let ready=false;try{ready=(await fetch(origin)).ok}catch{}if(ready)break;await new Promise(resolve=>setTimeout(resolve,200))}
  await page.route('https://fonts.googleapis.com/**',route=>route.fulfill({contentType:'text/css',body:''}))
  await page.route('**/api/**',async route=>{
    const req=route.request(),pathname=new URL(req.url()).pathname
    if(!pathname.startsWith('/api/'))return route.continue()
    const path=pathname.slice(4)
    if(path.endsWith('/sessions/contact')){
      if(req.method()==='POST' && holdContact)await new Promise(resolve=>{releaseContact=resolve})
      return route.fulfill({json:req.method()==='POST'?session:{session,candidates:[]}}).catch(()=>{})
    }
    if(path.endsWith('/tts/playback')){proof.playbackRequests.push(req.postDataJSON());return route.fulfill({contentType:'audio/wav',body:wav()})}
    if(req.method()!=='GET'){proof.writes.push({path,method:req.method()});return route.fulfill({json:{}})}
    let body={items:[]}
    if(path==='/users/me')body={id:'11111111-1111-4111-8111-111111111111',username:'synthetic',role:'admin',metadata:{onboarding_completed:true}}
    else if(path==='/bots')body={items:[bot]}
    else if(path===`/bots/${botId}`)body=bot
    else if(path.endsWith('/sessions'))body={items:[session,{...session,id:taskSession,title:'合成后台执行会话'}]}
    else if(path.endsWith(`/sessions/${taskSession}`))body={...session,id:taskSession,title:'合成后台执行会话'}
    else if(path.endsWith(`/sessions/${mainSession}`))body=session
    else if(path.endsWith('/messages'))body={items:busy?[]:[{id:'persisted-result-'+run,role:'assistant',turn_id:'result-turn-'+run,timestamp:'2026-10-28T10:00:00Z',messages:[{id:0,type:'text',content:`主管 Bot 转述后台任务结果第${run}段。`}]}]}
    else if(path.endsWith('/tasks'))body={items:[task]}
    else if(path.includes('/tasks/native_agent/'))body=task
    else if(path.endsWith('/events'))return route.fulfill({contentType:'text/event-stream',body:'event: activity_ready\ndata: {"type":"activity_ready","cache_invalidation":true}\n\n'})
    else if(path.endsWith('/queue'))body={follow_up:[],steer:[],steer_supported:true}
    else if(path==='/models')body=[{id:'synthetic-chat',name:'Synthetic',model_id:'synthetic',type:'chat',client_type:'openai-completions'}]
    else if(path==='/users/me/runtimes'||path==='/providers')body=[]
    else if(path.includes('/settings'))body={chat_model_id:'synthetic-chat',tts_model_id:'existing-configured-tts'}
    return route.fulfill({json:body})
  })
  await page.goto(`${origin}/bot/${botId}?session=${taskSession}`)
  await poll("document.querySelector('#app')?.__vue_app__ && document.body.textContent.includes('主管 Bot 转述后台任务结果第1段。')",'task chat mounted')
  await pointer("document.querySelector('textarea')")
  run=2;busy=true;socket.send(JSON.stringify(snapshot()))
  await poll("document.body.textContent.includes('主管 Bot 转述后台任务结果第2段。')",'new result-only projection observed')
  busy=false;socket.send(JSON.stringify(snapshot()))
  await poll("document.querySelector('[data-speech-state=playing]') && window.refusalAudit.players[0].currentTime>0.1",'task reply speaking')
  proof.checks.push('candidate result-only run with no user bubble speaks extracted assistant text')
  holdContact=true
  await read(`document.querySelector('#app').__vue_app__.config.globalProperties.$router.push('/bot/${botId}')`)
  await poll("document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('chat').contactNavigationPending === true",'new same-Bot contact gate pending')
  const pending=await observe('contact-return-pending-mask')
  pending.dom = await read("({url:location.pathname+location.search,pending:document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('chat').contactNavigationPending,visibleSpeech:[...document.querySelectorAll('[data-speech-state]')].map(el=>({state:el.dataset.speechState,visible:el.checkVisibility(),content:el.textContent})),body:document.body.innerText})")
  proof.navigation = pending.dom
  await capture('candidate-contact-pending')
  for (let attempt=0;attempt<50 && !releaseContact;attempt++) await new Promise(resolve=>setTimeout(resolve,100))
  assert.equal(typeof releaseContact,'function','contact fixture must receive the POST before release')
  holdContact=false
  releaseContact()
  assert(pending.players.every(player=>player.paused),'pending contact return must stop old task audio before resolution')
  assert(pending.bubbles.every(item=>item.state!=='playing' && item.animation==='none'),'pending contact return must stop old task frame')
  proof.checks.push('pending same-Bot return stops old task audio and frame before contact response')
  await poll("document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('chat').contactNavigationPending === false", 'main contact bound')
  await new Promise(resolve=>setTimeout(resolve,350))
  const returned=await observe('contact-resolved-without-catch-up')
  assert(returned.players.every(player=>player.paused))
  assert.equal(proof.playbackRequests.length,1)
  assert(returned.bubbles.every(item=>item.state!=='playing' && item.animation==='none'))
  proof.checks.push('resolved main contact does not restore old task playback')
  fs.writeFileSync(`${out}/result.json`,JSON.stringify(proof,null,2)+'\n')
  console.log(JSON.stringify(proof,null,2))
} catch (error) {
  if (debugSession) {
    const result = await debugSession.send('Runtime.evaluate', { expression: 'document.body.innerText', userGesture: false, returnByValue: true }).catch(() => ({}))
    fs.writeFileSync(`${out}/failure.txt`, result.result?.value || '')
    const shot = await debugSession.send('Page.captureScreenshot', { format: 'png' }).catch(() => ({}))
    if (shot.data) fs.writeFileSync(`${out}/failure.png`, Buffer.from(shot.data,'base64'))
  }
  if (debugSession) {
    const detail = await debugSession.send('Runtime.evaluate', {expression: `(()=>{const p=document.querySelector('#app')?.__vue_app__?.config.globalProperties.$pinia;const c=p?._s.get('chat');return {url:location.href,contactPending:c?.contactNavigationPending,contactBusy:c?.contactBusy,request:c?.contactOpenRequested,loading:c?.loadingChats,bot:c?.currentBotId,session:c?.sessionId,hasWorkspaceApi:!!p?._s.get('workspace-tabs')?.api}})()`,userGesture:false,returnByValue:true}).catch(()=>({}));proof.failureNavigation=detail.result?.value
  }
  fs.writeFileSync(`${out}/failure.json`, JSON.stringify(proof, null, 2)+'\n')
  throw error
} finally {
  await browser.close()
  for (const client of fixtureSockets.clients) client.terminate()
  await new Promise(resolve => fixtureSockets.close(resolve))
  await new Promise(resolve => fixtureHTTP.close(resolve))
  if (viteProcess && viteProcess.exitCode === null) {
    viteProcess.kill('SIGTERM')
    await new Promise(resolve => viteProcess.once('exit', resolve))
  }
}
