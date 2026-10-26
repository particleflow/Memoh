# Harmony source alignment to cbcee15

The complete shared Web source now locks to
`cbcee15a17dc87fd02eb4c198fba0aa45eb3fa45`, replacing `8a2422935916f56b5bacfac2f991824d1cb9bd9f`.
This is source alignment only. No HAP was built or published in this step; the
previous private compilation check of `273415c9` and distributed 1.0.9 remain
preserved, and neither contains this new shared source.

## Exact differences and ownership

[Archive verification](source-archive-verification.json) matched all 6,314 Git
blobs, retained the existing UI dependency and recreated the combined archive
using the project's existing deterministic recipe. [Alignment](alignment.json)
lists every changed Web file and the retained phone input hashes.

The 19 Web changes add the shared parent Bot run/tool result view and terminal
image-result recovery. The SDK and UI dependency did not change. All 43 phone
production adapters apply without edits to their implementation; only the
three upstream locale target hashes change due to the new shared strings.
The existing parent/child distinction, operation/run identities, terminal tool
uncertainty and snapshot recovery come from upstream, not a new phone component.
Legacy entry filtering remains the server's responsibility; this does not
claim a live server migration was performed or validated here.

The phone native classes, upload picker/File intake, four upload entrances,
delegation-scoped download authorization, login persistence and Windows-only
private injection code are byte-identical to `273415c9`. The public configuration
is empty. Shared chat-pane, media gallery, attachment display, task-result file
view and generated SDK are unchanged between these shared commits. No shared
upload/download implementation was edited. The file owner's unfinished head is
not included; its eventual overlap with phone patch anchors must be checked
against its exact commit, without another protocol or duplicate implementation.

## Test adaptation and validation

The first wider shared-test run passed 159 of 160 cases. One old task-list test
expected a direct browser download anchor; the phone correctly uses its native
save button. [Baseline evidence](baseline-link-comparison.json) and the
[original failure](baseline-link-failure.txt) reproduce the same failure using
the old test and containing component with unchanged phone attachment code.
This was not a full historical checkout run.

The minimal test-only adapter now clicks the actual native save button and
asserts the owning Bot, hash, size, filename, exact authorized URL/Bearer and
same-operation fetch/save/release sequence. It preserves every pagination,
pending/failed and task-completion assertion. It imports the real SDK media
method; only the OS reply is controlled. The remaining 41 task-list cases are
unchanged. [All 42 task-list cases passed](phone-task-list-final.txt), and lint
reports zero errors / two existing component-count warnings. Along with the
other seven unchanged suites from [the first run](shared-tests.txt), all 160
selected cases have passing results. The existing CI gate adds this second
test file; no new workflow or build environment was created.

The full generated Web typecheck passed with existing dependencies. Source
verification reports 128 manifest entries / 137 files, 43 phone production
adapters and two test adapters. The count increase is the new test fragment,
not proof of the unavailable historical source tree; 19 historical changes
remain unverified.

The current generated client at `http://127.0.0.1:4183` was exercised in
Chromium 390x844 with synthetic API/native boundaries. The existing picker,
paste/drop and recording send gesture still call the same native staging and
upload path. Unknown Create retains the original, sends no chat/task message
and does not consume its journal; logout closes the native bridge.
[Upload entry result](resumable-ui.json), [paste](paste-native-intake-390.png),
[recording](recording-native-intake-390.png).

The actual Tasks entry also passed [parent runtime UI verification](parent-ui.json):
distinct parent RunID/child task, actual tool call identity, aborted run with
unconfirmed tool result, stale-state removal on `runtime_dropped`, same-run
restoration after a fresh snapshot, and login retained after page reload.
Only runtime subscription/unsubscription and heartbeat frames were permitted;
no execution frame was sent. Page errors were empty. The reproducible
[browser harness](verify_parent_ui.py) uses synthetic events with the actual
generated page/store, not a replacement phone component. Screenshots were
inspected: [running](parent-running-390.png),
[terminal unconfirmed](parent-terminal-unconfirmed-390.png),
[resynchronized](parent-resynchronized-390.png).
Earlier harness setup/locator failures remain documented in
[fixture-failures.json](fixture-failures.json); none required a product fix.

These results do not establish ArkWeb/phone behavior, live image generation,
NAS receipt retention, LAN/IPv6 selection or phone-to-NAS end-to-end acceptance.
The final HAP waits for the fixed shared file-transfer head and unified source
lock, then uses the existing toolchain and private Windows injection/signing
process once. No new credentials, private endpoint or SDK binary enter Git.
