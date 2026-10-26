# Shared Web durable media recovery: isolated checkpoint

Acceptance mapping: F03/F05/F06/F08. **Implemented and isolated verification only; not NAS, Windows or phone field acceptance.** No production deployment, PR, merge, remote branch write, new service/protocol/API/schema, device identity, generic receipt lease extension, queue budget, model call or Windows 1 GiB rerun occurred.

## Baseline and isolation

- Isolated cloud checkout `/workspace/Memoh`, branch `fix/web-durable-media-isolated-20261026`, based on committed PR79 head `2d03f6cbabad67b242d33180cc36bf02d423e6d5` (product backend tree is the already deployed 9bb baseline).
- Original transfer branch `test/native-transfer-producer-combination` remained `12f9ca5cd2ec3cc082becca933db393d27eb3ac3` at every remote read, including the final checkpoint read. PR79 remained 2d03. Neither branch was written. The original running task was not stopped or accessed.
- Reused committed API/storage contracts, prior documented Windows/Linux evidence and the cross-end audit. The old task's uncommitted implementation, claimed 13 tests and claimed browser/PG results were unavailable and are not counted here.
- Current-source hashes are in `source-manifest.json`. The handoff archive's `CHECKPOINT.json` records the exact local tree and patch hashes; these are not a new commit SHA.

## Changes

Existing composer upload entries now use ordinary authenticated Bot-scoped media uploads with IndexedDB journal checkpoints, OPFS source copies and bounded incremental SHA256. Durable create-unknown precedes Create; no known/unknown outcome creates a replacement receipt. Resume GETs the original ID; 404 retains ID/offset/expiry. Lost PATCH reconciles before further bytes; lost Complete remains read-only until original GET is ready. The generic 24-hour expiry is retained exactly. Ready references require actual original size/hash readback.

Fresh account, exact API origin/prefix, Bot and session/draft bind each journal. Request-dispatch guards prevent a new token from being sent for an old identity, including SDK preparation races; redirects fail closed. This adds no network probe per block and no alternate URL/TCP routing. The browser remains platform-routed under the existing network contract.

Pause aborts local work without server Cancel. Removing a tray card dismisses the journal, preserving unknown-create fencing. Original journal draft tabs are restored without creating a session or sending; pending attachments pin the original tab so a preview cannot replace it. A different live session is never repointed to adopt an old journal. Missing local source explicitly requests re-selection; same-name/size is insufficient, SHA256 must match.

Ordinary asset downloads (dock and fallback) and delegation downloads now use the existing exact authorization route, Range/If-Range, closed OPFS chunk writes before IDB offset ACK, and complete size/hash verification. Authorization is rechecked before returning cached bytes; delegation denial never falls back to Bot/hash. Message confirmation clears browser-local upload state only.

## Executed byte/process proof

Current Vite UI at loopback 18082 and current baseline Go Server at 18080; real PostgreSQL schema **177/clean**, ordinary account JWT and BotMedia HTTP, one isolated originals root. These are development loopback URLs, not production TLS/LAN acceptance. No workspace/PC runtime was provisioned; the unrelated workspace-unreachable toast is retained in screenshots.

Deterministic input: **3,145,747 bytes (3 MiB + 19)**, SHA256 **d65ebdd932478b7c2d98e272b786a89ad80b4d36ce3fc0d72e2676a020bbf3af**. Real original receipt **17b5966f-95a1-4176-bfb8-df56d7f5f70b**, unchanged expiry **2026-10-27T04:46:30.975309072Z**.

