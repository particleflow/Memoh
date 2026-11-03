// Drive and assert the packaged HAP on a DevEco emulator (acceptance level 3).
//
// This is the emulator-side counterpart of acceptance.mjs. It drives the same
// packaged bundle, but through the real ArkUI shell + ArkWeb engine on a
// HarmonyOS image, so the native seams (ability lifecycle, read-aloud media
// playback, cross-origin fetch, resource interception) are in the loop.
//
// Every scenario runs in a device-level cold state: the process is stopped, the
// app's data (storage, cache, cookies) is wiped with `bm clean -d/-c`, and the
// app is started again. That mirrors level 2, which creates a fresh browser
// context per scenario, and it is the only state isolation the phone offers:
// the packaged document keeps its stream cursor in localStorage, so reusing
// data lets one scenario's cursor suppress the next reply.
//
// Evidence is restricted to three kinds, as in acceptance.mjs: text visible in
// the real app, requests the scripted service actually received, and the speech
// state the packaged bundle exposes on its own nodes. Timeline instrumentation
// is not injected: the app boots before this harness can attach, so an injected
// hook would miss the boot-time calls and report a false negative.
import { writeFileSync, mkdirSync } from 'node:fs'
import { join, resolve, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
import { execFileSync } from 'node:child_process'

const here = dirname(fileURLToPath(import.meta.url))
const HDC = process.env.ACCEPTANCE_HDC || 'C:/Program Files/Huawei/DevEco Studio/sdk/default/openharmony/toolchains/hdc.exe'
const SERIAL = process.env.ACCEPTANCE_SERIAL || '127.0.0.1:5555'
const BUNDLE = process.env.ACCEPTANCE_BUNDLE || 'cloud.begonia.workassistant'
const STUB_PORT = Number(process.env.STUB_PORT || 18081)
// The device reaches the host stub through the emulator gateway; the harness
// itself runs on the host, so control calls use loopback on the same port.
const DEVICE_HOST = process.env.ACCEPTANCE_DEVICE_HOST || '10.0.2.2'
const CONTROL = `http://127.0.0.1:${STUB_PORT}`
const PORT_BASE = Number(process.env.ACCEPTANCE_CDP_PORT || 9300)
const ACK_WINDOW_MS = 3000
// The stream start (run_accepted) is the earliest moment the ack block can be
// sent, and the stub itself holds it back by startDelay + 1200ms after the
// send. The emulator app adds ~2s of message pipeline over Chromium, so the
// observable window is measured from the stream's ack, not from the send.
const ACK_FROM_STREAM_MS = 2000
const FIRST_POST_WINDOW_MS = 4000
const HISTORY_MARK = '这是历史回复，加载时不应朗读。'
const sleep = ms => new Promise(r => setTimeout(r, ms))

let page = null
const hdc = (...args) => execFileSync(HDC, ['-t', SERIAL, ...args], { encoding: 'utf8' })

function ensureDevice() {
  const targets = execFileSync(HDC, ['list', 'targets'], { encoding: 'utf8' }).trim()
  if (targets.includes(SERIAL)) return
  const connect = execFileSync(HDC, ['tconn', SERIAL], { encoding: 'utf8' }).trim()
  if (!/Connect OK/i.test(connect)) throw new Error(`device not reachable: ${connect || 'no targets'}`)
}

async function waitFor(fn, timeout, interval = 200, label = 'condition') {
  const start = Date.now()
  let last
  while (Date.now() - start < timeout) {
    try { const value = await fn(); if (value) return value } catch (error) { last = error }
    await sleep(interval)
  }
  throw new Error(`timeout waiting for ${label}${last ? ': ' + last.message : ''}`)
}

class Page {
  constructor(socket) { this.socket = socket; this.id = 0; this.pending = new Map() }
  static async attach(port) {
    const list = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json()
    const target = list.find(t => t.type === 'page')
    if (!target) throw new Error('no ArkWeb page target; is the app running with web debugging on?')
    const socket = new WebSocket(target.webSocketDebuggerUrl)
    await new Promise((done, fail) => {
      socket.addEventListener('open', done, { once: true })
      socket.addEventListener('error', fail, { once: true })
    })
    const instance = new Page(socket)
    socket.addEventListener('message', event => {
      const message = JSON.parse(event.data)
      const pending = instance.pending.get(message.id)
      if (pending) { instance.pending.delete(message.id); pending(message) }
    })
    await instance.send('Runtime.enable')
    return instance
  }
  send(method, params = {}) {
    const id = ++this.id
    return new Promise(resolve => {
      this.pending.set(id, resolve)
      this.socket.send(JSON.stringify({ id, method, params }))
    })
  }
  async eval(expression) {
    const result = await this.send('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true, userGesture: true })
    if (result.result?.exceptionDetails) throw new Error(result.result.exceptionDetails.exception?.description || result.result.exceptionDetails.text)
    return result.result?.result?.value
  }
  wait(expression, timeout = 15000, label = expression) {
    return waitFor(() => this.eval(expression), timeout, 200, label)
  }
  close() { try { this.socket.close() } catch {} }
}

const control = (path, body, method = 'POST') =>
  fetch(`${CONTROL}${path}`, method === 'GET' ? { method } : { method, body: JSON.stringify(body ?? {}) }).then(r => r.json())

const text = async () => (await page.eval('document.body.innerText')) ?? ''
const speech = async () => (await control('/__control/speech', null, 'GET')).entries
const speechStates = async () => (await page.eval("[...document.querySelectorAll('[data-speech-state]')].map(n => n.getAttribute('data-speech-state'))")) ?? []
const playing = async () => (await speechStates()).includes('playing')
const visibility = async () => (await page.eval('document.visibilityState')) ?? 'unknown'
// The ability's window is named after the bundle's last segment plus an index
// (`workassistant0`), so the bundle's tail identifies it without hardcoding.
const windowTag = BUNDLE.split('.').pop()
const focusedWindow = () => {
  const dump = hdc('shell', "hidumper -s WindowManagerService -a '-a'")
  const win = (/Focus window: (\d+)/.exec(dump) || [])[1] || '?'
  const line = dump.split('\n').find(candidate => new RegExp(`\\s${win}\\s`).test(candidate)) || ''
  return { id: win, name: line.trim().split(/\s+/)[0] || '?' }
}

// ArkWeb stops running the page's render/observers while the document reports
// `visibilityState: hidden`, and read-aloud never starts in that state. On this
// image a freshly started app is *not* always the focused window: the IME's
// first-run guide (privacy notice + keyboard layout picker) is a full-screen
// window and takes window focus every time the login form's input is focused.
// A hidden page then renders no reply text and issues no speech request, which
// is exactly the flip this harness used to record. Neither the data wipe nor a
// restart clears it, because the guide belongs to the IME's own data.
//
// So every scenario waits for the app to actually own window focus and the
// document to report `visible` before injecting, and gives focus back if the
// IME (or anything else) has taken it.
async function ensureVisible(label, timeout = 40000) {
  const started = Date.now()
  while (Date.now() - started < timeout) {
    const state = await visibility()
    const owner = focusedWindow()
    if (state === 'visible' && owner.name.startsWith(windowTag)) return { state, owner: `${owner.id}:${owner.name}` }
    // Home parks whatever took focus, and the explicit start brings the ability
    // back to the front without restarting the process (so the document and its
    // stream cursor survive).
    hdc('shell', 'uitest uiInput keyEvent Home')
    await sleep(700)
    hdc('shell', `aa start -a EntryAbility -b ${BUNDLE}`)
    await sleep(1200)
  }
  const owner = focusedWindow()
  throw new Error(`page did not become visible (${label}): state=${await visibility()} focus=${owner.id}:${owner.name}`)
}

// The devtools socket carries the renderer pid, so it changes on every app
// restart. Each distinct socket gets its own local port: hdc keeps a binding
// for a local port, and a second rule on the same port silently keeps routing
// to the first (dead) renderer.
const forwardBySocket = new Map()
let nextForwardPort = PORT_BASE
const devtoolsSockets = () => hdc('shell', 'cat /proc/net/unix | grep webview_devtools_remote')
  .trim().split('\n').map(line => line.trim().split(/\s+/).pop().replace(/^@/, '')).filter(Boolean)
async function attachFresh(stale = new Set()) {
  page = await waitFor(async () => {
    // /proc/net/unix is not ordered by creation; the previous instance's socket
    // can still be listed here. Only a socket absent before this cold start
    // belongs to the renderer this scenario drives.
    const socket = devtoolsSockets().find(name => !stale.has(name))
    if (!socket) return null
    if (!forwardBySocket.has(socket)) {
      const port = nextForwardPort++
      const result = hdc('fport', `tcp:${port}`, `localabstract:${socket}`).trim()
      if (!/OK/i.test(result)) return null
      forwardBySocket.set(socket, port)
    }
    try { return await Page.attach(forwardBySocket.get(socket)) } catch { return null }
  }, 60000, 500, 'ArkWeb target')
}

// One scenario, one device-level cold app: stopped, data wiped, started again.
// Wiped data means no token, so every scenario signs in through the packaged
// login form; the scripted service answers that POST /auth/login. The
// scenario's own runtime_subscribe is the readiness signal; a fixed sleep
// would hide a boot that never reconnected.
async function coldStart({ seedHistory = false } = {}) {
  ensureDevice()
  hdc('shell', `aa force-stop ${BUNDLE}`)
  await waitFor(() => !hdc('shell', `pidof ${BUNDLE}`).trim(), 15000, 300, 'process exit')
  hdc('shell', `bm clean -n ${BUNDLE} -d`)
  hdc('shell', `bm clean -n ${BUNDLE} -c`)
  await control('/__control/reset', {})
  if (seedHistory) await control('/__control/history', {})
  const staleSockets = new Set(devtoolsSockets())
  const startedAt = Date.now()
  hdc('shell', `aa start -a EntryAbility -b ${BUNDLE}`)
  await attachFresh(staleSockets)
  // boot() appends the service-settings button only after main and the router
  // are live; before that the login form is inert and a click is lost.
  await page.wait('!!document.querySelector("#harmony-service-settings") && !!document.querySelector("#password")', 30000, 'login form')
  // Filling the form focuses an input, which raises the IME. If this is the
  // image's first run of it, the full-screen first-run guide takes focus and the
  // page goes hidden -- before sign-in, so waiting afterwards is not enough.
  await ensureVisible('login form')
  await page.eval(`(() => {
    const set = (selector, value) => {
      const input = document.querySelector(selector)
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(input, value)
      input.dispatchEvent(new Event('input', { bubbles: true }))
    }
    set('#username', 'acceptance'); set('#password', 'acceptance')
    return true
  })()`)
  // The form validates the bound values asynchronously; submit after they land.
  await sleep(500)
  await ensureVisible('before submit')
  await page.eval(`document.querySelector('#password').form.querySelector('button[type=submit]').click(), true`)
  await waitFor(async () => (await control('/__control/requests', null, 'GET')).items.some(item => item.path === '/auth/login'),
    10000, 200, 'POST /auth/login from the packaged form')
  const subscribed = await waitFor(async () => {
    const log = await control('/__control/messages', null, 'GET')
    return log.items.some(item => item.type === 'runtime_subscribe' && Date.parse(item.at) >= startedAt - 500)
  }, 60000, 300, 'runtime subscribe').then(() => true, () => false)
  if (!subscribed) throw new Error('app did not subscribe to the session stream after sign-in')
  await page.wait('!!document.querySelector("textarea")', 30000, 'composer')
  // The reply is absorbed while the initial history load is still open, so the
  // service must be quiet before the harness sends.
  await waitFor(async () => {
    const log = await control('/__control/requests', null, 'GET')
    const last = log.items.length ? Math.max(...log.items.map(item => Date.parse(item.at))) : 0
    return Date.now() - last >= 1200
  }, 20000, 300, 'service quiet')
  // A hidden document neither renders the stream nor starts read-aloud, so the
  // scenario must start from a visible page or its red would be meaningless.
  const visibilityState = await ensureVisible('before injection')
  return visibilityState
}

async function typeAndSend(value, { submit = true } = {}) {
  const typedAt = await page.eval(`(() => {
    const area = document.querySelector('textarea')
    if (!area) return null
    area.focus()
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set.call(area, ${JSON.stringify(value)})
    area.dispatchEvent(new Event('input', { bubbles: true }))
    return true
  })()`)
  if (!typedAt || !submit) return null
  await control('/__control/mark', {})
  // The stream's first frame can only be timed against the instant the send
  // left, so the send itself is the timestamp the caller measures from.
  const sentAt = Date.now()
  await page.eval(`(() => {
    const area = document.querySelector('textarea')
    area.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', code: 'Enter', keyCode: 13, bubbles: true, cancelable: true }))
  })()`)
  const visibilityState = await ensureVisible('after injection')
  if (visibilityState.state !== 'visible') throw new Error('page went hidden during injection')
  return sentAt
}

async function main() {
  const reportDir = resolve(process.env.ACCEPTANCE_REPORT_DIR || join(here, 'reports'))
  mkdirSync(reportDir, { recursive: true })
  const report = []
  const add = entry => { report.push(entry); console.log(`${entry.status.toUpperCase().padEnd(9)} ${entry.name}  ${entry.detail}`) }
  const pass = (name, ok, detail, extra = {}) => add({ name, status: ok ? 'pass' : 'fail', detail, ...extra })
  const uncovered = (name, detail) => add({ name, status: 'uncovered', detail })

  const ACK = '收到，我来处理。'
  const BODY = ['第一句已经说完。', '第二句跟上来了。', '第三句也来了。', '最后一句收尾。']

  try {
    ensureDevice()

    // ---------- E1 + G1 + G5 + V2 on one live reply ----------
    await coldStart()
    const sentAt = await typeAndSend('请朗读这段回复')
    let ackAt = -1
    for (let waited = 0; waited <= ACK_WINDOW_MS + 8000; waited += 100) {
      if ((await text()).includes(ACK)) { ackAt = Date.now() - sentAt; break }
      await sleep(100)
    }
    // The frame log is cumulative across scenarios (the scripted service is one
    // process, and `seq` restarts at the scenario boundary), so only a frame
    // stamped at or after this send belongs to this reply. Taking the newest
    // `seq === 1` without that filter silently measured a previous scenario's
    // frame.
    const log = await control('/__control/frames', null, 'GET')
    const ackFrameAt = log.items
      .filter(frame => frame.seq === 1 && frame.at >= sentAt)
      .map(frame => frame.at)
      .sort((left, right) => left - right)[0] ?? 0
    const fromStream = ackFrameAt && ackAt >= 0 ? ackAt - (ackFrameAt - sentAt) : -1
    pass('E1 acknowledgement reaches the real app UI', ackAt >= 0 && fromStream <= ACK_FROM_STREAM_MS,
      ackAt >= 0 ? `ack visible ${ackAt}ms after send, ${fromStream}ms after the ack frame (window ${ACK_FROM_STREAM_MS}ms)` : `ack not visible within ${ACK_WINDOW_MS}ms`,
      { ackAfterSendMs: ackAt, ackAfterFrameMs: fromStream, ackFrameOffsetMs: ackFrameAt ? ackFrameAt - sentAt : null })

    // Read-aloud start is proven by the app's own synthesis request; the visible
    // speech state is corroboration, because a short reply can finish playing
    // before this harness polls again. The acknowledgement line reads as its own
    // short task ahead of the body, so the carrier of the first sentence — not
    // the chronologically first POST — is the request G1 times, and G5 joins
    // every request in order instead of only the first POST plus appends.
    const first = await waitFor(async () => (await speech()).find(entry => entry.kind === 'post'), 30000, 250, 'first speech POST')
      .then(entry => entry, () => undefined)
    const sawPlaying = await waitFor(playing, 5000, 200, 'speech playing').then(() => true, () => false)
    await page.wait(`document.body.innerText.includes(${JSON.stringify(BODY[3])})`, 25000, 'final sentence').catch(() => {})
    // The on-screen tail races the read: G5 joins the requests only after the
    // last sentence has actually been sent for synthesis, so the join cannot
    // snapshot a stream that is still mid-body.
    await waitFor(async () => (await speech()).some(entry => (entry.text || '').includes(BODY[3])), 15000, 250, 'last sentence read').then(() => true, () => false)
    const timing = await control('/__control/speech', null, 'GET')
    const entries = timing.entries
    const carrier = entries.find(entry => (entry.text || '').includes(BODY[0]))
    const latency = carrier && timing.firstSentenceAt !== null ? carrier.ms - timing.firstSentenceAt : null
    pass('G1 automatic read-aloud starts on the first sentence', !!carrier && latency !== null && latency <= FIRST_POST_WINDOW_MS,
      carrier ? `first-sentence ${carrier.kind.toUpperCase()} ${latency}ms after the first completed sentence (window ${FIRST_POST_WINDOW_MS}ms), final=${carrier.final}, state played=${sawPlaying}`
        : 'no speech request carried the first sentence',
      { speechTiming: { firstSentenceAtMs: timing.firstSentenceAt, firstCarrierMs: carrier?.ms ?? null, latencyMs: latency } })
    const spoken = entries.map(e => e.text).filter(Boolean).join('')
    const ordered = spoken.includes(BODY[0]) && spoken.includes(BODY[3]) && spoken.indexOf(BODY[0]) < spoken.indexOf(BODY[3])
    pass('G5 whole body read in order without a dropped segment', ordered,
      ordered ? `spoken order kept, ${spoken.length} code points` : `spoken=${JSON.stringify(spoken).slice(0, 120)}`)

    const disclosure = await page.eval(`(() => {
      const marked = document.querySelector('[data-reasoning-disclosure]')
      const fallback = [...document.querySelectorAll('button')].find(n => /思考|Thinking|已思考/.test(n.innerText || ''))
      const node = marked || fallback
      return node ? { expanded: node.getAttribute('aria-expanded'), marker: !!marked, label: (node.innerText || '').trim().slice(0, 24) } : null
    })()`)
    if (!disclosure) uncovered('V2 thinking collapsed by default', 'no reasoning disclosure control in the packaged bundle')
    else pass('V2 thinking collapsed by default', disclosure.expanded === 'false',
      `aria-expanded=${disclosure.expanded} marker=${disclosure.marker} label=${disclosure.label}`)

    // ---------- V3c: clicking the reading bubble stops the audio ----------
    await coldStart()
    await typeAndSend('点击气泡测试')
    const bubblePlaying = await waitFor(playing, 30000, 200, 'read-aloud playing').then(() => true, () => false)
    const clicked = await page.eval(`(() => {
      const nodes = [...document.querySelectorAll('[data-chat-content]')]
      const target = nodes[nodes.length - 1]
      if (!target) return false
      target.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
      return true
    })()`)
    await sleep(1500)
    const afterBubble = await speechStates()
    pass('V3c bubble click stops reading', clicked && bubblePlaying && !afterBubble.includes('playing'),
      clicked ? `playing before click=${bubblePlaying}; state after=${afterBubble.join(',') || 'none'}` : 'assistant bubble not found')

    // ---------- V3b: typing must not interrupt ----------
    // The acknowledgement line reads as its own short task, so the state can
    // legitimately sit between tasks (idle) when the draft is typed mid-gap.
    // Typing violates the contract only by consuming the session: the read
    // must resume as the body tasks start, which is what this waits for.
    await coldStart()
    await typeAndSend('继续读')
    const typingStarted = await waitFor(playing, 30000, 200, 'read-aloud playing').then(() => true, () => false)
    await typeAndSend('正在打字的草稿', { submit: false })
    const stateRightAfterTyping = await speechStates()
    const resumedAfterTyping = await waitFor(playing, 8000, 200, 'read-aloud resumes after typing').then(() => true, () => false)
    pass('V3b typing does not stop reading', typingStarted && resumedAfterTyping,
      `playing before typing=${typingStarted}; state right after=${stateRightAfterTyping.join(',') || 'none'}; playing returned=${resumedAfterTyping}`)

    // ---------- V3d: a new submit stops reading ----------
    await coldStart()
    await typeAndSend('新消息停止朗读')
    const submitPlaying = await waitFor(playing, 30000, 200, 'read-aloud playing').then(() => true, () => false)
    await typeAndSend('新问题')
    await sleep(1500)
    const afterSubmit = await speechStates()
    pass('V3d new submit stops reading', submitPlaying && !afterSubmit.includes('playing'),
      `playing before submit=${submitPlaying}; state after=${afterSubmit.join(',') || 'none'}`)

    // ---------- G7: loading history must not read it aloud ----------
    await coldStart({ seedHistory: true })
    const historyVisible = await page.wait(`document.body.innerText.includes(${JSON.stringify(HISTORY_MARK)})`, 25000, 'history row')
      .then(() => true, () => false)
    await sleep(2000)
    const historySpeech = await speech()
    if (!historyVisible) uncovered('G7 history load does not read aloud', 'seeded history did not render in the packaged app on the emulator')
    else pass('G7 history load does not read aloud', historySpeech.length === 0, `speech requests after history render = ${historySpeech.length}`)

    // ---------- G8: background / foreground through the real ability ----------
    await coldStart()
    await typeAndSend('后台切换检查')
    const backgroundPlaying = await waitFor(playing, 30000, 200, 'read-aloud playing').then(() => true, () => false)
    hdc('shell', 'uitest uiInput keyEvent Home')
    await sleep(2000)
    const parked = await speechStates()
    hdc('shell', `aa start -a EntryAbility -b ${BUNDLE}`)
    await sleep(3000)
    const resumed = await waitFor(playing, 20000, 200, 'read-aloud resumed').then(() => true, () => false)
    pass('G8 background/foreground parks and resumes', backgroundPlaying && resumed,
      `playing before background=${backgroundPlaying}; state while backgrounded=${parked.join(',') || 'none'}; playing after return=${resumed}`)

    // ---------- native evidence ----------
    for (const name of ['chat', 'history', 'background']) {
      const remote = `/data/local/tmp/acceptance-${name}.png`
      hdc('shell', `uitest screenCap -p ${remote}`)
      hdc('file', 'recv', remote, join(reportDir, `acceptance-${name}.png`))
    }
  } catch (error) {
    add({ name: 'harness', status: 'fail', detail: String((error && error.stack) || error) })
  } finally {
    let lifecycle = ''
    try { lifecycle = hdc('shell', "hilog -x 2>/dev/null | grep -E 'lifecycle|client-ready|document-loaded' | tail -40") } catch {}
    const reportPath = join(reportDir, `acceptance-emulator-${Date.now()}.json`)
    writeFileSync(reportPath, JSON.stringify({
      generator: 'mobile/client/tools/acceptance/emulator.mjs',
      generatedAt: new Date().toISOString(), deviceHost: DEVICE_HOST, harness: 'desktop emulator',
      isolation: 'aa force-stop + bm clean -d/-c per scenario',
      scenarios: report, lifecycleLog: lifecycle.split('\n').slice(-40),
      speechRequests: await speech().catch(() => []),
      apiRequests: await control('/__control/requests', null, 'GET').then(r => r.items).catch(() => []),
    }, null, 2))
    console.log(`\nreport written to ${reportPath}`)
    if (page) page.close()
    for (const [socket, port] of forwardBySocket) { try { execFileSync(HDC, ['-t', SERIAL, 'fport', 'rm', `tcp:${port}`, `localabstract:${socket}`], { encoding: 'utf8' }) } catch {} }
  }
  process.exit(report.some(entry => entry.status !== 'pass') ? 1 : 0)
}

main().catch(error => { console.error(error); process.exit(2) })
