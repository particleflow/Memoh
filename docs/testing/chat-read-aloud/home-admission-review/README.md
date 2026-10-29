# Home URL admission and read-aloud availability

Source fix: `19015a545a38f3fd6ca2c3b17d71280556101756`, directly parented by
complete unified `28ac9393c7e20db34b804ad3ece63680eecded8c`, base tree
`886fa9f0d693e0952f27619a3ea17b678a120d90`. Branch:
`fix/chat-read-aloud-home-admission-20261028`. Cherry-pick this one commit onto
the unified line, or apply `home-admission-read-aloud.patch` after checking it.
No previous speech fix needs to be reapplied. The original speech branch
retains this evidence separately; its evidence commit does not change product
behavior by itself.

## Full baseline review and the missed outer boundary

The supplied Library item was materialized at version 1. ZIP SHA-256 is exactly
`cd0102b8443dc6e0bfcdd9aa81ea8ae1a252c3c10e9dc6dc68634e2252978e59`;
all 14 manifest members passed. Its bundle restores the complete 28ac commit
and exact tree. The ten mapped Web products and SDK type match the supplied
hashes (`source-map.json`): three locales, task directory, pane, panel wrapper,
anchor locator, Home, workspace store and task-link utility. The related
workspace component, panel visibility composable, speech composable/controller,
bootstrap and persistent main-container lifecycle were also inspected.

The task directory and URL admission validate before opening a new anchor.
Home sets `historyLoading` and makes the old workspace `invisible`, retaining
Dockview dimensions. During that await the old Bot/session/pane and old located
anchor remain unchanged. Dockview still reports visible/focused, so the already
admitted b43875 inner-anchor gate cannot stop this earlier playback. Rejection,
expired records and mismatched run identity retain the Home error mask without
delivering a new anchor at all. The persistent Home instance also ignores stale
results using its existing route/request guards.

## Minimal product change

Home computes `workspaceVisible` from the **same existing three mask conditions**
(history error, history loading and contact-navigation pending), and uses it for
both the existing mask and a read-only injected ref. ChatWorkspace re-provides
that ref: Dockview manually mounts panes with a copied `provides` object and
ordinary inherited injection alone does not cross that boundary. The speech
composable combines this ref with the existing pane gate for stopping,
automatic enqueue, replay and resume. Its existing synchronous disable watcher
aborts synthesis, releases native media, clears queued chunks and consumes
interrupted replies. Returning to availability never restarts them.

Four product files change, including the new three-line injection-key module;
one focused Home test file is added. The inner anchor gate, route admission,
Bot identity, authorization, history/queue persistence, controller, styles,
locales and SDK are unchanged. Standalone consumers without Home preserve the
previous default availability. No Dockview dimensions/visibility parameters are
changed to implement speech stopping.

## Executed seam matrix

All rows use the actual Home route lifecycle. The unit fixture keeps the old
pane's located anchor until the existing workspace-open action receives a new
one. The browser uses the complete actual app, Dockview and speech hooks.
Only API/WebSocket/synthesis responses are synthetic; no provider is contacted.

| Transition | During Home admission/error | After success or return |
| --- | --- | --- |
| Same Bot/session, different run | Old anchor remains; root mask stops old audio and frame | New anchor arrives; old queue stays silent |
| Cross Bot/session | Old Bot and old anchor remain while authorization waits; audio stops | Destination is admitted; old Bot audio stays stopped |
| HTTP 403 denial | Error mask stays silent; old pane gets no new anchor | Actual contact entry returns without catch-up; fresh reply speaks |
| HTTP 404 expired record | Same silence through pending and error | Contact return stays silent until a fresh reply |
| Wrong run association | Existing task-link validation rejects; old audio/frame stay stopped | Contact return retains the same behavior |
| Leave pending admission, then late success | Pending stops the old player before any new anchor | Late result cannot repoint contact or restart audio; new reply speaks |

