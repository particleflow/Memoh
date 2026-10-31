# Retired Agenvo records and native Runtime cutover

The production computer-task service uses the existing official Memoh Runtime
Exec transport to Herdr/Pi. The legacy Agenvo create/discover/configuration
routes are removed, `provider=agenvo` is rejected on create, and the owner,
Bot-target and account-grant lists select only `provider=memoh`. An active old
MCP connection does not make an Agenvo registration executable or selectable.
The shared resolver rejects an explicitly selected retired target before any
MCP/OAuth call and never substitutes the current primary computer.

## Preserve original history

Keep the old connection, OAuth row, runtime registration, mount, workdir,
session, schedule history, file credential, queue and operation records.
Connection deletion cascades into OAuth; runtime revocation removes mounts;
mount deletion can remove workdir bindings and clear session/schedule workdir
references. None is a retirement cleanup operation. A saved `unknown` operation
retains its original identity, revision, fingerprint, run, attempt and generation.
It cannot be replayed or copied to the native Runtime.

The existing MCP Enabled switch can disable an exact retired connection after
its source Bot and ID are identified. This is a reversible configuration action,
not required to make retired computer-task routing reject Agenvo. General MCP
management is a separate boundary: do not invoke Test, OAuth authorization or
changed-endpoint saves to repair the retired computer path. Other OAuth clients
must remain untouched. An already accepted request cannot be recalled by a flag.

## Bind future work explicitly

Inventory only the affected Bot, old mount, workdir, session and schedule IDs
using authorized metadata. Retired runtime/grant selector lists deliberately
omit old IDs; use existing saved records for historical mappings rather than
expecting those selectors to rediscover them.

Configure an already activated, owned native Runtime through
`PUT /users/me/runtimes/{id}/native-herdr`, following the
[native Herdr contract](native-runtime-herdr.md). Reuse the installed official
Pi/Herdr/Node/Git and the existing approved Magpie profile. No new provider or
legacy OAuth transport is enabled. A native Runtime UUID remains distinct from
old Agenvo connection/device IDs and their original file bindings.

For future work, use the existing Bot mount/grant and primary-target interfaces
to select the verified native mount. The literal target ID `native` denotes the
Server workspace, not the Windows Runtime. Changing a primary never migrates a
bound session or workdir. Preserve old records. An old workdir may be archived
through the existing workdir endpoint after its exact ID is verified; do not
substitute mount deletion or runtime revocation.

Pause only schedules whose future execution still references an old binding.
Update each schedule's complete existing execution block under its original ID,
preserving model, reasoning and time-budget fields. A new-session schedule uses
the explicitly selected native workdir; an existing-session schedule needs an
explicitly native-bound session. Re-enable only after verifying its target.
The native Bot delegation authority plan is owned separately; this procedure
adds no bypass, implicit grant, schedule replay or admission override.

Per-schedule live acceptance, the PC configuration migration and actual Windows
installation belong to the designated onsite writer. Source audits or an offline
report do not establish their completion.

## Source evidence

`internal/db/native_runtime_cutover_integration_test.go` verifies native-only
selection, retired registration/grant/transfer rejection, zero legacy transport,
and preservation of original rows and unknown operations in isolated PostgreSQL.
The source checkpoint is recorded in
[evidence/agenvo-project-retirement/checks.md](evidence/agenvo-project-retirement/checks.md).
Its results are historical evidence for that recorded commit, not a current
PC/NAS, browser or schedule acceptance claim.
