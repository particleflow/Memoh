import test from 'node:test'
import assert from 'node:assert/strict'
import { RECORD } from './capture-ego-navigation.mjs'
import { historyURL, initialMessagesMetadata, captureInitialHistory } from './capture-initial-history.mjs'

const messageIds = ['original-user', 'original-assistant']
const historyUrl = `https://fixture.invalid/bot/${RECORD.bot}?session=${RECORD.session}`
const event = (method, params) => ({ method, params, sessionId: 'cdp-target-not-conversation' })
const request = (id, path, type = 'Fetch') => event('Network.requestWillBeSent', { requestId: id, type, timestamp: 1,
  request: { method: 'GET', url: 'https://fixture.invalid' + path, headers: { authorization: 'PRIVATE' } } })
const response = (id, mimeType = 'application/json') => event('Network.responseReceived', { requestId: id, timestamp: 2,
  response: { status: 200, mimeType, headers: { 'set-cookie': 'PRIVATE' } } })
const finished = id => event('Network.loadingFinished', { requestId: id, timestamp: 3 })

function fixture({ noFinish = false, denyBody = false } = {}) {
  const journal = [], calls = []
  let navigated = false, drains = 0
  const denied = new Error('fixture body read denied')
  const path = `/api/bots/${RECORD.bot}`
  const page = {
    async cdp(method, params) {
      calls.push({ method, params })
      if (method === 'Page.navigate') { navigated = true; return { frameId: 'main', loaderId: 'initial' } }
      if (method === 'Runtime.evaluate') {
        if (!navigated) return { result: { type: 'string', value: 'about:blank' } }
        return { result: { type: 'object', value: {
          format: 'memoh-navigation-dom/v1', documentProtocol: 'https:', documentReadyState: 'complete',
          query: { session: [RECORD.session] }, homeMaskAncestors: [], paneCount: 1,
          panes: [{ scrollComputedDisplay: 'block', anchorPlaceholder: null, historySkeletonNodes: 11,
            renderedMessageCount: 0, visibleInputCount: 0,
            originalMessages: messageIds.map(id => ({ id, present: false, visible: false })) }],
        } } }
      }
      if (method === 'Network.getResponseBody') {
        if (denyBody) throw denied
        return { body: JSON.stringify({ items: messageIds.map(id => ({ id, turn_id: 'original-turn', role: 'assistant', timestamp: '2026-10-30T00:00:00Z', text: 'PRIVATE' })) }), base64Encoded: false }
      }
      return {}
    },
    async events() {
      drains++
      if (drains === 1) return []
      if (drains === 2) return [request('history', `${path}/messages?session_id=${RECORD.session}`), response('history'),
        ...noFinish ? [] : [finished('history')], request('sse', `${path}/sessions/events`, 'EventSource'), response('sse', 'text/event-stream'),
        request('workdir', `${path}/workdir`), event('Network.responseReceived', { requestId: 'workdir', response: { status: 404 } }), finished('workdir'),
        event('Network.webSocketCreated', { requestId: 'ws', url: `wss://fixture.invalid${path}/web/ws?token=PRIVATE` }),
        event('Network.webSocketFrameSent', { requestId: 'ws', timestamp: 4, response: { payloadData: JSON.stringify({ type: 'runtime_subscribe', session_id: RECORD.session }) } })]
      if (drains === 3) return [event('Network.webSocketFrameReceived', { requestId: 'ws', timestamp: 5, response: { payloadData: JSON.stringify({
        type: 'runtime_snapshot', session_id: RECORD.session, epoch: 'e', seq: 1,
        snapshot: { bot_id: RECORD.bot, session_id: RECORD.session, epoch: 'e', seq: 1, current_run_view: { messages: ['PRIVATE'] } },
      }) } })]
      return []
    },
  }
  return { page, journal, calls, denied, options: { historyUrl, messageIds, append: async row => { journal.push(row) }, captureMs: 25, sampleMs: 1 } }
}

test('accepts only the original ordinary history association and projects schema/IDs without contents', () => {
  assert.equal(historyURL(historyUrl).searchParams.get('session'), RECORD.session)
  assert.throws(() => historyURL(historyUrl + '&run=x'))
  assert.throws(() => historyURL(historyUrl + '&session=other'))
  const metadata = initialMessagesMetadata(JSON.stringify({ items: [{ id: messageIds[0], timestamp: 'not-a-date', text: 'PRIVATE' }] }), messageIds)
  assert.equal(metadata.originals[0].present, true); assert.equal(metadata.originals[1].present, false)
  assert.equal(metadata.items[0].timestampParseable, false)
  assert(!JSON.stringify(metadata).includes('PRIVATE'))
})

test('arms before the one navigation and samples an unrendered history without requiring DCL, a click or healthy prior messages', async () => {
  const f = fixture(), report = await captureInitialHistory(f.page, f.options)
  assert.equal(report.interrupted, null)
  assert.equal(report.domContentEventObserved, false)
  assert(f.calls.findIndex(x => x.method === 'Network.enable') < f.calls.findIndex(x => x.method === 'Page.navigate'))
  assert.equal(f.calls.filter(x => x.method === 'Page.navigate').length, 1)
  assert(f.journal.some(x => x.kind === 'dom' && x.panes[0].historySkeletonNodes === 11 && x.panes[0].renderedMessageCount === 0))
  assert(f.journal.some(x => x.kind === 'original-component-response' && x.metadata.originals.every(row => row.present)))
  assert(f.journal.some(x => x.events?.some(row => row.type === 'runtime_snapshot' && row.session_id === RECORD.session)))
  assert.deepEqual(report.expectedOpenStreamIds, ['sse']); assert.deepEqual(report.observedPendingFiniteRequestIds, [])
  assert(!JSON.stringify(f.journal).includes('PRIVATE'))
  assert(f.calls.every(x => ['Runtime.evaluate', 'Network.enable', 'Runtime.enable', 'Page.enable', 'Page.navigate', 'Network.getResponseBody'].includes(x.method)))
})

test('200 without the original request completion stays pending; no manual fetch substitutes for it', async () => {
  const f = fixture({ noFinish: true }), report = await captureInitialHistory(f.page, f.options)
  assert.deepEqual(report.observedPendingFiniteRequestIds, ['history'])
  assert(!f.calls.some(x => x.method === 'Network.getResponseBody'))
  assert(!f.journal.some(x => x.kind === 'original-component-response'))
})

test('a component-body denial retains the original failure, with no fallback read or second navigation', async () => {
  const f = fixture({ denyBody: true })
  await assert.rejects(captureInitialHistory(f.page, f.options), error => {
    assert.equal(error.cause, f.denied)
    assert.equal(error.captureState.navigateAttempted, true)
    return true
  })
  assert.equal(f.calls.filter(x => x.method === 'Network.getResponseBody').length, 1)
  assert.equal(f.calls.filter(x => x.method === 'Page.navigate').length, 1)
})
