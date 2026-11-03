// Phone-side behavioral acceptance harness.
//
// Loads the packaged, distributable web bundle (the file tree
// package_memoh_web.py writes into the HAP) in Chromium, points it at a
// scripted local service, drives complete event sequences and asserts only
// observable behaviour. Exit code 0 means every gated scenario passed.
//
// The scenario set mirrors the six gates fixed by PR #121's
// speech-field-acceptance.test.ts, but runs against the packaged dist rather
// than the combination tree:
//   1 first-sentence POST in the same tick window
//   2 failure rollback rereads (task failure ends only that task)
//   3 settle restores reading of the remaining text
//   4 early "ended" without error does not consume the queue
//   5 whole body read in order without a dropped segment
//   6 same-text replay issues zero new POST
// plus the HAP41 field symptoms: E1 ack, E2 automatic start, V2/V3 collapse
// and interruption matrix, and the two general guards (history, background).
import { readFileSync, mkdirSync, writeFileSync } from 'node:fs'
import { join, resolve, dirname } from 'node:path'
import { tmpdir } from 'node:os'
import { fileURLToPath } from 'node:url'
import { StubService, FAIL_TASK_BODY } from './stub-service.mjs'
import { launchChrome, CDP, openPage } from './cdp.mjs'

const here = dirname(fileURLToPath(import.meta.url))
// Default target is the packaged resource tree that package_memoh_web.py writes
// into the HAP, not a build-tree copy: the gate must read the distributable.
const DEFAULT_BUNDLE = resolve(here, '../../mobile/harmony/app/entry/src/main/resources/rawfile/client-web')
const CHROME = process.env.ACCEPTANCE_CHROME
  || 'C:/Users/Begonia/AppData/Local/ms-playwright/chromium-1223/chrome-win64/chrome.exe'

const ACK_TEXT = '收到，我来处理。'
const BODY = ['第一句已经说完。', '第二句跟上来了。', '第三句也来了。', '最后一句收尾。']
// Gate 1 replays the six-sentence body with 800 ms between sentences and a
// 3 s settle after the last one: "read on the first sentence" and "read when
// the reply settles" are then seconds apart, so a settle-time read cannot pass
// the first-sentence window the way the four short sentences let it (TF-18).
const LONG_BODY = FAIL_TASK_BODY
const LONG_DELAY_MS = 800
const LONG_SETTLE_MS = 3000
const ACK_WINDOW_MS = 3000
const FIRST_POST_WINDOW_MS = 1000

const sleep = ms => new Promise(r => setTimeout(r, ms))

const INSTRUMENT = `(() => {
  const state = { fetch: [], audio: [], errors: [] }
  Object.defineProperty(window, '__acc', { configurable: true, value: state })
  const realFetch = window.fetch.bind(window)
  window.fetch = async (input, init) => {
    const url = typeof input === 'string' ? input : (input && input.url) || ''
    const method = (init && init.method) || (input && input.method) || 'GET'
    const record = { url, method, at: Date.now() }
    state.fetch.push(record)
    try {
      const response = await realFetch(input, init)
      record.status = response.status
      record.doneAt = Date.now()
      return response
    } catch (error) {
      record.error = String(error && error.message)
      record.doneAt = Date.now()
      throw error
    }
  }
  const play = HTMLMediaElement.prototype.play
  HTMLMediaElement.prototype.play = function () {
    state.audio.push({ kind: 'play', at: Date.now() })
    return play.call(this).then(
      () => state.audio.push({ kind: 'playing', at: Date.now() }),
      error => { state.audio.push({ kind: 'rejected', at: Date.now(), message: String(error && error.message) }); throw error },
    )
  }
  const pause = HTMLMediaElement.prototype.pause
  HTMLMediaElement.prototype.pause = function () { state.audio.push({ kind: 'pause', at: Date.now() }); return pause.call(this) }
  window.addEventListener('error', event => state.errors.push({ at: Date.now(), message: String(event.message) }))
  window.addEventListener('unhandledrejection', event => state.errors.push({ at: Date.now(), message: 'unhandled: ' + String(event.reason) }))
})()`

const result = (name, ok, detail, extra = {}) => ({ name, status: ok ? 'pass' : 'fail', detail, ...extra })
// A scenario the harness could not drive (the packaged UI never exposed the
// control) is reported as uncovered and still fails the gate: an undriven
// assertion is not evidence of correct behaviour.
const uncovered = (name, detail) => ({ name, status: 'uncovered', detail })

