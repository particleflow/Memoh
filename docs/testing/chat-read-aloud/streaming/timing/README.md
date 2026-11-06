# Read-aloud timing: measured software events and unmeasured sound

The earlier “500ms not passed” mixed missing measurement with a performance failure. The real phone's request → acoustic speaker onset is **UNMEASURED**; there is no valid numeric excess or margin. Web software timings are now measured, with a fast path and deliberate slow controls. They must not be substituted for acoustic output.

## Exact current product audit

Unified source `b5e7aff6040d1efce36ee51a0c5d3f8915dbddf3` was fetched read-only. Its controller, composable, audio-source helper, playback handler and service match this branch's `eae24523bfe711d631758316073c24e3bd50335b` inputs byte-for-byte. Thus the observed branch is the actually admitted source, not an older MSE-only candidate. It is not evidence that a particular field Server/Web/HAP has been deployed.

- MSE-supported response MIME: incrementally append each chunk, await its updateend, continue before EOF.
- MSE unsupported (or unavailable): preserve native compatibility by collecting the same response to Blob, then play. **This branch waits for EOF. A long response in this branch is not audio streaming.** It does not request another synthesis or change the voice/provider.
- Last parent-reported field binding: Alibaba WSS with Qwen `qwen-audio-3.1-tts-flash`, no explicit format/sample-rate setting. The pinned SDK defaults to MP3/22050Hz and serializes those values; this is not proof of the bytes of an uncaptured production response. The new safe prefix hint can distinguish an observed RIFF/WAVE prefix from a declared MP3 MIME, but a header hint alone is not a fully validated codec.

## Existing evidence extracted without rerunning

The earlier [native-result.json](../evidence/native-result.json) uses a browser clock starting in the test button's run method, before its measurement AudioContext setup/resume. It has no real API/provider delay:

| Legacy fixture event, ms from that test trigger | Manual | Automatic |
|---|---:|---:|
| Synthesis call | 40.8 | 14.8 |
| First fixture bytes | 81.0 | 55.0 |
| First native playing | 82.4 | 55.6 |
| First nonzero decoded graph | 110.2 | 80.1 |
| First append | not recorded | not recorded |
| EOF | not reached/recorded: intentionally interrupted | not reached/recorded |
| Acoustic onset | not measured | not measured |

Relative to its synthetic synthesis call, the graph delays were 69.4/65.3ms. The larger trigger delays include the fixture's AudioContext setup; they are not production initialization measurements. Historical acceptance fields named “Magpie” in that old file do not identify an actual provider: it made zero provider calls.

[f523](../wss-seam/README.md) measured ordering/bytes/states: 8403 bytes and decoded graph activity before withheld EOF, matching full-file hash, cancellation and no late playback. It did **not** capture millisecond times. Its exact MP3-adapter evidence is reused, not relabeled as a fresh timed adapter run. The parent-relayed historical field 200 lasted 8460ms total; its TTFB, append, decoder, playing and acoustic onset were not logged. Total request duration is not an onset measurement; a 7960ms acoustic excess cannot be inferred from it.

## New objective Web measurement

See the generated [current measurement table](evidence/measurement-table.md), [complete event records](evidence/native-timing.json), [server observations](evidence/server-events.jsonl), and [machine-readable missing/measured status](evidence/measurement-summary.json). Run `python3 docs/testing/chat-read-aloud/streaming/timing/summarize.py` to regenerate tables offline.

The targeted Go test runs the actual HTTP playback handler, generated browser SDK, Vue composable/controller, and native Audio/MSE or Blob, with the existing MP3 and exact reviewed WAV. Only the upstream channel, identity/settings and text arrival are controlled. It deliberately supplies first chunks after 70ms (fast/automatic MP3), 620ms (slow MP3), or 40ms (WAV), and EOF at 450/900/780ms respectively. These are **local controlled delays, not measured cloud-model performance**. No application/database service or cloud request is started; loopback fixture listeners end with the test.

Final executed run, all browser columns relative to its logical synthesis request start:

