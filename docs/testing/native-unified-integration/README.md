# Native Runtime integration checkpoint

This is an isolated integration checkpoint, not a deployment or completed Windows cutover.

## Fixed inputs and preserved boundaries

| Component | Fixed source | Integration treatment |
| --- | --- | --- |
| Unified server/Web and producer 0.10.1 | `fa77a073fe6380d297474a6053a1445201bcab83` | Original parent, retained |
| Native Runtime to official Herdr CLI | `2030c2d522695f52a227f12421800fe075760813` (PR84) | Merge `92c8fb2b`; no textual conflicts |
| Scheduled workspace binding | `0ee7a42c22a5965e9d69ea9471729e6da0574d1f` | Merge `4ae3f402ac58814894a81580e6fd57f6c7384bd5`; four files, no conflicts |
| Harmony generated Web source | `4e3c4d279f7f3c7770843d0714682f3297253fd7` (PR82) | Consumer compatibility checkpoint, not a server merge: its root tree predates the unified server. Retain its source lock and mobile adaptations in the original mobile branch. |

The combined tree at `4ae3f402` is `6471042b6a2213a7750dab7870c348d98ae91c2d`. Existing 369 HTTP operations remain; the only added operation is `PUT /users/me/runtimes/{id}/native-herdr`. Swagger, SDK and sqlc regeneration produced no differences. `internal/agent/runtime`, `internal/contextview`, `packages/runtime`, and the producer source remain unchanged from fa77. No old queue, task, operation or mount is reassigned.

The source-only mobile workflow passed for fixed 4e3 (run 40186520445); contribution policy also passed (40186520460). This is not a newly built HAP or device test. Schedule Go 40185765745 and Docker PR 40185765773 passed for fixed 0ee7. Native Go 40190700983, ESLint 40190700998 and Docker PR 40190701025 passed, but Native delivery run 40190700957 failed at the remaining Windows transfer contracts step; later stages were not verified by that run. Source CI is not final combined-head CI.

## New integration finding

The new locator handler accepted a chat-route JWT for the same owner. In the real Server/JWT/handler chain with a synthetic store, it returned 200, performed one configuration write, and performed zero account-session validations. The account control returned 200 with one validation and one write. `native-account-probe.log` retains that failing assertion. No real credential, remote process, or production request was involved.

The integration fix rejects chat-route credentials at this new handler before reading or writing owner configuration. It reuses existing JWT classification and error handling, leaving account session validation and ordinary chat-token use unchanged. The regression covers normal account, revoked account, chat-route, chat-route with revoked account, query-token, and anonymous requests, including no-store. It does not change older Runtime management routes or introduce a new credential/permission scheme.

The fix is commit `b28ac71d96c58e089bacbf888ac175a452ef276d` (six product lines and one test file). Its tree before the integration test-gate/evidence commit is `6734851de989e75467e82d2b6bc7994bfb82223b`.

## Verification boundaries

See the accompanying command/result table and readable logs. Local PG fixtures use the existing isolated loopback test instance and allocate their own temporary databases. They cover 0176 admission durability, RLS, concurrent pause/claim/followup, configuration/transfer identity, transactional delivery, live-pause rollback refusal, preserved old operation identity, and disabling a retained legacy connection without OAuth calls. The initial local PG execution is non-race; the existing CI operation gate remains race-enabled and now requires the seven native fixtures without skips. The separate Pi history gate is unchanged.

The first concurrent lint and Web build both ended with exit 137; the environment reported OOM kills. Those results are retained, not counted as passes. Follow-up runs are serial. The initial pinned `go run sqlc@v1.31.1` invocation failed because the available Go 1.25.7 is below the generator's required 1.26; the already-installed exact sqlc v1.31.1 binary generated the same files without changing dependencies.

| Executed check | Actual result |
| --- | --- |
| `go test -p=1 -count=1 -timeout=180s` for nativeherdr, agenvo, computertask, userruntime, workspace/bridge, workspace, storage/providers/containerfs, schedule | Exit 0, all eight packages; `combined-go.log` |
| Selected `TestTriggerSchedule*`, `TestComputer*`, `TestHost*` in application/core | Exit 0; `assembly-schedule.log` contains the exact selected cases, not a full-suite claim |
| New handler account boundary | Initial red assertion retained; six green cases in `native-account-green.log`; final full `go test ./internal/handlers` exit 0, 4.560s |
| Isolated PG migration/admission/transfer/history | Exit 0, 15 top-level and eight subtest PASS, zero SKIP, 41.184s; exact selection in `postgres.log` |
| Web and SDK typechecks | Both exit 0 |
| Four shared task/target/observation test files | Exit 0, 80/80; `ui-tests.log` |
| Web production build | Initial 137; serial rerun exit 0, 5.21s, existing chunk-size warning retained |
| Scoped Go lint, v2.10.1 and existing rules | Initial 137. Serial run reported only three issues in the newly added handler test (format, unused receiver/parameter). Corrected those; final handler lint exit 0. Other scoped packages were unchanged after their clean diagnostics; this is not a new full-repository lint claim. |
| Current Server build and real UI | Build/start exit 0. The task-owned loopback Server and synthetic PG were reused; additive test migration ended at 176 clean. Actual Computers page and API showed the same saved native identity and offline state, with no device connected. Anonymous PUT was 401/no-store; normal account PUT was 200 without issuing a key. See `browser-proof.json` and screenshot. |

The UI fixture was a single newly inserted, unmounted native registry row with an explicitly unusable synthetic token, not enrollment. It was removed by exact ID/name/token match after verifying zero mount references. Existing rows, old mounts, operations, database files and source evidence were retained. The current synthetic Server remains running on its original loopback listener. No real Windows/Herdr process or model was invoked. The screenshot was inspected; its three displayed computers are synthetic test data.

Published text logs have only ANSI escapes removed and the known checkout prefix normalized to `<checkout>`. `manifest.json` gives original and published hashes plus the exact rule; these normalized files are not described as byte-identical originals. No private Server/configuration logs, tokens or connection settings were published.

## Remaining cutover conditions

- The original producer hook in the native batch collection path still requires the source owner's bounded CLI wiring. Do not enable the old plugin hook as a workaround or treat this pre-integration tree as a completed file-delivery chain.
- Retain legacy connection and mount rows. An existing `is_active=false` update must preserve the other connection fields. Deleting a mount can cascade into workdir/session/schedule bindings.
- Stop old writers before any approved switch. Do not use the application's generic migration `down` command to undo only 0176: it calls the full down operation. Native live pause blocks the 0176 down script; schema rollback is separate from reverting an image.
- No real Windows Runtime enrollment, WSS/TLS acceptance, host directory/producer queue configuration, old disk queue recovery, Pi execution, mobile package installation, or NAS deployment is established here.
- Reconnection must query the same operation. A transport acknowledgement, `not_applicable`, process exit, or end of a result page is not business or file-delivery success.