The six-case red (`red/home-tests.log`) fails with expected idle/received playing.
The final source check (`green/tests.log`) passes **8 new Home cases + the 4
existing inner-anchor cases**. The two supplementary Home cases cover aborting
in-flight synthesis/ignoring its late result, and rejecting an incomplete URL
before starting a task lookup. They were added after the initial six-case red.
Old queued replies and late `ended` events never synthesize again; the normal
new reply is the only additional request. Existing broad Bot/backend/JWT/media/
autoplay suites were not rerun.

Changed-file ESLint passes. The candidate UI contract guard passes with three
existing warnings, recorded in `green/ui-contract.log`. Dependencies are reused
without installation; the local UI commit exactly matches the gitlink. No
whole-app build or full typecheck was run.

## Actual Chromium evidence

Runtime: `http://127.0.0.1:18485`, isolated Vite dev preview of the full candidate,
Chromium 154.0.8037.57, actual Dockview mounting and native HTML audio. A trusted
pointer gesture establishes supported user activation. The observer calls the
native `play` and returns its exact Promise; the synthesis fixture returns a
quiet 10-second PCM WAV. Chromium is headless/muted; this verifies native media
state, not physical listening, semantic TTS or unconditional autoplay.

The native red reproduces old audio `paused=false`, time `0.350679`, with its
bubble still `playing` and both orbit/breathing animations under the Home mask.
The old pane still holds run 1 while the URL requests run 2. It fails at
`Home admission mask must immediately stop old native audio`.
[Red masked workspace](red/same-session-new-run-pending.png).

The final native matrix passes all six groups, with zero page errors. Every
pending case records zero actually visible bubbles, computed visibility hidden,
all players paused with cleared sources, and idle bubbles with no animation.
Every success/error/contact return keeps the request count unchanged. Seven
requests total belong solely to the seven deliberately emitted fresh replies;
none comes from restoring an interrupted queue. On the final real contact view,
only reply 13 plays and animates. See `green/result.json` and `green/browser.log`.

Current screenshots were captured and inspected:

- [Same-session URL waiting before pane anchor delivery](green/same-session-new-run-pending.png).
- [Cross-Bot URL waiting while the source Bot remains selected](green/cross-bot-pending.png).
- [Denied history remains masked and silent](green/denied-error.png).
- [Expired history remains masked and silent](green/expired-error.png).
- [Wrong run is rejected by existing validation](green/wrong-run-error.png).
- [Pending request abandoned for contact](green/stale-response-pending.png).
- [Real primary contact: only its fresh reply animates](green/primary-contact-new-reply.png).

A preliminary observer used default `checkVisibility()`, which does not include
CSS visibility in its default checks. Its auxiliary visible-element count was
not accepted as visibility evidence. The final observer explicitly reads
computed visibility in all visible-state predicates and counts, and all six
cases were captured again. The retained red's old auxiliary count is not a CSS
visibility claim; its mask, old-anchor and native-media fields establish the
failure. No old evidence was edited to manufacture a pass.

## Reproduction and consumer handoff

The task-owned worktree is `/workspace/memoh-readaloud-home-review`. The retained
Vite config aliases this exact SDK source, the matching UI and existing installed
dependencies; copy it into that tree's `apps/web/` to replay the retained
`browser-home-review.mjs`. `READ_ALOUD_EVIDENCE` selects a new output directory.
Its absolute paths record this environment and must be adapted elsewhere. The
runner owns and closes its Vite, browser and loopback WebSocket fixture; the
preview port was confirmed closed after the final run. This dev preview is not
a production image or package build.

`shared-consumer-delta.json` lists the four exact product source hashes. Harmony
must consume them together: Home provider/mask, workspace re-provision through
Dockview, effective speech gate and the new injection-key module. Copying only
the composable leaves its standalone fallback enabled and does not fix the Home
boundary. Preserve existing native adaptations when mapping the delta. SDK,
styles and all three locales require no new change for this fix. Signed 1.0.27
does not contain it. No Harmony build/sign/install or NAS write/deployment was
done. ArkWeb media/activation, physical audio, device motion/accessibility and
human acceptance remain for the unique field writer after unified integration.
