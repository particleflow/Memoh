# Independent durable follow-up queue review — efac

Source `efac3b837b6c5291c09a714dddb4b73251367361`, parent
`29685958ec896489371a894b6854ee8b2e9df9c5`. Read and reuse the author’s
`11e9b8a4d99dcdf2f7a85c4df505709a8da1e324` evidence. Source is **not admitted**
for combination pending the actual single-connection recovery defect below.
No source implementation was modified and no field, provider or paid call ran.

## P2: startup claimed-item reconciliation waits for its own pool connection

`cmd/internal/core/durable_followup.go:reconcileFollowUpClaims` calls
`Store.Change` and queries `runs.GetByInvocation` inside its callback.
`internal/followupqueue/store.go:Change` already owns a pool transaction until
that callback returns. With a configured one-connection PostgreSQL pool, the
real PostgreSQL ledger cannot acquire a connection. Recovery returns a context
deadline error and cannot classify the retained claimed item. The author’s
one-connection permission test does not exercise this nested real-ledger path;
the published core reconciliation test uses an in-memory ledger.

The [independent test](single-connection-counterexample_test.go.txt) uses the
actual Store, Backend, reconciliation function and PostgreSQL ledger. Both queue
and run relations are session-local TEMP tables, with an explicit pg_namespace
guard; no permanent row is queried. LIKE copies only the existing run schema.
A random synthetic tenant is set on that one fixture connection, not a production
RLS override. The direct same-pool ledger control returns ErrRunNotFound as
expected. Reconciliation should then retain the item as unknown without a
second connection. Instead [the accepted output](single-connection-red.log.txt)
returns `ledger(postgres): get run by invocation: context deadline exceeded`,
exit1, 0.53 seconds. The 500ms context bounds this test observation only; it is
not a new production timeout or task cancellation policy.

An [earlier tightened-control setup](initial-control-missing-tenant.log.txt)
failed because memoh.team_id was absent. It is retained and excluded from the
accepted reproduction; the final explicit tenant and direct-ledger control fix
that fixture defect. All temporary tables disappear on pool/connection close.
No migration, database down, business-table write or schema force was performed.

Run only this counterexample on the exact source by copying its Go text into
`cmd/internal/core/durable_followup_independent_review_test.go` and supplying an
already-authorized isolated loopback DSN as TEST_QUEUE_REVIEW_DSN:

```sh
go test ./cmd/internal/core -run '^TestIndependentClaimRecoveryWithSingleConnection$' -count=1 -v
```

Use the prepared official Go toolchain/cache. Do not print credentials, use a
production DSN or repeat author suites. Original owner should move ledger
observation outside the held queue transaction (or reuse that transaction), then
CAS the unchanged original claim/item under the existing lock. Retain unknown,
payload/origin, token and run identity checks. Raising pool size or dropping a
claimed item is not a correction. Include concurrent changes in the follow-up.

## Composition and recovery boundaries

Presence already occupies migration0182 in the unified line. This undeployed
queue increment must receive the next available number during qualified
integration, preserving both canonical tables and generated models. Do not
renumber deployed migrations, alter schema_migrations, run global down or restore
an older dump over new records. The author’s destructive global-down accident
and later experimental metadata loss are retained in its README; its repaired
fixture baseline is not a reconstruction of all lost experimental metadata.

The at-rest capability reuses existing auth.agent_credentials_encryption_key;
no new key is generated. Normal credential-free HTTP commands do not require a
new secret. Credential-bearing writes fail without the existing capability;
missing/wrong decrypt capability must remain an error, not an empty queue.
Before field upgrade, the sole writer checks only capability presence/validity,
never exports the value, lets old memory items naturally finish, and retains
key/ciphertext/queues/receipts. Old memory-only consumers cannot safely consume
pending durable or unknown rows. Restore by a compatible forward consumer,
not deletion, destructive down, old dump or unknown replay. Real UI/field restart,
Redis/cross-instance orchestration and retained-row performance remain unverified.
