import test from 'node:test'
import assert from 'node:assert/strict'
import { captureInitialHistory } from './capture-initial-history.mjs'
import { RECORD } from './capture-ego-navigation.mjs'

// Synthetic IDs only. They do not identify the two unextracted field POSTs.
const otherBot = '00000000-0000-4000-8000-000000000001'
const account = '00000000-0000-4000-8000-000000000002'
const contact = '00000000-0000-4000-8000-000000000003'
const route = '00000000-0000-4000-8000-000000000004'
const otherContact = '00000000-0000-4000-8000-000000000005'
const ids = ['original-user', 'original-assistant']
const ev = (method, params) => ({ method, params, sessionId: 'cdp-page' })
const complete = id => [ev('Network.responseReceived', { requestId: id, response: { status: 200 } }), ev('Network.loadingFinished', { requestId: id, timestamp: 3 })]
const req = (id, bot, suffix, method = 'POST', extra = {}) => ev('Network.requestWillBeSent', { requestId: id, timestamp: 1,
  request: { url: `https://fixture.invalid/api/bots/${bot}/${suffix}`, method, hasPostData: false, ...extra },
  initiator: { type: 'script', stack: { callFrames: [{ functionName: 'openContact', url: 'https://fixture.invalid/assets/main.js?token=PRIVATE', lineNumber: 4, columnNumber: 2 }] } } })

function fixture({ bind = false, changed = false, missingManifest = false, body = false, foreignOrigin = false } = {}) {
  let reads = 0, navigated = false
  const calls = [], journal = []
  const page = {
    async cdp(method, params) {
      calls.push({ method, params })
      if (method === 'Page.navigate') { navigated = true; return { frameId: 'main' } }
      if (method === 'Runtime.evaluate') return !navigated ? { result: { type: 'string', value: 'about:blank' } }
        : { result: { type: 'object', value: { format: 'memoh-navigation-dom/v1', documentProtocol: 'https:', query: { session: [RECORD.session] }, panes: [] } } }
      if (method === 'Network.getResponseBody') {
        if (params.requestId === 'messages') return { body: JSON.stringify({ items: ids.map(id => ({ id, timestamp: '2026-10-30T00:00:00Z' })) }) }
        const bot = params.requestId === 'c2' ? otherBot : RECORD.bot
        return { body: JSON.stringify({ id: bot === otherBot || changed ? otherContact : contact, bot_id: bot,
          created_by_user_id: account, route_id: route, type: 'chat', title: 'PRIVATE', metadata: { text: 'PRIVATE' } }) }
      }
      return {}
    },
    async events() {
      if (++reads === 1) return []
      if (reads === 2) return [req('messages', RECORD.bot, `messages?session_id=${RECORD.session}`, 'GET'), ...complete('messages'),
        req('c1', RECORD.bot, bind ? 'sessions/contact/bind' : 'sessions/contact', 'POST', {
          ...body ? { hasPostData: true, postData: '{"user_id":"PRIVATE"}' } : {},
          ...foreignOrigin ? { url: `https://foreign.invalid/api/bots/${RECORD.bot}/sessions/contact` } : {},
        }), ...complete('c1'),
        ...bind ? [] : [req('c2', otherBot, 'sessions/contact'), ...complete('c2')]]
      if (reads === 3) return [ev('Network.webSocketCreated', { requestId: 'ws', url: `wss://fixture.invalid/api/bots/${RECORD.bot}/web/ws` }),
        ev('Network.webSocketFrameReceived', { requestId: 'ws', response: { payloadData: JSON.stringify({
          type: 'runtime_snapshot', session_id: RECORD.session, epoch: 'e', seq: 1, snapshot: { bot_id: RECORD.bot, session_id: RECORD.session },
        }) } })]
      return []
    },
  }
  return { page, calls, journal, options: { historyUrl: `https://fixture.invalid/bot/${RECORD.bot}?session=${RECORD.session}`, messageIds: ids,
    append: async row => { journal.push(row) }, captureMs: 25, sampleMs: 1,
    initializationContactBotIds: missingManifest ? [RECORD.bot] : [RECORD.bot, otherBot],
    knownContacts: [{ botId: RECORD.bot, ownerIdentityId: account, sessionId: contact, routeId: route }] } }
}

test('continues through exactly manifested normal initialization contacts, recording scope/effect instead of losing history and WS evidence', async () => {
  const f = fixture(), report = await captureInitialHistory(f.page, f.options)
  assert.equal(report.interrupted, null)
  const results = f.journal.filter(row => row.kind === 'initialization-contact-response')
  assert.equal(results.length, 2)
  assert.equal(results[0].identity.matchesSavedBaseline, true)
  assert.equal(results[1].identity.matchesSavedBaseline, null)
  assert.equal(results[1].identity.bindingEffect, 'unknown')
  assert(f.journal.some(row => row.kind === 'original-component-response'))
  assert(f.journal.some(row => row.events?.some(e => e.type === 'runtime_snapshot')))
  assert(!JSON.stringify(f.journal).includes('PRIVATE'))
  assert.equal(f.calls.filter(row => row.method === 'Page.navigate').length, 1)
})

test('bind, unmanifested Bot, nonempty/identity body and foreign origin still stop with unknown effects and no re-navigation', async () => {
  for (const options of [{ bind: true }, { missingManifest: true }, { body: true }, { foreignOrigin: true }]) {
    const f = fixture(options)
    await assert.rejects(captureInitialHistory(f.page, f.options), error => {
      assert.match(error.captureState.interrupted.reason, /Unclassified or out-of-scope mutation/)
      return true
    })
    assert.equal(f.calls.filter(row => row.method === 'Page.navigate').length, 1)
    assert(!f.calls.some(row => row.method === 'Network.getResponseBody'))
    assert(!JSON.stringify(f.journal).includes('PRIVATE'))
  }
})

test('a contact differing from the saved authority is recorded then stops without rebinding or changing the ordinary history URL', async () => {
  const f = fixture({ changed: true })
  await assert.rejects(captureInitialHistory(f.page, f.options), error => {
    assert.match(error.captureState.interrupted.reason, /invalid\/changed account or binding identity/)
    return true
  })
  const row = f.journal.find(record => record.kind === 'initialization-contact-response')
  assert.equal(row.identity.matchesSavedBaseline, false)
  assert.equal(row.identity.bindingEffect, 'unknown')
  assert.equal(f.calls.filter(row => row.method === 'Page.navigate').length, 1)
  assert(f.calls.every(row => ['Runtime.evaluate', 'Page.navigate', 'Page.enable', 'Runtime.enable', 'Network.enable', 'Network.getResponseBody'].includes(row.method)))
})
