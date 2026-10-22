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

Ready proves prior publication, not successful association, continued existence
of the original, or retention pinning. The existing async Commit still verifies
Media and current grants before the durable committed ledger decision. Resolve
already recovers a committed decision without the temporary receipt. Only that
committed association permits existing queue payload release. A canceled or
unknown delivery is not eligible for cleanup.

Media publication and the operation database are separate transactions. If the
server stops between them, the original scoped staging receipt can still be
captured on retry while it exists. If it has expired before capture, neither a
matching hash on disk nor a reserved ledger entry proves completion. That case
stays `receipt_unavailable`: automatic mutation stops, lifecycle/producer feedback
report unknown, and source/snapshot/journal evidence stays intact. A database
commit whose acknowledgment is lost is also unknown until a fresh read confirms
the proof; it is not blindly retried as a new business operation.

No permanent staging-receipt retention or quota bypass is added. The 128 receipt
limit and ready staging TTL still apply; safe capacity turnover is a later
increment using this proof and the committed ledger. Proof entries are bounded
by the existing 128 deliveries/operation and 2 MiB operation limit. Physical power
loss, real PC/NAS trust configuration and production installation are not covered
by the isolated tests.