async function main() {
  const positional = process.argv.slice(2).filter(a => !a.startsWith('--'))
  const bundle = positional[0] ? resolve(positional[0]) : DEFAULT_BUNDLE
  readFileSync(join(bundle, 'index.html'))
  const reportDir = process.env.ACCEPTANCE_REPORT_DIR
    ? resolve(process.env.ACCEPTANCE_REPORT_DIR)
    : join(tmpdir(), 'memoh-acceptance-reports')
  mkdirSync(reportDir, { recursive: true })

  const service = new StubService({ bundleDir: bundle })
  await service.start()
  const origin = `http://stub.test:${service.port}`

  // The packaged bundle hashes attachments with crypto.subtle, which Chromium
  // exposes only in secure contexts; the harness origin is http on loopback.
  const chrome = await launchChrome({ executable: CHROME, args: [`--unsafely-treat-insecure-origin-as-secure=${origin}`] })
  const cdp = await CDP.connect(chrome.wsUrl)
  // One isolated browser context per scenario: storage, page-memory caches and
  // the socket are torn down with the page, so scenarios cannot leak into each
  // other. The document is created fresh for each case.
  async function newPage() {
    const { browserContextId } = await cdp.send('Target.createBrowserContext', { disposeOnDetach: false })
    const created = await openPage(cdp, browserContextId)
    await cdp.send('Page.addScriptToEvaluateOnNewDocument', { source: INSTRUMENT }, created.sessionId)
    return { page: created, browserContextId }
  }
  let { page, browserContextId } = await newPage()
  let runIndex = 0

  const report = []
  const add = entry => { report.push(entry); console.log(`${entry.status === 'pass' ? 'PASS' : entry.status === 'uncovered' ? 'N/A ' : 'FAIL'}  ${entry.name}  ${entry.detail}`) }
  const speech = async () => (await service.control('/__control/speech', null, 'GET')).entries
  const audio = async () => (await page.eval('window.__acc.audio.slice()')) ?? []
  const pauses = async () => (await audio()).filter(event => event.kind === 'pause').length
  const plays = async () => (await audio()).filter(event => event.kind === 'playing').length
  const bodyText = async () => (await page.eval('document.body.innerText')) ?? ''
  const wait = async (expression, timeout = 15000) => page.waitFor(expression, timeout).then(() => true, () => false)
  const waitBody = async (needle, timeout = 20000) => wait(`document.body.innerText.includes(${JSON.stringify(needle)})`, timeout)
  // The app treats the initial history load as a read-aloud gate: a reply that
  // streams inside that window is absorbed as baseline. Waiting for "the page
  // mounted" is not enough, so the harness waits until the page's own fetch
  // instrument has been quiet for a moment before it sends.
  async function networkIdle(quietMs = 900, timeout = 25000) {
    const start = Date.now()
    while (Date.now() - start < timeout) {
      const idle = await page.eval(`(() => {
        const calls = window.__acc ? window.__acc.fetch : []
        if (!calls.length) return false
        const last = Math.max(...calls.map(c => c.doneAt || c.at))
        return Date.now() - last >= ${quietMs}
      })()`).catch(() => false)
      if (idle) return true
      await sleep(150)
    }
    return false
  }

  async function type(value) {
    await page.eval(`(() => {
      const area = document.querySelector('textarea')
      if (!area) return false
      area.focus()
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set.call(area, ${JSON.stringify(value)})
      area.dispatchEvent(new Event('input', { bubbles: true }))
      return true
    })()`)
  }
  async function submit(value) {
    await type(value)
    await service.control('/__control/mark', {})
    const at = Date.now()
    await page.eval(`(() => {
      const area = document.querySelector('textarea')
      area.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', code: 'Enter', keyCode: 13, bubbles: true, cancelable: true }))
    })()`)
    return at
  }
  // The read-aloud control of the reply that was just read: searched inside
  // the last assistant row first, so a transcript that shows more than one
  // reply never redirects the gesture to a row this page never synthesized.
  async function clickAriaOnLastReply(pattern) {
    return page.eval(`(() => {
      const regex = new RegExp(${JSON.stringify(pattern)})
      const rows = [...document.querySelectorAll('[data-message-id]')].filter(n => n.querySelector('[data-chat-content]'))
      const row = rows[rows.length - 1]
      const scoped = row ? [...row.querySelectorAll('button, [role=button]')].find(n => regex.test(n.getAttribute('aria-label') || '')) : null
      const node = scoped || [...document.querySelectorAll('button, [role=button]')].find(n => regex.test(n.getAttribute('aria-label') || ''))
      if (!node) return false
      node.click()
      return scoped ? 'last-reply' : 'document'
    })()`)
  }
  async function clickLastAssistantBubble() {
    return page.eval(`(() => {
      const nodes = [...document.querySelectorAll('[data-chat-content]')]
      const target = nodes[nodes.length - 1]
      if (!target) return false
      target.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
      return true
    })()`)
  }
  async function reasoningDisclosure() {
    return page.eval(`(() => {
      const marked = document.querySelector('[data-reasoning-disclosure]')
      const fallback = [...document.querySelectorAll('button')].find(n => /思考|Thinking|思考中/.test(n.innerText || ''))
      const node = marked || fallback
      if (!node) return null
      return { expanded: node.getAttribute('aria-expanded'), marker: !!marked, label: (node.innerText || '').trim().slice(0, 30) }
    })()`)
  }
  // A scenario starts from a discarded browser context AND an empty service:
  // the previous case's transcript must not be fetched as this case's history
  // during boot, because that load is exactly the gate under test.
  async function scenario(name, delay = 700, { seedHistory = false, settleDelay = null } = {}) {
    await cdp.send('Target.disposeBrowserContext', { browserContextId }).catch(() => {})
    const created = await newPage()
    page = created.page
    browserContextId = created.browserContextId
    await service.control('/__control/scenario', { scenario: name, delay, settleDelay })
    await service.control('/__control/reset', {})
    if (seedHistory) await service.control('/__control/history', {})
    const nonce = `run=${++runIndex}`
    await page.goto(`${origin}/index.html?${nonce}`)
    await page.waitFor('document.readyState === "complete"', 20000).catch(() => {})
    await page.eval(`localStorage.setItem('token', 'acceptance-token')`)
    await page.eval(`sessionStorage.setItem('memoh.speechTiming', '1')`)
    // The boot URL differs from the seeding URL, so this is a real document
    // load; changing only the hash would keep the previous document alive.
    await page.goto(`${origin}/index.html?${nonce}&boot=1#/`)
    // The composer is disabled while the initial history loads; sending inside
    // that window is not a user-reachable state and would absorb the reply as
    // baseline history instead of reading it.
    const ready = await wait('(() => { const area = document.querySelector("textarea"); return !!area && !area.disabled })()', 30000)
    if (!ready) throw new Error('chat pane did not become ready: ' + (await bodyText()).slice(0, 200))
    // The read-aloud gate is released only after the initial history load
    // finishes; wait for the page's own request log to go quiet.
    await networkIdle(1000, 25000)
    return true
  }
  // The reply body is being read: a speech request that carries the sentence
  // has really left and audio has really started after it. The ack line reads
  // first as its own short task, so "some audio is playing" alone would
  // resolve before the body (or even before the reasoning block) exists.
  async function waitBodyReading(sentence, timeout = 30000) {
    const start = Date.now()
    while (Date.now() - start < timeout) {
      const status = await service.control('/__control/speech', null, 'GET')
      const carried = status.entries.find(entry => (entry.text || '').includes(sentence))
      if (carried && (await audio()).some(event => event.kind === 'playing' && event.at >= status.t0 + carried.ms)) return true
      await sleep(100)
    }
    return false
  }
  async function waitSettled(timeout = 15000) {
    const start = Date.now()
    while (Date.now() - start < timeout) {
      const status = await service.control('/__control/speech', null, 'GET')
      if (status.settledAt !== null) return status
      await sleep(100)
    }
    return service.control('/__control/speech', null, 'GET')
  }

  try {
    // ---------- E1: fast-reply acknowledgement reaches the UI in the window.
    await scenario('stream', 700)
    const sentAt = await submit('帮我看一下这个附件')
    let ackAt = -1
    for (let waited = 0; waited <= ACK_WINDOW_MS; waited += 50) {
      const visible = await page.eval(`(() => {
        const status = [...document.querySelectorAll('[role=status]')].map(n => n.innerText || '').join(' ')
        return (document.body.innerText + ' ' + status).includes(${JSON.stringify(ACK_TEXT)})
      })()`)
      if (visible) { ackAt = Date.now() - sentAt; break }
      await sleep(50)
    }
    add(result('E1 fast-reply acknowledgement in window', ackAt >= 0 && ackAt <= ACK_WINDOW_MS,
      ackAt >= 0 ? `ack visible ${ackAt}ms after send (window ${ACK_WINDOW_MS}ms)` : `ack not visible within ${ACK_WINDOW_MS}ms`,
      { acknowledgement: { visibleAtMs: ackAt, windowMs: ACK_WINDOW_MS } }))

    // ---------- Gate 1 + 5: automatic start on the first sentence; ordered body.
    // The measured interval starts at the first *completed sentence* in the
    // frame sequence (the scripted model's "text_chunk_ready"), not at the
    // keystroke: the contract is "first sentence -> speech request", not
    // "keystroke -> audio". The long body with a long settle makes the two
    // readings distinguishable: the request that carries the first sentence
    // must leave within the window and before the last sentence is even
    // streamed, so a read that only starts at settle cannot pass (TF-18).
    await scenario('long', LONG_DELAY_MS, { settleDelay: LONG_SETTLE_MS })
    await submit('请朗读这段回复')
    const started = await waitBodyReading(LONG_BODY[0], 30000)
    await waitBody(LONG_BODY[LONG_BODY.length - 1], 30000)
    const timing = await waitSettled(15000)
    // Leave room for a settle-time request so a late read is recorded, not missed.
    await sleep(1500)
    const firstSpeech = await speech()
    const firstSentenceRequest = firstSpeech.find(entry => (entry.text || '').includes(LONG_BODY[0]))
    const latencyMs = firstSentenceRequest && timing.firstSentenceAt !== null ? firstSentenceRequest.ms - timing.firstSentenceAt : null
    const beforeLastSentence = !!firstSentenceRequest && timing.lastSentenceAt !== null && firstSentenceRequest.ms < timing.lastSentenceAt
    const settleLeadMs = firstSentenceRequest && timing.settledAt !== null ? timing.settledAt - firstSentenceRequest.ms : null
    const ackPosts = firstSpeech.filter(entry => entry.kind === 'post' && entry.text === ACK_TEXT).length
    const g1Diagnostics = await page.eval(`(() => ({
      speechStates: [...document.querySelectorAll('[data-speech-state]')].map(n => n.getAttribute('data-speech-state')),
      speechEnabled: [...document.querySelectorAll('[data-chat-content]')].map(n => (n.innerText || '').slice(0, 30)),
      ttsFetch: (window.__acc.fetch || []).filter(f => /tts/.test(f.url)).map(f => ({ method: f.method, status: f.status, error: f.error })),
      errors: (window.__acc.errors || []).slice(-3),
    }))()`).catch(() => null)
    add(result('G1 automatic read-aloud starts on the first sentence', !!firstSentenceRequest && latencyMs !== null && latencyMs <= FIRST_POST_WINDOW_MS && beforeLastSentence,
      firstSentenceRequest
        ? `first-sentence ${firstSentenceRequest.kind.toUpperCase()} ${latencyMs}ms after the first completed sentence (window ${FIRST_POST_WINDOW_MS}ms), ${settleLeadMs}ms before settle, before the last sentence=${beforeLastSentence}, ack posts=${ackPosts}`
        : 'no speech request carried the first sentence',
      { speechTiming: { firstSentenceAtMs: timing.firstSentenceAt, lastSentenceAtMs: timing.lastSentenceAt, settledAtMs: timing.settledAt, firstSentenceRequestMs: firstSentenceRequest?.ms ?? null, firstSentenceRequestKind: firstSentenceRequest?.kind ?? null, latencyMs, settleLeadMs, windowMs: FIRST_POST_WINDOW_MS, beforeLastSentence, ackPosts, audioStarted: started }, diagnostics: g1Diagnostics }))
    const spoken = firstSpeech.map(entry => entry.text).filter(Boolean).join('')
    const ordered = spoken.includes(LONG_BODY[0]) && spoken.includes(LONG_BODY[LONG_BODY.length - 1])
      && spoken.indexOf(LONG_BODY[0]) < spoken.indexOf(LONG_BODY[LONG_BODY.length - 1])
    add(result('G5 whole body read in order without a dropped segment', ordered,
      ordered ? `spoken order kept, ${Array.from(spoken).length} code points` : `spoken=${JSON.stringify(spoken).slice(0, 120)}`))

    // ---------- V2: thinking collapsed by default (PR #118 semantics).
    const disclosure = await reasoningDisclosure()
    if (!disclosure) {
      add(uncovered('V2 thinking collapsed by default', 'no reasoning disclosure control in the packaged bundle'))
    } else {
      add(result('V2 thinking collapsed by default', disclosure.expanded === 'false',
        `aria-expanded=${disclosure.expanded} marker=${disclosure.marker} label=${disclosure.label}`))
    }

    // ---------- V3 matrix: only the reading bubble and a new submit stop it.
    // Each control gets its own reading session: a control that wrongly stops
    // the audio must not make the next case unmeasurable.
    await scenario('stream', 700)
    await submit('继续说')
    await waitBodyReading(BODY[0], 30000)
    const beforeToggle = await pauses()
    const toggled = await page.eval(`(() => {
      const marked = document.querySelector('[data-reasoning-disclosure]')
      const fallback = [...document.querySelectorAll('button')].find(n => /思考|Thinking/.test(n.innerText || ''))
      const node = marked || fallback
      if (!node) return false
      node.click()
      return true
    })()`)
    await sleep(1200)
    const afterToggle = await pauses()
    if (!toggled) add(uncovered('V3a thinking toggle does not stop reading', 'no reasoning disclosure control to click'))
    else add(result('V3a thinking toggle does not stop reading', afterToggle === beforeToggle,
      `pause events ${beforeToggle} -> ${afterToggle}`))

    await scenario('stream', 700)
    await submit('打字和点气泡')
    await waitBodyReading(BODY[0], 30000)
    const beforeTyping = await pauses()
    await type('正在打字的草稿')
    await sleep(1200)
    add(result('V3b typing does not stop reading', (await pauses()) === beforeTyping,
      `pause events ${beforeTyping} -> ${await pauses()}`))

    const beforeBubble = await pauses()
    const clicked = await clickLastAssistantBubble()
    await sleep(1500)
    const afterBubble = await pauses()
    add(result('V3c bubble click stops reading', clicked && afterBubble > beforeBubble,
      clicked ? `pause events ${beforeBubble} -> ${afterBubble}` : 'assistant bubble not found'))

    await scenario('stream', 700)
    await submit('再读一段')
    await waitBodyReading(BODY[0], 30000)
    const beforeSubmit = await pauses()
    await submit('新问题')
    await sleep(1500)
    const afterSubmit = await pauses()
    add(result('V3d new submit stops reading', afterSubmit > beforeSubmit,
      `pause events ${beforeSubmit} -> ${afterSubmit}`))

    // ---------- E2d / Gate 6: the same text is replayed, not re-synthesized.
    await scenario('stream', 700)
    await submit('朗读缓存检查')
    await waitBody(BODY[BODY.length - 1], 20000)
    await sleep(3000)
    const firstRead = (await speech()).length
    const replayClicked = await clickAriaOnLastReply('朗读|Read aloud')
    await sleep(3000)
    const secondRead = (await speech()).length
    add(result('G6 same-text replay issues zero new speech POST', !!replayClicked && secondRead === firstRead,
      replayClicked ? `speech requests ${firstRead} -> ${secondRead} (control in ${replayClicked})` : 'read-aloud action not found'))

    // ---------- Gate 3: a settled run still reads the text it had not spoken.
    await scenario('stream', 700)
    await submit('settle 后继续朗读')
    await waitBody(BODY[BODY.length - 1], 20000)
    await sleep(4000)
    const settledPosts = (await speech()).filter(entry => entry.kind === 'post').length
    const spokenAfterSettle = (await speech()).map(entry => entry.text).filter(Boolean).join('')
    add(result('G3 settled frame keeps the remaining text readable', settledPosts >= 1 && spokenAfterSettle.includes(BODY[BODY.length - 1]),
      `posts=${settledPosts} spokenTail=${spokenAfterSettle.includes(BODY[BODY.length - 1])}`))

    // ---------- Gate 4: an early "ended" without error must not consume the queue.
    await scenario('early-ended', 700)
    await submit('提前结束不吞队列')
    await waitBody(BODY[BODY.length - 1], 20000)
    await sleep(4000)
    const earlySpeech = await speech()
    const earlyPosts = earlySpeech.filter(entry => entry.kind === 'post').length
    const earlyText = earlySpeech.map(entry => entry.text).filter(Boolean).join('')
    add(result('G4 early ended without error does not drop queued text', earlyPosts >= 1 && earlyText.includes(BODY[BODY.length - 1]),
      `posts=${earlyPosts} tailSpoken=${earlyText.includes(BODY[BODY.length - 1])}`))

    // ---------- Gate 2: a task failure rolls back and the rest reads again.
    // The service rejects the first body task of the long reply (the ack's
    // own short task is not the reply body). The client must end only that
    // task: the text it had fed is rolled back and read again through the
    // next task, and the reply still reads to its last sentence, in order
    // (PR #121 "failure rollback rereads").
    await scenario('fail-task', 700)
    await submit('失败后继续朗读')
    await waitBody(FAIL_TASK_BODY[FAIL_TASK_BODY.length - 1], 30000)
    await sleep(6000)
    const failureSpeech = await speech()
    const bodyPosts = failureSpeech.filter(entry => entry.kind === 'post' && entry.text !== ACK_TEXT)
    const rejected = failureSpeech.find(entry => entry.kind === 'post' && entry.status === 503)
    const afterRejection = rejected ? failureSpeech.slice(failureSpeech.indexOf(rejected) + 1).map(entry => entry.text).filter(Boolean).join('') : ''
    const rolledBack = !!rejected && bodyPosts.length >= 2 && afterRejection.includes(rejected.text)
    const lastSentence = FAIL_TASK_BODY[FAIL_TASK_BODY.length - 1]
    const tailReread = afterRejection.includes(lastSentence)
      && afterRejection.indexOf(FAIL_TASK_BODY[0]) <= afterRejection.indexOf(lastSentence)
    add(result('G2 single task failure ends only that task (remaining text rereads)', rolledBack && tailReread,
      `bodyPosts=${bodyPosts.length} rejectedCodePoints=${Array.from(rejected?.text ?? '').length} rolledBack=${rolledBack} tailReread=${tailReread}`))

    // ---------- General: loading history must not read it aloud.
    await scenario('stream', 700, { seedHistory: true })
    await waitBody('这是历史回复', 10000)
    await sleep(3000)
    const historySpeech = await speech()
    add(result('G7 history load does not read aloud', (await bodyText()).includes('这是历史回复') && historySpeech.length === 0,
      `history visible=${(await bodyText()).includes('这是历史回复')} speechRequests=${historySpeech.length}`))

    // ---------- General: background/foreground parks instead of consuming.
    await scenario('stream', 700)
    await submit('后台切换检查')
    await waitBodyReading(BODY[0], 30000)
    await page.eval(`window.dispatchEvent(new CustomEvent('garnet:lifecycle', { detail: { state: 'background' } }))`)
    await sleep(700)
    await page.eval(`window.dispatchEvent(new CustomEvent('garnet:lifecycle', { detail: { state: 'foreground' } }))`)
    await sleep(2000)
    const resumed = await plays()
    add(result('G8 background/foreground parks and resumes', resumed >= 2,
      `play events=${resumed} (expect >=2: initial + resumed)`))

    // ---------- E8: attachment transfer ring, no status copy, formatted speed.
    // One scripted reply drives both real transfer paths of the packaged build:
    //   upload wire - a 64 MiB original staged through the composer file input;
    //   the stub reads the multipart body in paced byte segments (>= 3 s), so
    //   the wire window is seconds wide (uploadEvents timestamps), the receipt
    //   verifies, and the sent card lands on the ready badge. On this build the
    //   optimistic user turn appends only after preparation completes, so the
    //   upload-phase ring is recorded as a diagnostic through this window.
    //   verified media read - the same reply carries a Bot image attachment
    //   (content_hash of a stub-staged original). The rendered card runs the
    //   onProgress read over a paced body, so the E8 ring fills monotonically
    //   while the bubble-level ring carries the formatted speed.
    async function stageComposerFile(sizeBytes) {
      const filePath = join(reportDir, `e8-upload-${runIndex}-${Date.now()}.bin`)
      writeFileSync(filePath, Buffer.alloc(sizeBytes, 7))
      const { root } = await cdp.send('DOM.getDocument', {}, page.sessionId)
      const { nodeId } = await cdp.send('DOM.querySelector', { nodeId: root.nodeId, selector: 'input[type=file]:not([accept])' }, page.sessionId)
      if (!nodeId) throw new Error('composer file input not found')
      await cdp.send('DOM.setFileInputFiles', { files: [filePath], nodeId }, page.sessionId)
      return filePath
    }
    const liveProbe = `(() => ({
      rings: [...document.querySelectorAll('svg circle[stroke-dasharray]')].map(c => ({
        dasharray: c.getAttribute('stroke-dasharray'),
        dashoffset: c.getAttribute('stroke-dashoffset'),
      })),
      speeds: [...document.querySelectorAll('span.text-caption.tabular-nums')].map(n => n.textContent || '').filter(Boolean),
      cards: [...document.querySelectorAll('.group\\\\/att')].map(n => n.innerText || ''),
      badges: [...document.querySelectorAll('.group\\\\/att [class*=bottom-1]')].map(n => ({
        title: n.getAttribute('title'), aria: n.getAttribute('aria-label'), text: n.innerText || '',
      })),
    }))()`
    const BANNED_CARD_COPY = ['上传附件中', '正在上传', '上传中', '正在完整校验', '校验中', '准备中', '传输中', '失败', '等待中']
    async function watchTransfer(timeoutMs, requireSeen = false) {
      const samples = []
      const deadline = Date.now() + timeoutMs
      let quiet = 0
      let seen = !requireSeen
      while (Date.now() < deadline) {
        const probe = await page.eval(liveProbe).catch(() => ({ rings: [], speeds: [], cards: [], badges: [] }))
        samples.push(probe)
        if (probe.rings.length > 0) { seen = true; quiet = 0 } else quiet += 1
        if (seen && quiet >= 3) break
        await sleep(110)
      }
      return samples
    }
    function ringSeries(samples) {
      // Track the dashoffset of every filled arc keyed by its dasharray so two
      // concurrent rings never interleave into one series.
      const series = new Map()
      for (const probe of samples) {
        for (const ring of probe.rings) {
          if (ring.dashoffset == null) continue
          const key = ring.dasharray + '|' + ring.dashoffset.length
          if (!series.has(key)) series.set(key, [])
          series.get(key).push(Number(ring.dashoffset))
        }
      }
      return [...series.values()]
    }
    function monotonicFill(series) {
      return series.some(values => {
        const distinct = [...new Set(values.map(v => Math.round(v * 1000) / 1000))]
        if (distinct.length < 3) return false
        for (let i = 1; i < values.length; i++) if (values[i] > values[i - 1] + 1e-9) return false
        return distinct[distinct.length - 1] <= distinct[0] * 0.15
      })
    }
    // The E8 transfer rings (r = 8) are the only circles with a 50.265
    // dasharray; waiting for one excludes the unrelated decorative arcs.
    const E8_RING_SEEN = `!!document.querySelector('svg circle[stroke-dasharray="50.265"], svg circle[stroke-dasharray="12.566 50.265"]')`

    await scenario('bot-image', 700, { settleDelay: 12000 })
    await service.control('/__control/upload-config', { chunkDelayMs: 40, paceBytes: 512 * 1024, downloadChunkDelayMs: 50 })
    await service.control('/__control/bot-attachment', { sizeBytes: 4 * 1024 * 1024 })
    await stageComposerFile(64 * 1024 * 1024)
    const stagedProbe = await page.eval(liveProbe).catch(error => ({ error: String(error) }))
    await submit('发送这个附件，再看图片')
    const uploadDeadline = Date.now() + 60000
    let uploadEvents = []
    while (Date.now() < uploadDeadline) {
      uploadEvents = (await service.control('/__control/uploads', null, 'GET')).events
      if (uploadEvents.some(event => event.done)) break
      await sleep(200)
    }
    const uploadDone = uploadEvents.some(event => event.done)
    const uploadSpanMs = uploadEvents.length >= 2
      ? Date.parse(uploadEvents[uploadEvents.length - 1].at) - Date.parse(uploadEvents[0].at) : 0
    const readyBadge = await wait('!!document.querySelector(".group\\\\/att [class*=text-emerald] svg")', 15000).catch(() => false)
    await wait(E8_RING_SEEN, 30000).catch(() => false)
    const samples = await watchTransfer(30000, true)
    const series = ringSeries(samples)
    const ringsSeen = samples.some(probe => probe.rings.length > 0)
    const midCopy = [...new Set(samples.flatMap(probe => probe.cards))].join('\n')
    const midSpeeds = [...new Set(samples.flatMap(probe => probe.speeds))]
    const badgeTitles = samples.flatMap(probe => probe.badges).map(b => b.title || b.aria).filter(Boolean)
    add(result('attachment-transfer-ring-monotonic',
      ringsSeen && monotonicFill(series) && uploadDone && uploadSpanMs >= 3000 && readyBadge,
      `ringsSeen=${ringsSeen} series=${JSON.stringify(series.map(v => v.length))} monotonicFill=${monotonicFill(series)} uploadWireSpan=${uploadSpanMs}ms paced=${uploadSpanMs >= 3000} uploadDone=${uploadDone} readyBadge=${readyBadge}`,
      { stagedProbe, uploadEvents }))

    const bannedHits = BANNED_CARD_COPY.filter(word => midCopy.includes(word))
    const percentage = /(^|\s)\d+(\.\d+)?\s?%/.test(midCopy)
    add(result('attachment-card-no-status-copy', bannedHits.length === 0 && !percentage && badgeTitles.length === 0,
      `bannedCopy=${JSON.stringify(bannedHits)} percentage=${percentage} badgeTitles=${JSON.stringify(badgeTitles)}`,
      { cardText: midCopy.slice(0, 300) }))

    const speedMatches = midSpeeds.map(text => ({ text, ok: /^\d+(\.\d+)? (B|KB|MB)\/s$/.test(text) }))
    add(result('transfer-speed-format', midSpeeds.length > 0 && speedMatches.every(entry => entry.ok),
      `speedLabels=${JSON.stringify(midSpeeds)} allMatch=${speedMatches.every(entry => entry.ok)}`))

    // ---------- E8: a verified media read that dies mid-transfer marks the
    // card failed (alert badge + red border, no status copy). The stub serves
    // the injected Bot image attachment paced, then breaks the body part-way,
    // so the client read rejects inside the transfer and the failed state must
    // mount on the rendered card.
    await scenario('bot-image', 700, { settleDelay: 12000 })
    await service.control('/__control/upload-config', { chunkDelayMs: 0, paceBytes: 0, downloadChunkDelayMs: 50 })
    await service.control('/__control/bot-attachment', { sizeBytes: 1024 * 1024, failAfterChunks: 16 })
    await submit('这张图的传输会失败')
    let failedBadge = false
    let failedBorder = false
    let failedCopy = ''
    for (let waited = 0; waited <= 30000 && !failedBadge; waited += 150) {
      const probe = await page.eval(`(() => ({
        badge: !!document.querySelector('.group\\\\/att [class*=text-destructive] svg'),
        border: !!document.querySelector('.group\\\\/att [class*="border-destructive"]'),
        cards: [...document.querySelectorAll('.group\\\\/att')].map(n => n.innerText || '').join('\\n'),
      }))()`).catch(() => ({ badge: false, border: false, cards: '' }))
      failedBadge = probe.badge
      failedBorder = probe.border
      failedCopy = probe.cards
      await sleep(150)
    }
    // Only the E8 transfer rings (r = 8, dasharray 50.265) belong to this
    // assertion; unrelated decorative arcs (skeletons, streaming shimmer) stay
    // out of the check.
    const failedRings = await page.eval(`(() => [...document.querySelectorAll('svg circle[stroke-dasharray]')]
      .map(c => c.getAttribute('stroke-dasharray')))()`).catch(() => [])
    const failedRingGone = !failedRings.some(dash => dash === '50.265' || dash === '12.566 50.265')
    const failedBanned = BANNED_CARD_COPY.filter(word => failedCopy.includes(word))
    add(result('attachment-transfer-failure', failedBadge && failedBorder && failedRingGone && failedBanned.length === 0,
      `failedBadge=${failedBadge} border=${failedBorder} ringGone=${failedRingGone} bannedCopy=${JSON.stringify(failedBanned)}`,
      { cardText: failedCopy.slice(0, 300) }))
    await service.control('/__control/upload-config', { chunkDelayMs: 0, paceBytes: 0, downloadChunkDelayMs: 0 })

    // ---------- TF-17: quote-reply entries and the reply_to wire field.
    await scenario('stream', 700)
    await submit('引用这条回复试试')
    await waitBodyReading(BODY[0], 30000)
    await waitSettled(15000)
    await waitBody(BODY[BODY.length - 1], 10000)
    const quotedId = await page.eval(`(() => {
      const rows = [...document.querySelectorAll('[data-message-id]')].filter(n => n.querySelector('[data-chat-content]'))
      return rows.length ? rows[rows.length - 1].getAttribute('data-message-id') : null
    })()`)
    await page.eval(`(() => {
      const rows = [...document.querySelectorAll('[data-chat-content]')]
      rows[rows.length - 1].dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true }))
    })()`)
    const menuSeen = await wait('document.querySelectorAll(\'[role=menuitem]\').length >= 2', 8000).catch(() => false)
    const menuItems = menuSeen ? await page.eval(`[...document.querySelectorAll('[role=menuitem]')].map(n => (n.innerText || '').trim())`) : []
    const readAloudFirst = menuItems.length > 0 && /朗读|read aloud/i.test(menuItems[0])
    const quoteSecond = menuItems.length > 1 && /^(引用|quote)$/i.test(menuItems[1])
    const quoteCount = menuItems.filter(t => /^(引用|quote)$/i.test(t)).length
    add(result('quote-reply-menu-order', menuSeen && readAloudFirst && quoteSecond && quoteCount === 1,
      `menu=${JSON.stringify(menuItems)} readAloudFirst=${readAloudFirst} quoteSecond=${quoteSecond}`))

    await page.eval(`document.body.dispatchEvent(new MouseEvent('click', { bubbles: true }))`).catch(() => {})
    await sleep(300)
    const quoteButton = await page.eval(`(() => {
      const rows = [...document.querySelectorAll('[data-message-id]')].filter(n => n.querySelector('[data-chat-content]'))
      const row = rows[rows.length - 1]
      const buttons = row ? [...row.querySelectorAll('button[aria-label]')] : []
      const quote = buttons.filter(b => /^(引用|quote)$/i.test(b.getAttribute('aria-label') || ''))
      return { count: quote.length, iconOnly: quote.map(b => (b.innerText || '').trim() === ''), trailing: quote.length ? buttons[buttons.length - 1] === quote[quote.length - 1] : false }
    })()`)
    add(result('quote-reply-action-row-icon', quoteButton.count >= 1 && quoteButton.iconOnly.every(Boolean) && quoteButton.trailing,
      `count=${quoteButton.count} iconOnly=${quoteButton.iconOnly} trailing=${quoteButton.trailing}`))

    const chipCancelProbe = `[...document.querySelectorAll('button[aria-label]')].some(b => /^(取消引用|cancel quote)$/i.test(b.getAttribute('aria-label') || ''))`
    await page.eval(`(() => {
      const rows = [...document.querySelectorAll('[data-message-id]')].filter(n => n.querySelector('[data-chat-content]'))
      const row = rows[rows.length - 1]
      const button = row && [...row.querySelectorAll('button[aria-label]')].find(b => /^(引用|quote)$/i.test(b.getAttribute('aria-label') || ''))
      if (button) button.click()
    })()`)
    const chipCancelSeen = await wait(chipCancelProbe, 8000).catch(() => false)
    await page.eval(`[...document.querySelectorAll('button[aria-label]')].find(b => /^(取消引用|cancel quote)$/i.test(b.getAttribute('aria-label') || ''))?.click()`)
    await sleep(400)
    const chipGoneAfterCancel = !(await page.eval(chipCancelProbe))
    await page.eval(`(() => {
      const rows = [...document.querySelectorAll('[data-message-id]')].filter(n => n.querySelector('[data-chat-content]'))
      const row = rows[rows.length - 1]
      const button = row && [...row.querySelectorAll('button[aria-label]')].find(b => /^(引用|quote)$/i.test(b.getAttribute('aria-label') || ''))
      if (button) button.click()
    })()`)
    await wait(chipCancelProbe, 8000).catch(() => {})
    await submit('这条带引用')
    await sleep(600)
    const chipGoneAfterSend = !(await page.eval(chipCancelProbe))
    add(result('quote-reply-composer-roundtrip', chipCancelSeen && chipGoneAfterCancel && chipGoneAfterSend,
      `chip=${chipCancelSeen} clearedOnCancel=${chipGoneAfterCancel} clearedOnSend=${chipGoneAfterSend}`))

    await waitSettled(15000)
    await waitBodyReading(BODY[0], 30000)
    await waitSettled(15000)
    const messagesAfterQuote = (await service.control('/__control/messages', null, 'GET')).items
    const quotedSend = [...messagesAfterQuote].reverse().find(item => item.type === 'message' && (item.payload || '').includes('这条带引用'))
    const plainSend = [...messagesAfterQuote].reverse().find(item => item.type === 'message' && (item.payload || '').includes('引用这条回复试试'))
    const replyToMatch = quotedSend ? /"reply_to"\s*:\s*"([^"]+)"/.exec(quotedSend.payload || '') : null
    const plainHasReplyTo = plainSend ? /"reply_to"\s*:/.test(plainSend.payload || '') : null
    add(result('quote-reply-payload-reply-to', !!replyToMatch && replyToMatch[1] === quotedId && plainHasReplyTo === false,
      `replyTo=${replyToMatch ? replyToMatch[1] : null} quotedRow=${quotedId} plainSendCarriesReplyTo=${plainHasReplyTo}`,
      { quotedPayload: (quotedSend?.payload || '').slice(0, 400) }))
  } catch (error) {
    add(result('harness', false, String((error && error.message) || error)))
  } finally {
    const timing = await page.eval('window.__memohSpeechTiming ? window.__memohSpeechTiming.snapshot() : null').catch(() => null)
    const clientFetch = await page.eval('window.__acc ? window.__acc.fetch.slice() : null').catch(() => null)
    const clientErrors = await page.eval('window.__acc ? window.__acc.errors.slice() : null').catch(() => null)
    const domTurns = await page.eval(`[...document.querySelectorAll('[data-message-id]')].map(n => ({
      id: n.getAttribute('data-message-id'),
      speechState: n.querySelector('[data-speech-state]')?.getAttribute('data-speech-state') ?? null,
      text: (n.innerText || '').replace(/\\n/g, ' | ').slice(0, 160),
    }))`).catch(() => null)
    const reportPath = join(reportDir, `acceptance-${Date.now()}.json`)
    writeFileSync(reportPath, JSON.stringify({
      bundle,
      generator: 'mobile/client/tools/acceptance/acceptance.mjs',
      generatedAt: new Date().toISOString(),
      scenarios: report,
      speechTiming: timing,
      speechRequests: service.speech,
      apiRequests: service.apiLog,
      streamFrames: service.frames,
      clientFetch,
      clientErrors,
      domTurns,
    }, null, 2))
    console.log(`\nreport written to ${reportPath}`)
    await page.screenshot(join(reportDir, 'final.png')).catch(() => {})
    cdp.close()
    await chrome.close()
    await service.stop()
  }
  // The gate passes only when every scenario is a real pass: an uncovered
  // scenario is not evidence of correct behaviour.
  process.exit(report.some(entry => entry.status !== 'pass') ? 1 : 0)
}

main().catch(error => { console.error(error); process.exit(2) })
