# Existing replay gesture restores the same blocked audio

Source parent: `65d5d4ad128317b65f394bd27cb4b674a4f6bbae`.
Cleanup candidates reviewed: Web `9690095b4a6575af9f63915b05e713de38c55bf6`,
Harmony `afa60196940d1e21bbf514d401c763a79f90f9d0`.
This is an independent source increment, not deployment or 500 ms acceptance.

## Exact regression and minimal change

The provided cleanup Library archive was materialized without modifying it;
all ten manifest entries were verified. The commits were imported from its
source bundle, retaining the separate Web/Harmony source lines. The cleaned
Web removes the extra enable/resume/stop controls but retains these actual
expressions, recorded with the source hash in `candidate-contract.json`:

- Menu reachability: `!readAloud.canReplay(msg)`.
- Menu selection: `readAloud.replay(msg)`.
- Ordinary message click capture: `readAloud.stop()`.

The cleanup does not modify `useChatReadAloud`. Its existing replay function,
and the streaming candidate's same function, call `stop()` before synthesizing
again. That discards the already-loaded source after `NotAllowedError`. The
removed resume button had been its only UI recovery consumer.

Only `useChatReadAloud.ts` changes in product code. For the same active assistant
bubble in `blocked` or `paused`, the existing replay action calls the existing
`controller.resume()` synchronously, before stop, DOM extraction or any request.
The loaded player/URL and current queue are retained. The original menu is also
reachable for that blocked bubble while its text is still streaming. Other
messages still explicitly replay their own text. A fresh play rejection retains
the same source and never silently resynthesizes. Message clicks, input,
visibility/scope changes and errors retain their existing stop/consume behavior;
once stopped, the old run cannot be resumed by late events.

No new button, copy, gesture override, permission bypass, native policy change,
provider/model/voice setting or credential is added. `resume()` invokes native
`play()` and accepts normal browser rejection; it does not override that promise.

`cleanup-overlay.patch` contains this same guard against the exact cleaned Web
composable. Its application and resulting bytes were checked in an isolated
file tree (`evidence/cleanup-overlay-check.json`). It has no dependency on the
unadmitted streaming/DOM candidates, so the cleanup/unified owner may lift the
small guard independently. This mechanical check is not a full cleanup/HAP
runtime acceptance.

## Narrow regression evidence

Seven new controlled cases use the real composable/controller and actual shared
ContextMenu components. The only audio/SDK objects are synthetic. The menu
selection executes the exact reviewed candidate expression, not a new resume
button. Cases cover mouse and 700 ms touch menu selection, streaming-message
reachability, repeated rejection, message/input interruption, and another bubble.
The controlled player requires play to occur in the selection's same JS stack;
this tests the activation-sensitive contract without pretending to be a device.

- Pinned pre-fix source: **4 failed, 3 passed**, including repeated synthesis/new
  URL and no synchronous play from the existing menu. `evidence/before.log`
  preserves this expected negative result.
- Candidate: **7 passed**, no repeat synthesis for blocked recovery.
- A targeted real Chromium case additionally observed a native NotAllowedError
  before any activation. A trusted right-click/menu click then used the same URL
  and native player, reached native `playing`, and kept synthetic SDK calls at
  one. Native play promises and autoplay policy were unchanged. An ordinary
  message click then stopped it. Zero network/provider calls were made.
  See `evidence/native-resume.json`. This is an isolated in-memory page using
  the existing sine MP3 fixture and real composable/controller/MSE. It is not
  current app/HAP, physical touch, ArkWeb or acoustic onset evidence.
- Targeted ESLint and scoped strict Vue typecheck passed. No old 14+3/19 suites,
  old native MP3 fixture or unchanged full build was rerun.

The before runner reads the pinned parent composable from Git into a temporary
module and only resolves its relative imports. Normal runs import the current
composable directly; no duplicate production module is retained. Reproduce with
`AUTOPLAY_BEFORE=1` for the negative run and omit it for the candidate, using
`vitest.config.ts` in this directory. The targeted native script reuses the sine
fixture produced by the prior isolated probe; it does not synthesize user audio.

## Corrected current binding and WSS buffering audit

The parent relayed new read-only field evidence: the current binding is Alibaba
Cloud Speech / `alibabacloud-speech`, model `qwen-audio-3.1-tts-flash`, and an Aliyun
WSS host rather than a configured Magpie address. This supersedes earlier
provider assumptions. The Windows evidence files themselves were not opened in
this environment; this attribution is explicitly trusted parent evidence.
Historical requests did not record model identity, so current settings cannot
identify every older request. Reported 499/2.603 s and 200/8.460 s observations
are total server durations, not first-byte or audible-onset measurements.

At the provided `5a42d013` source anchor, the registry selects the pinned native
Alibaba speech SDK factory using the existing saved base URL/key. The model and
cloned voice are retained. Its `DoSynthesize` accumulates binary WSS audio until
task-finished; the original playback handler then returns complete `c.Blob`
audio. Its separate `DoStream` forwards each nonempty binary frame. The earlier
`2b9c8fc` playback candidate selects that streaming method and flushes each chunk,
removing the deterministic whole-utterance collector. This is a source finding,
not a claim that the candidate is deployed or explains every historical delay.

The inspected SDK sends MP3 and 22050 Hz when saved format/rate are absent.
Those are **requested defaults**, not a receipt of actual encoded field audio;
its MIME is statically mapped from the request configuration. The official
[Qwen WSS client contract](https://help.aliyun.com/en/model-studio/qwen-audio-tts-client-events)
supports PCM/WAV/MP3/Opus and the same default format/rate. Its
[server event contract](https://help.aliyun.com/en/model-studio/qwen-audio-tts-server-events)
delivers audio as binary frames after synthesis metadata. The native SDK's
run-task/continue-task/finish-task flow corresponds to that documented protocol;
no transport switch or new model/provider is required merely to expose its
existing stream. No WAV/PCM decoder or sample-metadata guess is included.

Detailed request/default/collection findings are in `evidence/qwen-wss-audit.json`.
No real synthesis, paid audition, cloning, configuration change, application
service startup, HAP build/sign/install or deployment was performed. The user's
Magpie-only preference versus the verified Aliyun binding remains parent-owned;
this task does not reroute or rewrite the completed voice configuration.

**Admission still pending:** independent review on the unified cleanup/native
consumers; actual ArkWeb gesture behavior; recorded WSS run-task format/rate,
first binary bytes and chunk timings for the current binding; and user-trigger
to acoustic/loopback first output at or below 500 ms. For this WSS path, do not
substitute the statically generated HTTP MIME or server total duration for an
encoded-frame receipt. Native `playing` and decoded data are not audible-onset
acceptance. The 500 ms requirement remains NOT PASSED.
