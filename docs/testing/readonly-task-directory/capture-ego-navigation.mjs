// Observer only. The sole field writer supplies an existing Ego 2.1 page,
// its already verified normal row-click callback, and an append-only JSON sink.
// No browser connection, navigation, fetch, DOM mutation or Vue/Pinia inspection.
export const RECORD = Object.freeze({
  bot: '791af2c4-2d89-4c57-b863-5d5ff53ff9f7',
  session: 'c736a091-4c7d-4196-9cd6-00588de2a0ae',
  task: '1d742cc8-2760-44f7-a889-d66cd8461502',
  run: '9dfd7812-592c-482c-8083-abacda7278d9',
})
const taskKeys = ['task_bot', 'task_type', 'task_id', 'task_record', 'run']
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms))
const object = value => value !== null && typeof value === 'object' && !Array.isArray(value)
const safeError = error => String(error?.message || error)
  .replace(/https?:\/\/[^\s"']+/g, value => safePath(value))
  .replace(/\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b/g, '[redacted-token]')
  .slice(0, 400)
const safePath = value => {
  if (typeof value !== 'string' || !value) return ''
  try {
    const url = new URL(value, 'https://observer.invalid')
    return ['http:', 'https:', 'chrome-extension:', 'moz-extension:'].includes(url.protocol)
      ? url.pathname : `[${url.protocol.replace(':', '')}]`
  } catch { return '' }
}
const routeKeys = ['session', ...taskKeys]

export function sampleDOM(input) {
  const { document, location, getComputedStyle, innerHeight, innerWidth } = globalThis
  const visible = element => {
    if (!element) return false
    const rect = element.getBoundingClientRect(), css = getComputedStyle(element)
    return rect.width > 0 && rect.height > 0 && css.display !== 'none' && css.visibility !== 'hidden'
  }
  const viewport = element => {
    if (!visible(element)) return false
    const rect = element.getBoundingClientRect()
    return rect.bottom > 0 && rect.right > 0 && rect.top < innerHeight && rect.left < innerWidth
  }
  const dock = document.querySelector('.dockview-theme-memoh')
  const masks = []
  for (let el = dock; el && el !== document.body; el = el.parentElement) {
    if (el.classList.contains('invisible')) masks.push({ tag: el.tagName, classes: el.className })
  }
  const roots = new Set()
  for (const button of document.querySelectorAll('button:has(svg.lucide-list-tree)')) {
    const footer = button.parentElement, root = footer?.parentElement
    if (footer?.classList.contains('shrink-0') && footer.classList.contains('justify-end')
      && root?.querySelector(':scope > section > section > [data-slot="scroll-area"]')) roots.add(root)
  }
  const panes = [...roots].map(root => {
    const scroll = root.querySelector(':scope > section > section > [data-slot="scroll-area"]')
    const previous = scroll.previousElementSibling
    const title = previous?.querySelector(':scope > .max-w-md > p')
    return {
      // This sibling is the PanePlaceholder in the actual 9270 template.
      anchorPlaceholder: title ? (title.textContent || '').trim().slice(0, 180) : null,
      scrollInlineDisplay: scroll.style.display,
      scrollComputedDisplay: getComputedStyle(scroll).display,
      historySkeletonNodes: scroll.querySelectorAll('[data-slot="skeleton"]').length,
      renderedMessageCount: scroll.querySelectorAll('[data-message-id]').length,
      originalMessages: input.messageIds.map(id => {
        const el = [...scroll.querySelectorAll('[data-message-id]')].find(row => row.dataset.messageId === id)
        return { id, present: !!el, visible: visible(el), inViewport: viewport(el) }
      }),
      visibleInputCount: [...root.querySelectorAll('textarea')].filter(visible).length,
    }
  })
  const query = Object.fromEntries(['session', 'task_bot', 'task_type', 'task_id', 'task_record', 'run']
    .filter(key => new URL(location.href).searchParams.has(key))
    .map(key => [key, new URL(location.href).searchParams.getAll(key)]))
  return { format: 'memoh-navigation-dom/v1', pageUtc: new Date().toISOString(), query,
    dockPresent: !!dock, homeMaskAncestors: masks, paneCount: panes.length, panes }
}

export function evaluationValue(reply) {
  // Raw CDP forms only; actual Ego wrapping still requires the writer’s help check.
  const command = reply?.result?.result ? reply.result : reply
  if (command?.exceptionDetails) throw new Error('DOM evaluation returned exceptionDetails')
  if (command?.result?.type !== 'object' || command.result.value?.format !== 'memoh-navigation-dom/v1') {
    throw new Error('Unverified Ego CDP result envelope; no click permitted')
  }
  return command.result.value
}

export function decodeEvent(entry) {
  if (!object(entry) || typeof entry.method !== 'string' || !object(entry.params)) return null
  return { method: entry.method, params: entry.params }
}

export function runtimeMetadata(payload) {
  if (typeof payload !== 'string') return null
  let frame
  try { frame = JSON.parse(payload) } catch { return null }
  if (!object(frame) || frame.session_id !== RECORD.session) return null
  const view = frame.snapshot?.current_run_view
  return Object.fromEntries(Object.entries({
    type: ['runtime_subscribe', 'runtime_unsubscribe', 'runtime_snapshot', 'runtime_delta', 'runtime_dropped', 'error'].includes(frame.type) ? frame.type : 'other',
    session_id: frame.session_id, epoch: frame.epoch, seq: frame.seq,
    snapshot_bot_id: frame.snapshot?.bot_id, snapshot_session_id: frame.snapshot?.session_id,
    cursor: frame.cursor ? { epoch: frame.cursor.epoch, seq: frame.cursor.seq } : undefined,
    run_id: view?.run_id, turn_id: view?.turn_id, state: view?.status,
    // Neither the snapshot transcript nor a command's text is retained.
  }).filter(([, value]) => value !== undefined))
}

export function responseMetadata(path, body) {
  let data
  try { data = JSON.parse(body) } catch { return { parsed: false } }
  if (path.endsWith('/messages')) return { parsed: true, kind: 'messages', items:
    (Array.isArray(data.items) ? data.items : []).map(row => ({ id: row.id, turn_id: row.turn_id, role: row.role })) }
  if (path.endsWith(`/tasks/bot_delegation/${RECORD.task}`)) return {
    parsed: true, kind: 'task', task_type: data.task_type, task_id: data.task_id, record_id: data.record_id,
    destination: data.destination ? Object.fromEntries(['bot_id', 'session_id', 'run_id', 'turn_id']
      .map(key => [key, data.destination[key]])) : null,
  }
  if (path.endsWith(`/sessions/${RECORD.session}`)) return { parsed: true, kind: 'session', id: data.id, bot_id: data.bot_id }
  return null
}

export async function captureRecipientNavigation(page, options) {
  const { messageIds, turnId, clickOnce, append } = options
  const captureMs = options.captureMs ?? 30000, preflightMs = options.preflightMs ?? 6500
  const sampleMs = options.sampleMs ?? 500, commandMs = options.commandMs ?? 3000
  if (!Array.isArray(messageIds) || messageIds.length !== 2 || !messageIds.every(id => typeof id === 'string' && id)) throw new Error('Supply the two original IDs from the saved receipt')
  if (!turnId || typeof clickOnce !== 'function' || typeof append !== 'function') throw new Error('Missing saved turn ID, verified row click or journal sink')
  if (typeof page?.cdp !== 'function' || typeof page?.events !== 'function') throw new Error('Ego cdp/events interface missing')
  if (![captureMs, preflightMs, sampleMs, commandMs].every(ms => Number.isFinite(ms) && ms > 0)) throw new Error('Invalid observation durations')
  const requests = new Map(), ws = [], unknownShapes = new Set()
  let phase = 'pre-arm', clickAttempted = false, clickReturned = false, pollingWitness = false, interrupted = null
  let journalFailure = null
  const emit = async record => {
    if (journalFailure) throw journalFailure
    try { await append({ hostUtc: new Date().toISOString(), phase, ...record }) }
    catch (error) { journalFailure = error; throw error }
  }
  const cdp = (method, params = {}) => page.cdp(method, params, { timeout: commandMs })
  const dom = async () => evaluationValue(await cdp('Runtime.evaluate', {
    expression: `(${sampleDOM.toString()})(${JSON.stringify({ messageIds })})`,
    returnByValue: true, awaitPromise: false,
  }))
  const drain = async () => {
    // events() drains the array: persist this batch before reading another one.
    const entries = await page.events()
    if (!Array.isArray(entries)) throw new Error('Ego events() did not return an array; stop without a second reader')
    const batch = [], bodies = []
    let nonReadObserved = false, readDenied = false, foreignTaskDetail = false
    for (const entry of entries) {
      const decoded = decodeEvent(entry)
      if (!decoded) { unknownShapes.add(object(entry) ? Object.keys(entry).sort().join(',') : typeof entry); continue }
      const { method, params: p } = decoded
      if (method === 'Network.requestWillBeSent') {
        const path = safePath(p.request?.url)
        if (!path.startsWith('/api/')) continue
        const rawQuery = new URL(p.request.url).searchParams
        const query = Object.fromEntries(['session_id', 'limit', ...routeKeys].filter(key => rawQuery.has(key)).map(key => [key, rawQuery.getAll(key)]))
        const request = { requestId: p.requestId, path, query, method: p.request.method,
          started: p.timestamp, wallTime: p.wallTime, phase, terminal: null }
        requests.set(p.requestId, request); batch.push({ ...request, event: method })
        if (request.method !== 'GET' && request.method !== 'HEAD' && request.method !== 'OPTIONS') nonReadObserved = true
        if (phase !== 'pre-arm' && path.startsWith(`/api/bots/${RECORD.bot}/tasks/`)
          && path !== `/api/bots/${RECORD.bot}/tasks/bot_delegation/${RECORD.task}`) foreignTaskDetail = true
      } else if (method === 'Network.responseReceived' && requests.has(p.requestId)) {
        const request = requests.get(p.requestId); request.status = p.response?.status
        batch.push({ event: method, requestId: p.requestId, timestamp: p.timestamp, status: request.status })
        const navigationRead = request.path === `/api/bots/${RECORD.bot}/tasks`
          || request.path === `/api/bots/${RECORD.bot}/tasks/bot_delegation/${RECORD.task}`
          || request.path === `/api/bots/${RECORD.bot}/sessions/${RECORD.session}`
          || (request.path === `/api/bots/${RECORD.bot}/messages` && request.query.session_id?.[0] === RECORD.session)
        if ([401, 403, 404].includes(request.status) && navigationRead) readDenied = true
      } else if (['Network.loadingFinished', 'Network.loadingFailed'].includes(method) && requests.has(p.requestId)) {
        const request = requests.get(p.requestId)
        request.terminal = method; request.finished = p.timestamp
        batch.push({ event: method, requestId: p.requestId, timestamp: p.timestamp, canceled: p.canceled,
          blockedReason: p.blockedReason, errorText: p.errorText ? safeError({ message: p.errorText }) : undefined })
        if (phase === 'preflight' && request.phase === 'preflight' && method === 'Network.loadingFinished'
          && request.path === `/api/bots/${RECORD.bot}/tasks` && request.status === 200) pollingWitness = true
        if (phase !== 'pre-arm' && method === 'Network.loadingFinished' && request.method === 'GET' && request.status === 200
          && ((request.path === `/api/bots/${RECORD.bot}/messages` && request.query.session_id?.[0] === RECORD.session)
            || request.path === `/api/bots/${RECORD.bot}/sessions/${RECORD.session}`
            || request.path === `/api/bots/${RECORD.bot}/tasks/bot_delegation/${RECORD.task}`)) bodies.push(request)
      } else if (['Network.webSocketFrameSent', 'Network.webSocketFrameReceived'].includes(method)) {
        const metadata = runtimeMetadata(p.response?.payloadData)
        if (metadata) { const record = { event: method, requestId: p.requestId, timestamp: p.timestamp, ...metadata }; ws.push(record); batch.push(record) }
      } else if (['Network.webSocketClosed', 'Network.webSocketFrameError'].includes(method)) {
        batch.push({ event: method, requestId: p.requestId, timestamp: p.timestamp })
      } else if (method === 'Runtime.exceptionThrown') {
        const e = p.exceptionDetails || {}, frames = e.stackTrace?.callFrames || []
        batch.push({ event: method, timestamp: p.timestamp, exceptionId: e.exceptionId,
          className: e.exception?.className, frames: frames.map(frame => ({
            functionName: frame.functionName, path: safePath(frame.url), scheme: String(frame.url || '').split(':')[0],
          })) })
      } else if (method === 'Runtime.consoleAPICalled' && p.type === 'error') {
        batch.push({ event: method, timestamp: p.timestamp, argumentTypes: (p.args || []).map(arg => arg.type),
          historyLoadError: (p.args || []).some(arg => arg.value === 'Failed to load session messages:') })
      }
    }
    await emit({ kind: 'event-batch', observedEntryCount: entries.length, unknownEnvelopeKeys: [...unknownShapes], events: batch })
    if (nonReadObserved && phase !== 'pre-arm') throw new Error('Non-read HTTP request observed; do not repeat or infer its outcome')
    if (readDenied && phase !== 'pre-arm') throw new Error('Scoped read denied/unavailable; no probe, fallback or repeat')
    if (foreignTaskDetail) throw new Error('A different task detail was requested; stop this capture')
    for (const request of bodies) {
      // Read only the already completed component response. Never fetch its URL.
      const reply = await cdp('Network.getResponseBody', { requestId: request.requestId })
      const result = typeof reply?.body === 'string' ? reply : reply?.result
      if (typeof result?.body !== 'string' || result.base64Encoded) throw new Error('Unverified response-body envelope/encoding; no alternate fetch')
      const metadata = responseMetadata(request.path, result.body)
      await emit({ kind: 'component-response-metadata', requestId: request.requestId, path: request.path, metadata })
      if (metadata?.kind === 'task' && (metadata.task_id !== RECORD.task || metadata.record_id !== RECORD.task
        || metadata.task_type !== 'bot_delegation' || metadata.destination?.bot_id !== RECORD.bot
        || metadata.destination.session_id !== RECORD.session || metadata.destination.run_id !== RECORD.run
        || metadata.destination.turn_id !== turnId)) throw new Error('Original execution association unavailable/changed; do not choose another destination')
    }
  }
  try {
    await emit({ kind: 'capture-contract', record: RECORD, messageIds, turnId,
      source: '9270da0252128a296d9a641235cdfd32ab44cfda', captureMs, preflightMs, sampleMs, commandMs,
      note: 'Ego method signatures supplied by parent; actual result/event envelopes are checked before the sole click. No page.evaluate shortcut or external browser adapter.' })
    await drain()
    requests.clear(); ws.length = 0; unknownShapes.clear(); pollingWitness = false
    phase = 'preflight'
    await cdp('Network.enable'); await cdp('Runtime.enable')
    const start = await dom(); await emit({ kind: 'dom-before', ...start })
    if (start.query.session?.length !== 1 || start.query.session[0] !== RECORD.session || taskKeys.some(key => start.query[key])
      || start.paneCount !== 1 || start.homeMaskAncestors.length
      || !start.panes[0].originalMessages.every(message => message.present && message.visible)
      || start.panes[0].visibleInputCount !== 0) throw new Error('Start must be the loaded original read-only history without a typed task query; no click')
    const deadline = Date.now() + preflightMs
    while (!pollingWitness && Date.now() < deadline) { await drain(); if (!pollingWitness) await sleep(sampleMs) }
    if (!pollingWitness || unknownShapes.size) throw new Error('Network event envelope/coverage not verified by the existing recipient directory poll; no click')
    phase = 'row-click'; clickAttempted = true
    await emit({ kind: 'one-click-begin' }); await clickOnce(); clickReturned = true
    await emit({ kind: 'one-click-returned' })
    phase = 'observe'; const until = Date.now() + captureMs
    while (Date.now() < until) { await drain(); await emit({ kind: 'dom', ...await dom() }); await sleep(sampleMs) }
    await drain(); await emit({ kind: 'dom-final', ...await dom() })
  } catch (error) {
    interrupted = { name: error?.name || 'Error', message: safeError(error) }
    if (journalFailure) {
      const failure = new Error('Journal write failed; no further drain, click or write attempt is permitted')
      failure.captureState = { clickAttempted, clickReturned, interrupted,
        requests: [...requests.values()], runtimeFrames: ws, unknownEnvelopeKeys: [...unknownShapes] }
      throw failure
    }
    await emit({ kind: 'capture-stopped', clickAttempted, clickReturned, error: interrupted })
  }
  const report = { clickAttempted, clickReturned, interrupted, unknownEnvelopeKeys: [...unknownShapes],
    requests: [...requests.values()], runtimeFrames: ws,
    observedPendingRequestIds: [...requests.values()].filter(request => !request.terminal).map(request => request.requestId),
    networkPollPipelineVerified: pollingWitness,
    coverage: 'No absent WS frame or hidden internal flag is inferred. A received response is not complete without loadingFinished. No refresh, probe or second click is performed.' }
  await emit({ kind: 'capture-summary', ...report })
  return report
}
