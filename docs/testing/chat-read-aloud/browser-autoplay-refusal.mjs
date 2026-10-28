// Real Chromium autoplay-policy refusal with synthetic server/audio fixtures only.
// Observation uses raw CDP userGesture:false; native play() promises are returned unchanged.
// Run with READ_ALOUD_ORIGIN pointing to the current Vite app. No provider call.
import { chromium } from '/opt/codex/cua_node/lib/node_modules/playwright/index.mjs'
import fs from 'node:fs'
import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { spawn } from 'node:child_process'
import { WebSocketServer } from '../../../node_modules/.pnpm/node_modules/ws/wrapper.mjs'
const origin = process.env.READ_ALOUD_ORIGIN || 'http://127.0.0.1:18482'
const isolatedURL = new URL(origin)
assert(isolatedURL.protocol === 'http:' && ['127.0.0.1', 'localhost'].includes(isolatedURL.hostname) && isolatedURL.port && isolatedURL.pathname === '/' && !isolatedURL.username && !isolatedURL.password && !isolatedURL.search && !isolatedURL.hash, 'refusal runner is restricted to an explicit loopback HTTP port')
const out = process.env.READ_ALOUD_EVIDENCE || '/tmp/memoh-autoplay-refusal/final'
fs.mkdirSync(out, { recursive: true })
const botId = '10000000-0000-4000-8000-000000000001', sessionId = '20000000-0000-4000-8000-000000000001'
const bot = { id: botId, name: 'read-aloud-synthetic', display_name: '朗读交互模拟验证', status: 'ready', current_user_permissions: ['manage'], chat_model_id: 'synthetic-chat', runtime_type: 'native' }
const session = { id: sessionId, bot_id: botId, title: '仅合成音频 · 无模型调用', type: 'chat', runtime_type: 'native', channel_type: 'local', created_at: '2026-10-28T10:00:00Z', updated_at: '2026-10-28T10:00:00Z' }
// A playable, quiet 2-second PCM WAV. This verifies media events, not TTS quality.
function wav() {
  const rate = 8000, frames = rate * 2, data = Buffer.alloc(44 + frames * 2)
  data.write('RIFF', 0); data.writeUInt32LE(data.length - 8, 4); data.write('WAVEfmt ', 8)
  data.writeUInt32LE(16, 16); data.writeUInt16LE(1, 20); data.writeUInt16LE(1, 22)
  data.writeUInt32LE(rate, 24); data.writeUInt32LE(rate * 2, 28); data.writeUInt16LE(2, 32); data.writeUInt16LE(16, 34)
  data.write('data', 36); data.writeUInt32LE(frames * 2, 40)
  for (let i = 0; i < frames; i++) data.writeInt16LE(Math.round(Math.sin(i * Math.PI / 20) * 50), 44 + i * 2)
  return data
}

