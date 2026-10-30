/* One no-fee shape observation, on the existing idle fixed-contact Page only.
 * No imports/endpoints/subscriptions. This does not declare any discovered shape
 * to be a stable Ego contract. Only method/key/type metadata leaves the function.
 */
async function probeEgoNetworkShape(page, cfg) {
  if (!(cfg.timeoutMs > 0) || !(cfg.durationMs > 0) || cfg.reloadExistingPage !== true)
    throw new Error('Supply explicit observation budgets and existing-page reload scope');
  const report = {consumer: 'Ego 2.1.0 (parent-reported)', paidCalls: 0, reloads: 0,
    batches: [], examples: {}, cdpEnableShape: null, evaluateShape: null, bodyObservation: null,
    retention: 'not established', crossTurnContinuity: 'not established', overflow: 'not established'};
  const failure = e => ({errorType:e?.name || 'Error', errorCode:typeof e?.code==='number'?e.code:undefined,
    errorMessage:String(e?.message || '').replace(/(?:https?|wss?):\/\/[^\s"<>]+/gi,'[address]').replace(/Bearer\s+\S+/gi,'[auth]')});
  const protectedKey = /headers|authorization|cookie|token|password|secret/i;
  function shape(v, depth = 0) {
    if (depth > 7) return '[deeper]';
    if (v === null) return 'null';
    if (Array.isArray(v)) return {arrayLength: v.length, firstItem: v.length ? shape(v[0], depth+1) : null};
    if (typeof v === 'object') return Object.fromEntries(Object.entries(v).map(([k,x]) =>
      [protectedKey.test(k) ? '[protected-field]' : k.replace(/(?:https?|wss?):\/\/\S+/gi,'[address]'),
        protectedKey.test(k) ? '[omitted]' : shape(x, depth+1)]));
    if (typeof v === 'string' && /^(Network|Page|Runtime|DOM)\.[A-Za-z]+$/.test(v)) return v;
    return typeof v;
  }
  function* nodes(v, path=[]) {
    if (!v || typeof v !== 'object') return;
    if (!Array.isArray(v)) yield {value:v,path};
    for (const [k,x] of Object.entries(v)) yield* nodes(x,[...path,Array.isArray(v)?Number(k):k]);
  }
  function target(request) {
    if (request?.method !== 'GET' || typeof request.url !== 'string' || !request.url.startsWith(cfg.apiRoot.replace(/\/$/,'')+'/')) return false;
    const u=new URL(request.url), rootPath=new URL(cfg.apiRoot).pathname.replace(/\/$/,'');
    return u.pathname === rootPath+`/bots/${cfg.bot}/sessions/${cfg.contact}` ||
      (u.pathname === rootPath+`/bots/${cfg.bot}/messages` && u.searchParams.get('session_id')===cfg.contact);
  }
  const eligible = new Set(); let attemptedBody=false;
  try {
    report.cdpEnableShape=shape(await page.cdp('Network.enable',{}, {timeout:cfg.timeoutMs}));
    // A small read-only evaluate establishes its actual return wrapper without credentials.
    report.evaluateShape=shape(await page.evaluate(() => ({probe:'ego-capture-shape',readyState:document.readyState})));
    await page.cdp('Page.reload', {ignoreCache:false}, {timeout:cfg.timeoutMs}); report.reloads++;
    const deadline=Date.now()+cfg.durationMs;
    do {
      const events=await page.events(); // destructive; sole consumer, no separate .events debug calls
      report.batches.push({array:Array.isArray(events),count:Array.isArray(events)?events.length:null});
      if (!Array.isArray(events)) {report.arrayMismatch=shape(events);break;}
      for (const event of events) {
        const pairs=[...nodes(event)].filter(({value:v})=>typeof v.method==='string' && v.method.startsWith('Network.') && v.params && typeof v.params==='object');
        if (!pairs.length) {
          if (!report.unrecognizedExample) report.unrecognizedExample=shape(event);
          continue;
        }
        for (const {value:v,path} of pairs) {
          if (!report.examples[v.method]) report.examples[v.method]={methodPath:[...path,'method'],paramsPath:[...path,'params'],shape:shape(event)};
          if (v.method==='Network.requestWillBeSent' && target(v.params.request)) eligible.add(v.params.requestId);
          if (!attemptedBody && v.method==='Network.loadingFinished' && eligible.has(v.params.requestId)) {
            attemptedBody=true;
            try {
              const reply=await page.cdp('Network.getResponseBody',{requestId:v.params.requestId},{timeout:cfg.timeoutMs});
              const bodies=[...nodes(reply)].filter(({value:x})=>typeof x.body==='string' && typeof x.base64Encoded==='boolean');
              report.bodyObservation={replyShape:shape(reply),candidates:bodies.map(({value:x,path:p})=>({resultPath:p,bodyType:typeof x.body,base64Encoded:x.base64Encoded,bodyLength:x.body.length}))};
              // No body content or real request ID is returned, and no retention retry is made.
            } catch(e) {report.bodyObservation={outcome:'unknown',...failure(e)};}
          }
        }
      }
      if (attemptedBody) break;
    } while(Date.now()<deadline);
    if (!attemptedBody) report.bodyObservation={outcome:'not_observed',reason:'no exact scoped GET completion in this one observation'};
  } catch(e) {report.operationUnknown={...failure(e)};}
  report.networkDisableAttempted=false; // Ego subscription ownership/cancel contract is not supplied.
  report.staleElementRefs='Reacquire through Ego after CDP; never reuse pre-probe refs';
  return report;
}