| Boundary | Executed result |
| --- | --- |
| Actual UI file selection/send; first PATCH 200 response dropped after real HTTP commit; Chromium PID 12270 killed | IDB patch-unknown/offset0, same receipt, full OPFS source retained |
| New Chromium PID12845, same profile | Source restored; GET original receipt; only remaining three PATCHes; Complete200 response dropped after real commit; complete-unknown/offset3145747; process killed |
| New Chromium PID13098 | GET same original receipt ready; HEAD and four real Range reads; full size/SHA verified before attachment reference returned |
| Primary upload totals across those three processes | **Create1 / PATCH4 / Complete1**; same receipt/expiry; zero model-message attempts. First two CDP logs did not retain numeric PATCH header offsets, so those fields remain null rather than fabricated. Counter4 plus four exact file chunks and server offset enforcement establish no acknowledged first-chunk retransmission. |
| Actual download button; PID13606 killed after interrupted second Range | Download IDB offset1048576 and OPFS prefix retained |
| New Chromium PID15150 | Ranges1048576–2097151,2097152–3145727,3145728–3145746; first block not requested again; actual saved `recovered-original.bin` is3145747 bytes with exact full input SHA |
| Lost Create (separate draft, same real bytes) PID15747; reopen PID16547 | First call Create1 then create-unknown without local ID; reopen retry sends **zero Create** and retains unknown intent. Server had a ready original; this does not invent the lost receipt ID. |
| Lost OPFS source; original tab had been replaced; final PID21342 in narrow620px/dark/Chinese | Recovered original tab/scope and displayed explicit source re-selection. Re-selected original bytes used same existing receipt, no Create/PATCH/Complete. Synthetic chat `messageSent:true` consumed the actual UI callback and removed only the main local upload journal. Unknown-create journal and verified download journal remained. |

The chat confirmation above is explicitly synthetic to avoid a model/business call. It is not an actual delivered chat message. PostgreSQL fixture session/history/delegation/computer-operation counts remain **0**. The original server file still has the exact input size/SHA after local ACK cleanup. Each successful browser phase recorded zero page exceptions. A teardown listener race emitted Node's unsettled-await warning after the final proof files were saved; Chromium had already been killed. The helper teardown was corrected; no transfer was repeated for that warning.

Machine-readable phase requests/journals/PIDs are in `browser-process-proof.json`; authoritative byte/reference and preservation checks are adjacent JSON files.

## Necessary checks and retained failures

- Current affected product/tests: **160 unique tests exercised successfully across scoped runs**, not one invented all-green aggregate. The 8-file run has158 passed and1 test-fixture setup failure (missing fake panel). That fixture was corrected and the exact collision case passed (99 other store cases filtered, not rerun). The final server-MIME case passed (18 other media cases filtered). Earlier 57/58-test increments remain historical, not extra distinct tests.
- Full Web typecheck and affected ESLint passed. UI contract passed with three existing warnings, no new violation. Locale duplicate-key check passed.
- One initial changed-Web production build passed in7.93s. Subsequent final guard/tab changes were verified with current Vite UI, typecheck and targeted tests; that earlier build is not relabelled as a final-tree bundle, and no already-passed version was rebuilt. No full Go/source matrix or previous large-byte suites were rerun.
- Initial download helper incorrectly required a textarea while a restored asset tab was active; it timed out before clicking Download. The helper readiness selector was corrected and only the unfinished download resumed. Retained log is adjacent.
- Source re-selection exposed a real ephemeral/mobile tab restoration gap; the final product fix restores the original journal scope and pins pending attachment tabs. The earlier missing-source UI wait failed before any send; its log is retained. Final screenshot shows explicit re-selection and final journal proof shows successful same-receipt cleanup.
- Initial offline dependency resolution could not find local registry metadata; the normal online pnpm action succeeded. Frozen-lock install then passed. Only the direct existing `@noble/hashes@2.2.0` importer entry is added; unrelated registry metadata churn was removed. Initial typecheck exposed duplicate restored dependency versions, resolved by that frozen install. Later two local typing errors were fixed; current typecheck is clean.

Screenshots were captured and visually inspected from the actual running UI. They remain private local evidence in `/workspace/scratch/durable-evidence/`: `upload-complete.png`, `download-success.png`, `source-reselection-dark-zh.png`, `reselect-ack.png`. They were not uploaded to Library or a new PR.

## Root handoff / limits

No commit or push was attempted. This is the requested concrete pre-commit coordination handoff. The root remains the sole integration owner. Repository `packages/ui/skills/web/SKILL.md` lines95–98 additionally requires rendered human verification before any commit/amend/push; the current screenshots/interactions are agent verification, not human QA. This local requirement is recorded rather than silently bypassed or turned into a new product approval flow.

Keep old-task unknown history and both diffs if that task resumes. Root should reconcile any existing authorization/verification, recheck the old remote again, and apply only this reviewed patch to its combination line. No new PR or deployment is needed here.

Field NAS/Windows/Harmony, LAN/IPv6 selection, final signed client integration, real delegated download across processes and a real confirmed chat message remain outside this isolated proof. Existing native evidence is preserved separately. A conflicting already-open session remains untouched rather than silently adopting another session's upload. Existing0-size legacy attachment metadata is not repaired by this increment.
