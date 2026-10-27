# Harmony conversation monitor and failure-surface consumer

Base: `d594e279dfd38208a9f5651e896c8db614cb27ba` (the already built 1.0.17).
Fixed shared UI: `40765b9e0979d561c49e58d2f384799e6c589e27`.
This is a source-only shell/consumer checkpoint. No version increment, HAP,
signing, injection, Windows operation, model call or production deployment occurs.
The existing 1.0.17 artifact and its rollback identity remain unchanged.

## Result and minimum source seams

The old mobile trigger called openMobileNav and showed global activity. Its
source and the obsolete test expecting that behavior are removed from the active
mobile inputs and builder overlay. Git history preserves both. The builder now
uses the final shared conversation trigger, reader and recursive row component;
the footer has Task and related-Bot instances scoped to the active chat-view
Bot/session/visibility. No SessionTasks detail panel is mounted by that footer.
The unrelated global sidebar is not used as conversation membership.

The list reads only the existing authorized session/tasks/delegations GETs.
Persisted ancestry and delegation source/target session IDs establish membership;
unknown stays unknown. Rows show names and actual states only. Selection
revalidates the original Bot/session identity before using the existing route
and workspace chat tab. No logs, time, summary, result, raw ID/JSON, workspace,
device, additional detail panel, retry control or restoration button is added.

Same-run/turn runtime_run_failed metadata is retained through the shared
send/retry/edit result and startup failure, then the shared composer predicate
suppresses its duplicate only when that identified error is already present in
its assistant reply. It does not compare translated text, delete reply history,
hide unrelated/security errors or assume an unidentified error is the same.
The six corresponding composer replacements are pinned from the final shared
source; the shared error helper/tests are reused verbatim.

Seven new shared UI/helper/test inputs are pinned verbatim in sharedConversationMonitor.
Three existing store/type/test files consume only the minimal final shared hunks,
with full base/result SHA256 checks before the existing phone adaptations.
Seven generated outputs remain verbatim shared source. Send/types retain their existing phone preparation and
wire adapters; the shared store test has one reviewed phone fixture adjustment.
The shared composer receives only the two scoped footer entries and error
metadata/de-duplication seams, not the newer Web multipart/recovery implementation.
No entire shared tree or new API is imported.

## Integration corrections and preserved checks

The actual generated context initially exposed two distinct issues, whose logs
are retained in this task:

1. The default-off image collector accessed window during non-browser store-test
   import. A typeof-window guard now makes that import inert outside a browser;
   the browser collector, lifetime, privacy and native commands are unchanged.
   A new Node import regression verifies this.
2. One shared preflight-cleanup test supplied inline base64 bytes. The phone's
   existing NAS reference gate correctly rejected those before a run existed,
   so its simulated later server callback was invalid. The phone test adapter
   now uses the actual generated SDK, authenticated synthetic HTTP, size and
   SHA256-verified NAS bytes. It waits for that real gate before emulating the
   original server callbacks, retains every original cleanup/restoration assertion
   and adds a verified-read assertion. It does not bypass authorization or permit
   inline-byte messages. An initial fixture attempt raced async hashing and timed
   out; the readiness wait corrected the fixture without weakening assertions.

The new shared result metadata plus existing phone gate also exceeded the store's
600-line ESLint limit. Only the already-existing preparation block was moved to
its existing harmony-files owner; it preserves callback execution, reference
validation, cancellation/cause and message-acknowledgement behavior. Three focused
regressions cover invalid direct references, one producer invocation and
cancellation after preparation. No max-lines rule or assertion was relaxed.

The original 43 phone adapters, native upload/download recovery, four attachment
entry paths, scoped originals/hash/size checks, login/HTTPS binding migration,
empty public configuration and default-off image timing remain. There is one
additional phone test adapter, so the producer reports 46 host patches including
the two existing package/dependency patches. No native ETS, SDK dependency,
permission, service, TLS or endpoint policy change is part of this checkpoint.

## Actual validation

- 221 tests in ten files pass in the actual generated phone Web context: shared
  store/runtime/send-context, scoped reader/dialog, failure matching, original
  attachments and timing/import/preparation regressions. Earlier 218/217/failure
  counts are intermediate evidence, not the final result.
- Complete generated Web typecheck and production Web build pass. No HAP build
  is run. The existing normal chunk-size warning remains.
- Affected ESLint passes with zero errors and 26 generated-phone composer layout
  warnings. These warnings are not silently suppressed or presented as zero.
- Source inventory passes: 163 reviewed sources / 172 source-and-support files.
  The historical nineteen unrecovered/unverified later changes remain unknown;
  these new copies do not reconstruct that original checkpoint.
- The current compiled application runs at local loopback, Chromium 390×844,
  using synthetic read-only scoped APIs. Task opens a real shared dialog (not the
  global drawer), displays three levels, excludes unrelated Bot/task/delegation
  fixtures and contains only name/state rows. Child Agent and expert Bot clicks
  reach the correct original authorized sessions. Related Bots contains only the
  supervisor and linked expert. Existing phone Back closes the dialog. No global
  activity request, business write, model/retry/recovery call, login loss or page
  exception occurs. This is not a real ArkWeb/device/production acceptance.

[Tasks](conversation-tasks-390.png), [related Bots](conversation-bots-390.png),
[row crop](conversation-task-dialog.png), [DOM/interaction proof](browser-monitor-flow.json),
[source bindings](source-binding.json), [221 tests](tests-composed.txt),
[types](types-composed.txt), [lint](lint-composed.txt),
[Web build](web-build-composed.txt), [source check](source-check-composed.txt).
Screenshots are current actual compiled UI, inspected after the final combination;
names, IDs and private-looking diagnostic markers belong only to fabricated fixtures.
The synthetic WS fixture does not implement real runtime admission/history;
root execution state therefore remains unknown, not an invented running/completed
status. The relation dialog itself uses its real read-only GET path.

Error de-duplication is covered by the exact shared predicate, source wiring and
real store-result metadata regressions. This run did not reproduce the particular
user field screenshot or capture its actual assignment branch; it does not claim
that all identically worded unrelated errors are removed.

## Safe recovery remains a separate final combination

Adapter `2782ee0607967f6eff0f7df48cd72d195181551b` has been handed to the unified
owner, who is composing the final unique-hint click and generated SDK. This
checkpoint does not preempt that owner or independently call the raw endpoint.
The known intended route is POST
/bots/{bot_id}/sessions/{session_id}/decisions/{decision_id}/recover with
original_run_id. 202 is admission only; a duplicate refers to the same recovery
run. 409 is not permission for whole-turn retry or answer resubmission.

No whole-turn retry is attached to the hint and no accepted answer is replayed.
Final recovery UI/SDK, actual server compatibility, original phone acceptance and
any paid field recovery remain unverified here. Wait for the fixed final shared
head, then combine the exact components/SDK and make one necessary next-version
HAP. Do not rebuild 1.0.17, silently claim this source is installed, or use the
pending click as a reason to drop these completed independent shell corrections.

Published log text normalizes trailing whitespace only. Original failures and
raw logs remain in the existing evidence workspace; no prior package/evidence
or user data is removed. No private origin, credential, signature or device
binding is supplied to or committed by this source step.

Before handoff, three redundant full shared source copies were replaced by the guarded small patches above. Normal regeneration compared 1389 Web/SDK/UI files byte-for-byte with the 221-tested, typed, built and browser-verified context: zero changed files ([proof](compact-source-binding.json)). Those checks and screenshots are reused; no identical suite/build is repeated for this source-only packaging correction.
