# Streaming read-aloud candidate — acceptance incomplete

Base: `5a42d013e801cb6229ee029113e3a63782bc3e13` in `particleflow/Memoh`.
This is a source candidate for unified owner `01a15840-f35b-757a-a2a0-e20479062007`.
It is not a deployment, signed HAP, or evidence that the physical device meets
500 ms. No real provider, voice cloning, paid audition, model import, credential
change, or application service startup was performed.

## Confirmed causes and changes

- `LiveSpeechReplies.update` previously waited for `message.streaming` to become
  false. It now submits new visible text while the reply is streaming. Sentence
  boundaries start immediately; a short unpunctuated opening flushes after 80 ms.
  The timer is not restarted by every token. Already-spoken prefixes are tracked
  per run; history, interrupted runs and failed utterances cannot automatically
  restart. Baseline history is not Markdown-parsed on every streamed delta.
- `useChatReadAloud` previously requested `parseAs: 'blob'`. It now obtains the
  authenticated SDK response body with `parseAs: 'stream'`. Both manual replay
  and automatic replies use this same transport and native streaming decoder.
- `BotAudioHandler.Playback` previously called `Service.Synthesize` and returned
  a complete `Blob` response. `Service.StreamPlayback` now exposes the existing
  Twilight provider stream without calling `Bytes`. The handler writes and
  flushes each nonempty chunk, requests no reverse-proxy buffering, and cancels
  the upstream on cancellation or write failure. Upstream failure after the first
  bytes aborts HTTP with `http.ErrAbortHandler`, so the client cannot accept a
  clean EOF for a truncated utterance. Existing server recovery explicitly
  rethrows that sentinel. Authorization and the 500-rune request limit remain.
- Native `MediaSource` appends chunks sequentially as they arrive; no full-body
  `Blob`, `arrayBuffer`, `decodeAudioData`, or fixed prebuffer is used on the
  production path. Manual requests bound their opening to 80 Unicode points;
  subsequent requests retain the existing 500-point limit, avoiding an arbitrary
  multiplication of requests for the rest of a long replay.
- Read-aloud uses the chat renderer's `parseMarkdownToStructure` and the same
  single-dollar policy. Link labels are spoken; hidden href/title/reference
  destinations and Markdown delimiters are not. Visible naked URLs, headings,
  ordinary code, table cells, currency and literal brackets are retained. Partial
  link labels wait until the parser can distinguish them from source markup.
  HTML text and MathML are projected without hidden script/style/annotations.

No changes were made to `chat-pane.vue`, `message-item.vue`, Home masks, text
streaming, Bot history, attachments, UI cleanup, SDK generation, Agent core,
permissions, voice configuration, provider selection, or HAP sources. The old
`StreamToFile` attachment path is deliberately unchanged.

## Targeted verification

- 14 new frontend cases passed across `chat-read-aloud.streaming.test.ts` and
  `useChatReadAloud.streaming.test.ts`. These cover live deltas, partial links,
  visible content retention, actual composable SDK streaming, the 80 ms flush,
  manual replay, cancellation, hiding, unsupported codecs and no automatic retry.
- Two new handler tests passed with `-race`: first bytes flush before provider
  EOF, cancellation releases the handler, and a partial provider failure aborts
  committed HTTP. They use Echo contexts and response recorders, with no listener.
- One new audio-service test passed with `-race`. The real installed
  OpenAI-compatible adapter runs over a synthetic `RoundTripper`/`io.Pipe`,
  forwards a short first read without waiting for EOF or a full 32 KiB buffer,
  and serializes the saved model, cloned voice, output format and speed unchanged.
- Affected playback handler tests also passed after adapting their fake service
  to the streaming interface. No old Home/cancellation browser suite was rerun.
  Prior gate evidence remains at `c96c0ef29454ee7b2412f1c69ae79915c3644c07`;
  it is not represented as verification of this new streaming implementation.
- Targeted ESLint and strict `vue-tsc` passed for the four production TypeScript
  modules, resolving this candidate's SDK and source aliases. The isolated type
  config is included for reproducing the scope. This is not a full-app typecheck.
- `native-fixture.mjs` runs real Chromium over an isolated in-memory page and
  locally generated sine-wave MP3. Real player/MediaSource/AudioContext are used,
  without changing native play promises or overriding autoplay policy. Manual
  and automatic source paths both delivered nonzero decoder/output-graph samples
  before EOF. Exact final observations are in `evidence/native-result.json`.
  These are **not physical audible-onset timings**, provider timings, or an
  application UI pass. The screenshot shows only the isolated probe.

## Remaining blockers and minimum evidence

1. **500 ms audible onset: NOT PASSED / unverified.** No existing field recording
   or Magpie byte-arrival trace is present in this environment. SDK streaming
   support proves neither real Magpie incremental production nor its latency.
   Minimum evidence is a current, authorized field trace using the existing
   provider/model/voice/format, with user-gesture/auto-trigger time, upstream first
   bytes, first playable codec frames, native output timing, and acoustic or
   loopback detection of the first nonzero output. HTTP success, `playing`, and
   analyser activity alone do not meet the requested audible criterion. Prefer
   existing captures; any new paid real call remains outside this task's scope.
2. **Device/codec compatibility: pending.** The current candidate requires native
   `MediaSource.isTypeSupported` for the saved audio format. MP3 is locally proven.
   WAV/raw PCM or a device without the relevant MSE decoder fail explicitly and
   cancel the body; there is no hidden whole-Blob fallback and no voice/format
   override. Verify the existing Magpie response format and actual ArkWeb support
   before integrating. A non-MSE current format needs a separate incremental
   decoder implementation; do not silently change the saved voice configuration.
3. **Real current app/HAP UI: pending, not performed.** User instructions prohibit
   opening services or deploying/signing. No running current application was
   available, and HAP `12faf4c06db01d8fffc29aad3a247b4a85e884e7` was not built or
   touched. The required real-flow screenshots and physical autoplay/interruption
   behavior remain absent. No ready-for-review or human-QA claim is made.
4. **Custom renderer fidelity: pending.** Ordinary Markdown semantics are covered.
   The projector is not a read of the final rendered DOM. Mermaid/custom plugin
   presentations and CSS-hidden plugin output have not been validated against
   their actual displayed labels; do not claim exhaustive visible-body fidelity.

The integration owner should apply the isolated source candidate on the unified
head and keep these acceptance items open. Do not deploy based on the synthetic
latency numbers.
