# Isolated Ego click-buffer observation

## Evidence already supplied

The parent relays the sole writer's `navigation603-preflight/OUTCOME.json`:
no production row was clicked. In that run, events had the direct shape
`{method, params, sessionId}`, CDP/evaluation results were direct, and a completed
GET response body was readable. This owner has not read the raw Windows file.
Do not repeat those shape/body checks or the eight local observer tests.

Official Ego help, as relayed by the parent, exposes
`page.click(selector, {button?, clickCount?, delay?, position?, force?, timeout?, label?})`
and destructive `page.events()`, but makes no click-buffer retention promise.
This is an undocumented behavior, not a denial or a missing user authorization.
Use the following one isolated experiment instead of asking for approval or
assuming that the click preserves events.

## Isolation and execution

The sole writer supplies a **new, owned, disposable Ego page at `about:blank`**,
using its existing supported space/page creation flow. Do not pass a production
page, its execution history, or a reused user tab. Creation/close API names are
intentionally not invented here. If that supported page cannot be obtained,
report that exact setup limitation without navigating a production page.

Run the function below once, in the writer's normal foreground executor, with
that page and its existing append-only local JSON sink. Retain the real exec
session if it yields. Supply a fresh ASCII `nonce`, e.g. `ego349-buffer-01`.
There is no server, external browser, application, login, model, API or database
in this experiment. `Page.navigate` loads only an in-memory static `data:` URL;
`fetch` below addresses only local `data:` bytes, not HTTP. CSP blocks external
connections/resources/forms. The only DOM modification is in this explicitly
owned synthetic page. No production DOM, policy or cache is changed.

