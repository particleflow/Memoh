import test from 'node:test'
import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import vm from 'node:vm'
import { RECORD, sampleDOM, evaluationValue, runtimeMetadata, responseMetadata, captureRecipientNavigation } from './capture-ego-navigation.mjs'

const require = createRequire(new URL('../../../package.json', import.meta.url))
const vitestRequire = createRequire(require.resolve('vitest/package.json'))
const { JSDOM } = vitestRequire('jsdom')
const ids = ['original-user', 'original-assistant'], turnId = 'original-turn'
const route = { session: [RECORD.session] }
const snapshot = (clicked = false) => ({ format: 'memoh-navigation-dom/v1', query: clicked
  ? { ...route, task_bot: [RECORD.bot], task_type: ['bot_delegation'], task_id: [RECORD.task], task_record: [RECORD.task], run: [RECORD.run] }
  : route, paneCount: 1, homeMaskAncestors: [], panes: [{ visibleInputCount: 0, originalMessages: ids.map(id => ({ id, present: true, visible: true, inViewport: true })) }] })
const task = { task_type: 'bot_delegation', task_id: RECORD.task, record_id: RECORD.task,
  destination: { bot_id: RECORD.bot, session_id: RECORD.session, run_id: RECORD.run, turn_id: turnId } }

test('DOM witnesses distinguish an outer mask, hidden anchor scroll area and message skeleton without Vue state or message text', () => {
  const dom = new JSDOM(`<div class="invisible"><div class="dockview-theme-memoh"><div>
    <section><section><div><div class="max-w-md"><p>加载中</p></div></div>
      <div data-slot="scroll-area" style="display:none"><div data-slot="skeleton"></div>
        <div data-message-id="original-user">PRIVATE USER TEXT</div>
        <div data-message-id="original-assistant">PRIVATE RESULT</div>
      </div></section></section>
    <div class="flex shrink-0 justify-end"><button><svg class="lucide-list-tree"></svg>任务</button></div>
  </div></div></div>`, { url: `https://fixture.invalid/bot/recipient?session=${RECORD.session}&token=PRIVATE-TOKEN` })
  dom.window.Element.prototype.getBoundingClientRect = function () {
    const hidden = !!this.closest('[style="display:none"]')
    return { width: hidden ? 0 : 100, height: hidden ? 0 : 30, top: 10, bottom: 40, left: 10, right: 110 }
  }
  const result = vm.runInNewContext(`(${sampleDOM.toString()})(${JSON.stringify({ messageIds: ids })})`, {
    document: dom.window.document, location: dom.window.location, URL,
    getComputedStyle: dom.window.getComputedStyle.bind(dom.window), innerHeight: 844, innerWidth: 390,
  })
  assert.equal(result.homeMaskAncestors.length, 1)
  assert.equal(result.panes[0].scrollInlineDisplay, 'none')
  assert.equal(result.panes[0].anchorPlaceholder, '加载中')
  assert.equal(result.panes[0].historySkeletonNodes, 1)
  assert.equal(result.panes[0].originalMessages[0].present, true)
  assert.equal(result.panes[0].originalMessages[0].visible, false)
  assert(!JSON.stringify(result).includes('PRIVATE'))
  dom.window.close()
})

test('only demonstrated CDP return shapes are accepted; raw message/snapshot content is excluded', () => {
  assert.equal(evaluationValue({ result: { type: 'object', value: snapshot() } }).paneCount, 1)
  assert.equal(evaluationValue({ id: 1, result: { result: { type: 'object', value: snapshot() } } }).paneCount, 1)
  assert.throws(() => evaluationValue({ unexpected: snapshot() }), /Unverified/)
  const frame = runtimeMetadata(JSON.stringify({ type: 'runtime_snapshot', session_id: RECORD.session, epoch: 'e', seq: 1,
    snapshot: { current_run_view: { run_id: RECORD.run, turn_id: turnId, status: 'completed', messages: ['PRIVATE'] } } }))
  assert.equal(frame.run_id, RECORD.run); assert(!JSON.stringify(frame).includes('PRIVATE'))
  const messages = responseMetadata(`/api/bots/${RECORD.bot}/messages`, JSON.stringify({ items: [{ id: ids[0], turn_id: turnId, role: 'user', text: 'PRIVATE' }] }))
  assert.deepEqual(messages.items, [{ id: ids[0], turn_id: turnId, role: 'user' }])
})

function event(method, params) { return { method, params } }
function request(requestId, path, method = 'GET') {
  return event('Network.requestWillBeSent', { requestId, timestamp: 1, wallTime: 2, request: {
    url: 'https://fixture.invalid' + path, method, headers: { Authorization: 'PRIVATE' }, postData: 'PRIVATE',
  } })
}
const terminal = requestId => [event('Network.responseReceived', { requestId, timestamp: 1.1, response: { status: 200, headers: { 'set-cookie': 'PRIVATE' } } }), event('Network.loadingFinished', { requestId, timestamp: 1.2 })]

