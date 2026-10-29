# Preserve native WAV playback alongside streaming

This fixes the confirmed v12 compatibility regression: native Audio could play the reviewed WAV, but the MSE-only candidate canceled it because `MediaSource.isTypeSupported('audio/wav')` was false. The same 44,144-byte WAV is used unchanged here; no synthesized voice, provider request or format override was introduced.

Production changes are limited to two files, with 34 added and 2 removed lines:

- `prepareSpeechSource` selects the existing MSE implementation when the browser supports the response MIME. Otherwise it consumes the **same response** into a Blob and uses the existing native Audio playback path. There is no second request, provider/voice change, codec whitelist, or new PCM support claim. It preserves the response MIME and bytes; native Audio remains responsible for decoding.
- The controller awaits source preparation and discards/revokes any source completed after an interruption. Abort and first read/allocation/decode errors clear the old queue. There is no automatic synthesis retry. Unsupported MSE no longer implies unsupported native playback.

The lower-level `openSpeechStream` remains an MSE-only primitive. Production playback now goes through `prepareSpeechSource`; calling the low-level primitive directly still tests only MSE capability, not the product's compatibility selection.

## Exact reviewed counterexample: red → green

Input Library `libfile_598d489937848191912dd0bed9de5159`, version 12, ZIP SHA256 `4baa516ab7934e8ca0ec292521eefc7dfc60e8e5c428087466fab6e937b785da`. The original source report is `streaming-wav-regression/evidence.json` in that ZIP. The local materialization was verified against this hash before extraction.

Fixture SHA256: `0d280d2e0596a5f0159ec44a1ae6bc20c12da09aa77bc8f228c4b7877bf7945e`; mono 22050Hz, 16-bit, 1 second. `fixture.wav` is byte-identical to the reviewed fixture. The native reproduction bundles the actual checkout's `ChatReadAloud`, using one controlled streaming response that withholds EOF after 4096 bytes. It keeps the native play promise unchanged and uses no autoplay-policy override.

- **Before**, executed on `f523c815a30630ed67723674b93be04d0dddf16c`: one response, MSE WAV unsupported, native WAV `canPlayType: maybe`; controller enters `error`, cancels the stream, never plays. See [before.json](evidence/before.json).
- **After**: while EOF is withheld, no Blob URL or playback is created; after the same response completes, exactly one `audio/wav` Blob URL is created and native playback progresses. One response, no cancellation, no resynthesis. See [after.json](evidence/after.json).

The fallback waits for this response's EOF, as the prior Blob path did. That compatibility behavior is not advertised as streaming or as meeting 500ms. Supported MSE encodings retain their streaming route.

## Focused validation

Nine new tests in `speech-wav-compat.test.ts` pass: WAV bytes/type and EOF boundary, pending-read stop/queue discard, already-aborted input, abort with an upstream cancel promise that never settles, first body error, URL allocation error, native decode error, stop between source completion and controller continuation, and supported-MSE selection still forwarding bytes before EOF. Targeted ESLint, scoped production TypeScript and JavaScript syntax checks also pass. [Verification and source hashes](evidence/verification.json) and [test output](evidence/new-tests.log) are included.

The old 14/19/7 suites and f523's controlled MP3 full-chain test were **not rerun**. The existing MSE implementation from `function mediaEvent` through `openSpeechStream` is byte-identical to f523, and backend service/handler are unchanged. The prior [f523 MP3 evidence](../wss-seam/README.md) is reused with its original execution/version boundary; the new selector is covered by the one focused MSE routing test, not presented as a fresh MP3 full-chain execution.

Selected executor commands, with existing dependencies:

```sh
source /workspace/.memoh-cloud/env.sh
NODE_OPTIONS=--no-experimental-webstorage node /workspace/Memoh/node_modules/vitest/vitest.mjs \
  run --config docs/testing/chat-read-aloud/streaming/wav-compat/vitest.config.ts
WAV_COMPAT_OUT="$PWD/docs/testing/chat-read-aloud/streaming/wav-compat/evidence/after.json" \
  node docs/testing/chat-read-aloud/streaming/wav-compat/native-wav.mjs
```

## Independent patch and admission

[production.patch](production.patch) contains only the two production-file changes. It applies cleanly to `2b9c8fc24ff55bbaaa88cc112438006d82fe0513`, and the resulting controller class and helper match this executed candidate. It does not require the rendered-body or replay-gesture changes to apply, and does not overwrite the composable or UI. The full commit's parent is f523; the patch can be reviewed independently when composing the streaming candidate. It requires the streaming APIs introduced in 2b9 and cannot be applied as a replacement for the admitted legacy Blob composition alone.

This completes the requested **source compatibility candidate**, subject to unified independent review. Zero real cloud calls, no provider/voice/configuration changes, no new product controls, no deployment or HAP build/sign/install. Full application UI and human QA were not performed under the task's no-application-service restriction. Phone codec/ArkWeb and actual acoustic onset ≤500ms remain post-deployment field validation, not prerequisites for producing this patch. Current real response encoding remains uncaptured; MP3/WAV fixture results do not identify it. The SDK idle WSS cancellation/billing limitation documented in f523 is unchanged.
