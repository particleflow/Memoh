# Harmony candidate: live Router context after initialize recovery

Base Harmony source81c995a45626c9af5d40c87bd74ae3df68f5bad2 already contains the
admitted1fa17 history-transport fix. This step maps candidate
500d86f52ae97ba92ac8245bbafbe66ba21543db from shared1fa17, with evidence commit
a7e69c4d63d528bf578939b45b4c8198878ccbaa. It is a candidate under unified review,
not a final release or new HAP.36 remains the original signed/distributed package
per the parent's current field receipt; no old package is rebuilt or relabeled.

## Exact consumer changes

Consume all three production seams together: new chat/route-context.ts; chat-list
closure/export; Home's real-router binding before URL synchronization. The helper
keeps a live per-store, unpersisted Router reference. No Web-only global Router,
second history-intent state, inferred identity, new protocol or permission is added.

Current phone Home differs from the upstream base only in its already approved
Garnet Assistant document title. Add only chatStore.bindRouteContext(router) after
the existing useChatStore call; removing that line restores the exact prior phone
Home bytes. Do not replace the whole Home with old Web source. chat-list and the
new helper match candidate bytes. Three existing test fixtures only receive the
admitted no-op/mock action; existing phone expectations/content remain. The new
route-context test is exact upstream. See [source-mapping.json](source-mapping.json)
and [upstream-consumer-map.json](upstream-consumer-map.json).

The actual isolated context from the original generator verifies all seven new
mapped sources,167 paired entries and47 host patch names, the retained12 source-
identity inputs and readonly composer/lifetime final digest. All199 other tracked
client files remain byte-identical to81c995; SDK/native files, private configuration,
version, identity, permissions, login, attachments and background queues are unchanged.
The previously admitted ensureWebSocket bootstrap code remains unchanged and its
chat-store injection is preserved. No frontend assets/HAP are rebuilt in this step.

## Actual validation, including unresolved failures

The targeted phone-context run has29 passes and8 failures, exit1. Passing files
cover two new route-context lifecycle cases plus existing Home navigation/not-found.
All eight failures are the existing Home read-aloud admission fixture. Before the
new binding, the actual81c995 phone context also failed those same eight named tests
at api.player.dispatchEvent because its mock player was absent. The new binding
is not sufficient to explain those failures. Their underlying cause is unknown;
this is not a fully green suite or a read-aloud fix. No assertions/audio production
code are changed to hide them. Raw [baseline](voice-baseline.log.txt),
[targeted](targeted.log.txt) and [verification](verification.json) are retained.
The independent read-aloud owner retains responsibility for its new corrections.

Generated phone-context typecheck and seven-file ESLint exited0. The original
context generator and193-source/209-file manifest checks pass; unmodified earlier
20-case bootstrap/contact and SDK evidence are reused, not falsely rerun. See
[context-verification.json](context-verification.json). No physical ArkWeb, live
backend/model/paid work, browser/phone manipulation, signing or deployment occurred.

## Acceptance and composition boundary

The [original owner's full frontend red/green](https://github.com/particleflow/Memoh/blob/a7e69c4d63d528bf578939b45b4c8198878ccbaa/docs/testing/history-retry-route-context/README.md)
proves a later-initializer ownership defect: a store created before Router retained
null, so a retry could subscribe the fixed contact while URL/history still namedS.
Both accepted fixture runs showed two rows, not the field's eleven skeletons.
No native/full mounted-app or new field reproduction is claimed by this source
consumer. The real eleven-skeleton/zero-message cause and actual acceptance remain
open. Do not replay models/answers, force refresh, remove snapshot atomicity, clear
history or add management UI to work around this unresolved field state.

The existing [25-item acceptance gaps](../harmony-history-bootstrap-1fa17/acceptance-gaps.json)
remain open at their recorded scopes. U02 now additionally has this Router-context
candidate; V01 must distinguish candidate source from the existing36 package.
The stable release compatibility lock remains1fa17; the500d candidate is recorded
separately. Await unified review, the read-aloud and quick-reply owners, then an
exact final consumer map before one new version/build. No new version is assigned,
no artifact uploaded, and no field writer or other module is taken over here.
