# Persisted-target and state-driven recovery corrections

Two independent product patches address reproducible compatibility failures. Previously delivered766a and e7f91 commits remain unchanged.

| Patch | Product commit | Minimum semantic base |
| --- | --- | --- |
| Restore recorded contact target | `73730b91c9e7993f9d8d02666ac12e6d40bf8d24` | `766a99b1c5a331f595e864554be82d90e2d270ea` |
| State-driven saved-result recovery | `0bd268a7c0c91ef17ce9a2ab122a89c42614eb1b` | `e7f91d6b32c20f22e5edc3331a0a433d589ed9b5` |

The Git ancestry contains both, but each exact patch was independently checked against its listed base using an isolated Git index (`independent-apply.json`). The remote-target patch does not depend on deploying the recovery API. The state patch extends the earlier recovery API. Combined tested tree: `8b04d9556b54cf4bdae74a6503c7dfa38b442187`. No schema migration, official Agent-core or UI changes.

## Recorded target restoration

`remote-red.log` and the included regression source show a currently authorized saved remote mount A being replaced by changed primary B under766a. The old native-only branch left the continuation context empty. This was not a hypothetical issue and was independently reproduced by the integration owner too.

The correction restores a strictly parsed persisted selection on the owned root chat. `Authority.ContactSource` reuses current user, team, Bot/chat and session-ownership checks without imposing the cross-Bot device-profile registration contract on ordinary chat. It does not infer a target from runtime type, name, default or missing metadata. For a recorded remote target, current workspace_read is checked and the explicit request/context retain that exact target. The existing application/workspace resolver then checks the actual mount, registration, ownership, runtime capabilities and availability. Its missing/offline/revoked/owner errors are retained; no alternative Primary target is selected. The built-in Native target keeps its existing no-extra-workspace-read behavior. Explicit selections, workdir-owned bindings and external runtime/channel contracts retain their existing boundaries.

`contact-green.log`:21 cases across native regression/boundaries and remote changed-default, channel-less queue, ordinary bridge without delegation profile, offline/revoked/owner change/missing mount, read revocation and conflicting metadata. The real workspace manager resolves the recorded mount and surfaces the expected existing error; the store probe rejects any primary lookup. `authority.log`:5 affected authority groups/22 subtests preserve ancestry, both-Bot permissions, selection and revocation behavior.

## Recovery follows persisted state

`red-pg.log` reproduces two rejected valid states under e7f91: attention after the first summary was aborted, and a saved failed/unknown outcome. The API no longer requires attempt3 or a completed-only execution. It takes the original event and its actual current expected attempt. It rechecks persisted attention/publication evidence, current authority and the exact original invocation, including a run admitted before its run ID was attached. An active original invocation cannot be mistaken for missing execution. A saved failure/unknown with no admission can be reported as that outcome; no specialist run is created and unknown is never converted to success.

Summary policy is configurable through `[bot_delegation].result_summary_attempts`; omitted configuration preserves the prior default3. It is an automatic retry policy, not a recovery eligibility threshold. New events snapshot the configured budget; an existing snapshot survives configuration changes. Legacy events without that field use current policy for future automatic attempts. A policy change never automatically rearms attention. Every explicit audited recovery grants exactly one result-only summary, even when the prior interruption occurred below the automatic budget. Admission accepts only the current persisted pending receipt, not an older or caller-selected attempt. Numeric checks only validate representation/positivity/overflow, not an incident-specific attempt cap.

The supported endpoint remains:

```http
POST /bots/{original_bot}/sessions/{original_session}/delegations/{original_delegation}/delivery/recover
Content-Type: application/json

{"event_id":"<original persisted event>","expected_attempt":<current persisted attempt>}
```

A repeated body resumes the same authorization and cannot add another attempt. Committed publication rejects repeated recovery; an uncertain HTTP outcome must be read back before retrying. Existing trusted result-delivery V1/Web/native-Agent capability boundaries and original destination remain enforced. The original execution, failed run rows and audit are retained.

`green-pg.log`:6 new/affected real PostgreSQL cases, covering early interruption, failed/unknown outcomes, unknown with an unbound active invocation, configured budget/snapshot changes, one-attempt recovery and stale receipt/legacy isolation. HTTP/config tests pass, including configured1 and7 and invalid configuration. Earlier unchanged broad suites were reused rather than rerun. Swagger and SDK were regenerated; SDK typecheck, go vet, formatting, diff checks and backend build passed. golangci-lint is unavailable and is not claimed.

## Running application evidence

Web `http://127.0.0.1:28111/bot/local-dialog`, backend28090, loopback scripted provider28120, isolated schema181 PostgreSQL database `memoh_saved_result_recovery_20261030`. The two new cases were synthetic persisted fixtures: completed execution with an aborted first summary, and unknown with no original execution admission; both carried policy7 and their exact trusted Web route. No production records/configuration, paid calls or new schedules were used.

The actual API and Web delivered each original event once at attempt2, with one assistant receipt and one audit entry. Unknown remained unknown and its original execution count stayed0; the completed fixture retained one original execution. Both old aborted summary rows remained. The provider saw two result-only calls total, no tools and no other requests. An actual backend process restart and page refresh retained identical publication IDs and made no new model calls (`readback.json`).

For the remote case, this isolated contact temporarily recorded remote A while its previous primary was removed, making the ordinary default Native. A fresh authenticated WebSocket input omitted a target, as a continuation does. The resulting run failed with the matching server diagnostic `remote runtime is offline`; no native/model fallback occurred. The original metadata/primary flag was restored after the fixture probe. This verifies unavailable-target handling, not successful online remote execution. The different remote-primary B case is covered by the manager regression tests.

[Live saved-outcome reports](state-reports.png) and [reports after process restart](restart-reports.png) are actual inspected screenshots. The synthetic earlier chat history is retained, not duplicate recovery. Machine task envelopes were not rendered in the main chat.

Fixture caveats: an initial setup lacked the trusted return-route snapshot and was correctly rejected404 before any model call; only those newly seeded rows were corrected. The initial WebSocket observer filtered by invocation_id and missed the terminal event after run_accepted. We read that exact existing run and matching diagnostic instead of sending another message. `browser.py` preserves that probe; `readback.py` completed the final readback/restart checks. PostgreSQL fixture setup also corrected its per-source/target unsettled uniqueness and team context. None of these incomplete probes is counted as a passing test. Private configs/auth/raw logs are excluded; scripts require the existing private isolated harness. Final binary SHA: `6a358a30a14e8e6817748958cc9a244ba4e8704fe6590ec7d4ee51ba66f187ce`.

## Integration boundary

The unique field writer must run the integrated version before recovering the real event. Neither patch automatically changes that event or its failed attempts. Stop older delivery writers during normal replacement: older serializers do not understand new recovery/policy fields. Preserve latest history and receipts across rollback; do not run schema down or overwrite newer messages with an old database. The schema remains181.

Online remote A execution and integrated field recovery remain unverified here. The earlier A/B field timing still indicates the supervisor's own run outlasted the specialist; this correction does not change session concurrency or claim to improve model/tool duration. No new UI or session creation/reset behavior was introduced.

- [ ] Human QA passed
