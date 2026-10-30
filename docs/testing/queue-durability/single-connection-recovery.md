# Single-connection recovery correction

Product correction: `f13f0ea09374788ea028c02168f7d99874c43570`.

This increment follows `11e9b8a4d99dcdf2f7a85c4df505709a8da1e324` / product efac. The parent reports efac was excluded from the current4425 release because independent review reproduced this defect and candidate migration182 conflicts with the fast-reply migration. That report is not a cloud claim to have read the independent review's unretrieved exact test. This owner reproduced the defect independently below. Migration numbering is reserved for integration; no migration, table, credential, deployment, model request or old dump was changed in this increment.

## Cause and minimal change

`cmd/internal/core/durable_followup.go:reconcileFollowUpClaims` called `runs.GetByInvocation` inside `Store.Change`. `Change` held a PostgreSQL transaction/row lock and its pool connection. The real PostgreSQL ledger obtained another connection from the same pool. With MaxConns1 it waited for the connection held by its own caller until the test context expired. Earlier claim-authority tests used the single connection, but the recovery classification test used an in-memory ledger; that evidence did not cover this nested acquisition.

This is not an unclosed list cursor: generated `ListSessionFollowUpQueueKeys` consumes its result and defers rows.Close before returning its slice, and `Store.Keys` does not retain rows.

The fix reads the queue snapshot and original-invocation ledger results before entering the queue mutation transaction. Inside the row lock it compares the complete current item with the observed item, including claim identity and payload, before applying a resolution. A changed item/claim is preserved. Ledger failure returns without a queue write. Missing admission remains unknown, never an instruction to replay. Existing admitted/lost-run and promotion classifications are retained. No pool minimum, new product timeout, fixed attempt count or alternative execution identity is added.

## Narrow real-PostgreSQL red/green

`TestDurableFollowUpRecoverySinglePostgresConnection` uses the same pool for the queue and `ledger.NewPostgres(sqlc.New(pool), pool)`, MaxConns1, and the production `db.SetDefaultTeamOnConnect` hook. It creates/deletes only its own randomized queue row; its ledger lookups are read-only. It does not create a user, credential, session, run or model request. The two-second deadline belongs only to this regression's observation context.

The three subcases are:

1. Missing original admission: preserve original item, classify unknown and release all pool connections.
2. Replace claim after the real ledger lookup, before queue mutation: preserve the newer claim/status; do not overwrite it with the stale missing-admission conclusion.
3. Inject a ledger-read failure after the real lookup: preserve the exact error and claimed item instead of clearing, consuming or retrying it.

With the efac product bytes and the corrected production-like test connection setup, all three fail with `ledger(postgres): get run by invocation: context deadline exceeded` (red exit1, approximately6 seconds combined). With the fix, all three pass (approximately0.03 seconds combined). The directly affected existing `TestDurableFollowUpRecoveryKeepsUnknownAndOriginalLostRun` also passes; that test still uses an in-memory ledger for admitted/lost classification and is not mislabeled as a fully PostgreSQL-backed admitted-run test. Zero skips. The narrow tests did not rerun the prior process/model fixtures or broad suite.

An initial new-test attempt omitted the team connection hook: after removing the connection deadlock it correctly failed closed with `memoh.team_id is not set`. This was a fixture configuration error, not bypassed authorization. Installing the existing production hook corrected the test. The archived red and green logs use that same corrected setup; the preliminary fixture error log is retained separately.

The isolated target was the previously restored `memoh_queue_durability_recovered_20261030`, reached only via local PostgreSQL. The quarantined42-dirty clone and its dump were untouched. No down/up or schema alteration occurred. Prior migration-incident documentation and restore limitations remain in force.

See `single-connection/red.txt`, `green.txt`, `fixture-team-binding-error.txt`, and `result.json` for exact outcomes and product binding. Source test additions and the one recovery function are the only code changes. No official Agent-core files changed.

## Integration and remaining acceptance

Apply this increment on top of efac; do not publish efac alone with the known recovery deadlock. Integration owns renumbering the still-unreleased additive queue migration after checking its actual head. This fix does not edit182 or perform a migration. It is compatible with the queue envelope/table introduced by efac and retains its key, history, unknown and rollback requirements.

The parent-reported4425 release is independent and need not wait for this patch. Independent review of this increment and migration numbering remain before the later queue release. New process-restart, real UI/HAP and production acceptance were not performed in this narrow correction; prior results retain their recorded source/version limits. No tool operation is denied or uncertain, and no production action was attempted.
