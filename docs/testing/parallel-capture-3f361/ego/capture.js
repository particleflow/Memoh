/* Run in the existing Ego Node host execution with its supplied Page.
 * Host timer pattern: a7665a65d capture-initial-history.mjs, field-used by writer.
 * No page-owned pacing, imports, external endpoints or new browser.
 * page.events() has a single owner and is read/clear, never an EventEmitter.
 */
async function createEgoCapture(page, cfg) {
  if (typeof setTimeout !== 'function') throw new Error('Ego Node host timer required; no Page timer fallback');
  const c = cfg.contract;
  if (!c?.evidence || !['methodPath', 'paramsPath', 'cdpResultPath', 'evaluateResultPath'].every(k => Array.isArray(c[k])))
    throw new Error('Actual consumer contract paths and evidence are required before capture');
  if (![cfg.timeoutMs, cfg.stopDrainMs, cfg.pollIntervalMs].every(x => Number.isFinite(x) && x > 0))
    throw new Error('Explicit finite observer timeout, drain budget and read interval required');
  const root = cfg.apiRoot.replace(/\/$/, ''), prefix = `/bots/${cfg.bot}/sessions/${cfg.contact}`;
  const records = [], requests = new Map(), sockets = new Set(), roles = {}, delegations = new Map(), frames = [];
  const ignoredRequests = new Set(), uncertainRequests = new Set();
  const anchors = new Map(), blocked = new Set(), secrets = new Set([root, root.replace(/^https?:\/\//, '').split('/')[0]]);
  let seq = 0, batch = 0, armed = null, credential = null, readyHTTP = false, readyWS = false;
  let stopped = false, fault = false, tail = Promise.resolve(), lastReadEnded = null;
  let stopRequested = false, stopReason = null, stopPromise = null;
  const activeSamples = new Set();
  const metrics = {reads: 0, nonempty: 0, events: 0, maxBatch: 0, waits: 0, unknowns: 0,
    minimumReadGapMs: null, wsSentFrames: 0, wsReceivedFrames: 0, queueRequests: 0, queueMappings: 0};
  const at = (x, path) => path.reduce((v, key) => v?.[key], x);
  const parse = x => { if (typeof x !== 'string') return x; try { return JSON.parse(x); } catch { return x; } };
  function* objects(x) { x = parse(x); if (!x || typeof x !== 'object') return;
    if (!Array.isArray(x)) yield x; for (const v of Object.values(x)) yield* objects(v); }
  function clean(x) {
    if (typeof x === 'string') {
      for (const value of secrets) if (value) x = x.split(value).join('[REDACTED]');
      return x.replace(/(?:https?|wss?):\/\/[^\s"<>]+/gi, '[ADDRESS]').replace(/Bearer\s+\S+/gi, '[AUTH]')
        .replace(/\b(?:\d{1,3}\.){3}\d{1,3}(?::\d+)?\b/g, '[IP]');
    }
    if (Array.isArray(x)) return x.map(clean);
    if (x && typeof x === 'object') return Object.fromEntries(Object.entries(x).map(([k,v]) =>
      [/authorization|cookie|token|password|secret|headers/i.test(k) ? '[protected-field]' : clean(k),
       /authorization|cookie|token|password|secret|headers/i.test(k) ? '[OMITTED]' : clean(v)]));
    return x;
  }
  const failure = e => ({errorType: e?.name || 'Error', errorCode: e?.code, errorMessage: clean(String(e?.message || ''))});
  const emit = (kind, data = {}) => { const r = clean({seq: ++seq, kind, ...data}); if (/unknown|mismatch|missing/.test(kind)) metrics.unknowns++; records.push(r); return r.seq; };
  function scoped(url) {
    if (typeof url !== 'string' || !url.startsWith(root + '/')) return null;
    const path = url.slice(root.length).split('?')[0];
    if (path === prefix || path.startsWith(prefix + '/')) return path;
    if (path === `/bots/${cfg.bot}/messages` && new URL(url).searchParams.get('session_id') === cfg.contact) return path;
    return null;
  }
  async function command(method, params) {
    const raw = await page.cdp(method, params, {timeout: cfg.timeoutMs});
    const value = at(raw, c.cdpResultPath);
    if (!value || typeof value !== 'object') throw new Error('CDP_RESULT_CONTRACT_MISMATCH');
    return value;
  }
  async function textBody(result, field) {
    if (typeof result[field] !== 'string') throw new Error('BODY_CONTRACT_MISMATCH');
    if (result.base64Encoded === true) {
      const raw = await page.evaluate(({text}) => {
        const bytes = Uint8Array.from(atob(text), x => x.charCodeAt(0));
        return new TextDecoder().decode(bytes);
      }, {text: result[field]});
      const decoded = at(raw, c.evaluateResultPath);
      if (typeof decoded !== 'string') throw new Error('EVALUATE_CONTRACT_MISMATCH');
      return parse(decoded);
    }
    if (field === 'body' && result.base64Encoded !== false) throw new Error('BODY_ENCODING_UNKNOWN');
    return parse(result[field]);
  }
  function bind(invocation, transport, requestId) {
    if (!armed || !invocation) return;
    roles[armed] = {requestInvocation: invocation, transport, requestId,
      admittedInvocation: transport === 'ws' ? invocation : null};
    emit('role_request', {role: armed, ...roles[armed]}); armed = null;
  }
  function discover(value, evidenceSeq) {
    for (const obj of objects(value)) if (obj.delegation_id && obj.source_session_id === cfg.contact && obj.target_bot_id) {
      if (!delegations.has(obj.delegation_id)) emit('delegation_observed', {delegationId: obj.delegation_id, evidenceSeq});
      delegations.set(obj.delegation_id, obj);
    }
  }
  function correlate() {
    const a = roles.A; if (!a) return;
    for (const [body, evidenceSeq] of frames) for (const obj of objects(body))
      if ((a.admittedInvocation && obj.invocation_id === a.admittedInvocation) || (a.runId && obj.run_id === a.runId))
        discover(obj, evidenceSeq);
  }
  async function consume(method, p) {
    if (method === 'Network.requestWillBeSent') {
      const old = requests.get(p.requestId);
      if (old || p.redirectResponse) { if (old) old.done = true; uncertainRequests.add(p.requestId); emit('request_redirect_or_reuse_unknown', {requestId: p.requestId}); }
      const path = scoped(p.request?.url); if (!path) { ignoredRequests.add(p.requestId); return; }
      for (const [key, value] of Object.entries(p.request.headers || {})) if (key.toLowerCase() === 'authorization') {
        credential = value; secrets.add(value); secrets.add(value.replace(/^Bearer\s+/i, ''));
      }
      const r = {path, method: p.request.method, done: false, status: null}; requests.set(p.requestId, r);
      let body = parse(p.request.postData);
      if (body === undefined && p.request.hasPostData) {
        try { body = await textBody(await command('Network.getRequestPostData', {requestId: p.requestId}), 'postData'); }
        catch (e) { emit('request_body_unknown', {requestId: p.requestId, ...failure(e)}); }
      }
      emit('http_request', {requestId: p.requestId, path, method: r.method, body});
      if (r.method === 'POST' && path === prefix + '/follow-up-queue') { metrics.queueRequests++; bind(body?.invocation_id, 'queue', p.requestId); }
    } else if (method === 'Network.responseReceived') {
      const r = requests.get(p.requestId); if (!r) { if (!ignoredRequests.has(p.requestId)) emit('orphan_network_event_unknown', {requestId: p.requestId, method}); return; }
      r.status = p.response?.status;
      emit('response_headers_observed', {requestId: p.requestId, status: r.status}); // no headers/body yet
    } else if (method === 'Network.loadingFinished') {
      const r = requests.get(p.requestId); if (!r) { if (!ignoredRequests.has(p.requestId)) emit('orphan_network_event_unknown', {requestId: p.requestId, method}); return; } if (r.done) return;
      try {
        const body = await textBody(await command('Network.getResponseBody', {requestId: p.requestId}), 'body');
        const evidenceSeq = emit('http_response_complete', {requestId: p.requestId, status: r.status, body});
        if (r.method === 'GET' && r.status === 200) readyHTTP = true;
        for (const [role, v] of Object.entries(roles)) if (v.transport === 'queue' && v.requestId === p.requestId) {
          if (!uncertainRequests.has(p.requestId) && r.status === 202 && typeof body?.item_id === 'string') {
            Object.assign(v, {itemId: body.item_id, admittedInvocation: 'follow-up:' + body.item_id,
              enqueuedDuringRunId: body.enqueued_during_run_id});
            metrics.queueMappings++; emit('queue_mapping', {role, evidenceSeq, ...v});
          } else emit('queue_unresolved', {role, evidenceSeq});
        }
      } catch (e) { emit('response_body_unknown', {requestId: p.requestId, ...failure(e)}); }
      r.done = true; // unavailable/expired body is unknown, never a POST retry
    } else if (method === 'Network.loadingFailed') {
      const r = requests.get(p.requestId); if (!r) { if (!ignoredRequests.has(p.requestId)) emit('orphan_network_event_unknown', {requestId: p.requestId, method}); return; }
      r.done = true; emit('request_failed_unknown', {requestId: p.requestId, canceled: p.canceled === true});
    } else if (method === 'Network.webSocketCreated') {
      if (p.url?.split('?')[0] === root.replace(/^http/, 'ws') + `/bots/${cfg.bot}/web/ws`) {
        sockets.add(p.requestId); readyWS = true; emit('socket_bound', {requestId: p.requestId});
      }
    } else if (method === 'Network.webSocketFrameSent' || method === 'Network.webSocketFrameReceived') {
      if (!sockets.has(p.requestId)) { emit('unbound_socket_frame', {requestId: p.requestId}); return; }
      if (p.response?.opcode !== 1) { emit('non_text_frame_unparsed', {requestId: p.requestId, opcode: p.response?.opcode}); return; }
      const body = parse(p.response.payloadData), sent = method.endsWith('Sent');
      if (sent) metrics.wsSentFrames++; else metrics.wsReceivedFrames++;
      const evidenceSeq = emit(sent ? 'ws_sent' : 'ws_received', {requestId: p.requestId, body});
      if (sent && body?.type === 'message' && body.session_id === cfg.contact) bind(body.invocation_id, 'ws');
      if (!sent) { frames.push([body, evidenceSeq]); correlate(); }
    } else if (method === 'Network.webSocketClosed' || method === 'Network.webSocketFrameError') {
      if (sockets.has(p.requestId)) emit('socket_closed_or_error', {requestId: p.requestId, method});
    }
  }
  async function pauseRead(delayMs) {
    if (delayMs <= 0) return;
    metrics.waits++;
    // Await in the Node host, as in the field-used a766 initial-history observer.
    // Page reload/context replacement cannot destroy this timer. This does not
    // promise survival of host termination or cancel any in-flight Page command.
    await new Promise(resolve => setTimeout(resolve, delayMs));
  }
  async function drainOne(deadline = Infinity) {
    if (stopped || fault) return false;
    try {
      const now = Date.now();
      const delay = lastReadEnded === null ? 0 : Math.min(cfg.pollIntervalMs, Math.max(0, lastReadEnded + cfg.pollIntervalMs - now));
      if (now + delay > deadline) {
        await pauseRead(Math.max(0, deadline - now));
        emit('read_deferred_at_observer_budget'); return false;
      }
      await pauseRead(delay);
      if (Date.now() > deadline) { emit('read_deferred_at_observer_budget'); return false; }
    } catch (e) { fault = true; emit('observer_wait_unknown', {...failure(e)}); return false; }

    let events;
    if (lastReadEnded !== null) {
      const gap = Date.now() - lastReadEnded;
      metrics.minimumReadGapMs = metrics.minimumReadGapMs === null ? gap : Math.min(metrics.minimumReadGapMs, gap);
    }
    try { events = await page.events(); }
    catch (e) { fault = true; emit('events_read_unknown', {...failure(e)}); return false; }
    lastReadEnded = Date.now(); metrics.reads++;
    if (Array.isArray(events)) { metrics.nonempty += events.length > 0 ? 1 : 0; metrics.events += events.length; metrics.maxBatch = Math.max(metrics.maxBatch, events.length); }
    // The buffer was cleared by that call. Number it before any further await.
    emit('buffer_read', {batch: ++batch, count: Array.isArray(events) ? events.length : null});
    if (!Array.isArray(events)) { fault = true; emit('event_array_contract_mismatch'); return false; }
    for (let i = 0; i < events.length; i++) {
      const method = at(events[i], c.methodPath), params = at(events[i], c.paramsPath);
      if (typeof method !== 'string' || !params || typeof params !== 'object') {
        fault = true; emit('event_envelope_contract_mismatch', {batch, index: i}); continue;
      }
      // Record source ordering even for ignored domains; never serialize their raw params.
      emit('event_observed', {batch, index: i, method, requestId: params.requestId, cdpTimestamp: params.timestamp});
      try { await consume(method, params); }
      catch (e) { fault = true; emit('event_processing_unknown', {batch, index: i, ...failure(e)}); }
    }
    return true;
  }
  function drain(deadline = Infinity) { const next = tail.then(() => drainOne(deadline)); tail = next.catch(() => {}); return next; }
  async function get(path) {
    if (!readyHTTP || fault || blocked.has(path) || stopped || stopRequested) return [null, null];
    const startSeq = emit('probe_start', {path});
    let r;
    try {
      const raw = await page.evaluate(async ({url, auth, timeoutMs}) => {
        const controller = new AbortController(), timer = setTimeout(() => controller.abort(), timeoutMs);
        try {
          const response = await fetch(url, {method: 'GET', headers: auth ? {Authorization: auth} : {},
            credentials: 'include', cache: 'no-store', redirect: 'error', signal: controller.signal});
          const text = await response.text(); let body; try { body = JSON.parse(text); } catch { body = text; }
          return {status: response.status, body};
        } catch { return {status: null, error: 'transport_unknown'}; }
        finally { clearTimeout(timer); }
      }, {url: root + path, auth: credential, timeoutMs: cfg.timeoutMs});
      r = at(raw, c.evaluateResultPath);
      if (!r || !Object.hasOwn(r, 'status')) throw new Error('EVALUATE_RESULT_CONTRACT_MISMATCH');
    } catch (e) { r = {status: null, ...failure(e)}; }
    const evidenceSeq = emit('probe_end', {path, startSeq, ...r});
    if (r.status !== 200) { blocked.add(path); emit('probe_blocked', {path}); return [null, evidenceSeq]; }
    return [r.body, evidenceSeq];
  }
  async function sampleImpl() {
    if (stopRequested || stopped) return;
    await drain(); if (fault || stopped || stopRequested) return;
    let b, bSeq, turn, historySeq;
    for (const [role, r] of Object.entries(roles)) {
      if (!r.admittedInvocation) continue;
      const [v, evidenceSeq] = await get(prefix + '/invocations/' + encodeURIComponent(r.admittedInvocation));
      emit('run_binding', {role, evidenceSeq, observation: v});
      if (!v?.found) continue;
      if (v.session_id !== cfg.contact || v.invocation_id !== r.admittedInvocation || !v.run_id || !v.turn_id) {
        emit('invocation_identity_mismatch', {role}); continue;
      }
      Object.assign(r, {runId: v.run_id, turnId: v.turn_id});
      if (role === 'A') {
        correlate(); const [h, hs] = await get(`/bots/${cfg.bot}/messages?session_id=${encodeURIComponent(cfg.contact)}&limit=100`);
        for (const t of h?.items || []) if (t.turn_id === v.turn_id) discover(t, hs);
      }
      if (role === 'B' && v.state === 'completed') { b = v; bSeq = evidenceSeq; }
    }
    if (b) {
      const [h, hs] = await get(`/bots/${cfg.bot}/messages?session_id=${encodeURIComponent(cfg.contact)}&limit=100`);
      historySeq = hs; turn = h?.items?.find(t => t.turn_id === b.turn_id);
      if (!turn) emit('history_window_missing', {turnId: b.turn_id});
    }
    for (const [id, original] of delegations) {
      const [d, ds] = await get(prefix + '/delegations/' + encodeURIComponent(id));
      if (!d?.target_run_id) continue;
      const anchor = JSON.stringify([d.target_bot_id, d.target_session_id, d.target_run_id]);
      if (d.delegation_id !== id || d.source_session_id !== cfg.contact || d.target_bot_id !== original.target_bot_id ||
          !d.target_session_id || (original.target_run_id && original.target_run_id !== d.target_run_id) ||
          (anchors.has(id) && anchors.get(id) !== anchor)) { emit('worker_identity_mismatch', {delegationId: id}); continue; }
      anchors.set(id, anchor);
      if (b && turn && d.target_run_state === 'running') emit('overlap_candidate', {bRun: b.run_id, bTurn: b.turn_id,
        bCompletedSeq: bSeq, persistedHistorySeq: historySeq, subsequentWorkerSeq: ds,
        workerRun: d.target_run_id, delegationId: id, requiresFinalTextAndUIReview: true});
    }
    await drain();
  }
  function sample() {
    if (stopRequested || stopped) return Promise.resolve();
    const work = sampleImpl(); activeSamples.add(work);
    work.then(() => activeSamples.delete(work), () => activeSamples.delete(work));
    return work;
  }
  function requestStop(reason = 'writer_stop') {
    if (!stopRequested) { stopRequested = true; stopReason = reason; emit('observer_stop_requested', {reason}); }
  }
  function summary() {
    return {consumer: cfg.consumer, contractEvidence: c.evidence, pacingRealm: 'node-host', ...metrics, stopped, fault,
      stopReason, readIntervalMs: cfg.pollIntervalMs, scopedGetObserved: readyHTTP, sourceSocketCreated: readyWS, pendingResponses: [...requests.values()].filter(r => !r.done).length,
      bufferContinuity: 'UNVERIFIED', businessCancellationSent: false, networkDisableAttempted: false};
  }
  function stop(reason = 'writer_stop') {
    requestStop(reason);
    if (stopPromise) return stopPromise;
    stopPromise = (async () => {
      await Promise.allSettled([...activeSamples]); await tail;
      const until = Date.now() + cfg.stopDrainMs;
      do {
        if (!await drain(until)) break;
      } while (!fault && [...requests.values()].some(r => !r.done) && Date.now() < until);
      for (const [requestId, r] of requests) if (!r.done) emit('response_unknown_on_stop', {requestId});
      stopped = true;
      emit('stopped', {roles, unresolvedArm: armed, delegationIds: [...delegations.keys()], ...summary(),
        status: 'capture_only_not_acceptance'});
      credential = null; secrets.clear();
      return records.slice();
    })();
    return stopPromise;
  }
  async function collectFor(durationMs) {
    if (!Number.isFinite(durationMs) || durationMs <= 0) throw new Error('Finite observer duration required');
    const until = Date.now() + durationMs;
    emit('observer_window', {durationMs});
    while (!stopRequested && !stopped && !fault && Date.now() < until) {
      if (!await drain(until)) break;
    }
    return stop(fault ? 'collector_fault' : stopRequested ? stopReason : 'observation_budget');
  }
  emit('started', {contractEvidence: c.evidence, consumer: cfg.consumer, readIntervalMs: cfg.pollIntervalMs, bufferContinuity: 'UNVERIFIED'});
  try { await command('Network.enable', {}); await drain(); }
  catch (e) { fault = true; emit('network_enable_unknown', {...failure(e)}); }
  return {
    drain, sample, collectFor, requestStop, stop, summary,
    async arm(role) {
      if (stopRequested) throw new Error('Observer is stopping; do not submit another input');
      await drain();
      if (stopped || stopRequested || fault || !readyHTTP || !readyWS) throw new Error('Fresh socket, successful scoped GET and valid contract required');
      if (!['A', 'B'].includes(role) || roles[role] || armed) throw new Error('Role already bound or pending');
      armed = role; emit('armed', {role});
    },
    records(afterSeq = 0) { return records.filter(r => r.seq > afterSeq); }
  };
}