| Profile | First bytes | Append done | playing | Nonzero graph | EOF | Graph versus 500ms |
|---|---:|---:|---:|---:|---:|---:|
| fast MP3 | 78.7ms | 79.3ms | 80.0ms | 98.6ms | 456.8ms | under by 401.4ms |
| slow MP3 | 626.7ms | 626.8ms | 628.3ms | 644.0ms | 905.0ms | over by 144.0ms |
| WAV Blob | 43.6ms | n/a | 788.7ms | 811.4ms | 783.6ms | over by 311.4ms |
| automatic MP3 | 73.8ms | 74.0ms | 74.4ms | 80.6ms | 453.4ms | under by 419.4ms |

These are **software graph comparisons**, not acoustic pass/fail. The slow MP3's server first chunk is 621.3ms on the server's own clock: the injected upstream wait dominates. WAV waits another 740.0ms between first bytes and EOF; that identifies the compatibility collector as its delay. For automatic mode, operation observation → first nonempty text block is 49.6ms, first emitted text chunk/request 81.1ms, native playing 155.5ms, graph 161.7ms. Text appearance → chunk wait is 31.5ms in this scenario. The actual test click preceded operation observation by 1.2ms. The manually selected fast operation's root → graph is 110.1ms, with click → root 0.2ms, so button → graph is approximately 110.3ms.

A prior iteration is retained as `measurement-before-signature.json`/`first-measurement.log`. It measured fast graph 98.6ms, slow 644.7ms, WAV 795.7ms, automatic 81.2ms relative to requests. That is a separate measurement before final prefix instrumentation, not an additional sample of the exact final source. The roughly one-frame WAV graph variation illustrates why a polled graph timestamp is not speaker timing. There is no p95, load distribution, real-model latency, physical speaker result, or 500ms guarantee in these single scenarios.

## Minimal production observation wiring

Off by default. Setting the existing page's `sessionStorage['memoh.speechTiming']='1'` enables bounded page-memory metadata for subsequent **normal** reads. No controls, recording permission, retry, codec/voice setting or persistent content log is added.

- A manual operation begins in the original replay handler before visible-text extraction. Automatic timing begins when the composable first observes an eligible live reply; this does **not** include the earlier send-message-to-first-reply latency owned by text streaming. First nonempty source text and first emitted speech chunk are separate events. Every request gets a child ID while sharing the operation clock, including queued chunks.
- The HTTP request uses the already permitted `X-Request-ID: speech-timing-<UUID>` only when armed. Server opt-in accepts only canonical UUIDs; it logs `handler_start`, `adapter_request`, `adapter_first_audio`, `http_first_flush`, and `upstream_eof` or cancellation with that anonymous ID and server-local elapsed milliseconds. No raw request ID, body, target, credentials or URL is added to these observations.
- `adapter_first_audio` is **the first nonempty SDK-channel chunk observed by the handler**. It includes resolution/connect/generation/channel delivery up to that application boundary; it is not a WSS wire/read callback timestamp or a pure model-generation number. Existing SDK queueing remains inseparable. The response emits `Server-Timing: speech_first_audio;dur=...` and exposes it only on diagnostic playback responses. Global CORS permissions are unchanged.
- Browser events: request invocation, response headers, source branch/declared MIME class, first nonempty reader bytes, bounded header-signature enum, first append call/updateend, native play call/playing/end, body EOF, Blob ready, stop/error/blocked. First bytes may contain container metadata; append completion is decoder acceptance, not proof of audible or even non-silent samples.
- Export contains at most 32 anonymous records. The collector never stores text, URLs, message/Bot IDs, raw error strings, raw MIME parameters, tokens or audio bytes. Header inspection keeps at most 12 bytes only inside the active observer and exports an enum. Cryptographic UUID fallback supports engines with getRandomValues but without randomUUID. No insecure random ID source is used.

