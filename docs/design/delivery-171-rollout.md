# Delivery migration 0171 deployment boundary

This is a review-only dependency of the disabled delivery candidate. It is not
authorization to migrate or enable a production system. The independently
reviewed client, operation store, async handoff and configuration changes must
be integrated together before any production activation is considered.

## Upgrade

1. Stop all operation/delivery writers, including old processes and background
   jobs. Record the schema version and take the existing database backup.
2. Review the existing delivery ledgers before changing the version. Version 1
   upload terminal facts can be read by the candidate; version 1 download facts
   cannot be promoted automatically because cancellation did not attest a local
   publication fence. Preserve and reconcile them separately. Do not delete or
   rewrite ledger fields to get past a version error.
3. Deploy the fully integrated writer set and apply migration 0171 using the
   existing migration mechanism. The trigger rejects deletion, non-object
   replacement and version downgrade of an existing ledger. It does not make
   old binaries compatible. Current execution SQL must preserve the ledger and
   use the existing operation revision CAS.
4. Verify schema version/clean state, fresh and upgraded database behavior,
   permissions, mixed-writer rejection and read/write consistency before enabling
   any reviewed route. PC identity, HTTPS and destination storage activation are
   separate prerequisites. No production activation is performed by this patch.

## Rollback

The 0171 down migration removes the trigger and its function only. It does not
downgrade version-2 ledgers, remove publication outcomes, or restore compatibility
with old writers. A version-2 ledger must continue to fail closed in old readers.

Stop all writers before a rollback. Prefer rolling back the disabled feature
while keeping the preserving SQL and the guard. If a schema downgrade is required,
first reconcile outstanding scans and local publication outcomes, preserve their
facts and review a compatible data migration or backup restoration. Do not run
old writers against live delivery ledgers after dropping the guard: an old
UPDATE can silently erase them. Do not automate schema rollback as feature
disablement, and do not reuse delivery IDs to hide an uncertain outcome.

Read-only status validation cannot prove a rolling deployment is safe. CI uses
isolated databases and verifies up/down/up, but production readiness requires
the complete integrated writer set and separately approved PC security bindings.
