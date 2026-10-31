# Native producer integration checkpoint

This checkpoint merges source `03e4fb6220a4b9f13f2b642e8dd3acaca7545a48` into the previously verified unified head `e75341d8a943ff350f516b148e71073650576b71`. The merge is `384414933c8734b5e5bca663b99d9d7e292b4389`, tree `9e0a6666165f19a4c7ceb521e68a00ff5c5af825`; there were no conflicts. The original source commits, fa77, scheduled PC binding and account-only locator guard remain ancestors.

## Change and preserved contracts

Native completion and bounded collection wakeups invoke the existing producer CLI with explicit `producer.program_path`, `producer.config_dir` and `producer.state_dir`. Agenvo keeps its original hook. The producer implementation, original queues, task/operation identities and official Agent/Runtime cores are unchanged. All 370 HTTP operations remain. Swagger/SDK regeneration has zero additional differences. Schema remains 176; this increment contains no SQL change or migration.

The new real CLI/PG regression was not selected by the existing operation gate. The integration adds only its exact name to that gate's selection and mandatory-PASS list. Existing race execution, 90-second limit, operation/CAS/RLS cases, no-skip enforcement and the separate Pi history gate are unchanged. This is not a CI timeout increase or a response to the source's earlier Windows timeout.

## Executed verification

| Check | Actual result |
| --- | --- |
| `go test -p=1 -count=1 -timeout=180s ./internal/agenvo ./internal/computertask ./internal/userruntime` | Exit 0, all three packages. This includes the generated completion script with synthetic Pi output and the production collector; it is not live Windows/Herdr acceptance. |
| Existing isolated PG, selected native/admission/history tests plus `TestNativeProducerCLIUsesPinnedDirectoryPairAndRecoversUnknownCommit` | Exit 0, 16 top-level and eight subtest PASS, zero SKIP, 50.899 seconds. Local execution is non-race; the final remote gate is race-enabled. |
| New CLI/PG recovery contract | PASS in 3.50 seconds: committed upload with lost acknowledgement, repeated collector processes recover the original delivery, one Create/Commit/Resolve, original operation and legacy directory bytes retained. The test uses real production CLI processes and isolated synthetic TLS/PG/files. |
| `TestNativeHerdrConfigurationRequiresAccountSession` | Exit 0. Normal account 200/one write; revoked account 401/zero writes; chat-route, revoked chat account and query chat token 403/zero writes; anonymous 401/zero writes; no-store retained. |
| Swagger/SDK generation; SDK and Web `typecheck` | Exit 0; no generated drift and no type errors. An initial mistyped `type-check` command returned success without selecting a script; its output is retained and is **not** counted as verification. The corrected Web command actually executed `vue-tsc`. |
| Server build and current application | Exit 0. Reused the existing task-owned loopback Server, synthetic database and Web dev server. No schema migration was invoked. |
| Actual Computers page and database readback | Passed: account saves all three producer references, the exact tuple persists, API and UI expose the same Runtime identity and Offline state, anonymous PUT is 401/no-store, no page errors, no Runtime key issuance or PC/Herdr/model launch. |

The browser used the existing isolated page at `http://127.0.0.1:18402/settings/runtimes` and Server at loopback port 18400. [The inspected screenshot](native-registry-offline.png) shows synthetic records only. The new unmounted fixture row was removed by exact ID/name/unusable-token match after checking the actual binding table. An initial cleanup precheck used a nonexistent table name and failed before any deletion; that failure and the corrected result are recorded. Existing database records, queues, configuration and original files were retained.

Readable logs, result JSON and hashes are included. Text copies remove ANSI escapes and normalize the known checkout prefix to `<checkout>` and terminal blank lines; they are not claimed byte-identical to their raw originals. No private Server/configuration log, real credential or device address is included. These are automated checks, not human QA or field acceptance.

## Windows handoff and remaining acceptance

The Windows owner reports that the existing package can supply these values through configuration without a new endpoint build:

