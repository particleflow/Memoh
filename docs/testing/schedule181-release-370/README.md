# Independent schedule181 release source

The local complete archive replaces multi-round assembly instructions with one complete, recoverable source candidate. It is a subsequent independent batch, not a change to the unified owner's in-flight fast-path or Harmony release.

- Reported field base: `370922014a2435b83d4a2c33cd0b0611bc85de11`, schema180. The parent supplied the deployment fact; this task read the exact public Git ref, not the live NAS.
- Final candidate: `37e3e8cf7cec162669701d572032c3d2639b9751`, tree `2844e708fd3b1030e15518e80cd384e76aec6ef1`.
- Normal pushed branch: `integrate/schedule181-after-370`.
- Original schedule owner candidate: `cd8c7dbbcb7e2c4e4d5d05f3058edda343d640f6`.
- Complete embedded UI submodule: `f69402a55dd87a59c10722fe11507ad8fd7048f5`.

## Recover the source without other handoff packages

`full-candidate.bundle` contains the entire reachable history, including370 and all seven combination commits; it has no prerequisite commit. `ui-submodule.bundle` contains the exact UI submodule history and detached HEAD. Neither contains local configs, credentials, binaries, screenshots or database backups.

In a new directory, use standard Git:

```sh
git clone --branch integrate/schedule181-after-370 full-candidate.bundle checkout
git clone ui-submodule.bundle checkout/packages/ui
git -C checkout/packages/ui checkout --detach f69402a55dd87a59c10722fe11507ad8fd7048f5
git -C checkout rev-parse HEAD HEAD^{tree}
git -C checkout status --short
```

The empty gitlink directory created by the parent clone is the destination for the second clone. These are source reconstruction commands only. No deploy, migration or model execution is implied. Dependency installation still uses the pinned lockfile and normal package tooling; the bundles are source-complete, not an offline dependency cache or signed HAP distribution.

`SOURCE-MAP.json` maps all six original schedule commits to their combination commits, plus the one test-only alignment commit. `increment-from-370.patch` and `commits-from-370.patch` are alternatives for an existing exact370 checkout, not additional steps after cloning this candidate. Do not merge the old dedicated branch wholesale or reapply main-chat commits already present in370.

## Composition and evidence

Six original commits, in order: 1e909d33f (trusted schedule origin/result), 55955839d (persistent due claim/recovery), 89200ec17 (pause row lock), 7a40b3ec2 (definite-busy refund), 1a776f9af (refund acknowledgement readback), cd8c7dbbc (registration generation). Their exact full IDs and new IDs are in the source map.

Only one manual conflict occurred: internal/taskresult/delivery.go Parse. The combined code retains370's errors.New and includes schedule_fire alongside the two existing kinds. The automatic merge in cmd/internal/core/task_result_delivery.go preserves370's lifecycle-owned signalResultDelivery and formatting. Those are the only two of33 schedule-source paths that differ from the original final candidate; the other31 are byte-identical. Web, SDK, official Agent core, channel, handlers, sessiontask, computertask and agenvo trees remain byte-identical to370. Historical incremental migrations through180 are unchanged.

A further test-only commit changes the full-chain upgrade guard's target/name from180 to181. Its existing preservation assertions remain intact. No additional product behavior or UI was developed.

The evidence matrix separates historical checks at their original revisions, current composition checks, inherited370 source without supplied external run receipts, and remaining matching-version acceptance. Old report files retain their historical unfinished lists; the matrix supersedes those lists where later evidence closes them. Do not treat an old report's 'next step' as a new request.

New checks are limited to composition seams: package compilation, current schedule_fire unknown dispatch against real PG, the modified upgrade guard, a real forward-only180→181 migration of an isolated old clone, and current application history smoke. Old wide suites, schedule race suites and model scenarios were not rerun. sqlc regeneration leaves the composed tree unchanged. The product binary was built at c45cbcaa8; the final37e3e8cf7 differs only in the test's name/version assertion.

Current browser smoke: http://127.0.0.1:28109/bot/local-dialog, upgraded isolated DB, fixed Bot entry and Tasks, retained legacy failed fire visible without a fabricated destination, zero page exceptions or task/schedule writes. Screenshot locally inspected at /workspace/bot-dialog-handoff/schedule181-release/legacy-history.png; excluded from Library. Initial dev startup lacked the uninitialized UI submodule; initializing the pinned submodule and reinstalling locked dependencies fixed it without product edits. A baseline dialog accessibility warning remains; no new small print/control was added.

See FORWARD-AND-RECOVERY.md before future authorized release. This package is not a production acceptance receipt or permission to interrupt the active release batch.

## Remote handoff fallback

The official large-file Library workflow stopped at hosted-app tool discovery with HTTP401 before upload preparation. It was not retried or replaced by direct upload. This documentation branch publishes only the already-authorized repository handoff, not the archive or a substitute Library upload. Full product source is reachable at branch integrate/schedule181-after-370 / commit37e3e8cf7cec162669701d572032c3d2639b9751. Fetch that exact commit and initialize the pinned UI submodule through normal Git if the local standalone bundles are unavailable. All substantive reports, source mappings, old-evidence receipts and new seam/forward results are present here. Source files are already in the candidate commit; no multi-round handoff reconstruction is needed. LIBRARY-STATUS.json records the precise archive and unresolved save failure. The source candidate itself is unchanged by this documentation-only branch.
