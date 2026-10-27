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

## Historical metadata after cutover

`computer_task_get` and the existing session-task projection continue to read
persisted operation state and receipt metadata after a native profile change.
They require the current Bot grant, original user/session ownership and the
same mounted Runtime/instance. The saved target snapshot must match the
operation's original fingerprint. Rows without a snapshot keep the exact
current fingerprint check; legacy registrations keep their existing rules.

This comparison is used only by metadata projection. Result collection, live
recovery, cancellation, logs and artifact operations retain their original
execution pin. Historical references do not authorize filesystem access or
resubmission, and unknown operations remain unknown. Reads do not update
operation rows or contact the native transport.

## Owner readback after a lost acknowledgement

Use the existing authenticated owner endpoint
`GET /users/me/runtimes/{id}/admission`. Its response now includes
`config_sha256`, `target_fingerprint` and `agent_config_dir`, alongside the
original identity, `request_id`, string `revision` and `paused` fields.
The registration and gate are read in one PostgreSQL MVCC statement, so a
concurrent atomic cutover is observed entirely before or entirely after commit.
No credential column is selected and the full provider configuration is never
projected to HTTP. GET does not create or update a gate and contacts no host.

The owner client must match the Runtime/instance and pause request, require
`paused: true`, and compare the current directory to its intended directory.
Adopt the returned digest, fingerprint and revision only after those checks.
If the directory or identity differs, retain the pause and report the observed
winner; do not replay PUT or resume automatically. A later cutover can still
win after GET returns, so every subsequent write must use the returned CAS
facts and handle a conflict. This endpoint does not freeze future writes.
Use this single readback instead of the credential-management Runtime list or
joining two separate GET responses. No migration is required for this query.
