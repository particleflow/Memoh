# Bounded resumable admission

Follow-up to PR #13 at c616db12. The two-minute Herdr action budget remains.
Admission must survive that budget and process death, without re-reserving a
job, restarting its full copy, or retaining quota without a cancellation path.

A matching logical delivery may resume an unadmitted reservation. Immutable
operation/Bot/spec/locator fields must match; conflicting reuse fails. An
admitted replay is idempotent. No upload HTTP request is possible before final
admission. Canceling a partial admission records local cancellation; retirement
then releases its private artifacts and reservation under the same locks.

Upload admission gets one bounded 8 KiB progress file, containing two checksummed
4 KiB slots. Flush data and tree before writing/flushing the next slot. A torn
new slot falls back to the preceding valid slot; unacknowledged tails are
truncated. Slot version, delivery binding, monotonic generation, offsets,
serialized SHA-256 state and open-file identities are checked. Journal growth
is independent of admission retries. Existing unadmitted schema-2 records lack
this binding: explicitly reject resume and allow local cancel/retire instead.

Each action copies at most 64 MiB using 1 MiB buffers and checkpoints every
8 MiB (also at cooperative cancellation). It resumes tree construction in bounded
batches after verifying the full expected source hash. A persisted Merkle
frontier binds copied blocks: the completed on-disk tree must have that root.
This preserves integrity across resumes without rereading the entire prefix.
A changed source tail fails the expected full hash. A changed private snapshot
block fails its later proof; a changed tree fails the frontier/root comparison.

Finalization has a durable sealed phase before readonly metadata changes; a
restart can finish sealing the same verified files. No physical path or secret
is stored in the progress record. The same queue root and ownership checks apply;
this is transfer progress, not another task database or directory registry.

Tests cover deadline, process exit, same-ID continuation/conflict, torn slots,
unacknowledged tails, source mutation, resumed tree/finalization, cancellation
and quota reclamation. Windows binding remains a separate patch; these tests
must not claim native Windows or real PC/NAS acceptance.
