# Controlled recovery of exhausted saved-result reports

An already completed specialist could have a saved result while all three supervisor-summary attempts failed. The old `POST .../delivery` only reconciled the exhausted budget and returned acceptance; it did not recover the missing report. This change adds an explicit, authenticated recovery of the saved result. It never calls delegation submission, creates a specialist session, executes the specialist, or resets its state.

Product: `e7f91d6b32c20f22e5edc3331a0a433d589ed9b5`, based on the unchanged native-continuation fix `766a99b1c5a331f595e864554be82d90e2d270ea`. Integrate this product commit after766; documentation is a separate optional commit. No official Agent-core, UI, SQL/migration or schedule configuration changes. The generated OpenAPI and SDK are included; their generation and SDK typecheck passed.

## Supported operation

Use the existing source user's authenticated API session and the original source Bot, source session and delegation IDs:

```http
POST /bots/{source_bot}/sessions/{source_session}/delegations/{delegation}/delivery/recover
Content-Type: application/json

{"event_id":"<exact original saved event>","expected_attempt":3}
```

The operation is restricted to already completed native Web V1 delegation results. `expected_attempt` must be an exhausted attempt of at least3. A new grant requires the event to be `attention`, no published message, a proven terminal previous summary invocation, and the exact completed original specialist run. Missing/uncertain admission, an active previous summary, changed identity/event/recipient, a published receipt or inaccessible authority cannot mint a new grant. Failed/unknown specialist executions, legacy delivery and other channels are not opted in.

The existing JSON compare-and-set appends an audit entry with the requesting user/time and previous terminal run/state. Attempt3 becomes4; attempts1–3 and all original run rows are preserved. Each explicit grant authorizes exactly one extra summary, without reopening the automatic three-attempt budget. Native ingress supplies the same deterministic queue/invocation ID to concurrent or repeated requests for the same event+attempt. Only saved result data is reconstructed; the summary turn has no action tools.

HTTP202 is acceptance/status, not proof the user received a report. Repeating the exact body after an uncertain response resumes that same authorization, including while its summary is active; it cannot grant attempt5 if attempt4 fails. A separate explicit authorization with expected_attempt4 is needed after a proven terminal failure. New grants while an attempt is active and already-published results return409. Wrong ownership/access returns404 through the existing handler policy. The response contains event state, monotonically increasing attempt and recovery audit.

Permission, owned conversation, both Bot grants, source placement and saved return route are rechecked by existing authority on recovery, queued admission and final publication. The old original execution is read only. A receipt committed with the final assistant message wins even if its acknowledgement or subsequent run completion was lost.

## Evidence

- `pg.log`:11 new real PostgreSQL subtests. Eight concurrent authorizations keep one audit/attempt; uncertain enqueue then a fresh connection/host resumes the same receipt; a failed recovery does not grow the budget automatically; a second explicit grant remains monotonic; stale grants conflict; committed receipts win before acknowledgement and after reconnect; active/missing previous admission and an active original worker conflict; wrong event/source/owner, revoked authority, changed recipient, legacy/unknown and ungranted higher attempts are rejected. Original execution count and failed rows are retained. The failure injection substitutes only the ingress port, not PostgreSQL CAS or receipt observation.
- `http.log`: authenticated route, malformed/too-small/overflowing attempts, conflict mapping, revoked access and legacy/unknown exclusion; no Submit/Run/CreateSession/ordinary Deliver calls.
- `red.json`: on exact766 binary, new recovery endpoint404; existing delivery endpoint200 left the same synthetic event attention/attempt3 and no report.
- `green.json`: actual running backend, Chromium Web and schema181 PostgreSQL clone. Four concurrent authenticated recovery requests returned202, but only one loopback scripted summary was called. A new grant while active returned409. The live original chat displayed one final report, assistant receipt count1, attempt4/report_state reported. Original specialist execution, delegation count and failed history were unchanged. No tools were offered. Repeating after publication returned409. An actual backend process restart retained the same message/run/receipt and made no additional model calls.
- `final-readback.json`: after adding generated OpenAPI/SDK only, the exact final product was built and run; it served the new API contract and displayed the same single report without further model calls. The behavioral red/green binary differed only in the subsequently generated API contract/comment. Final binary SHA is recorded here and in RESULT.json. Backend build used `-buildvcs=false`; source/tree/binary are recorded explicitly.

Environment: Web `http://127.0.0.1:28111/bot/local-dialog`, API28090, loopback scripted provider28120, isolated database `memoh_saved_result_recovery_20261030` cloned from the earlier native-continuation fixture. Zero enabled schedules and no active runs at runtime setup. Zero paid requests. No production IDs, credentials, private configs or raw server logs are included. Scripts reuse the existing private local fixture helper and are not standalone credentials/bootstrap bundles.

Screenshots were captured from the real application and visually inspected: [before recovery](red-attention.png), [live report](green-reported.png), [restart](green-restart.png), [exact final product](final-reported.png). Earlier synthetic chat messages are retained history, not duplicate recovery executions. The backend assertions supply provenance/receipt evidence; screenshots alone do not establish it.

Fixture preparation initially used a duplicate Bot name and shared specialist session, then corrected those fixture constraints before11/11 passed. An initial runtime probe used the wrong schedule table name and a wrong build flag; both failed before the recovery request and were corrected. They are not counted as passing tests. Unchanged previous broad suites were not rerun. golangci-lint is not installed; it was not claimed as run. go vet, gofmt and diff checks passed.

## Integration and field boundary

Deploy the native workspace fix and this increment before the unique field writer requests recovery. Stop old result-delivery writers as part of normal replacement; do not mix an older writer with newly authorized attempts above3. This is additive JSON on schema181, not a database migration. Older versions reject higher receipt attempts and can drop unknown recovery-audit fields when rewriting an event; rollback while recovery is pending must therefore keep those writers stopped and preserve the latest database/receipt history for a compatible forward recovery. Do not execute a schema down or restore an older database over new messages.

For the real original event, use the same original source user/Bot/contact/delegation/event and expected_attempt3. Do not modify its database row directly. If the HTTP outcome is uncertain, read its durable publication/attempt before retrying the identical authorization. Successful field acceptance requires the original event reported with one persisted assistant receipt in the original chat, one new audited summary attempt, all original failed runs and the specialist run unchanged, and no repeated expert task. A stale/published409 is not a reason to create another task or choose another event. No field operation was performed here.

This increment does not change main-chat serial admission. Parent-provided field timing showed B's run created about13ms after supervisor A terminated; A itself ended about11.5s after the specialist. This supports supervisor-turn duration rather than a demonstrated post-terminal queue delay. Real tool start/end durations were unavailable. Natural supervisor responsiveness and the original production recovery remain separate acceptance items.

- [ ] Human QA passed
