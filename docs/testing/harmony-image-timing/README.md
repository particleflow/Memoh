# Temporary local image-stage measurement

Source-only follow-up to `a2f3c9f6c597f71688c5677dccfcfc2ce659e95e` (1.0.16).
The deployed 1.0.16 package and its configuration are unchanged. This patch is
for combination with the unified owner's directory/attachment work before a
single subsequent package; it is not an independently released phone version.

## Actual scope

The phone inline image path already reads the complete authenticated NAS
response, checks the declared byte length and SHA256, creates a Blob and then
lets the image element load. This patch timestamps those existing stages; it
does not change the request, origin, TLS, credential, delegation, receipt,
original root or content-verification rules. Both content images and attachment
thumbnails are connected. The SDK's existing configured fetch is retained.
The separate lightbox request is not attributed to the inline card.

Default off. An explicitly armed window collects at most five newly mounted
image references for 120 seconds. Output contains only a local sequence (1–5),
monotonic milliseconds, byte count, feature booleans and numeric failure codes.
No URL, content hash, filename, user/Bot/session/run ID, credential, image body,
HiLog entry, file, remote upload, new UI, permission or debug endpoint is added.
Logout, pagehide, app background/detach, take and timeout clear collection.
Native output is independently whitelisted, bounded and expires after 120s.
The desktop filter strips surrounding service metadata instead of saving it.

The fixed three native commands use the existing app-owned WebView controller.
They do not enable Web debugging or execute caller-provided JavaScript.
`arm`/`take` require the ready local chat at `begonia-client://app/index.html`;
foreign pages and unready views cannot be sampled. `clear` can clear a local
view after chat readiness is lost. No command starts a fetch or model task.

## Taking the existing five images on the actual device

This requires the eventual combined build, the already-authorized onsite writer
and a normally authorized existing HDC connection. It has **not** been performed
on Harmony/ArkWeb or a physical phone. Do not enable remote Web debugging,
change security settings or bypass a release-device permission denial to make
these commands work. If the device refuses app onDump, retain that exact error
as the remaining readback blocker.

1. Keep the app loaded and logged in, viewing a different existing conversation.
   First arm the temporary collector (no `-D`, force-stop or data clearing):

   ```text
   hdc shell "aa start -b cloud.begonia.workassistant -a EntryAbility --ps garnetImageTiming arm"
   ```

2. Revisit the messages containing the same five existing images. No generation,
   upload or paid task is needed. The five newly mounted image references are
   numbered in encounter order. Reopening already loaded images without a new
   component mount cannot reconstruct their original stages. Historical timing
   from before collection was armed cannot be backfilled.

3. After display finishes, within the 120-second window, take a numeric snapshot:

   ```text
   hdc shell "aa start -b cloud.begonia.workassistant -a EntryAbility --ps garnetImageTiming take"
   ```

4. Use the application's current **AbilityRecord ID**, not its PID or an invented
   fixture ID. The onsite owner must obtain/verify this locally using the
   device's supported AbilityManagerService help/list; do not publish that list.
   The service forwards the app client dump to its `UIAbility.onDump`:

   ```text
   hdc shell "hidumper -s AbilityManagerService -a '-i <record-id> -c'" | python image_timing_readback.py
   ```

   `take` is asynchronous; if the first read has no completed snapshot, repeat
   only this read after completion, not `take` (which already cleared Web rows).
   The filter prints only `GarnetImageTiming` numeric JSON. An empty snapshot or
   invalid/missing output is unavailable evidence, not a zero-latency result.
   Do not save raw service output or turn on persistent console logging.

5. Clear at the end (or let the short window expire):

   ```text
   hdc shell "aa start -b cloud.begonia.workassistant -a EntryAbility --ps garnetImageTiming clear"
   ```

