# Historical-run read-aloud stop boundary

Source fix: `b43875b5c541afef744909335004c0cb2f146c3a`, branch
`fix/chat-read-aloud-history-gate-20261028`. **Cherry-pick only this commit** into
the unified combination. It changes the pane speech gate and adds four narrow
tests. Its parent `81597deb05bac53edfa2dea3f75dbcd066382c1f` is just the local
cherry-pick of already-admitted `bbbb448a`; do not reapply that parent.
`history-run-read-aloud.patch` is the equivalent standalone patch.

## Exact source and scope

The parent's `/workspace/unified-source-recovery/combo-seam/` evidence paths do
not exist on this machine. Fetches for the unpushed v6 and unified SHAs returned
`upload-pack: not our ref`; these were missing remote objects, not approval
rejections. The supplied Library file was materialized at version 6. Its ZIP
SHA-256 matches `52c5b32b5cea66b36ab0b49ece69f1bf9f5f883e47a1018313f4fd56b7a6ac55`
and all 151 manifest entries were verified. Its incremental bundles reconstruct
exact v6 `8d48dfe7c7e84849c7986608f89a114b7bf85d75`. The isolated test tree adds
only the already-approved contact speech gate, then this new fix. This does not
claim verification of the parent's complete uncommitted aggregate tree.

The existing `useTaskRunAnchor` sets `pending` while revalidating a changed run
in the same session, and `unavailable` for failure/missing history. The pane
hides its message scroll area for both statuses. The former speech gate still
accepted its unchanged Bot/session/visible/focused context, leaving the hidden
old player and animation running.

The fix additionally requires `!props.taskAnchor || status === 'located'`.
It reuses the existing synchronous disabled watcher to stop audio, abort
synthesis, clear queued work and consume interrupted replies. The speech hook
is initialized after the anchor hook, because its immediate watcher reads the
new gate during setup. No Bot/session/run identity, router permission, backend,
controller, styling, translation, or new UI is changed. `verification.json`
records the unchanged controller/composable/anchor source bytes.

## Narrow red/green tests

`useChatReadAloud.task-anchor.test.ts` executes the pane's actual speech enable
expression with the real `useTaskRunAnchor` and `useChatReadAloud`; only synthesis,
media and validation are simulated. The valid red is `red/composable.log`:
three failures (expected idle, received playing; unavailable still playing;
synthesis not aborted), while the ordinary-chat control passes. The first
exploratory fixture had an overbroad expression extraction and failed parsing;
that fixture error is not counted as the red evidence.

`green/composable.log`: four tests pass:

- Same-session historical run change stops the old player and queued reply.
  Resolving the new anchor and a late old `ended` event never restart either;
  explicit replay of the newly located reply remains available.
- Unavailable history disables playback/replay, including later live messages.
- Pending history aborts in-flight synthesis; its late response after the new
  run is located never creates a player.
- Ordinary chat without an anchor continues auto-reading even when the
  unused locator status is pending/unavailable.

Only this new test file ran; prior media, autoplay, JWT, Bot-owner and backend
suites were not rerun. Changed-file ESLint passed. The UI contract guard passed
with three existing warnings. Its first candidate run failed because the
worktree's UI submodule source was absent; the same exact submodule commit
`f69402a55dd87a59c10722fe11507ad8fd7048f5` was confirmed and locally reused.
No dependencies were installed or changed. A full build/typecheck was not run.

## Actual Chromium media/UI check

The affected app pane ran through Vite at `http://127.0.0.1:18484`, Chromium
154.0.8037.57. The retained runner uses synthetic REST/WebSocket fixtures and a
10-second WAV, and delegates observation to native `HTMLMediaElement.play`
without replacing its Promise. A trusted pointer gesture establishes activation.
No real model or TTS endpoint is contacted. No previous autoplay-refusal checks
were rerun.

The browser uses the existing `openSessionChatPinned` workspace consumer to
change the anchor of the same mounted session, as cached pane/run selection
already does. The actual locator's validation request is held or rejected by
the fixture. This covers the pane/locator/media seam; it is not an end-to-end
URL admission, backend authorization or true-device test.

Red: native player `paused=false`, currentTime `0.24708`, hidden bubble `playing`
with `chat-speech-orbit, chat-speech-breathe` while loading. The assertion
`pending historical run must stop old native audio` fails as expected.
[Red pending screenshot](red/history-pending.png) also shows the old stop control.

Green: the four new browser checkpoints pass. Pending clears the player source,
pauses audio, sets the bubble idle and removes both animations. After locating
the new history and after unavailable history, playback request count remains
one. Returning to ordinary unanchored chat and receiving a new live reply makes
exactly the second request; only the new bubble animates. The initial playback
checkpoint is prerequisite setup, not another broad feature regression test.
The synthetic fixture's full history is not a transcript-deduplication test.

Inspected current screenshots:

- [Pending: old player and animation stopped](green/history-pending.png).
- [Unavailable: silent with no active bubble frame](green/history-unavailable.png).
- [Ordinary chat: new reply plays and its bubble animates](green/ordinary-chat-new-reply.png).

`green/result.json` contains the media/state observations; `green/browser.log`
contains the runner output. Screenshots alone do not establish audio behavior.
The browser, WebSocket fixture and Vite preview were closed after the run.

## Reproduction and handoff limits

The isolated worktree is `/workspace/memoh-readaloud-history-review`. Its exact
source SDK is aliased in the retained Vite config; the matching installed
dependencies and UI source are reused from `/workspace/Memoh`. Copy
`vite.read-aloud-review.config.ts` into that worktree's `apps/web/` before running
`node docs/testing/chat-read-aloud/browser-history-review.mjs` from the original
evidence branch. `READ_ALOUD_EVIDENCE` selects a new output directory. The retained
runner removes only unused earlier contact-hold fixture code; this history
scenario is the one executed. Absolute fixture paths describe this environment.

No paid model, new service, build, signature, NAS/field write or deployment was
performed. Harmony 1.0.27 does not include this fix. Its owner must map the same
conditional and initialization order into the new shared anchor consumer;
existing SDK/style/locale contracts are unchanged. ArkWeb native activation,
media/interruption, final device animation/accessibility and human acceptance
remain for the unique field writer after integration. This source fix and
isolated browser evidence do not replace that acceptance.