| Native profile field | Existing host value |
| --- | --- |
| `producer.program_path` | `transfer.pluginRoot` joined with `transferqueue.exe` |
| `producer.config_dir` | Existing `transferConfigDir` |
| `producer.state_dir` | Existing `transferStateDir` |
| `work_dir` | Existing `sourceRoot`, equal to `transfer.json.source_root` |

All three producer references must be canonical local Windows absolute paths. Send the full existing profile with the added fields. They enter the new task fingerprint; do not redirect or re-enqueue old tasks. This mapping is a cross-team handoff, not a claim that this cloud environment inspected the user's host. The host's additional `/tmp` Go probe was denied execution; its three assertions did not run and are not counted as passes here.

Actual Runtime enrollment, credential authorization, positive TLS acceptance, host directory/queue binding, real Pi results and phone/PC end-to-end recovery still require the coordinated field acceptance. No production mount was deleted, no credential was created and no deployment or image publication was performed by this integration step. Keep old connection/mount records; never use the generic migration CLI `down` as a single-step rollback of 0176. Live native pauses still block its down script.

After exact final-head CI succeeds, the existing candidate image workflow is `.github/workflows/docker.yml` on the unified branch with `validated_sha` equal to that full final SHA. Its `validate-ref` requires equality with `GITHUB_SHA`. Docker PR validation does not publish images; candidate publication does not authorize NAS deployment or main/latest promotion.

## Bounded Windows verification budget correction

The final integration's first Native attempt failed in the unchanged aggregate Windows source-test process, before CLI/PG/package stages could run. The [retained step output](native-attempt1-step.log) ends with the package-wide five-minute alarm, not a preceding assertion failure. This is a CI execution limit, not evidence of a field device fault or authorization failure.

| Evidence | Observed result |
| --- | --- |
| Source `2030c2d`, run 40190700957 attempt 1, job 121160882054 | Aggregate timeout at 300.06 seconds. Earlier passing tests included PublishCancelRace 77.04s, BucketBudget 74.83s and MigrationProcessCrash 47.85s; the current test had run two seconds. Existing source evidence is retained in `docs/evidence/native-herdr/windows-ci-2030c2d.json`. |
| Integration `3a3cd00e`, run 40199820161 attempt 1, job 121188411058 | Aggregate timeout at 300.030 seconds. BucketBudget passed in 109.72s, ExplicitMigration in 54.16s and PublishCancelRace in 28.98s. MigrationProcessCrash had run 44s; its last started subcase was at zero seconds, in a Windows FlushFileBuffers stack. |
| Passing e753 run 40193260634 | Whole job 1,161s (19m21s); source Windows aggregate 239s; packaged verification 308s across its existing groups. |
| Passing source 03e4 run 40199092938 | Whole job 512s (8m32s); source Windows aggregate 119s; packaged verification 154s. |

The two source deadline failures and the measured runner variability justify ten minutes for the **unchanged** remaining Windows aggregate. The packaged verifier repeats those same native publication/catalog cases, so only its corresponding aggregate receives the same ten-minute deadline. All other per-process deadlines remain unchanged. The whole-job limit increases from 25 to 35 minutes: two aggregate limits each gain five minutes, preserving the original overall headroom. Both process and job limits remain finite.

No test is moved, removed, skipped or weakened. No production timeout, TLS behavior, filesystem flush, persistence code, credential or permission changes. `budget-check.json` records a command/selector comparison after removing only the changed budget literals and YAML comments; all executable lines otherwise match. `windows-budget-timings.json` contains the read-only GitHub step timestamps reduced to durations.

One job-specific retry of the unchanged 3a3 head was accepted before the budget correction was requested. It is not the correction and must not be used as the new candidate's acceptance. The subsequent branch update uses the existing concurrency cancellation policy; it does not repeatedly retry the five-minute limit. The original five successful workflows are retained as historical evidence, while publication must use the corrected exact head's checks. No unrelated local business suite or UI check is rerun for this verification-only change.