The official [aa documentation](https://raw.githubusercontent.com/openharmony/docs/master/en/application-dev/tools/aa-tool.md)
describes Want string parameters and recommends hidumper over deprecated aa
dump. [hidumper](https://raw.githubusercontent.com/openharmony/docs/master/en/application-dev/dfx/hidumper.md)
describes service-specific options; verify the installed device's help first.
Local SDK26.0.0.105 declares WebviewController.runJavaScript (API9+) and
UIAbility.onDump. These declarations alone do not establish release-device
access or actual ArkWeb execution.

## Interpretation

| Numeric interval | What it bounds |
| --- | --- |
| card → request | Component watcher creation to actual fetch invocation |
| request → headers | Fetch invocation to response headers available |
| headers → body | Remaining body read and SDK arrayBuffer parsing |
| hashStart → hashEnd | Existing SHA256 check |
| blob → load | Object-URL assignment to image load event |
| decodeStart → decodeEnd | Explicit decode-promise completion after load; not isolated decoder CPU cost |
| render | Only actual ElementTiming renderTime when the engine supplies it |

`card` is the component watcher observation time, not proof of a displayed card
or paint. Resource Timing candidates must be fetch/XHR entries whose start is at
or after this specific request invocation. `resourceMatch: 1` denotes exactly
one candidate; `resourceMatch: 2` denotes multiple candidates and emits no
Resource Timing phase or size fields. Missing candidates have no match value.
`responseStart`, `responseEnd`, `encodedBodySize` and `transferSize` are copied
only from a unique candidate, and zero, masked or missing fields remain absent.
Do not treat a cached response or positive transfer size alone as a measurement
of external network throughput. The [Resource Timing specification](https://www.w3.org/TR/resource-timing/)
limits cross-origin phase/size data. Full `bytes` is the already-read body size. To
estimate body-read MB/s, use bytes / 1,000,000 divided by positive
(body − headers)/1000; report it separately from card-to-load latency and from
MiB/s. It is not a TCP throughput/ACK measurement or the user's 5 MB/s acceptance.

Paint support is feature-detected. Unsupported or missing ElementTiming render
is unknown; neither rAF nor page FCP is substituted. `load`/decode do not prove
actual screen presentation. The existing server response numbers are reused,
not remeasured or mistaken for phone throughput.

## Compatibility and combination seams

No SDK declaration, API schema, persistent configuration, deployment slot,
identity/permission, local login storage or native transfer implementation changes.
GET `/bots/{bot}/media/{hash}` and an existing explicit delegation URL keep their
original authorization and no-fallback behavior. The new activity endpoint and
its production availability remain independent of this measurement.

The unified owner's directory/attachment contract is not yet fixed in this
patch. It can reuse the same originalBlob boundary and the post-adaptation
attachment-block/card hooks. Changes are confined to image timing imports,
watcher timestamps, object-URL binding and image events/ElementTiming attribute;
no directory semantics, root/receipt rules, paths, preview/download contract or
third protocol are invented. The owner must reapply/review these two component
seams if its shared source changes, then run the combined types and attachment
checks before the next single HAP build. Native save/RCP is not this inline path.

## Verification status

See [verification.json](verification.json) and included logs. Ten collector/real-component tests and twelve existing scoped-attachment tests pass. Seven native-controller isolation cases and three desktop filter cases pass; generated Web types/build pass. The broader upstream attachment test has two browser-anchor assertions and two missing-toast-mock unhandled errors, reproduced unchanged on the unmodified 1.0.16 baseline (six other cases pass); these are retained failures, not waived or counted green. ESLint has zero errors and eleven warnings (two capture-listener attribute-order additions and nine inherited layout warnings), with no suppression.

The actual current compiled browser application rendered five 69-byte synthetic PNG references using the original authenticated SDK/hash/Blob path. Its synthetic WebSocket fixture does not implement runtime admission/history readiness, so the existing explicit history-commit callback was used solely to expose the HTTP history in this isolation run. It does not prove the real WS/production flow. Five image GETs, no added measurement/model requests, retained login, zero page errors and all numeric stage records were verified. [Screenshot](synthetic-image-card-runtime-390.png) contains five tiny 1x1 synthetic images, not the user's images; it demonstrates the unchanged surrounding UI, not full-size decoding or throughput. No real-phone performance conclusion is drawn. All device, official ArkTS compile,
release readback and original five user-image measurements remain unperformed
for this source-only patch. The native class tests transpile the actual class
with a synthetic controller; they are not official ArkWeb/ArkTS execution.
No HAP was rebuilt, signed, uploaded or deployed in this round.

Published text logs normalize trailing whitespace only; original logs remain in the local evidence directory. Reproducible local evidence: [types](types.txt), [Web build](web-build.txt), [component/attachment run](collector-components-tests.txt), [same two baseline failures](baseline-attachment-tests.txt), [native isolation](native-readback-tests.txt), [desktop filter](desktop-filter-tests.txt), [source inventory](source-check.txt), [lint](lint.txt). The old all-in-one Node/Python subprocess-filter trial stalled and was stopped; the final Python filter tests run directly and pass, while the native test remains a separate seven-case runner.

## Request correlation follow-up

The Web collector, existing ArkTS numeric whitelist and Windows filter now carry
`responseStart`, `encodedBodySize`, `transferSize` and numeric `resourceMatch`
(1 unique, 2 ambiguous). They retain the same fixed arm/take/onDump instructions
above, five-row limit, 120-second expiry and default-off behavior. No extra fetch,
permission, TLS setting, UI, service or persistent logging is introduced.

Synthetic unit/adapter checks cover old and concurrent same-URL entries, unique
matching, zero/missing cached or masked fields and whitelist filtering. They do
not establish physical ArkWeb timing. The previous five 69-byte synthetic PNGs
are fixtures, not measurements of the user's five existing images. Actual
phone timing and the requested 5 MB/s outcome remain unverified.
