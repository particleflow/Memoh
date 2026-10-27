# Paused native profile cutover

The owner endpoint `PUT /users/me/runtimes/{id}/native-herdr/profile-cutover`
changes only `herdr.agent.config_dir` after checking the current configuration
digest, target fingerprint, instance and paused admission request/revision.
It performs no host action. Existing operation fingerprints are retained.

A successful cutover returns HTTP 200 with the existing `Admission` shape,
including `paused: true`, the unchanged `request_id`, and a **new** string
`revision`. Every successful cutover increments that revision in the same
transaction as the configuration update. A rollback changes neither value.
The caller must retain the returned revision for its next cutover or eventual
resume through `PUT /users/me/runtimes/{id}/admission`. Do not retain the
pre-cutover revision in the Windows/NAS coordination state. Admission must
remain paused until both configurations are ready.

A sequence A -> B -> A therefore does not revive the first A -> B request:
its old revision conflicts even though the digest and fingerprint match again.
After a lost acknowledgement, read the owned runtime configuration identity
and admission state through their existing GET endpoints before deciding what
happened; do not blindly replay a write. Ordinary paused profile PUTs remain
rejected, and no configuration write authorizes execution of historical tasks.

This correction updates unreleased migration 0178 and canonical 0001. A database
that already applied the earlier development copy of 0178 needs an explicit
migration upgrade during integration; rerunning a migration runner alone will
not apply a changed file. No deployed production migration was changed here.
