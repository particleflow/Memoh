// Synthetic contract/UI verification only. Every API and WebSocket is intercepted.
// Run with READ_ALOUD_ORIGIN pointing to the current Vite app. No provider call.
import { chromium } from '/opt/codex/cua_node/lib/node_modules/playwright/index.mjs'
import fs from 'node:fs'
import {createHash} from 'node:crypto'
import assert from 'node:assert/strict'
const origin = process.env.READ_ALOUD_ORIGIN || 'http://127.0.0.1:18954'
const out = process.env.READ_ALOUD_EVIDENCE || '/tmp/memoh-read-aloud-browser'
fs.mkdirSync(out, { recursive: true })
const botId = '10000000-0000-4000-8000-000000000001', sessionId = '20000000-0000-4000-8000-000000000001'
const bot = { id: botId, name: 'read-aloud-synthetic', display_name: '朗读交互模拟验证', status: 'ready', current_user_permissions: ['manage'], chat_model_id: 'synthetic-chat', runtime_type: 'native' }
const session = { id: sessionId, bot_id: botId, title: '仅合成音频 · 无模型调用', type: 'chat', runtime_type: 'native', channel_type: 'local', created_at: '2026-10-28T10:00:00Z', updated_at: '2026-10-28T10:00:00Z' }
// A playable, quiet 8-second PCM WAV. This verifies media events, not TTS quality.
function wav() {
  const rate = 8000, frames = rate * 8, data = Buffer.alloc(44 + frames * 2)
  data.write('RIFF', 0); data.writeUInt32LE(data.length - 8, 4); data.write('WAVEfmt ', 8)
  data.writeUInt32LE(16, 16); data.writeUInt16LE(1, 20); data.writeUInt16LE(1, 22)
  data.writeUInt32LE(rate, 24); data.writeUInt32LE(rate * 2, 28); data.writeUInt16LE(2, 32); data.writeUInt16LE(16, 34)
  data.write('data', 36); data.writeUInt32LE(frames * 2, 40)
  for (let i = 0; i < frames; i++) data.writeInt16LE(Math.round(Math.sin(i * Math.PI / 20) * 50), 44 + i * 2)
  return data
}
const browser = await chromium.launch({ headless: true, executablePath: '/usr/bin/chromium', args: ['--no-sandbox', '--autoplay-policy=document-user-activation-required'] })
let debugPage
const proof = { synthetic: true, realProviderCalls: 0, playbackRequests: [], writes: [], pageErrors: [], checks: [] }
try {
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 }, hasTouch: true, colorScheme: 'light' })
  await context.addInitScript(() => {
    localStorage.setItem('token', 'synthetic-only'); localStorage.setItem('language', 'zh'); localStorage.setItem('memoh:harmony:service-config:v1', JSON.stringify({version:1,apiOrigin:'https://api.example.invalid/api',hapDownloadUrl:'',resetPending:false}))
    window.nativeRequests = []
    window.begoniaNative = { request: async raw => {
      const r = JSON.parse(raw); window.nativeRequests.push({command:r.command,photos:r.photos})
      if (r.command === 'voice.start' || r.command === 'voice.status') return JSON.stringify({ok:true,data:{handle:r.handle,state:'recording',durationMs:100,peaks:[0.2],text:'',speechError:''}})
      if (r.command.startsWith('upload.')) return JSON.stringify({ok:true,data:[]})
      if (r.command === 'download.fetch') {
        if (!r.url.startsWith('https://api.example.invalid/api/bots/') || r.authorization !== 'Bearer synthetic-only') return JSON.stringify({ok:false,error:'MEDIA_DOWNLOAD_HTTP_403'})
        return JSON.stringify({ok:true,data:{operation:r.operation,hash:r.hash,size:r.size}})
      }
      return JSON.stringify({ok:true,data:true})
    }}
    const NativeAudio = window.Audio
    window.auditSpeechPlayers = []
    window.Audio = class extends NativeAudio { constructor(...args) { super(...args); window.auditSpeechPlayers.push(this) } }
  })
  const page = await context.newPage(); debugPage = page; page.setDefaultTimeout(20000)
  page.on('pageerror', error => { proof.pageErrors.push(error.message); console.log('PAGEERROR_STACK', error.stack, 'after', proof.checks.at(-1)) })
  page.on('console', msg => { if (msg.type() === 'error') console.log('CONSOLE', msg.text()) })
  page.on('requestfailed', req => console.log('REQUESTFAILED', req.url(), req.failure()?.errorText))
  page.on('response', res => { if (res.status() >= 400) console.log('RESPONSE', res.status(), res.url()) })
  let socket, seq = 0, busy = true, run = 1, delayNext = false, explicit = false
  const audioHash=createHash('sha256').update(wav()).digest('hex')
  const messageBlocks=()=>[{ id:0,type:'text',content:`第${run}段合成回复：朗读时只有当前气泡的边缘呼吸环绕。` },...(explicit?[{id:1,type:'attachments',attachments:[{type:'audio',mime:'audio/wav',name:'synthetic.wav',bot_id:botId,content_hash:audioHash,size:wav().length}]}]:[])]
  function snapshot() {
    return { type: 'runtime_snapshot', session_id: sessionId, epoch: 'speech-fixture', seq: ++seq, snapshot: {
      bot_id: botId, session_id: sessionId, epoch: 'speech-fixture', seq,
      current_run_view: { run_id: `speech-run-${run}`, turn_id: `speech-turn-${run}`, generation: '1', status: busy ? 'running' : 'completed',
        started_at: '2026-10-28T10:00:00Z', updated_at: '2026-10-28T10:00:00Z',
        messages: messageBlocks(),
        request_user_turn: { turn_id: `speech-turn-${run}`, role: 'user', text: '请回复一段文字。', timestamp: '2026-10-28T10:00:00Z' } },
      updated_at: '2026-10-28T10:00:00Z' } }
  }
  await page.routeWebSocket('**/api/bots/**', ws => { socket = ws; ws.onMessage(message => {
    const data = JSON.parse(message)
    if (data.type === 'runtime_subscribe') ws.send(JSON.stringify(snapshot()))
  }) })
  await page.route('https://fonts.googleapis.com/**', route => route.fulfill({ contentType: 'text/css', body: '' }))
  await page.route('**/api/**', async route => {
    const req = route.request(), pathname = new URL(req.url()).pathname
    if (!pathname.startsWith('/api/')) return route.continue()
    if (req.method() === 'OPTIONS') return route.fulfill({status:204,headers:{'Access-Control-Allow-Origin':'*','Access-Control-Allow-Headers':'authorization,content-type','Access-Control-Allow-Methods':'GET,POST,OPTIONS'}})
    const path = pathname.slice(4)
    if (path.endsWith('/sessions/contact')) return route.fulfill({ json: req.method() === 'POST' ? session : { session, candidates: [] } })
    if (path.includes('/media/') && req.method()==='GET') return route.fulfill({contentType:'audio/wav',body:wav()})
    if (path.endsWith('/tts/playback')) {
      proof.playbackRequests.push(req.postDataJSON())
      if (delayNext) { delayNext = false; await new Promise(resolve => setTimeout(resolve, 500)) }
      return route.fulfill({ contentType: 'audio/wav', body: wav() }).catch(() => {})
    }
    if (!['GET', 'HEAD'].includes(req.method())) { proof.writes.push({ path, method: req.method() }); return route.fulfill({ json: {} }) }
    let body = { items: [] }
    if (path === '/users/me') body = { id: '11111111-1111-4111-8111-111111111111', username: 'synthetic', role: 'admin', metadata: { onboarding_completed: true } }
    else if (path === '/bots') body = { items: [bot] }
    else if (path === `/bots/${botId}`) body = bot
    else if (path.endsWith('/messages')) body = { items: busy ? [] : [
      { id: `history-user-${run}`, turn_id: `speech-turn-${run}`, role: 'user', text: '请回复一段文字。', timestamp: '2026-10-28T10:00:00Z' },
      { id: `history-assistant-${run}`, turn_id: `speech-turn-${run}`, role: 'assistant', messages: messageBlocks(), timestamp: '2026-10-28T10:00:00Z' },
    ] }
    else if (path.endsWith('/sessions')) body = { items: [session] }
    else if (path.endsWith(`/sessions/${sessionId}`)) body = session
    else if (path.endsWith('/events')) return route.fulfill({ contentType: 'text/event-stream', body: 'event: activity_ready\ndata: {"type":"activity_ready","cache_invalidation":true}\n\n' })
    else if (path.endsWith('/queue')) body = { follow_up: [], steer: [], steer_supported: true }
    else if (path === '/models') body = [{ id: 'synthetic-chat', name: 'Synthetic', model_id: 'synthetic', type: 'chat', client_type: 'openai-completions' }]
    else if (path === '/users/me/runtimes' || path === '/providers') body = []
    else if (path.includes('/settings')) body = { chat_model_id: 'synthetic-chat', tts_model_id: 'existing-configured-tts' }
    return route.fulfill({ json: body })
  })
  await page.goto(`${origin}/#/bot/${botId}`)
  await page.getByText('第1段合成回复',{exact:false}).first().waitFor({timeout:60000})
  await page.waitForTimeout(500);assert.equal(proof.playbackRequests.length,0)
  run=2;busy=true;socket.send(JSON.stringify(snapshot()))
  await page.getByText('第2段合成回复',{exact:false}).first().waitFor();await page.waitForTimeout(200)
  busy=false;socket.send(JSON.stringify(snapshot()))
  await page.waitForFunction(()=>document.querySelector('[data-speech-state="playing"],[data-speech-state="blocked"]'))
  if(await page.getByRole('button',{name:'点击启用朗读',exact:true}).isVisible())await page.getByRole('button',{name:'点击启用朗读',exact:true}).click()
  await page.waitForFunction(()=>document.querySelectorAll('[data-speech-state="playing"]').length===1)
  const mic=page.locator('.harmony-voice-mic');assert.equal(await mic.isDisabled(),false)
  const box=await mic.boundingBox()
  await mic.evaluate(el=>el.addEventListener('pointerdown',e=>{window.microPointer=e.pointerId},{capture:true}))
  await page.mouse.move(box.x+box.width/2,box.y+box.height/2);await page.mouse.down()
  await page.waitForFunction(()=>window.nativeRequests.some(r=>r.command==='voice.start'))
  assert.equal(await page.locator('[data-speech-state="playing"]').count(),0,'native microphone pointerdown must stop reply audio before recording')
  await mic.dispatchEvent('pointercancel',{pointerType:'mouse',pointerId:await page.evaluate(()=>window.microPointer),isPrimary:true,button:0,bubbles:true});await page.mouse.up()
  await page.waitForFunction(()=>window.nativeRequests.some(r=>r.command==='voice.cancel'))
  proof.checks.push('native microphone begins on the existing bridge, stops reply audio, and cancel sends no chat message')
  await page.locator('.chat-composer-edge button[aria-label]').filter({has:page.locator('svg')}).first().click()
  await page.getByRole('menuitem',{name:'照片',exact:false}).click()
  await page.waitForFunction(()=>window.nativeRequests.some(r=>r.command==='upload.select' && r.photos===true))
  proof.checks.push('photo selection still reaches original upload.select photos=true; synthetic cancellation adds no attachment')
  explicit=true;run=3;busy=true;socket.send(JSON.stringify(snapshot()))
  await page.getByText('第3段合成回复',{exact:false}).first().waitFor();busy=false;socket.send(JSON.stringify(snapshot()))
  await page.getByRole('button',{name:'播放录音',exact:true}).waitFor()
  await page.getByRole('button',{name:'保存录音原件',exact:true}).click()
  await page.waitForFunction(()=>window.nativeRequests.some(r=>r.command==='download.save'))
  await page.getByRole('button',{name:'播放录音',exact:true}).click()
  await page.getByRole('button',{name:'暂停录音',exact:true}).waitFor()
  await page.getByRole('button',{name:'暂停录音',exact:true}).click()
  assert.equal(proof.playbackRequests.length,1,'explicit audio must not synthesize its caption again')
  proof.checks.push('explicit audio retains play/pause/save controls; existing download.fetch/save bridge preserved; no duplicate caption synthesis')
  await page.setViewportSize({width:390,height:844});await page.emulateMedia({colorScheme:'dark'})
  await page.screenshot({path:`${out}/native-controls.png`})
  proof.nativeCommands=await page.evaluate(()=>window.nativeRequests)
  assert.deepEqual(proof.pageErrors,[]);assert.deepEqual(proof.writes,[])
  fs.writeFileSync(`${out}/result.json`,JSON.stringify(proof,null,2));console.log(JSON.stringify(proof,null,2))
} catch (error) {
  if (debugPage) { fs.writeFileSync(`${out}/failure.txt`, await debugPage.locator('body').innerText()); await debugPage.screenshot({ path: `${out}/failure.png` }) }
  fs.writeFileSync(`${out}/failure.json`, JSON.stringify(proof, null, 2))
  throw error
} finally { await browser.close() }
