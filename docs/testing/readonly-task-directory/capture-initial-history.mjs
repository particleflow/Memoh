// Initial-load observer for the original read-only history. No row click,
// manual API probe, store inspection, app mutation, reload or synthetic WS.
import { RECORD, sampleDOM, evaluationValue, decodeEvent, responseMetadata, runtimeMetadata } from './capture-ego-navigation.mjs'

const problem = message => Object.assign(Error(message), { observerReason: message })
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms))
const pathOf = value => {
  try { const url = new URL(value); return ['http:', 'https:', 'ws:', 'wss:', 'chrome-extension:', 'moz-extension:'].includes(url.protocol) ? url.pathname : `[${url.protocol.replace(':', '')}]` }
  catch { return '' }
}
const target = `/api/bots/${RECORD.bot}`
const messagePath = `${target}/messages`, sessionPath = `${target}/sessions/${RECORD.session}`
const wsPath = `${target}/web/ws`

export function historyURL(value) {
  const url = new URL(value)
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.hash
    || !/^\/bot\/[^/]+$/.test(url.pathname) || url.searchParams.getAll('session').length !== 1
    || url.searchParams.get('session') !== RECORD.session
    || [...url.searchParams.keys()].some(key => key !== 'session')) throw problem('Use the saved ordinary original history URL, with session only')
  return url
}

export function initialMessagesMetadata(body, messageIds) {
  let data
  try { data = JSON.parse(body) } catch { return { parsed: false } }
  const itemsArray = Array.isArray(data?.items), items = itemsArray ? data.items : []
  return { parsed: true, itemsArray, itemCount: items.length, originals: messageIds.map(id => ({ id, present: items.some(row => row?.id === id) })),
    items: items.map(row => ({ id: row?.id, turn_id: row?.turn_id,
      role: ['user', 'assistant'].includes(row?.role) ? row.role : 'other',
      timestampType: typeof row?.timestamp, timestampParseable: typeof row?.timestamp === 'string' && Number.isFinite(Date.parse(row.timestamp)) })) }
}