Browser events share `performance.now()` and `startMS`; server elapsed values share `time.Since(start)`. Match IDs, **never subtract server and browser epochs**. Comparing client total duration with a server-local component only gives an approximate residual including middleware, transport, browser delivery and diagnostic overhead; it does not isolate pure network or model latency.

## Web and original ArkWeb device: passive sampling steps

The fixed [device-sampling.mjs](device-sampling.mjs) literals have been executed and checked in Chromium. They only arm/take/clear metadata; none sends or plays audio.

Web console, on the existing authenticated public origin, with this source actually loaded:

```js
sessionStorage.setItem('memoh.speechTiming', '1'); // no reload or playback
// Wait for the user's next ordinary automatic read or original replay gesture.
JSON.stringify(window.__memohSpeechTiming?.snapshot() ?? []);
// Keep this metadata and matching server speech_timing rows for the same IDs.
sessionStorage.removeItem('memoh.speechTiming');
window.__memohSpeechTiming?.clear();
```

For the original HAP/ArkWeb, its owner can execute the same **fixed literals** via the already-existing trusted-document `WebviewController.runJavaScript`: `arm`, then `take` after the next normal read, then `clear`. Preserve packaged-document/origin and diagnostic-access checks, and whitelist numeric/enum/UUID fields before any native log write. No arbitrary-JS bridge or new UI is needed. The inspected older HAP reference `afa6019` has a fixed image-only dump adapter; it is not an existing speech export. The current HAP owner's diagnostic wiring must be checked or connected to the existing diagnostic dispatcher before claiming automatic speech export on a real phone. This agent has not modified/built/signed/installed HAP or enabled a remote inspector. The file is an exact caller recipe, not a claim that a deployed phone already exports it.

Use the phone's **ordinary public NAS route**; neither LAN membership nor USB is required. The sole writer first records actual Server/Web/HAP revision/digest and verifies these observations are present. If the existing secure diagnostic route cannot run the three literals, report that exact missing route; do not silently substitute desktop Chromium. Do not add a debug listener or weaken Web security.

Use current authorization for source diagnostics/local controlled audio only. Passive observation of the user's already-requested normal read creates no additional synthesis charge. This does not authorize an extra cloud audition, synthetic replay, voice change, clone or codec edit. If no normal operation occurs, leave the real-model sample pending rather than manufacture a paid sample. The sole writer collects matching server observation rows from that one operation. This agent does not occupy or change the live deployment.

## How to verify acoustic onset and its error

The selected executor has no `/dev/snd`, `arecord`, `pactl` or `pw-record`. The native experiment samples an AnalyserNode through requestAnimationFrame, not a microphone/system output capture. It logs first nonzero graph and preceding zero sample; in the final run those polling brackets are about 16–17ms. Its maximum observed rAF gap includes a cold 56.6ms gap. AudioContext reported base/output latency 10/32ms; these are software estimates and must not be added to the graph timestamp and called measured speaker onset.

For an actual acoustic result, keep the original device's speaker route/volume and record its **physical speaker with a microphone** while the user performs the same normal operation. A screen recorder's internal-audio track or a loopback proves delivered samples, not physical speaker sound. Ensure recording contains audio; high-frame-rate modes can omit it. Keep speech recordings on the device/private measurement workstation; export timings and uncertainty, not message audio/content.

A minimal no-USB method is an independent camera+microphone recording the phone, visible original replay selection/touch release, and speaker in one A/V file (e.g. 60fps with 48kHz audio). Calibrate that capture's A/V offset with a visible/audible physical clap before the operation; locate request gesture and first speech waveform, reject incidental noise, and check the audible onset by listening. The result is user-gesture → acoustic onset. It is **not automatically an exact HTTP-call-start measurement**: include the gesture-to-handler alignment uncertainty. The software trace supplies the observed operation → HTTP-call delay; do not align independent clocks by their wall times.

