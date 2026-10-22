# Computer delivery association, version 1

Base: main `75a70fe961808ed5ec2284767cb92ea85f30890f`. This change does
not import the Windows client stack, change upload/Range protocols, move data,
or install an Agent, plugin, daemon, credential or TLS configuration.

## API and identity boundary

The host calls `DeliveryService.Commit` or `Resolve` with a process-local
`DeliveryIdentity`. The identity extends the existing `HostComputerBinding`
with operation, device, instance, attempt and generation. It must be supplied
by an authenticated execution host, never reconstructed from request fields,
an operation ID, an account-wide Bot token or Agenvo OAuth credentials.

The candidate HTTP adapter exposes POST
`/internal/computer-deliveries/{operation_id}/{delivery_id}/commit` and
`.../resolve`. Its bounded JSON body contains `version: 1`, `direction`
(`upload` or `download`), `content_hash`, `size_bytes`, `mime`, and (resolve
only) `cancel`. IDs in the URL are selectors, checked against the host identity.
The response contains only version, outcome and decimal operation revision.
Resolve outcomes are `absent`, `committed`, or `canceled`; errors never mean
absence. All responses, including routing/auth errors, are `no-store`.

The adapter is deliberately **not registered in the application router**.
There is no approved PC-to-Memoh execution credential/verifier yet. A future
host adapter must authenticate and revoke a credential scoped to the exact
Bot/user/session/run/target/device/instance/operation/attempt/generation, then
establish the context. A normal JWT or forged header does not establish it.
Fixtures invoke the real handler with explicitly synthetic host identities.

## Persistence and cancellation

The existing `computer_operations.details.delivery_ledger` contains a versioned
map of at most 128 immutable entries. Each stores the reference, scope digest,
outcome and server timestamp, without bytes, paths, tokens or raw output.
The operation owns its ledger lifetime. No separate task state or filesystem
registry is introduced. Only a dedicated revision-CAS update may change this
field; ordinary execution updates preserve it at the SQL boundary, even when
their Details value lacks the field. Delivery updates leave execution state, cancel intent,
receipt fields and business completion unchanged.

An exact repeated commit returns committed. A different reference/scope for the
same delivery ID conflicts. Resolve with `cancel=true` atomically returns an
existing outcome or writes a cancellation tombstone. A late commit cannot cross
that tombstone; cancellation cannot retract an already committed reference.
Plain absent is not cancellation acknowledgement. Tombstones count toward the
bound; a full ledger fails explicitly. No automatic eviction permits ID reuse.

Unknown database commit outcomes, lost HTTP responses, timeouts and process
death are reconciled by the same resolve operation. Idempotent replay still
requires fresh authorization. The service does not retry an unknown commit as
if it had failed. Known serialization/CAS conflicts are bounded to three attempts.

## Authorization and transaction boundary

The PostgreSQL adapter uses one SERIALIZABLE transaction, the existing pool's
team/RLS context, transaction-bound Bot/account services, session-task ownership
and ancestry checks, and Agenvo target registration validation. It rechecks an
active account, chat/read/write/exec grants, target ownership/revocation,
connection ownership, current target fingerprint, and the persisted pi-json-v1
attempt/generation before every commit, replay, resolve or fence. Legacy/TUI
operations without that persisted identity fail closed in version 1.

These reads and the existing operation revision CAS form one transaction.
A revocation committed before the transaction snapshot is denied. An overlapping
revocation may finish before an association that read the earlier authorized
snapshot; it does not retroactively retract that association. Other existing
writers do not all use SERIALIZABLE, so this is not a global serializability
claim or a claim of instantaneous cancellation of in-flight requests. See the
[PostgreSQL isolation contract](https://www.postgresql.org/docs/18/transaction-iso.html).

New commits use the existing Media `OpenSeekable` Range stream and check Bot,
SHA-256 and actual stream length. `Resolve`/`Stat` alone does not fill length and
is insufficient. MIME is bounded declared metadata, not proof of content type;
the existing Media endpoint retains its own serving policy. Resolve and replay report ledger truth even
if an asset was subsequently removed. This transaction does not pin media
against deletion, promise retention, or prove a PC disk write. For download,
the trusted host must finish Range/hash validation and local publication before
committing its attestation. A registered reference is not business success.
This preserves the current Media integrity policy without modifying it, but
costs a full verification scan on a new commit (two passes for Originals).
The first transaction only authorizes and checks for an existing outcome. The
scan runs outside the transaction; a new transaction reauthorizes and CASes.
Revocation or cancellation during the scan therefore prevents a new commit.
Verified content is reused across at most three known CAS aborts in that call.
Replay/resolve performs none. Large-object commit latency and the client call
budget need acceptance measurement; a future cheap verified metadata API is a
cross-module change, not an excuse to trust unverified fixture sizes here.

## Compatibility, validation and rollout

No schema migration or production path change is needed. The additive ledger
is version 1; unknown versions fail closed. All server processes must deploy the
new embedded SQL before enabling delivery writes; old binaries do not preserve
the ledger. Mixed-version writers are unsupported. Rolling back that rule while delivery
writes are enabled is unsupported. The frozen PR #14/#15 artifacts are unchanged;
their Commit/Resolve port needs a small explicit mapping to this API after
identity review, not a second upload protocol.

Tests cover response loss (including actual context timeout/cancellation),
commit/cancel races, repeated and conflicting delivery IDs, unknown versions,
entry bounds, forged/cross-scope identities, revoked grants and target changes,
media mismatch, SQL CAS conflicts and execution-writer preservation. Synthetic
handler tests are not real PC/NAS acceptance. Private HTTPS with normal
certificate verification, approved scoped identity and revocation, host plugin
wiring, real Windows acceptance and the repository's real UI/human QA remain
separate rollout prerequisites. No production credentials are created here.
