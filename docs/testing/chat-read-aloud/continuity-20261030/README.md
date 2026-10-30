# Automatic triggering and continuous playback correction

Source fix: [`e57d218b34ccdd8bf1e55b790de62f7d73de6eb5`](https://github.com/particleflow/Memoh/commit/e57d218b34ccdd8bf1e55b790de62f7d73de6eb5), direct parent `cefdf0cbb3207d2a07db17ef365e1b8d2d43ca3f`, product tree `9059475f4ad453f176dd073e97c6ddeacc4cd9b9`. Independent branch `fix/chat-continuous-read-aloud-20261030`. Cherry-pick the source fix; evidence is separate. No changes to the original read-aloud branch, Bot navigation/auth policy, Agent core, database, providers or service topology.

**Both reported user failures remain unresolved pending field verification.** These are reproduced client defects and local corrections, not proof of the cause on the user's affected device or final listening acceptance. No paid TTS/model call, build, signing, NAS deployment or phone installation occurred.

Read [the primary-source design/alternatives](DESIGN.md), [machine verification](verification.json), [Web/HAP36 source hashes](shared-coverage.json) and [unique-writer field steps](FIELD_STEPS.md).

## Actual delta evidence

| Observation | Before | Corrected |
|---|---:|---:|
| Fast live reply settles before post-render watcher | 0 automatic requests | 1; baseline history and interrupted tail silent |
| Native MP3 first nonzero graph sample | 307.6ms | 306.8ms |
| Native MP3 boundary graph gap | 283.3ms | None detected at 50ms threshold |
| Native player play calls / sources for two responses | 2 / 2 | 1 / 1 |
| Second request starts relative to first | 1473.8ms | 652.6ms, during first playback |
| Normal controlled stream underruns after first playing | 0 | 0 |
| Deliberately slower next response | — | 1 underrun, 1416.4ms detected gap; loading then playing |

The fixture sends first headers at 250ms and the remaining body at another 400ms. The intentional-underrun profile delays the second response headers to 2200ms. Each independently encoded synthetic MP3 is 1.2s. The browser is Chromium `154.0.8037.57`, real native Audio/MSE/generated SDK/composable/controller; the endpoint and text are local synthetic fixtures, not a provider. Measurements are software graph/native-event observations, **not acoustic speaker or TTS quality measurements**. The final normal RAF sample gap maximum was 46.5ms; smaller codec-padding gaps can be missed. Chromium was headless and has `--mute-audio`. No unconditional latency/seamlessness claim is made.

[Before timeline](timeline-red/timeline.json), [final timeline](timeline-final/timeline.json), [slow producer timeline](underrun/timeline.json). The earlier `timeline-green` is retained; use `timeline-final` for the final native conclusions.

Five new unit cases pass: fast automatic finalization, sentence continuity, one-response lookahead/native-clock backpressure, stop/late event/new reply, and prefetch error clearing. [Initial two red tests](unit-red.log.txt), [final five green tests](unit-green.log.txt). The relevant existing controller/stream/WAV/timing selection first passed 30 cases and failed one assertion for the intentionally removed 80-rune opening cap. That test now verifies the unchanged server 500-rune cap and passes with cancellation/late-response checks: [initial log](regression-initial.log.txt), [corrected selected case](api-bound-green.log.txt). No unchanged old 12+6, original autoplay five, authorization matrix, full Go suite or builds were repeated.

[Real new-path policy evidence](timeline-final/autoplay.json): fresh document with no user activation receives native `NotAllowedError`; the trusted replay click reuses the exact source; message click stops it. No mocked `play()` rejection, permission grant or allow-autoplay launch flag is used. **Policy-only observation uses raw CDP `userGesture:false` and no WebAudio interception.** Playwright polling can itself grant user activation, and a WebAudio-routed media element changes what is being observed; those preliminary harness attempts were not accepted as refusal evidence. Graph sampling remains confined to the separate gesture-activated timing profile.

[Prefetch-in-flight input cancellation](timeline-final/cancel.json) waits until the second HTTP synthesis has actually started before native typing. Old queued work never restarts after its response could arrive. The slow profile also passes cancellation. [Actual chat UI evidence](actual-ui-final/result.json) uses the current Vite app, mocked configuration with an explicit configured TTS ID, real runtime WebSocket frames, actual visible Markdown roots, native MP3 decoding, trusted typing/message click and computed bubble animation. New primary-chat replies auto-read; click/input changes the bubble to idle with animation `none`; a late same-run frame and cleared input do not create another request. No manual replay is used to pass this automatic check.

Screenshots inspected from `http://127.0.0.1:18595`:

- [Automatic reading](actual-ui-final/automatic-normal.png): only the current text bubble has the orbit/breathing edge.
- [Message click stops](actual-ui-final/click-stopped.png): media paused and edge removed.
- [Typing stops](actual-ui-final/input-stopped.png): draft `x`, media paused, no edge.

The first actual-app fixture attempt failed to load because the fresh matching UI worktree had no installed `@vue/tsconfig` link; no UI verification was claimed for it. Linking the existing matching UI dependencies recovered startup. The recovered old-source probe observed the forced partial text `Home`, not the intended full sentence; its failed assertion is retained in [the raw output](automatic-partial-text-red.log.txt). This is separate from the fast-finalization red unit case. No unknown field failure is relabeled as a fixture failure.

[ESLint](eslint.log.txt), [UI contract](ui-contract.log.txt) and [normal installed commit checks](source-commit-checks.log.txt) pass. The [targeted Vue/TypeScript check](targeted-typecheck.log.txt) exits 0 using the actual current generated SDK aliases and the five affected files. The [full app check](full-typecheck-failed.log.txt) exits 2: installed SDK/media-policy context mismatches plus initially three errors in the new fixture. Those fixture errors were corrected; targeted check then passes. A full app green result is **not** claimed. The initial source commit had no generated Husky entry in this fresh worktree; normal Husky setup was installed, the unpublished own commit was recreated with the normal staged lint hook, and only the checked commit above is the delivered source.

## Reproduction

Use the existing installed matching dependencies, current generated SDK aliases and matching UI gitlink `f69402a55dd87a59c10722fe11507ad8fd7048f5`. This task installed no product dependency. The saved configs and browser-tool imports reflect this executor's installed tooling; adjust only executor paths on a different machine. `automatic-browser.mjs` generates its owned temporary config from `vite-review.config.ts.txt` and refuses to overwrite an existing file; remove only your own previous generated config before running it. It starts/stops only the isolated Vite/loopback fixture it owns. No field browser is used.

```sh
node node_modules/vitest/vitest.mjs run apps/web/src/pages/home/composables/speech-continuity.test.ts --project web
SPEECH_EVIDENCE=/tmp/speech-native node docs/testing/chat-read-aloud/continuity-20261030/timeline-browser.mjs
SPEECH_UNDERRUN=1 SPEECH_EVIDENCE=/tmp/speech-underrun node docs/testing/chat-read-aloud/continuity-20261030/timeline-browser.mjs
READ_ALOUD_ORIGIN=http://127.0.0.1:18595 READ_ALOUD_EVIDENCE=/tmp/speech-ui node docs/testing/chat-read-aloud/continuity-20261030/automatic-browser.mjs
cp docs/testing/chat-read-aloud/continuity-20261030/tsconfig-review.json.txt apps/web/tsconfig.speech-review.json
node apps/web/node_modules/vue-tsc/bin/vue-tsc.js --noEmit -p apps/web/tsconfig.speech-review.json
```

The checked-in synthetic audio SHA256 is `bbe5e9557cbef19c19ecb11b96d78e30110a5bc9703d50c8df7a2d6cc0fc2a70`. It was generated locally, without a voice/model, with:

```sh
ffmpeg -hide_banner -loglevel error -f lavfi -i 'sine=frequency=440:sample_rate=44100:duration=1.2' -c:a libmp3lame -b:a 64k fixture.mp3
```

The HAP36 pinned source bytes of all three modified modules match the Web parent exactly; the other three mapped rendering/timing files are unchanged. The current HAP does not acquire this correction until its owner updates the existing shared snapshot/lock, verifies the generated context and follows the authorized delivery flow. ArkWeb MIME support, supported activation, interruption/old queue, ring/reduced-motion/a11y and native foreground/background behavior remain device conditions. Neither HAP distribution nor Chromium tests prove installation or ArkWeb acceptance.