export async function captureInitialHistory(page, options) {
  const { append, messageIds } = options, url = historyURL(options.historyUrl)
  const captureMs = options.captureMs ?? 45000, sampleMs = options.sampleMs ?? 500, commandMs = options.commandMs ?? 3000
  if (typeof append !== 'function' || messageIds?.length !== 2 || !messageIds.every(id => typeof id === 'string' && id)) throw problem('Supply the existing journal sink and original two UI IDs')
  if (![captureMs, sampleMs, commandMs].every(ms => Number.isFinite(ms) && ms > 0)) throw problem('Invalid observation window')
  const requests = new Map(), sockets = new Map(), unknown = new Set()
  let phase = 'arm', navigateAttempted = false, navigateReturned = false, domReady = false, interrupted = null, journalFailure = null, lastCDPMethod = null
  const cdp = (method, params = {}) => { lastCDPMethod = method; return page.cdp(method, params, { timeout: commandMs }) }
  const emit = async record => {
    if (journalFailure) throw journalFailure
    try { await append({ hostUtc: new Date().toISOString(), phase, ...record }) }
    catch (error) { journalFailure = error; throw error }
  }
  const drain = async (setup = false) => {
    const entries = await page.events()
    if (!Array.isArray(entries)) throw problem('Unknown events result; no alternate reader')
    const records = [], bodies = []
    let stop = null
    for (const raw of entries) {
      const event = decodeEvent(raw)
      if (!event) { unknown.add(raw && typeof raw === 'object' ? Object.keys(raw).sort().join(',') : typeof raw); continue }
      const { method, params: p } = event, base = { event: method, cdpSessionId: raw.sessionId, timestamp: p.timestamp }
      if (method === 'Network.requestWillBeSent') {
        let requestURL
        try { requestURL = new URL(p.request?.url) } catch { continue }
        if (requestURL.origin !== url.origin && !requestURL.pathname.startsWith('/api/')) continue
        const query = Object.fromEntries(['session_id', 'session', 'limit', 'before_message_id', 'before'].filter(key => requestURL.searchParams.has(key))
          .map(key => [key, requestURL.searchParams.getAll(key)]))
        const request = { requestId: p.requestId, cdpSessionId: raw.sessionId, path: requestURL.pathname, query,
          method: p.request.method, resourceType: p.type, started: p.timestamp, wallTime: p.wallTime, terminal: null,
          redirectedFromStatus: p.redirectResponse?.status }
        requests.set(p.requestId, request); records.push({ ...base, ...request })
        if (!['GET', 'HEAD', 'OPTIONS'].includes(request.method)) stop = 'Non-read request observed; outcome unconfirmed, no repeat'
      } else if (method === 'Network.responseReceived' && requests.has(p.requestId)) {
        const request = requests.get(p.requestId)
        Object.assign(request, { status: p.response?.status, mimeType: p.response?.mimeType,
          fromDiskCache: p.response?.fromDiskCache, fromServiceWorker: p.response?.fromServiceWorker,
          expectedStream: p.type === 'EventSource' || p.response?.mimeType === 'text/event-stream' })
        records.push({ ...base, requestId: p.requestId, status: request.status, mimeType: request.mimeType,
          fromDiskCache: request.fromDiskCache, fromServiceWorker: request.fromServiceWorker, expectedStream: request.expectedStream })
        const originalRead = request.path === sessionPath || (request.path === messagePath && request.query.session_id?.[0] === RECORD.session)
        if (originalRead && [401, 403, 404].includes(request.status)) stop = 'Target history read unavailable; no manual probe or retry'
      } else if (['Network.loadingFinished', 'Network.loadingFailed'].includes(method) && requests.has(p.requestId)) {
        const request = requests.get(p.requestId)
        Object.assign(request, { terminal: method, finished: p.timestamp, canceled: p.canceled, blockedReason: p.blockedReason })
        records.push({ ...base, requestId: p.requestId, canceled: p.canceled, blockedReason: p.blockedReason, hasErrorText: !!p.errorText })
        if (method === 'Network.loadingFinished' && request.method === 'GET' && request.status === 200
          && (request.path === sessionPath || (request.path === messagePath && request.query.session_id?.[0] === RECORD.session))) bodies.push(request)
      } else if (method === 'Network.webSocketCreated') {
        const path = pathOf(p.url)
        if (path === wsPath) { sockets.set(p.requestId, path); records.push({ ...base, requestId: p.requestId, path }) }
      } else if (method.startsWith('Network.webSocket') && sockets.has(p.requestId)) {
        if (['Network.webSocketFrameSent', 'Network.webSocketFrameReceived'].includes(method)) {
          let frame
          try { frame = JSON.parse(p.response?.payloadData) } catch {
            records.push({ ...base, requestId: p.requestId, parsed: false })
            if (method === 'Network.webSocketFrameSent') stop = 'Unparsed outbound WS frame observed; observation-only scope cannot be confirmed'
            continue
          }
          const metadata = runtimeMetadata(p.response?.payloadData)
          if (metadata) records.push({ ...base, requestId: p.requestId, ...metadata,
            epochType: typeof frame.epoch, seqType: typeof frame.seq, snapshotPresent: !!frame.snapshot,
            snapshotEpoch: frame.snapshot?.epoch, snapshotSeq: frame.snapshot?.seq })
          else records.push({ ...base, requestId: p.requestId, type: ['error', 'runtime_subscribe', 'runtime_unsubscribe'].includes(frame?.type) ? frame.type : 'other',
            originalSession: frame?.session_id === RECORD.session, hasMessage: typeof frame?.message === 'string' })
          if (method === 'Network.webSocketFrameSent' && !['runtime_subscribe', 'runtime_unsubscribe'].includes(frame?.type)) stop = 'Non-observation WS command observed; outcome unconfirmed, no repeat'
        } else if (['Network.webSocketHandshakeResponseReceived', 'Network.webSocketClosed', 'Network.webSocketFrameError'].includes(method)) {
          records.push({ ...base, requestId: p.requestId, status: p.response?.status, hasErrorMessage: !!p.errorMessage })
          if (method === 'Network.webSocketHandshakeResponseReceived' && [401, 403, 404].includes(p.response?.status)) stop = 'Target WS handshake unavailable; no manual probe or retry'
        }
      } else if (method === 'Page.domContentEventFired') { domReady = true; records.push(base) }
      else if (['Page.frameNavigated', 'Page.loadEventFired'].includes(method)) records.push({ ...base, path: p.frame?.url ? pathOf(p.frame.url) : undefined })
      else if (method === 'Runtime.exceptionThrown') {
        const e = p.exceptionDetails || {}
        records.push({ ...base, className: e.exception?.className, exceptionId: e.exceptionId,
          frames: (e.stackTrace?.callFrames || []).map(frame => ({ functionName: frame.functionName, path: pathOf(frame.url) })) })
      } else if (method === 'Runtime.consoleAPICalled' && p.type === 'error') records.push({ ...base,
        argumentTypes: (p.args || []).map(arg => arg.type), historyLoadError: (p.args || []).some(arg => arg.value === 'Failed to load session messages:') })
    }
    await emit({ kind: 'event-batch', observedEntryCount: entries.length, unknownEnvelopeKeys: [...unknown], events: records })
    if (setup) { requests.clear(); sockets.clear(); unknown.clear(); domReady = false; return }
    if (stop) throw problem(stop)
    if (unknown.size) throw problem('Changed event envelope; partial evidence only, no adapter fallback')
    for (const request of bodies) {
      const result = await cdp('Network.getResponseBody', { requestId: request.requestId })
      if (typeof result?.body !== 'string' || result.base64Encoded) throw problem('Unknown completed response body; no fetch fallback')
      const metadata = request.path === messagePath ? initialMessagesMetadata(result.body, messageIds) : responseMetadata(request.path, result.body)
      await emit({ kind: 'original-component-response', requestId: request.requestId, path: request.path, metadata })
      if (metadata?.kind === 'session' && (metadata.id !== RECORD.session || metadata.bot_id !== RECORD.bot)) throw problem('Original execution session identity changed')
    }
  }
  try {
    const blank = await cdp('Runtime.evaluate', { expression: 'location.href', returnByValue: true })
    if (blank.exceptionDetails || blank.result?.value !== 'about:blank') throw problem('Explicitly navigate the newly owned page to about:blank before arming; no application navigation performed')
    await emit({ kind: 'capture-contract', source: '9270da0252128a296d9a641235cdfd32ab44cfda', record: RECORD, messageIds,
      path: url.pathname, captureMs, sampleMs, commandMs,
      boundary: 'Page.navigate interval may be incomplete; positive events only. No assumed WS coverage or already-rendered history prerequisite.' })
    await cdp('Network.enable'); await cdp('Runtime.enable'); await cdp('Page.enable')
    await drain(true)
    phase = 'initial-navigation'; navigateAttempted = true
    await emit({ kind: 'navigation-begin' })
    const navigation = await cdp('Page.navigate', { url: url.href })
    navigateReturned = true
    await emit({ kind: 'navigation-returned', frameId: navigation.frameId, loaderId: navigation.loaderId, hasErrorText: !!navigation.errorText })
    if (navigation.errorText) throw problem('Initial navigation returned an error; no retry')
    if (typeof navigation.frameId !== 'string' || !navigation.frameId) throw problem('Unknown navigation result; outcome uncertain, no second navigation')
    phase = 'observe'
    const until = Date.now() + captureMs
    await sleep(sampleMs)
    while (Date.now() < until) {
      await drain()
      // Do not require DCL events, an unmasked Home, a pane or messages to exist.
      const reply = await cdp('Runtime.evaluate', { expression: `Object.assign((${sampleDOM.toString()})(${JSON.stringify({ messageIds })}), { documentProtocol: location.protocol, documentPath: location.pathname, documentReadyState: document.readyState })`, returnByValue: true })
      const dom = evaluationValue(reply)
      await emit({ kind: 'dom', ...dom })
      if (['http:', 'https:'].includes(dom.documentProtocol) && (dom.query.session?.length !== 1 || dom.query.session[0] !== RECORD.session
        || ['task_bot', 'task_type', 'task_id', 'task_record', 'run'].some(key => dom.query[key]))) throw problem('Ordinary original history association changed; no correction/reload')
      await sleep(sampleMs)
    }
    await drain()
  } catch (error) {
    // Store no raw tool error: a URL/error may contain auth or response content.
    interrupted = { name: error?.name || 'Error', lastCDPMethod,
      reason: error?.observerReason || 'External/observer failure; exact original error is retained as cause, not serialized into the journal' }
    if (journalFailure) {
      const failure = new Error('Journal failed; no further append, drain or navigation', { cause: error })
      failure.captureState = { navigateAttempted, navigateReturned, requests: [...requests.values()] }
      throw failure
    }
    await emit({ kind: 'capture-stopped', navigateAttempted, navigateReturned, interrupted })
    const failure = new Error('Initial-history observation stopped; no repeat', { cause: error })
    failure.captureState = { navigateAttempted, navigateReturned, interrupted, requests: [...requests.values()], targetSocketIds: [...sockets.keys()] }
    throw failure
  }
  const report = { navigateAttempted, navigateReturned, interrupted, domContentEventObserved: domReady, requests: [...requests.values()],
    observedPendingFiniteRequestIds: [...requests.values()].filter(r => !r.terminal && !r.expectedStream).map(r => r.requestId),
    expectedOpenStreamIds: [...requests.values()].filter(r => !r.terminal && r.expectedStream).map(r => r.requestId),
    targetSocketIds: [...sockets.keys()], coverage: 'Navigation/WS event completeness unknown. A completed component response or received snapshot does not prove client commit. No refresh, task click or manual API request.' }
  await emit({ kind: 'capture-summary', ...report })
  return report
}
