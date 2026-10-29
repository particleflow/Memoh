# Incremental speech review: cancelable task identity validation

Reviewed exact combined source `ac7eba92fee9c2824f058b1394a6c55362b787e8`, tree
`23e1e57250261333a6437a6c01b4497a3d7d1e6a`, parent
`5a42d013e801cb6229ee029113e3a63782bc3e13`. The supplied source increment is
`76471e0ef3ab9470dcf17c1d85ddb27cece14109`.

**No confirmed speech defect was found; no product patch is proposed.** This
handoff contains only new difference tests, browser observations and the source
audit. It does not authorize or perform publishing, builds, signing or deployment.

## Source scope

Library item `libfile_598d489937848191912dd0bed9de5159`, version 5, was officially
materialized. ZIP SHA-256 matches
`ee7b726306537475a0a0b38bbfd30734e5426254d5b3fcfdf1dcc316d11f3cdb`;
all 28 manifest entries were checked. The bundled Git history restores the exact
combined commit and tree. All four combined product hashes match the supplied
consumer map. This is the complete candidate, not an old pane snapshot.

The new helper bounds the caller and native fetch to 15 seconds with
Promise.race and AbortSignal. Timeout or cancellation releases its deadline and
listeners; an uncooperative late transport does not settle the old caller again.
Home aborts superseded/off-route/disposed checks while retaining its request and
route fences. The locator aborts on anchor/session change, hiding, deactivation
and disposal; a controller/generation fence prevents an old finally/rejection
from releasing or overwriting the new selection's admission slot.

`source-audit.json` records byte equality against 5a42 for the speech controller,
speech composable, Home visibility key, Dockview re-provider, message component
and animation CSS. The actual pane enable expression and Home mask/provider are
also identical. Therefore the existing admission/visibility disable watcher
still stops speech synchronously and consumes old read-aloud work. The new
unavailable/pending states never enable speech by themselves. No Bot identity,
permissions, navigation architecture, backend, SDK, persisted queue or history
was changed during this review.

## Only new difference tests executed

`tests.log` reports **six passing cases in two new files**. They run the actual
combined Home/locator/identity helper and speech hook, with synthetic SDK
transport/audio; fake timers control only setTimeout/clearTimeout in these tests.

| New case | Observed result |
| --- | --- |
| Home deadline | At 14,999 ms the request remains pending; at 15,000 ms it aborts, the mask remains, and speech stays idle. Returning contact permits fresh speech; late authorization cannot interrupt that new bubble. |
| Leave Home route | Old read aborts on settings entry. After contact return, a new reply remains playing through late 403 and crossing the canceled old deadline. |
| Pane deadline | Timeout sets unavailable, clears old playback, and late success cannot locate or restart it. Ordinary unanchored chat can read its new reply. |
| New selection while old SDK transport ignores abort | New anchor is located without settling the old Promise; its fresh reply can play. Old rejection/deadline cannot clear the new selection or its current speech. |
| Hide/show | Hidden validation aborts. Showing revalidates immediately; only the fresh reply plays, including after old success/deadline. |
| Dispose | Request aborts, all old player sources remain empty and late success produces no new synthesis/player. |

The Home and pane fixtures read the actual pane enable expression, rather than
copying the desired gate. Initial playback and one queued live reply provide the
interruption prerequisite; this is not another execution of the old general
speech suite. The prior 12 source + 6 browser matrix and the task owner's 36
cases were **not rerun**, nor was a full CI/build/typecheck/UI suite run.
Changed-fixture ESLint passed (`eslint.log`); retained fixture/runner lint passed
(`evidence-eslint.log`). No implementation source needed modification. The first commit hook used the
environment fallback pnpm 11, which attempted an automatic dependency reinstall
and stopped with ERR_PNPM_ABORTED_REMOVE_MODULES_DIR_NO_TTY before committing.
The installed dependency metadata records pnpm 10.27.0; the existing compatible
local pnpm tooling is used for the same lint hook, without purging/installing
project dependencies or bypassing the hook. The retained Vitest log only trims
its trailing blank line for the repository whitespace check.

## Two new actual Chromium observations

URL: `http://127.0.0.1:18486`, exact ac7eba candidate through a task-owned Vite
preview, real Dockview and native HTMLMediaElement, Chromium 154.0.8037.57. The
existing local WebSocket/REST and quiet 10-second PCM WAV fixtures were reused.
A trusted pointer gesture establishes supported activation. Native play() is
observed and its original Promise returned. The browser is headless/muted:
these are media-engine/state/animation checks, not physical listening or semantic
TTS acceptance. No real TTS/model call is made.

1. **Actual 15-second timeout:** measured 15,020 ms from route selection to the
   error mask, without mocked browser timers. The native identity GET records
   `net::ERR_ABORTED`. Old players stay paused with empty sources and old bubbles
   stay idle with animation `none`, both during pending and after timeout. The
   actual primary contact return does not synthesize the old queue. Only fresh
   reply 3 plays/orbits after the withheld success is released.
2. **Actual off-route cancellation:** leaving for `/settings/profile` aborts the
   new native identity GET. After returning to the real primary contact, only
   fresh reply 5 speaks/animates. Releasing the canceled old response as 403 does
   not hide contact, restore an old player or stop that new reply.

There were zero page errors and three synthetic playback requests total, for
fresh replies 2, 3 and 5 only. Both canceled native task GETs are recorded in
`browser/result.json`. Native fetch respects abort, so late fixture responses
are not evidence of an abort-ignoring browser transport; the separate SDK tests
above specifically verify that case against the real helper.

Current screenshots were captured and inspected:

- [Deadline pending](browser/deadline-pending.png).
- [Actual timeout: error mask with no speech/frame](browser/deadline-error.png).
- [Fresh primary reply after late success](browser/deadline-new-contact-reply.png).
- [Off-route cancellation starts from a stopped pending pane](browser/cancel-off-route-pending.png).
- [Fresh primary reply survives late denial](browser/cancel-new-contact-reply.png).

Computed visibility is read explicitly; screenshots alone are not the playback
proof. The browser, Vite and loopback fixture stopped after the run; port 18486
was confirmed closed. No field browser or production service was used.

## Replay and limits

The retained candidate worktree is `/workspace/memoh-readaloud-cancel-review`.
Its installed dependencies and matching UI source are reused, and its SDK source
is explicitly aliased by the retained Vite config. No installation was needed.
To replay the six new source cases, copy the two retained fixture files into:

- `apps/web/src/pages/home/index.read-aloud-cancellation.test.ts`.
- `apps/web/src/pages/home/composables/useChatReadAloud.cancellation.test.ts`.

Then run only those two paths with the existing `vitest run --project web`.
The fixture relative imports intentionally target those candidate locations;
they are not standalone tests runnable directly inside this evidence directory.
For native replay copy the retained Vite config into candidate `apps/web/`, then
run the retained `browser-cancellation-review.mjs`. Its absolute paths describe
this environment; READ_ALOUD_EVIDENCE selects a new output directory.

This review does not extend deadlines to general initialization, alter task
admission strategy or establish the original NAS loading cause. It does not
claim physical ArkWeb signal/deadline/KeepAlive behavior, trusted phone gesture
activation, device audio/accessibility or human QA. Harmony must consume the
combined four-file cancellation delta while retaining the existing speech gates
and native adaptations. No HAP, signature, production image or NAS deployment
was created or changed; field acceptance remains with the unique writer.
