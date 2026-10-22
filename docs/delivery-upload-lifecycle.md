# Task upload progress leases

The generic `/bots/{bot_id}/media/uploads` contract remains the fixed 24-hour
receipt lifetime documented in [resumable uploads](resumable-uploads.md).
Expired ready receipt collection does not delete the published original. A
receipt is temporary reconciliation evidence, not the original's identity.

New uploads through the verified `/internal/computer-transfers` task adapter use
`UploadStore.CreateTransfer` after the existing operation authorization and
receipt-creation claim. The private receipt records `progress_lease: 1`; no client
flag, new endpoint, credential, filesystem root or registry is introduced. The
method itself is not an authorization mechanism. Ordinary Create and receipts
without this field retain the old behavior, including existing task receipts.
No receipt is recreated or silently upgraded.

For new task receipts:

- Creation reserves the same bounded capacity and sets an initial 24-hour deadline.
- A verified positive-length append renews the deadline to at least 24 hours from
  accepted progress. Offset and deadline are saved in the same atomic receipt,
  after payload fsync. A failed/unknown save is reconciled by the original receipt
  ID; a visible rename is not acknowledged before directory-sync recovery.
- First successful original publication changes the receipt to ready and provides
  at least another 24 hours for completion-response reconciliation and association.
  Replayed complete does not renew it. This does not prove NAS ledger commitment.
- GET, rejected/replayed chunks, incomplete/failed completion, canceled requests
  and cancellation do not renew. The original quota and maximum file size still
  apply. The deadline never moves backwards when the server clock moves backwards.
- At the persisted deadline, idle receipts are still inaccessible and collectible.
  Startup, hourly cleanup and pre-Create cleanup use the same receipt lock and
  Sweep policy. Busy appends/completions hold that lock until the durable update.

Progress can continue beyond the original creation deadline while each accepted
chunk arrives before its current inactivity deadline. This is not unlimited
offline resume: a 24-hour idle gap still expires. There is no total wall-clock
duration cap on an upload making bounded, positive byte progress; active/session,
owner and byte quotas remain enforced. Clock jumps forward can still expire an
idle receipt; monotonic time cannot provide a cross-restart timestamp guarantee.

## Compatibility and remaining lifecycle work

The public Upload JSON is unchanged. Existing clients already replace their
cached `expires_at` from each authoritative response and retain the same upload,
delivery and operation IDs. Existing receipts are not automatically adopted into
the new policy. Unknown policy versions fail explicitly and are not swept.

All writers sharing the originals root must run the same lease-aware version.
An old writer does not understand the new private policy field and could drop it
on mutation; mixed-version writing or downgrade with active leased receipts is
unsupported. Drain such receipts before rollback. This change requires no SQL
migration and does not move roots, alter production configuration or enable
device authentication.

This increment does **not** solve the 128 unexpired NAS receipt limit, retained
Windows receipt/publication limits, unknown creation after response loss, or
ready receipts waiting longer than the new reconciliation window for a committed
NAS association. It does not pin all ready receipts or erase unknown/blocked
evidence to make capacity. The next retirement step must first persist the exact
ready proof in the existing authorized operation ledger, then remove only its
temporary receipt with replay resolving from that durable proof. It must cover
proof/deletion crash boundaries before enabling automatic turnover.

## Evidence scope

Synthetic virtual-clock tests reproduce the original 23-hour-progress/25-hour
collection failure and verify all three cleanup call paths, including reopening
the file store for startup. These exercise the shared Sweep operation; they do
not claim to start the production FX lifecycle or wait 24 real hours. Failure
tests cover rejected activity, idle expiry, payload/journal fsync failure,
unknown post-rename acknowledgement, backward clock movement, legacy expiry and
expired ready collection with intact original bytes/hash. A real HTTPS task
handler fixture checks that authorized Create selects the progress policy while
receipt isolation and revocation remain enforced. Credentials and data are
synthetic; these are not physical power-cut or real PC-to-NAS acceptance results.