Protocol references: [Page.navigate](https://chromedevtools.github.io/devtools-protocol/tot/Page/#method-navigate),
[Runtime console events](https://chromedevtools.github.io/devtools-protocol/tot/Runtime/#event-consoleAPICalled),
[Network events](https://chromedevtools.github.io/devtools-protocol/tot/Network/).
These document CDP, not Ego's retention behavior. If an actual method is denied,
stop that action; do not change browser, scheme, permissions or transport to
obtain the same observation.

```js
async function observeClickBuffer(page, append, nonce) {
  if (!/^[a-zA-Z0-9_-]{1,60}$/.test(nonce)) throw Error('Use a fresh ASCII nonce')
  const wait = ms => new Promise(resolve => setTimeout(resolve, ms))
  const cdp = (method, params = {}) => page.cdp(method, params, { timeout: 3000 })
  const read = async expression => {
    const reply = await cdp('Runtime.evaluate', {
      expression, returnByValue: true, awaitPromise: true,
    })
    if (reply.exceptionDetails || !reply.result || !('value' in reply.result)) {
      throw Error('Unexpected direct CDP result; stop, do not adapt/retry')
    }
    return reply.result.value
  }
  const write = async record => append({ hostUtc: new Date().toISOString(), nonce, ...record })
  // Preserve only this synthetic page's nonce markers; never console payloads.
  const drain = async phase => {
    const entries = await page.events()
    if (!Array.isArray(entries)) throw Error('Unexpected events result')
    const events = []
    for (const e of entries) {
      if (!e || typeof e.method !== 'string' || !e.params) throw Error('Unknown event envelope')
      const p = e.params
      if (e.method === 'Runtime.consoleAPICalled') {
        for (const a of p.args || []) {
          if (typeof a.value === 'string' && a.value.startsWith(nonce + ':')) {
            events.push({ method: e.method, marker: a.value, timestamp: p.timestamp, sessionId: e.sessionId })
          }
        }
      } else if (e.method === 'Network.requestWillBeSent' && p.request?.url?.startsWith('data:text/plain,')) {
        const marker = decodeURIComponent(p.request.url.slice('data:text/plain,'.length))
        if (marker.startsWith(nonce + ':')) events.push({ method: e.method, marker,
          requestId: p.requestId, timestamp: p.timestamp, sessionId: e.sessionId })
      } else if (['Network.responseReceived', 'Network.loadingFinished', 'Network.loadingFailed'].includes(e.method)) {
        events.push({ method: e.method, requestId: p.requestId, timestamp: p.timestamp,
          status: p.response?.status, canceled: p.canceled, sessionId: e.sessionId })
      }
    }
    // Save before another destructive read. A failed append is never retried.
    await write({ phase, observedEntryCount: entries.length, events })
    return events
  }
  if (await read('location.href') !== 'about:blank') throw Error('Owned blank page required; no navigation performed')
  await write({ phase: 'start', clickAttempted: false,
    scope: 'isolated data page only; production click coverage remains unknown' })
  const labels = ['baseline', 'before', 'click', 'after'].map(x => nonce + ':' + x)
  const localURL = label => 'data:text/plain,' + encodeURIComponent(label)
  const clickCode = `document.querySelector('output').textContent=String(++window.probeClicks);console.info(${JSON.stringify(labels[2])});void fetch(${JSON.stringify(localURL(labels[2]))}).then(r=>r.text())`
  const html = `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline'; connect-src data:; form-action 'none'; base-uri 'none'"><title>Ego isolated buffer probe</title><script>window.probeClicks=0</script><button id="ego-buffer-probe" type="button" onclick="${clickCode.replaceAll('&', '&amp;').replaceAll('"', '&quot;')}">Probe once</button><output>0</output>`
  const url = 'data:text/html;charset=utf-8,' + encodeURIComponent(html)
  const nav = await cdp('Page.navigate', { url })
  if (nav.errorText) throw Error('Synthetic page navigation failed; stop')
  // Wait for this one document, not another navigation. A timeout stops the run.
  const until = Date.now() + 3000
  let ready = false
  while (Date.now() < until) {
    ready = await read(`location.href===${JSON.stringify(url)} && document.title==='Ego isolated buffer probe' && !!document.querySelector('#ego-buffer-probe')`)
    if (ready) break
    await wait(100)
  }
  if (!ready) throw Error('Synthetic document not ready; no click')
  await cdp('Runtime.enable'); await cdp('Network.enable')
  await drain('setup')
  const marker = label => read(`(async()=>{console.info(${JSON.stringify(label)});const r=await fetch(${JSON.stringify(localURL(label))});await r.text();return true})()`)
  await marker(labels[0]); await wait(250)
  const baseline = await drain('no-click-control')
  const consoleSeen = label => baseline.some(e => e.method === 'Runtime.consoleAPICalled' && e.marker === label)
  const network = baseline.find(e => e.method === 'Network.requestWillBeSent' && e.marker === labels[0])
  if (!consoleSeen(labels[0]) || !network || !baseline.some(e => e.method === 'Network.loadingFinished' && e.requestId === network.requestId)) {
    await write({ phase: 'stop-before-click', reason: 'Local control pipeline incomplete; no retention conclusion' })
    return
  }
  // No events() read between this completed marker and the sole click.
  await marker(labels[1]); await wait(250)
  await write({ phase: 'one-click-begin', clickAttempted: true })
  // Do not wrap in a retry or call a helper that reads events().
  await page.click('#ego-buffer-probe', { timeout: 3000 })
  await drain('immediately-after-click')
  await wait(500); await drain('late-after-click')
  const clicks = await read('Number(document.querySelector("output").textContent)')
  await write({ phase: 'synthetic-dom-count', clicks })
  await marker(labels[3]); await wait(250); await drain('after-control')
  await write({ phase: 'end', productionClickCoverage: 'unknown', websocketCoverage: 'untested' })
}
```

Only this function reads that page's event buffer. The append sink must not call
`page.events()`. No `force`, second click, request interception, CDP/event-buffer listener,
network service, `setDocumentContent`, production `evaluate` mutation or custom
Input command is included. A thrown click has an uncertain outcome: preserve
the original tool result/journal and close only this owned disposable page;
do not call the function or click again. An append/read denial stops without
fallback/retry. Use the existing owner's supported close operation in cleanup;
do not signal old processes. Cleanup failure is reported with the actual page/
space receipt, not described as complete.

## Read the result without extending its claim

Correlate request IDs across the saved batches; a Network marker needs its
request and `loadingFinished`, not merely response200. `sessionId` is a CDP
target-session discriminator, not a Memoh conversation ID. Console timestamps
and Network timestamps have different units; do not subtract them as latency.

| Recorded observation | Allowed conclusion |
| --- | --- |
| Control complete; `before` Console and local Network request/terminal retained after click; `click` markers and DOM count1; `after` control present | This invocation retained these buffered event classes across this one synthetic click, with post-click delivery observable. No permanent Ego contract or WS/production guarantee follows. |
| `before` absent but baseline/after controls present and count1 | Pre-click retention was not observed across this boundary; a drain or transport/delivery gap remains possible. Do not diagnose which internal Ego component lost it. |
| Count1 but missing `click` events, late batch or post-control incomplete | Event coverage is partial/unknown despite a real DOM click. |
| No complete baseline, non1 count, result error or unknown shape | Inconclusive. No production click based on this sample and no unchanged retry. |

The production observer must **always** label the `page.click` interval as
potentially incomplete, even after the first outcome. Retained positive events
can be correlated; absent frames/requests cannot establish snapshot loss,
request cancellation, an empty history or a fixed defect. DOM sampling after
the click still distinguishes Home masking, rendered-but-anchor-hidden rows,
and absent rows/history skeletons. If it only shows generic loading without
retained network/runtime evidence, keep the root cause unknown. This removes
the need for a promise that Ego does not provide.

Return the bounded experiment receipt to the parent before the separately
authorized original-record capture. The experiment itself is not production
navigation or NAS acceptance. This protocol has only been syntax-checked here;
the sole writer has not executed it as of this revision.