const args = ['--no-sandbox', '--autoplay-policy=document-user-activation-required', '--disable-features=PreloadMediaEngagementData,MediaEngagementBypassAutoplayPolicies']
const browser = await chromium.launch({ headless: true, executablePath: '/usr/bin/chromium', args })
const proof = { source: '62abcbe1a50f2c0b9f29de855222dcf7d3da9661', syntheticServerAndWav: true, realNativePolicyRefusal: false, realProviderCalls: 0, playbackRequests: [], writes: [], pageErrors: [], checks: [], observations: [] }
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
  const button = text => `([...document.querySelectorAll('button')].find(el=>el.textContent.trim()===${JSON.stringify(text)}))`
  const bubble = id => `([...document.querySelectorAll('[data-speech-state]')].find(el=>el.textContent.includes('第${id}段拒绝恢复验证')))`
  const pointer = async (elementExpression, which = 'left') => {
    const point = await read(`(()=>{const el=${elementExpression};if(!el)throw Error('missing pointer target');el.scrollIntoView({block:'center',behavior:'instant'});const r=el.getBoundingClientRect();return{x:r.x+r.width/2,y:r.y+Math.min(r.height/2,20)}})()`)
    const buttons = which === 'left' ? 1 : 2
    await cdp.send('Input.dispatchMouseEvent', { type: 'mousePressed', ...point, button: which, buttons, clickCount: 1 })
    await cdp.send('Input.dispatchMouseEvent', { type: 'mouseReleased', ...point, button: which, buttons: 0, clickCount: 1 })
  }
  page.on('pageerror', error => proof.pageErrors.push(error.message))
  let socket, seq = 0, busy = true, run = 1, delayNext = false
  const settled = new Map()
  const reply = id => id === 1 ? '打开时的历史回复。' : `第${id}段拒绝恢复验证：` + '这是合成音频的队列验证，打断后旧片段必须取消。'.repeat(25)
  function snapshot() {
    if (!busy) settled.set(run, [
      { id: `history-user-${run}`, turn_id: `speech-turn-${run}`, role: 'user', text: '请回复一段文字。', timestamp: '2026-10-28T10:00:00Z' },
      { id: `history-assistant-${run}`, turn_id: `speech-turn-${run}`, role: 'assistant', messages: [{ id: 0, type: 'text', content: reply(run) }], timestamp: '2026-10-28T10:00:00Z' },
    ])
    return { type: 'runtime_snapshot', session_id: sessionId, epoch: 'speech-fixture', seq: ++seq, snapshot: {
      bot_id: botId, session_id: sessionId, epoch: 'speech-fixture', seq,
      current_run_view: { run_id: `speech-run-${run}`, turn_id: `speech-turn-${run}`, generation: '1', status: busy ? 'running' : 'completed',
        started_at: '2026-10-28T10:00:00Z', updated_at: '2026-10-28T10:00:00Z',
        messages: [{ id: 0, type: 'text', content: reply(run) }],
        request_user_turn: { turn_id: `speech-turn-${run}`, role: 'user', text: '请回复一段文字。', timestamp: '2026-10-28T10:00:00Z' } },
      updated_at: '2026-10-28T10:00:00Z' } }
  }
  fixtureSockets.on('connection', ws => { socket = ws; ws.on('message', message => {
    const data = JSON.parse(String(message))
    if (data.type === 'runtime_subscribe') ws.send(JSON.stringify(snapshot()))
  }) })
  const vitePort = new URL(origin).port
  viteProcess = spawn(process.execPath, ['node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--port', vitePort, '--strictPort'], {
    cwd: new URL('../../../apps/web', import.meta.url),
    env: { ...process.env, MEMOH_WEB_PROXY_TARGET: `http://127.0.0.1:${fixtureHTTP.address().port}` },
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  viteProcess.stdout.on('data', data => fs.appendFileSync(`${out}/vite.log`, data))
  viteProcess.stderr.on('data', data => fs.appendFileSync(`${out}/vite.log`, data))
  let ready = false
  for (let attempt = 0; attempt < 150; attempt++) {
    if (viteProcess.exitCode !== null) throw new Error(`Vite exited ${viteProcess.exitCode}`)
    try { ready = (await fetch(origin)).ok } catch { /* loopback startup only */ }
    if (ready) break
    await new Promise(resolve => setTimeout(resolve, 200))
  }
  assert(ready, 'isolated Vite must start')
  await page.route('https://fonts.googleapis.com/**', route => route.fulfill({ contentType: 'text/css', body: '' }))
  await page.route('**/api/**', async route => {
    const req = route.request(), pathname = new URL(req.url()).pathname
    if (!pathname.startsWith('/api/')) return route.continue()
    const path = pathname.slice(4)
    if (path.endsWith('/sessions/contact')) return route.fulfill({ json: req.method() === 'POST' ? session : { session, candidates: [] } })
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
    else if (path.endsWith('/messages')) body = { items: [...settled.values()].flat() }
    else if (path.endsWith('/sessions')) body = { items: [session] }
    else if (path.endsWith(`/sessions/${sessionId}`)) body = session
    else if (path.endsWith('/events')) return route.fulfill({ contentType: 'text/event-stream', body: 'event: activity_ready\ndata: {"type":"activity_ready","cache_invalidation":true}\n\n' })
    else if (path.endsWith('/queue')) body = { follow_up: [], steer: [], steer_supported: true }
    else if (path === '/models') body = [{ id: 'synthetic-chat', name: 'Synthetic', model_id: 'synthetic', type: 'chat', client_type: 'openai-completions' }]
    else if (path === '/users/me/runtimes' || path === '/providers') body = []
    else if (path.includes('/settings')) body = { chat_model_id: 'synthetic-chat', tts_model_id: 'existing-configured-tts' }
    return route.fulfill({ json: body })
  })

  await page.goto(`${origin}/bot/${botId}`)
  await poll("document.querySelector('#app')?.__vue_app__ && document.body.textContent.includes('打开时的历史回复。')", 'shared app initial baseline')
  await new Promise(resolve => setTimeout(resolve, 500))
  assert.equal(proof.playbackRequests.length, 0)
  assert.equal(await read('navigator.userActivation.hasBeenActive'), false, 'no page evaluator or pointer may grant activation before refusal')
  run = 2; busy = true; socket.send(JSON.stringify(snapshot()))
  await poll("document.body.textContent.includes('第2段拒绝恢复验证')", 'live reply observed')
  busy = false; socket.send(JSON.stringify(snapshot()))
  await poll("document.querySelector('[data-speech-state=blocked]') && window.refusalAudit.plays[0]?.outcome === 'rejected'", 'real policy rejection')
  const blocked = await observe('native-policy-refused')
  assert(blocked.nativePlay.includes('[native code]'))
  assert.equal(blocked.hasBeenActive, false)
  assert.equal(blocked.plays[0].name, 'NotAllowedError'); assert.equal(blocked.plays[0].isDOMException, true)
  assert.equal(blocked.plays[0].hasBeenActive, false); assert.equal(blocked.plays[0].muted, false); assert.equal(blocked.plays[0].volume, 1)
  assert.equal(blocked.players[0].paused, true); assert.equal(blocked.players[0].time, 0)
  assert(blocked.bubbles.every(item => item.state !== 'playing' && item.animation === 'none'))
  assert.equal(proof.playbackRequests.length, 1)
  proof.realNativePolicyRefusal = true
  await capture('01-native-refusal')
  assert.equal(await read('navigator.userActivation.hasBeenActive'), false, 'raw screenshot must not introduce user activation')
  proof.checks.push('native unmuted play rejects with genuine NotAllowedError/DOMException before any activation; blocked UI and zero frame motion')

  // Queue another completed reply while the first segment remains blocked.
  run = 3; busy = true; socket.send(JSON.stringify(snapshot()))
  await poll("document.body.textContent.includes('第3段拒绝恢复验证')", 'second live reply observed')
  busy = false; socket.send(JSON.stringify(snapshot()))
  await new Promise(resolve => setTimeout(resolve, 500))
  assert.equal(proof.playbackRequests.length, 1, 'blocked first segment must retain remaining chunks and later reply without playing them')
  assert.equal(await read('navigator.userActivation.hasBeenActive'), false)
  await pointer(button('点击启用朗读'))
  await poll("document.querySelectorAll('[data-speech-state=playing]').length===1 && window.refusalAudit.players[0]?.currentTime>0.05", 'trusted gesture resumes native media')
  const resumed = await observe('trusted-enable-gesture-resumed')
  assert.equal(resumed.plays.length, 2); assert.equal(resumed.plays[1].outcome, 'fulfilled')
  assert.equal(resumed.plays[1].player, resumed.plays[0].player); assert.equal(resumed.plays[1].src, resumed.plays[0].src)
  assert.equal(proof.playbackRequests.length, 1, 'resume must use loaded bytes without another TTS request')
  assert(resumed.inputs.some(item => item.type === 'click' && item.trusted && item.active))
  assert.equal(resumed.bubbles.filter(item => item.state === 'playing').length, 1)
  assert(resumed.bubbles.find(item => item.state === 'playing').animation.includes('chat-speech-orbit'))
  await capture('02-trusted-gesture-resumed')
  proof.checks.push('trusted browser pointer activates Enable; same native media/URL resumes; no resynthesis; only current bubble animates')

  await pointer(bubble(2))
  await poll("!document.querySelector('[data-speech-state=playing]') && window.refusalAudit.players.every(el=>el.paused && el.getAttribute('src')==='')", 'message click interruption')
  await new Promise(resolve => setTimeout(resolve, 2700))
  socket.send(JSON.stringify(snapshot()))
  await new Promise(resolve => setTimeout(resolve, 350))
  const clicked = await observe('message-click-cancelled-all-queued-work')
  assert.equal(proof.playbackRequests.length, 1)
  assert.equal(clicked.plays.length, 2)
  assert(clicked.bubbles.every(item => item.state !== 'playing' && item.animation === 'none'))
  await capture('03-click-stopped')
  proof.checks.push('trusted message click stops resumed media/frame and clears long-reply chunks plus later queued reply; nothing resumes past old audio duration or repeated completed snapshot')

  await pointer(bubble(3), 'right')
  await poll("[...document.querySelectorAll('[role=menuitem]')].some(el=>el.textContent.trim()==='重播朗读')", 'replay menu')
  await pointer("([...document.querySelectorAll('[role=menuitem]')].find(el=>el.textContent.trim()==='重播朗读'))")
  await poll("document.querySelectorAll('[data-speech-state=playing]').length===1 && window.refusalAudit.plays.at(-1)?.outcome==='fulfilled' && window.refusalAudit.players[0]?.currentTime>0.05", 'explicit gesture replay after cancelled refusal queue')
  assert.equal(proof.playbackRequests.length, 2)
  assert(await read(`${bubble(3)}.dataset.speechState==='playing'`))
  proof.checks.push('trusted right-click/menu replay explicitly synthesizes only the selected message after cancellation')
  await pointer("document.querySelector('textarea')")
  await cdp.send('Input.insertText', { text: '输入新消息立即打断' })
  await poll("!document.querySelector('[data-speech-state=playing]') && window.refusalAudit.players.every(el=>el.paused && el.getAttribute('src')==='')", 'trusted input interruption')
  await cdp.send('Input.dispatchKeyEvent', { type: 'rawKeyDown', key: 'a', code: 'KeyA', modifiers: 2, windowsVirtualKeyCode: 65 })
  await cdp.send('Input.dispatchKeyEvent', { type: 'keyUp', key: 'a', code: 'KeyA', modifiers: 2, windowsVirtualKeyCode: 65 })
  await cdp.send('Input.dispatchKeyEvent', { type: 'rawKeyDown', key: 'Backspace', code: 'Backspace', windowsVirtualKeyCode: 8 })
  await cdp.send('Input.dispatchKeyEvent', { type: 'keyUp', key: 'Backspace', code: 'Backspace', windowsVirtualKeyCode: 8 })
  await poll("document.querySelector('textarea').value === ''", 'draft cleared')
  await new Promise(resolve => setTimeout(resolve, 2700))
  socket.send(JSON.stringify(snapshot())); await new Promise(resolve => setTimeout(resolve, 350))
  const typed = await observe('trusted-input-stopped-and-draft-cleared')
  assert.equal(proof.playbackRequests.length, 2)
  assert.equal(typed.plays.length, 3)
  assert(typed.inputs.some(item => item.type === 'input' && item.trusted))
  assert(typed.bubbles.every(item => item.state !== 'playing' && item.animation === 'none'))
  assert.equal(await read("document.querySelectorAll('audio[controls]').length"), 0)
  await capture('04-input-stopped')
  proof.checks.push('trusted native text input stops explicit replay and pending chunks; clearing the draft and later snapshots do not restore audio/frame')
  assert.deepEqual(proof.pageErrors, []); assert.deepEqual(proof.writes, [])
  fs.writeFileSync(`${out}/result.json`, JSON.stringify(proof, null, 2)+'\n')
  console.log(JSON.stringify(proof, null, 2))
} catch (error) {
  if (debugSession) {
    const result = await debugSession.send('Runtime.evaluate', { expression: 'document.body.innerText', userGesture: false, returnByValue: true }).catch(() => ({}))
    fs.writeFileSync(`${out}/failure.txt`, result.result?.value || '')
    const shot = await debugSession.send('Page.captureScreenshot', { format: 'png' }).catch(() => ({}))
    if (shot.data) fs.writeFileSync(`${out}/failure.png`, Buffer.from(shot.data,'base64'))
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
