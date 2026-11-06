# Durable task upload ready proof

Task transfer routes persist the server's exact ready receipt in the existing
`computer_operations.details.delivery_ledger` before acknowledging Complete or
Get-ready. The same operation revision CAS binds the original upload ID, delivery,
Bot, owner, session, target, device/instance, run, attempt/generation, hash, size
and MIME. This is not an API accepting a client assertion of a receipt.

The private ledger format is v4. HTTP delivery outcomes remain v3 and resumable
upload responses are unchanged. Migration 171 already rejects ledger removal or
version downgrade; migration 172 supplies the stable device credential store.
No new table, migration, credential, directory or service is introduced. Upgrade
all delivery writers together. A v3 writer must fail explicitly on a v4 row;
rolling back the binary does not roll back the proof or authorize deleting it.
Existing execution CAS writers preserve the complete ledger.

Recovery uses the original task endpoint and upload ID. After fresh device and
operation authorization, Get, repeated Complete and repeated task Create can
return the persisted ready result even after the staging receipt expires. The
receipt's original `expires_at` is retained; it is the staging expiry, not a new
lease or proof lifetime. The client already accepts ready results with that
timestamp. No extra upload, delivery, attempt or Pi invocation is created.
Automatic queue recovery here assumes its original upload ID was checkpointed
before completion. The separate unknown-Create case is unchanged and never
automatically reissued.

Ready proves prior publication, not successful association, continued existence
of the original, or retention pinning. The existing async Commit still verifies
Media and current grants before the durable committed ledger decision. Resolve
already recovers a committed decision without the temporary receipt. Only that
committed association permits existing queue payload release. A canceled or
unknown delivery is not eligible for cleanup.

Media publication and the operation database are separate transactions. If the
server stops between them, the original scoped staging receipt can still be
captured on retry while it exists. New task-held receipts survive TTL; for a
historical generic receipt that expired before capture, neither a
matching hash on disk nor a reserved ledger entry proves completion. That case
stays `receipt_unavailable`: automatic mutation stops, lifecycle/producer feedback
report unknown, and source/snapshot/journal evidence stays intact. A database
commit whose acknowledgment is lost is also unknown until a fresh read confirms
the proof; it is not blindly retried as a new business operation.

The proof itself does not bypass the 128 receipt limit. The subsequent
[task receipt lifecycle](delivery-upload-lifecycle.md) holds unconfirmed task
evidence and retires temporary receipts only after committed association. Proof entries are bounded
by the existing 128 deliveries/operation and 2 MiB operation limit. Physical power
loss, real PC/NAS trust configuration and production installation are not covered
by the isolated tests.

## Verification boundaries

`TestTransferReadyUnknownCASAndRestart` models a committed operation transaction
with a lost acknowledgment, including real context cancellation/deadline error
types. `TestTaskTransferReadyProofAfterStagingGC` uses real task HTTP handlers,
Media and Sweep, with a synthetic expired receipt clock; its DB is a fixture.

The `ready-proof-cli` subtest of
`TestComputerDeliveryPostgresAuthorizationCASAndPreservation` uses the full
formal migration chain, a NOBYPASSRLS PostgreSQL role, real device registry,
Server/Media handlers, TLS and the production CLI built from the tested source.
It drops a successful Complete response, or kills only the isolated CLI process
after server proof commit and before client checkpoint. It marks the synthetic receipt as historical generic policy (0), expires it
via real Sweep, recreates the server/Media objects,
and invokes fresh CLI processes. Both cases associate the original delivery with
one Create and one Complete. The no-proof cross-store interruption keeps unknown
feedback, journal/snapshot and source payload, without new uploads or commits.
This is a POSIX NAS/CLI fixture; native Windows tests remain separate, and a
process kill is not evidence of physical power-loss behavior.