function fakePage({ shapeUnknown = false, write = false, denyBody = false, unfinished = false } = {}) {
  let reads = 0, clicked = false
  const calls = [], journal = []
  const page = {
    async cdp(method, params, options) {
      calls.push({ method, params, options })
      if (method === 'Runtime.evaluate') return { result: { type: 'object', value: snapshot(clicked) } }
      if (method === 'Network.getResponseBody') {
        if (denyBody) throw new Error('body read denied')
        return { body: JSON.stringify(task), base64Encoded: false }
      }
      return {}
    },
    async events() {
      reads++
      if (reads === 1) return [] // pre-arm batch is kept separate
      if (reads === 2) {
        if (shapeUnknown) return [{ unknown: 'PRIVATE', data: {} }]
        if (write) return [request('w', `/api/bots/${RECORD.bot}/sessions/contact`, 'POST')]
        return [request('p', `/api/bots/${RECORD.bot}/tasks?limit=20`), ...terminal('p')]
          .filter(row => !unfinished || row.method !== 'Network.loadingFinished')
      }
      if (reads === 3 && clicked) return [request('task', `/api/bots/${RECORD.bot}/tasks/bot_delegation/${RECORD.task}`), ...terminal('task'),
        event('Network.webSocketFrameReceived', { requestId: 'socket', timestamp: 3, response: { payloadData: JSON.stringify({ type: 'runtime_snapshot', session_id: RECORD.session, snapshot: { current_run_view: { run_id: RECORD.run, turn_id: turnId, messages: ['PRIVATE'] } } }) } })]
      return []
    },
  }
  return { page, calls, journal, options: { messageIds: ids, turnId, captureMs: 5, preflightMs: 50, sampleMs: 1,
    clickOnce: async () => { clicked = true }, append: async row => { journal.push(row) } }, isClicked: () => clicked }
}

test('self-checks the consuming event/result interface, reads each drained batch serially and calls only the supplied single click', async () => {
  const f = fakePage(), report = await captureRecipientNavigation(f.page, f.options)
  assert.equal(report.clickAttempted, true); assert.equal(report.clickReturned, true); assert.equal(report.interrupted, null)
  assert.equal(report.runtimeFrames[0].run_id, RECORD.run)
  assert.equal(report.requests.find(row => row.requestId === 'task').terminal, 'Network.loadingFinished')
  assert(f.journal.some(row => row.events?.some(e => e.event === 'Network.requestWillBeSent' && e.method === 'GET')))
  assert(!JSON.stringify(f.journal).includes('PRIVATE'))
  assert(f.calls.every(call => ['Network.enable', 'Runtime.enable', 'Runtime.evaluate', 'Network.getResponseBody'].includes(call.method)))
  assert(f.calls.every(call => call.options.timeout === 3000))
})

test('an unknown event wrapper or a write stops before clicking and never fabricates empty successful coverage', async () => {
  for (const input of [{ shapeUnknown: true }, { write: true }]) {
    const f = fakePage(input), report = await captureRecipientNavigation(f.page, f.options)
    assert.equal(report.clickAttempted, false); assert.equal(f.isClicked(), false)
    assert(report.interrupted)
    if (input.write) assert(f.journal.some(row => row.events?.some(e => e.method === 'POST')))
  }
})

test('a denied CDP body read has no fetch, convenience-evaluate or retry fallback', async () => {
  const f = fakePage({ denyBody: true }), report = await captureRecipientNavigation(f.page, f.options)
  assert.equal(report.clickAttempted, true); assert.match(report.interrupted.message, /denied/)
  assert.equal(f.calls.filter(call => call.method === 'Network.getResponseBody').length, 1)
})

test('HTTP200 without a terminal event remains pending and cannot pass consumer preflight', async () => {
  const f = fakePage({ unfinished: true }), report = await captureRecipientNavigation(f.page, f.options)
  assert.equal(report.clickAttempted, false); assert.equal(report.networkPollPipelineVerified, false)
  assert.equal(report.requests[0].status, 200); assert.equal(report.requests[0].terminal, null)
  assert.deepEqual(report.observedPendingRequestIds, ['p'])
})

test('a click transport failure is uncertain, recorded separately from return, and never invokes a second click', async () => {
  const f = fakePage(); let attempts = 0
  f.options.clickOnce = async () => { attempts++; throw new Error('click transport outcome unknown') }
  const report = await captureRecipientNavigation(f.page, f.options)
  assert.equal(attempts, 1); assert.equal(report.clickAttempted, true); assert.equal(report.clickReturned, false)
  assert(report.interrupted); assert(f.journal.some(row => row.kind === 'one-click-begin'))
  assert(!f.journal.some(row => row.kind === 'one-click-returned'))
})

test('a journal rejection is not retried and permits no event drain or click', async () => {
  const f = fakePage(); let writes = 0
  f.options.append = async () => { writes++; throw new Error('write denied') }
  await assert.rejects(captureRecipientNavigation(f.page, f.options), error => {
    assert.equal(error.captureState.clickAttempted, false)
    return /Journal write failed/.test(error.message)
  })
  assert.equal(writes, 1); assert.equal(f.isClicked(), false); assert.equal(f.calls.length, 0)
})
