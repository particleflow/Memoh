# Harmony task observation and bound-login audit

This is a fixed historical audit. The legacy preparer/dialog/adapter discussed
below have since been removed; [current observation production](../mobile/SHARED-OBSERVATION.md)
uses the complete archive and guarded overlays. The old source hashes and
results below retain their original qualification.

The existing cloud recovery workspace, branch `recovery/harmony-source-20261023`,
source checkpoint `796ed4487167ebc8985d16fa1d9fef6c79eebb39` and official API26
SDK 26.0.0.105 remained available. No environment replacement, SDK download,
HAP rebuild, signing action or production change was needed for this work.

## Actual implementation and contract

The 1.0.7 Web build uses the upstream `session-tasks.vue`, `SessionTaskResults`,
`useSessionTasks` and Bot delegation components from pinned `40f0a69`.
The restored legacy `HarmonyTasks.vue` and `harmony-tasks.ts` are not inputs to
that build. They were not activated or changed. Inspecting only those legacy
files would incorrectly describe the shipped behavior.

The existing conversation Tasks entry provides these server-authorized reads:

| User action | Existing contract / behavior |
| --- | --- |
| List conversation execution tasks | `GET /bots/{bot_id}/sessions/{session_id}/tasks`; server aggregates accessible descendants |
| Inspect one task | Same prefix plus `/{task_type}/{task_id}`; run state and bounded visible assistant text |
| Inspect a specialist Bot handoff | Existing `/delegations` list/detail, with independent execution and delivery states |
| Open its downstream tasks | Uses the server-returned `target_bot_id` and `target_session_id`; the source delegation remains authorized and polled |
| Inspect result references | Delegation attachment references retain the exact scoped path; no broader Bot/hash fallback |

The task service contract at current PR79 candidate
`0a324013f9fe1beec77ac948d0af8f210194ba52` is byte-identical to the pinned 40f
`internal/sessiontask/service.go`. Subagent states include accepted, running,
waiting_decision, finishing, completed, failed, aborted and lost. Its visible
assistant text is bounded to 16 KiB, and computer final summaries to 2 KiB.
Running output is not a final business outcome. The UI distinguishes execution
from return delivery and says accepted delivery does not establish supervisor
processing. There is no fabricated percent-complete or success inference from
process exit, task completion or an unrecovered result.

This is navigable conversation → specialist session → execution detail,
with periodic reads. It is not a scheduling queue, a complete immediate-parent
tree, a live tool/log stream or an editor. `parent_session_id` identifies the
requested aggregation root, not an immediate owner edge. Client-side parsing
must not invent relationships or retrieve hidden reasoning/raw session data.

## Confirmed gap and minimal correction

The old shared component discards downstream navigation after a transient
source-delegation read failure. Four new upstream regression cases reproduced
this against the actual 1.0.7 source: network/503 × manual reload/poll recovery.

The existing `build_memoh_web.py` host-patch mechanism now applies the two
upstream module changes from fixed PR79. `memoh_web.lock.json` records both
baseline and candidate hashes, plus the upstream regression-test hash. Generated
context comparison confirms exactly two files change, and their bytes equal
the candidate: `useSessionTasks.ts` and `session-tasks.vue`. The original 40f
archive and all other generated source bytes remain unchanged.

The correction clears stale content and aborts downstream reads during a
transient parent failure, retains only navigation, and resumes after the same
relation is freshly authorized. A 401/403/404, changed relation, identity or
scope still clears it. No new API, authorization rule, provider configuration,
Agent-core code, session migration, cancellation or execution control is added.
Magpie/provider selection and native Bot model/memory configuration remain
server-owned; this patch does not initialize or overwrite them.

## Login persistence

`harmony-entry.ts` reads existing `memoh:harmony:service-config:v1` at startup.
It calls authentication cleanup only while `resetPending` is true. Saving the
same API base preserves that false flag; the user store reads `token` from
existing local storage. The WebView origin stays `begonia-client://app`.
No login/persistence code was changed.

