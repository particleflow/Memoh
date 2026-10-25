# Harmony shared source alignment to 52609f7

The phone's complete shared source and SDK now lock to released candidate
`52609f7db4e59d7e528c2c5f8aa0330aa3ebbbe3`, replacing
`cbcee15a17dc87fd02eb4c198fba0aa45eb3fa45`. This is a source-only update on the
original Harmony branch. No version increment, HAP build, signing, publication,
NAS deployment or Windows operation occurred in this step.

## Exact changes and minimum implementation

[alignment.json](alignment.json) lists all **21 Web and 4 SDK** changed files.
The full [source archive verification](source-archive-verification.json) matches
all **6,448 Git blobs** of that exact commit. The UI gitlink remains
`f69402a55dd87a59c10722fe11507ad8fd7048f5`; package manifests and dependency lock
are unchanged. The existing deterministic archive recipe and cached UI were
reused. No floating branch or unfinished file-owner/ASR patch was included.

| Shared change | Phone integration and boundary |
| --- | --- |
| `native-task-resume.vue`, `useNativeTaskResume.ts`, host scope provider | The existing message tool card and parent-run view directly render the shared original-operation action. The complete original arguments and operation/target identity are retained. Missing scope/arguments stay read-only. No phone-specific action or API was added. |
| Generated SDK resume route/types | The shared authenticated POST uses the original Bot/session/`computer_operation`/operation ID. Merely opening a card sends nothing. Submitted/unchanged acknowledgements do not imply business completion; uncertain response is persisted in session storage and cannot trigger automatic repeat. |
| First-send ACK timeout | The shared runtime keeps the send outcome unknown without automatically cancelling the possibly running task. Existing explicit-stop semantics remain. This fixes the demonstrated timeout defect; it does not establish the cause of a historical cancelled image task. |
| Cancel observation and reason | Existing stop, Bot-switch and cleanup paths carry the upstream allowlisted reason. Generated SDK gains optional invocation-observation fields. A received control is not proof that it caused an old cancellation. |

All **43 existing phone adaptations** apply without an implementation edit.
Only the three locale files and `message-item.vue` have changed upstream target
hashes. The two phone attachment test adapters remain. The source changes in
this branch are the existing shared lock, its provenance hash and the reviewed
generated-context baseline, plus these evidence files. No builder logic or
shared product component required a phone-specific fix.

The old `mobile/SHARED-OBSERVATION.md` describes the earlier legacy host. The
current complete-source builder does not import `HarmonyTasks`; this update
uses the actual shared message/parent-run components above. The published
combination's `docs/testing/resume-abort-combination/README.md` and the repository
session-runtime requirements supplied the current behavioral checks.

## Preserved 1.0.11 behavior

The [recorded phone input hashes](alignment.json) are unchanged from
`4fda303783b8b135bc2b6cc9b9170e47d02a2332`: native shell, identity/permissions,
Windows configuration injector, explicit same-service HTTP-to-HTTPS binding,
persistent login, four file intake paths, scoped delegation download and the
original receipt/hash/recovery matrix. The public source configuration is empty.
No old private address, real binding, credential or signing material is present.
The 1.0.11 package and its onsite signing/distribution result remain separate;
that package does not contain this newer shared source.

## Validation

- Source checks: **128 entries / 137 files**, 43 reviewed phone adaptations and
  2 attachment test adapters; empty configuration. Historical 19 unverified
  changes are not reclassified as recovered by this update.
- Full generated Web typecheck passed. The empty [typecheck log](typecheck.txt)
  corresponds to exit zero.
- [Eight relevant shared suites](shared-tests.txt): **143 passed**, including
  original-task resume component/composable, parent run, WebSocket abort reason,
  stream/realtime behavior and both phone attachment suites.
- [ACK/abort selection](ack-abort-tests.txt): **10 passed / 138 unrelated skipped**.
  This is a selected check, not a claim that all chat-store tests were run.
- [Existing configuration/native bridge checks](service-config.json): **178
  passed**, including the explicit HTTP-to-HTTPS upgrade and refusal cases.
- [Targeted lint](lint.txt): zero errors, five existing multi-component test
  warnings. Assertions and compiler settings were not weakened.

The existing Vite application at `http://127.0.0.1:4183` was verified to serve the
new generated source, then exercised in Chromium **390x844** with synthetic
authenticated API/native/runtime boundaries. Opening Tasks and tool details
sent zero recovery POSTs. One explicit click sent exactly one POST with the
original operation, target and complete original body. A synthetic 503 left the
outcome unknown; reload retained that state, did not repeat the POST or dispatch
a model request, and preserved login (one login total). Page errors were empty.
The action was scrolled into view using the existing dialog and its bounds were
verified inside the phone viewport. No layout patch was needed.

[UI report](native-resume-ui.json) and [reproduction script](verify_resume_ui.py):

- [Original task action](native-resume-action-visible-390.png).
- [Unknown acknowledgement](native-resume-unknown-390.png).
- [Reload retains unknown without retry](native-resume-reload-390.png).

Screenshots were inspected by the agent. These checks do not establish actual
Herdr/Windows recovery, live TLS/NAS behavior, phone installation or human QA.
Automatic LAN-first/IPv6 fallback and final shared file acceptance remain
outstanding. A future package must use an explicitly fixed final shared/source
commit; this update does not rebuild the already validated 1.0.11 HAP.
