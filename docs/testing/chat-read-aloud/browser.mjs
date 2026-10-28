// Synthetic contract/UI verification only. Every API and WebSocket is intercepted.
// Run with READ_ALOUD_ORIGIN pointing to the current Vite app. No provider call.
import { chromium } from '/opt/codex/cua_node/lib/node_modules/playwright/index.mjs'
import fs from 'node:fs'
import assert from 'node:assert/strict'
const origin = process.env.READ_ALOUD_ORIGIN || 'http://127.0.0.1:18082'
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
    localStorage.setItem('token', 'synthetic-only'); localStorage.setItem('language', 'zh')
    const NativeAudio = window.Audio
    window.auditSpeechPlayers = []
    window.Audio = class extends NativeAudio { constructor(...args) { super(...args); window.auditSpeechPlayers.push(this) } }
  })
  const page = await context.newPage(); debugPage = page; page.setDefaultTimeout(20000)
  page.on('pageerror', error => proof.pageErrors.push(error.message))
  page.on('console', msg => { if (msg.type() === 'error') console.log('CONSOLE', msg.text()) })
  page.on('requestfailed', req => console.log('REQUESTFAILED', req.url(), req.failure()?.errorText))
  page.on('response', res => { if (res.status() >= 400) console.log('RESPONSE', res.status(), res.url()) })
  let socket, seq = 0, busy = true, run = 1, delayNext = false
  function snapshot() {
    return { type: 'runtime_snapshot', session_id: sessionId, epoch: 'speech-fixture', seq: ++seq, snapshot: {
      bot_id: botId, session_id: sessionId, epoch: 'speech-fixture', seq,
      current_run_view: { run_id: `speech-run-${run}`, turn_id: `speech-turn-${run}`, generation: '1', status: busy ? 'running' : 'completed',
        started_at: '2026-10-28T10:00:00Z', updated_at: '2026-10-28T10:00:00Z',
        messages: [{ id: 0, type: 'text', content: `第${run}段合成回复：朗读时只有当前气泡的边缘呼吸环绕。` }],
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
    else if (path.endsWith('/messages')) body = { items: busy ? [] : [
      { id: `history-user-${run}`, turn_id: `speech-turn-${run}`, role: 'user', text: '请回复一段文字。', timestamp: '2026-10-28T10:00:00Z' },
      { id: `history-assistant-${run}`, turn_id: `speech-turn-${run}`, role: 'assistant', messages: [{ id: 0, type: 'text', content: `第${run}段合成回复：朗读时只有当前气泡的边缘呼吸环绕。` }], timestamp: '2026-10-28T10:00:00Z' },
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
  await page.goto(`${origin}/bot/${botId}`)
  await page.waitForFunction(() => document.querySelector('#app')?.__vue_app__, {}, { timeout: 60000 })
  await page.waitForTimeout(1500)
  fs.writeFileSync(`${out}/initial.txt`, await page.locator('body').innerText())
  await page.screenshot({ path: `${out}/initial.png` })
  await page.getByText('第1段合成回复', { exact: false }).first().waitFor()
  await page.waitForTimeout(700)
  assert.equal(proof.playbackRequests.length, 0, 'opening history/in-progress reply must not catch up automatically')
  run = 2; busy = true; socket.send(JSON.stringify(snapshot()))
  const text = page.getByText('第2段合成回复', { exact: false }).first()
  await text.waitFor(); await page.waitForTimeout(300)
  assert.equal(proof.playbackRequests.length, 0, 'streaming response must not synthesize incomplete text')
  busy = false; socket.send(JSON.stringify(snapshot()))
  await page.waitForFunction(() => document.querySelector('[data-speech-state="playing"],[data-speech-state="blocked"]'))
  if (await page.getByRole('button', { name: '点击启用朗读', exact: true }).isVisible()) {
    assert.equal(await page.locator('[data-speech-state="playing"]').count(), 0)
    await page.screenshot({ path: `${out}/autoplay-blocked.png` })
    await page.getByRole('button', { name: '点击启用朗读', exact: true }).click()
    proof.checks.push('this fresh Chromium document blocked autoplay; explicit user gesture starts already loaded audio')
  } else proof.checks.push('this controlled Chromium document permitted autoplay; rejection/resume branch is covered by the mock media test')
  await page.waitForFunction(() => document.querySelectorAll('[data-speech-state="playing"]').length === 1)
  assert.equal(await page.locator('audio[controls]').count(), 0)
  await page.screenshot({ path: `${out}/playing.png` })
  proof.checks.push('actual playback; exactly one frame; no audio controls')
  await page.waitForFunction(() => window.auditSpeechPlayers.at(-1)?.currentTime > 0)
  await page.evaluate(() => window.auditSpeechPlayers.at(-1).pause())
  await page.getByRole('button', { name: '继续朗读', exact: true }).waitFor()
  assert.equal(await page.locator('[data-speech-state="playing"]').count(), 0)
  await page.getByRole('button', { name: '继续朗读', exact: true }).click()
  await page.waitForFunction(() => document.querySelectorAll('[data-speech-state="playing"]').length === 1)
  proof.checks.push('native audio currentTime advances; native pause removes frame; user resume restores it')
  await text.click()
  await page.waitForFunction(() => document.querySelectorAll('[data-speech-state="playing"]').length === 0)
  await page.waitForTimeout(650); assert.equal(proof.playbackRequests.length, 1)
  proof.checks.push('left click stops audio and does not restart')
  await text.click({ button: 'right' }); await page.getByRole('menuitem', { name: '重播朗读', exact: true }).click()
  await page.waitForFunction(() => document.querySelectorAll('[data-speech-state="playing"]').length === 1)
  const input = page.locator('textarea').first(); await input.fill('开始输入新消息')
  await page.waitForFunction(() => document.querySelectorAll('[data-speech-state="playing"]').length === 0)
  await input.fill(''); await page.waitForTimeout(650); assert.equal(proof.playbackRequests.length, 2)
  proof.checks.push('right-click replay; typing interrupts and clearing draft does not replay')
  delayNext = true
  await text.click({ button: 'right' }); await page.getByRole('menuitem', { name: '重播朗读', exact: true }).click()
  await page.waitForFunction(() => document.querySelector('[data-speech-state="loading"]'))
  await input.fill('取消在途合成'); await page.waitForTimeout(800)
  assert.equal(await page.locator('[data-speech-state="playing"]').count(), 0)
  await input.fill(''); assert.equal(proof.playbackRequests.length, 3)
  proof.checks.push('late synthetic audio after cancellation cannot start playback')
  // Exercise the actual Reka context-menu touch long-press route.
  const box = await text.boundingBox()
  await text.dispatchEvent('pointerdown', { pointerType: 'touch', pointerId: 2, button: 0, clientX: box.x + 10, clientY: box.y + 10, bubbles: true })
  await page.getByRole('menuitem', { name: '重播朗读', exact: true }).waitFor()
  await page.screenshot({ path: `${out}/long-press.png` })
  await page.getByRole('menuitem', { name: '重播朗读', exact: true }).click()
  await page.waitForFunction(() => document.querySelectorAll('[data-speech-state="playing"]').length === 1)
  proof.checks.push('touch pointer long-press opens replay menu')
  await page.emulateMedia({ reducedMotion: 'reduce', colorScheme: 'dark' }); await page.setViewportSize({ width: 390, height: 844 })
  const animation = await page.locator('[data-speech-state="playing"]').evaluate(el => getComputedStyle(el, '::after').animationName)
  assert.equal(animation, 'none')
  await page.screenshot({ path: `${out}/reduced-motion-dark-narrow.png` })
  await page.getByRole('button', { name: '停止朗读', exact: true }).click()
  assert.equal(await page.locator('[data-speech-state="playing"]').count(), 0)
  proof.checks.push('reduced-motion static frame; dark/narrow; keyboard-accessible stop action')
  assert.deepEqual(proof.pageErrors, [])
  fs.writeFileSync(`${out}/result.json`, JSON.stringify(proof, null, 2))
  console.log(JSON.stringify(proof, null, 2))
} catch (error) {
  if (debugPage) { fs.writeFileSync(`${out}/failure.txt`, await debugPage.locator('body').innerText()); await debugPage.screenshot({ path: `${out}/failure.png` }) }
  fs.writeFileSync(`${out}/failure.json`, JSON.stringify(proof, null, 2))
  throw error
} finally { await browser.close() }