Report an interval, not a guessed exact value. Include at least video-frame quantization (60fps is 16.7ms per frame), PCM/onset-window resolution (e.g. a 5ms RMS window adds up to 5ms), measured residual A/V synchronization error, and any unresolved gesture-to-operation alignment. These example components alone are 21.7ms plus the measured A/V/alignment bounds; no fixed total error is asserted. A microphone recorded in the app itself additionally needs input-buffer/delivery-delay calibration and a check that enabling capture did not alter speaker routing/AEC. Without that calibration, a late mic callback does not prove late speaker output.

Call ≤500ms acoustically passed only if the identified speech onset's **upper** uncertainty bound is ≤500ms. Call it exceeded only if the **lower** bound is >500ms. Otherwise it is indeterminate near the boundary. With no verified audio track, valid trigger reference or bounded alignment, report acoustic onset UNMEASURED/INVALID, never replace it with playing/append/graph.

## Provider option and next decision, without a paid probe

Official Qwen WSS [client events](https://help.aliyun.com/en/model-studio/qwen-audio-tts-client-events) support MP3 (default), WAV, PCM and Opus, separate format/voice parameters, and segmented continue-task input. [Server events](https://help.aliyun.com/en/model-studio/qwen-audio-tts-server-events) describe sentence-synthesis metadata followed by binary audio frames. Thus one WSS task can produce incremental MP3 with the existing cloned voice; no re-clone or fallback synthesis request is needed to use that protocol. These documents and SDK serialization do not prove actual deployed response bytes or ArkWeb MSE capability.

Next normal-operation metadata chooses the next repair:

1. First adapter chunk already >500ms: the unresolved budget is primarily the upstream acquisition/generation boundary, not a decoder-only fix. Separate wire/model latency needs adapter wire events or existing provider logs; the new channel observation alone cannot isolate it.
2. Bytes arrive quickly but branch is Blob: EOF collection is the known delay. Check observed prefix/MIME and the current explicit format setting. If MP3 is already the default, do not “fix” it by blindly writing MP3 again. If a streamable format and device decoder are confirmed, the sole writer can propose a narrowly justified format adjustment retaining the same model/voice; no configuration change is made here.
3. MSE bytes/append are quick but playing is late: investigate actual device decoding/buffering/autoplay from the recorded events. A blocked event needs the supported original replay gesture, not a new synthesis.
4. First text → chunk is late: inspect the existing flush/render schedule using the two separate marks; the automatic operation clock starts at reply observation, not the original chat request.

## Validation and handoff

Only this new measurement ran: one timed local browser test with four deliberate profiles and one server opt-in/privacy test, both `-race`; six focused timing-metadata tests; changed-file ESLint; scoped production/new-test TypeScript; syntax/patch checks. Existing 14/19/7/9 suites and f523 were not repeated. No full application/human/HAP/ArkWeb/production acoustic QA is claimed.

```sh
source /workspace/.memoh-cloud/env.sh
MEMOH_SPEECH_TIMING_BROWSER=1 \
MEMOH_SPEECH_TIMING_OUT="$PWD/docs/testing/chat-read-aloud/streaming/timing/evidence" \
go test ./internal/handlers -run '^TestPlaybackTiming(ControlledBrowser|OptInUsesOnlyOpaqueIDAndMonotonicDurations)$' -race -count=1 -v
NODE_OPTIONS=--no-experimental-webstorage node /workspace/Memoh/node_modules/vitest/vitest.mjs \
  run --config docs/testing/chat-read-aloud/streaming/timing/vitest.config.ts
```

The [isolated six-file source patch](production.patch.gz) applies to the verified unified source b5e7aff6. It is gzip-compressed to retain normal Go diff context without treating embedded context indentation as repository-file whitespace. Apply with `gzip -dc production.patch.gz | git apply --check`, then `gzip -dc production.patch.gz | git apply`. Decompressed and compressed hashes and exact source equality are recorded in source-contract.json. It is for unified independent review. It adds diagnostics only; it does not claim to fix a real cloud/phone 500ms violation that has not yet been measured. Source work and controlled Web measurement are completed now; normal-operation field observations remain a separate concrete next step, not a reason to defer this delivery.