In a fresh isolated browser profile, the real configuration and login screens
were used once. Page reload and closing/reopening the browser process retained
the same credential and configuration, with exactly one login request. Task
503/403 reads also left that valid credential intact. No credential bytes or
fingerprints are recorded in public evidence.

This does not prove an actual signed HAP overwrite upgrade. The previously
reported old 1.0.6 installation had no recorded API binding; safely associating
its unbound credential with a new endpoint is a separate migration problem.
This work does not silently reuse that credential or migrate conversations.
Expired/revoked credentials and explicit logout still require authentication.

## Executed checks

- Generated context: exact candidate hashes for two modules; all other archive
  file bytes unchanged. [Receipt](testing/harmony-observation-recovery/context-verification.json).
- Upstream component/composable regressions: baseline 4 failed / 26 passed;
  corrected five-file suite 119 passed, no failures. These tests use synthetic
  SDK responses. [Before](testing/harmony-observation-recovery/shared-before.log.txt),
  [after](testing/harmony-observation-recovery/shared-after.log.txt).
- Web `vue-tsc --noEmit`: exit 0; changed-module ESLint: exit 0.
  [Typecheck](testing/harmony-observation-recovery/typecheck.log.txt),
  [lint](testing/harmony-observation-recovery/lint.log.txt).
- Existing service configuration/native-adapter checks: 115 passed; synthetic
  storage/IO, not Harmony OS execution.
  [Log](testing/harmony-observation-recovery/service-config.log.txt).
- Actual Vite client at `http://127.0.0.1:4179`, Chromium 390×844, Chinese and
  dark mode: source delegation → downstream detail; running/failed visible
  text; pending delivery and scoped attachment reference; 503 clearing/manual
  recovery; parent 403 removal; login retention. Zero task writes and zero page
  errors. HTTP was intercepted with isolated synthetic data: no real Server/PG,
  model, PC, NAS or production requests. [Report](testing/harmony-observation-recovery/browser-audit.json),
  [harness](testing/harmony-observation-recovery/browser-audit.py).

Screenshots were opened and inspected:
[pending delivery](testing/harmony-observation-recovery/delegation-pending-390.png),
[paused read](testing/harmony-observation-recovery/downstream-paused-390.png),
[failed child in dark mode](testing/harmony-observation-recovery/downstream-failed-dark-390.png).
The screenshots establish the controlled browser states, not native acceptance.
Vite blocked optional font files outside the validation-root allow list; the
screenshots use available fallback fonts. No font-fidelity acceptance is claimed.

The validation copy reuses existing installed dependencies. To repeat the test,
generate context with `context_archive`, compare its two modules to the locked
candidate, and extract into a separate validation checkout. Use the fixed
candidate's `session-tasks.test.ts` (hash recorded in the lock) with the existing
four delegation test files. Run the existing Web typecheck and Vitest commands;
no dependency installation or HAP packaging is needed. The browser harness takes
`HARMONY_AUDIT_WORKSPACE` and expects that workspace's original client fixture,
evidence directory and Vite validation checkout as recorded in the script.

## Artifact and acceptance boundary

The previously delivered 1.0.7 unsigned HAP is preserved byte-for-byte:
SHA256 `4912fd74241a0e8109935a4e26a6b93be92fb8e600fc2179f5e7cd3650fa9af9`.
It does **not** include this source follow-up. No new HAP, ZIP or signing package
was produced, and the native version remains 1.0.7. A future release build must
regenerate its Web package and native source binding from this checkpoint.

Live Server/PG integration for this mobile host, signed device overwrite,
ArkWeb lifecycle/permission/TLS behavior, real attachment byte transfer and
human acceptance remain unverified. The browser's synthetic HTTP is not a
substitute for those runtime checks. Original source ZIPs, private configuration,
toolchain, prior build evidence and deliverables remain intact.
