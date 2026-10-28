# Contact/task read-aloud seam review

Scope: compare accepted Memoh `a0a821a4cdaa32121a945d04e80c76bba4b6f2b4`
with candidate `2edfd88dafec2291df6eee9343a7a2bd494da24a`; inspect Harmony
1.0.27 source `2195e92f1ff0a354e6cb4ab4bc86a4f1533d42c7`, whose shared
parent supplied by the owner is `b66c1b3eb1f93f765379c97dbf4bf7d3ac7a9ae0`.
This is isolated source review and synthetic-media browser evidence. It is not
NAS release, ArkWeb playback, device installation, or packaged-build acceptance.
The previous five genuine autoplay-refusal checkpoints in `autoplay-refusal/`
remain untouched and were not rerun.

## Confirmed new defect and minimal handoff

Returning from an existing task session to the same Bot contact sets
`contactNavigationPending`. The new `index.vue` hides the workspace with
`v-show`, leaving its chat pane mounted. Dockview visibility/focus do not change,
and the existing speech `enabled` expression does not consume the new flag.
Consequently old task audio continues while the screen says loading, and its
hidden bubble remains `playing` with both orbit/breathing animations.

`red/failure.json` records native audio `paused: false`, currentTime 0.206245,
and `chat-speech-orbit, chat-speech-breathe`; the assertion fails as expected.
`red/candidate-contact-pending.png` shows the masked workspace. The red runner
stops at that assertion and makes no claim about the subsequent resolved path.

`candidate-contact-gate.patch` adds just `&& !chatStore.contactNavigationPending`
to the existing speech enable condition in candidate `chat-pane.vue`. The existing
synchronous disabled watcher already stops the native player, aborts synthesis,
clears queued work, and marks interrupted replies consumed. No navigation/store
implementation or UI is changed. The source-only commit is
`bbbb448a0c6fd6f35f90d28316f4d5a51c5e9dc6`, on independent branch
`fix/chat-read-aloud-contact-gate-20261028`, directly parented by the exact
candidate. The unified owner can cherry-pick that one-line commit. The patch is
also retained here for inspection/application (`git apply --check` first).
The original speech branch retains evidence only; its older product tree does
not import the other owner's contact-store development. Cherry-picking this
evidence commit alone does not update product behavior.

`green/result.json` records three passing targeted checks in Chromium
154.0.8037.57 with a real native media player and a synthetic 10-second WAV:

1. A new assistant-only result turn, with no request-user bubble, requests only
   the assistant result text once; initial history does not trigger synthesis.
2. While the same-Bot contact response is withheld, the old task audio is paused,
   its source cleared, the bubble is idle, and both animations are absent.
3. Once contact resolution completes, the old reply/queue stays stopped and the
   playback request count remains one.

Only local API/WebSocket/WAV fixtures are used; no real TTS or model provider is
contacted. Trusted CDP input supplies activation; this targeted test does not
repeat or extend the separately recorded native autoplay-refusal evidence.
The player observer delegates to the real native `play` and returns its original
Promise. Stop clears the URL and may produce a native empty-source media error;
the bubble still remains idle with no animation.

Two early green fixture iterations timed out at contact binding: the fixture
kept the name-normalization follow-up POST pending as well. The final runner
releases its one intentional hold and allows later contact requests normally.
That fixture correction is distinct from the one-line product patch. A first
preview config import also failed before loading the app; the retained config
is copied into the candidate app package to retain its ESM package context.

## Other seams reviewed

`source-audit.json` records SHA-256 byte equality for the speech controller,
composable, message item/actions, animation CSS, transcript projection/merge,
message types, and Dockview pane visibility implementation. The pane's speech
wiring, draft watcher, replay and message-click handlers are unchanged apart
from the proposed pending gate. Task directory labels are outside speech's
message list.

The backend delta hides machine task inputs in both live `NewRequestUserTurn`
(`DelegationResultID`) and persisted `ConvertMessagesToUITurns` (`IsInternalInput`).
`IsInternalFeedback` excludes `bot_task_result`, retaining that result's own
durable turn rather than folding it into earlier feedback. Speech continues to
select assistant text and deduplicate by the existing turn/run key; no new raw
machine-input speech or duplicate result defect was established. The targeted
assistant-only fixture above exercises this new consumer seam; it does not
execute the actual backend. Existing Bot-owner persistence/projection tests were
read as source evidence, not rerun or claimed as our own passing results.

## Harmony boundary

At the exact Harmony source, the two added speech module sources match Web/a0
byte-for-byte. The contact overlay adds the same pending mask, while the speech
overlay still installs the old enable expression without the pending flag.
The lock's read-aloud and contact overlay hashes match the inspected JSONs;
these references are recorded in `source-audit.json`.

Therefore the source seam requires the corresponding one-line gate in the
phone's read-aloud mapping too, with its source hashes/lock updated by its owner.
The existing signed 1.0.27 is **not** claimed to contain this proposed fix.
No HAP/Web production build, signature, NAS write or phone operation was done.
ArkWeb genuine activation/refusal, trusted long-press/replay, navigation/input
interruption and final device motion/accessibility remain for the unique
writer's field acceptance after both consumers integrate the fix.

## Reproduction setup retained

The executed detached candidate worktree is
`/workspace/memoh-readaloud-review-2edf`. Its installed dependencies are symlinked
from the original `/workspace/Memoh`, whose lock/package/UI submodule references
match for this delta. The candidate SDK source is explicitly aliased so the test
does not accidentally use the old built SDK. The candidate Vite config differs
only in test filesystem allow roots and source aliases; it is retained here.
It is a loopback dev preview, not a production or Harmony build.

To replay in this environment, copy the retained config into the candidate's
`apps/web/vite.read-aloud-review.config.ts`, then run:

```sh
READ_ALOUD_EVIDENCE=/tmp/contact-task-review \
  node docs/testing/chat-read-aloud/browser-contact-task-review.mjs
```

The runner launches and closes its own isolated Vite, browser and WebSocket
fixture. Its absolute dependency/config paths document the tested environment;
adjust those only when reproducing elsewhere. The patch was also checked with
changed-file ESLint; see `candidate-eslint.log`. No earlier broad tests were
repeated and no new UI is introduced.
