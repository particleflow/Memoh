# Native Runtime host handoff to the existing transfer component

Fixed producer baseline: `0894a6eb8ff69c8796e2da5450fa172d4cd5ef81`,
`memoh.delivery` 0.10.1. This handoff adds no product protocol, queue or service.
The host control connection may change; the NAS file channel and its ownership
must remain separate. This is task input/result exchange, not directory mirroring
or propagation of deletions/conflicts.

## Interfaces and ownership to retain

| Owner / producer | Consumer | Existing interface and invariant |
| --- | --- | --- |
| Unified Windows host locator | `HostConfig` / `OpenHerdrQueue` | The same `HERDR_PLUGIN_CONFIG_DIR` and `HERDR_PLUGIN_STATE_DIR`; do not derive roots from a Runtime session or reconnect generation. |
| Existing private file configuration | Finite CLI | Preserve `transfer.json` bytes (HTTPS API base including `/api`, source root), `device.credential`, queue locator/profile and HostBinding. A new control credential is not a file credential. |
| Existing task/closed-result owner | `enqueue-result` / `collect-result` | Preserve original operation/Bot/user/session/target/run/attempt/generation, declared source reference, hash/size and deterministic delivery ID. A reconnect never creates or closes a manifest and never invokes this producer again. |
| Existing input handoff owner | `enqueue-download` stdin | Preserve the original job/reference and task selectors. Only an actual input admission may enqueue; a connection event cannot fabricate one. |
| Existing queue | Host finite scheduler | `lifecycle-summary`: exactly `schema`, `state`, `next_due_at`, `error_class`. If due, invoke `advance-results` with no delivery/body override. Keep locks, original receipts, retries and association reconciliation in this queue. |
| Existing finite invocation | Host/result observer | Match output to that invocation. Exit/summary/feedback/reference retain their 0.10.1 semantics. `not_applicable` skips only outbound feedback; ready/registered/control acceptance are not committed delivery. |

The examined Windows consumer is
[`transfer-lifecycle.mjs` at 3cde880e](https://github.com/particleflow/windows-endpoint/blob/3cde880ed3e5721b9a10c24ba258cbd53d26c4c6/transfer-lifecycle.mjs).
It already exposes `hint('network')`, `hint('completion')`, `hint('owner')`,
startup/stop and finite ticks. Its [0.10.1 result parser](https://github.com/particleflow/windows-endpoint/blob/3cde880ed3e5721b9a10c24ba258cbd53d26c4c6/transfer-contract.mjs)
accepts explicit `not_applicable` and retains the `unavailable` guard.
These are existing local consumer calls, **not newly defined native Runtime
wire event names**. The Runtime/Herdr connector is owned by the other workstream;
its authenticated callback mapping still needs integration evidence.

## Event mapping, without task replay

| Observed host event | Existing transfer action |
| --- | --- |
| Startup/restart with the same approved roots | Read summary of the existing queue; do not initialize a replacement when it is absent/corrupt. |
| Authenticated control offline-to-online edge | Coalesced `network` hint, then summary. Repeated notifications do not reset retries or business identities. |
| Original task result closure/admission completed | Original producer owns the closure/registration once; `completion` only hints the finite scheduler afterward. |
| Persisted waiting deadline | Advance at/after the queue's deadline; a network hint may reread it, never shorten it. |
| Credential/configuration/permission failure, blocked or unknown | Preserve attention/evidence. A reconnect is not owner acknowledgement or authority to clear it. |
| Host shutdown | Close the hint/spawn gate and classify the issued finite result before stopping owned components. Reopen the same queue next time. |

There must be no Pi submit, new task ID, HostFS byte copy, arbitrary shell or
manifest collection in a reconnect callback. HostFS/control-plane access cannot
replace the file module's TLS, authorization, hash, publication and NAS ledger.
The [lifecycle contract](delivery-lifecycle-summary.md) owns backoff/fairness and
the [feedback contract](delivery-producer-feedback.md#cli-feedback-applicability)
owns result classification; this document does not add a second state machine.

Routing also stays in the existing component: optional `transfer-network.json`
under the same config root supplies explicit LAN IP preference; TCP failure falls
back to the original authority using IPv6. Preserve original origin/SNI, `/api`,
task scope and credential. TLS/authentication failures must not trigger address
fallback. See the already executed [route evidence](testing/transfer-lan-preference.md).
The Runtime endpoint's address is not a substitute NAS API base.

## Server identity dependency: not solved by a wake test

In this fixed producer tree `internal/userruntime/transfer.go` verifies the
existing stored transfer credential and an `agenvo.DecodeTarget`-derived digest
of connection/device/instance. `internal/handlers/computer_transfers_device.go`
binds the verified device to the existing operation authorization. A transient
connection epoch is not the task's attempt/generation or a new target identity.
Changing those persistent device/target selectors may invalidate the credential
or operation: the Runtime/Server owners must define their authorized mapping.
Do not silently rebind old jobs, substitute a Runtime token, or issue credentials
from this component. This patch cannot establish native Runtime authentication.

### Agreed native identity projection; implementation dependency

The coordinated native adapter contract uses `kind=remote`, `provider=memoh`.
`target_id` is still the Bot mount, while `runtime_id` is the registered runtime
UUID. For a **new native target**, the file verifier projects
`connection_id=runtime_id`, `device_id=runtime_id`, and
`instance_id=herdr.instance_id`. This is a trusted registry lookup after owner,
team, active/activated and revocation checks, not a client assertion. The native
control token (`mrk`) is never a file token; the existing owner-only
`transfer-credential` GET/POST/DELETE surface keeps separate `mft` credentials.
This is the agreed source contract, not evidence that its implementation has
landed in this producer tree. No third selector or client protocol is added here.

The existing integration points are:

| Existing location | Requirement for the native adapter / retained boundary |
| --- | --- |
| `internal/userruntime/transfer.go`; `db/postgres/queries/runtime_transfer_credentials.sql`; `internal/db/store/runtime_transfer.go` | Today decoding and all credential queries assume Agenvo, including the active owned HTTP MCP connection join. The source owner must add native registry authorization without dropping the legacy join, owner/team/revocation checks or config/credential CAS. `RuntimeTransferRecord` currently contains config but no provider discriminator; do not infer a provider from a UUID or accept arbitrary config. |
| `internal/handlers/computer_transfers_device.go` | Reuse `DeviceTransferVerifier`; its verified tuple replaces untrusted request device/instance fields. Request operation/task selectors remain lookup inputs, never authorization. Keep transient verifier failure retryable and actual revocation denied. |
| `internal/computertask/delivery_postgres.go`; `delivery.go:authorizeDeliveryRecord` | Resolve the mounted target through the same transaction's owner/Bot/session permissions and compare the verified tuple and persisted execution fingerprint before the ledger CAS. The current concrete resolver is Agenvo; the native source must supply its authoritative projection. |
| Existing producer metadata and `internal/transferclient/producer_feedback.go` | Preserve operation/Bot/user/session/target/run/attempt/generation and `root_hash`. The root hash remains a source/producer binding, not a new HTTP authentication field. |

Keep these existing hashes distinct: the file credential target digest hashes
the connection/device/instance tuple; `TargetFingerprint` hashes the entire
execution target; ledger scope hashes `DeliveryIdentity`; local HostBinding
comes from the original local configuration. Replacing one with another, or
recomputing old stored values under the native projection, would mix identities.
The native source's foreground command mode is also not automatically eligible
for file-result delivery: the current transaction requires `herdr_pi_task`,
`pi-json-v1` and its persisted attempt/generation. Supporting another execution
kind would need a separately aligned producer contract, not a bypass here.

Old Agenvo operations, credentials, selectors, HostBinding, payloads and receipts
remain unchanged. A native mount cannot inherit that queue's jobs merely because
it reaches the same PC. Retain old jobs and report their original unavailable or
unauthorized identity when applicable; do not rewrite, delete, re-enqueue or
re-execute them. An already authorized credential replacement for the **same**
target tuple is different: credential ID is not the ledger namespace, and the
original task/reference/receipt must still resolve after fresh authorization.
No credential replacement or queue migration is performed by this patch.

`TestTransferCredentialPurposeAndStableTuple` uses a synthetic store to verify
rejection of non-file tokens before lookup, verifier reconstruction and rejection
of tuple rebinding. `TestTransferPrincipalReplacementPreservesReceiptAndScope`
exercises both legacy and the agreed native tuple projection at the existing
verified-principal boundary: fresh service/principal, original receipt and
committed reference, no ledger/revision changes or second media verification, and
rejection of another tuple or execution fingerprint. Its transaction/media are
synthetic; the concrete native provider registration, SQL authorization and
Runtime connection are **not** exercised or claimed complete.

### Local profile locator contract (existing CLI interface)

The CLI has no `--config`/`--state` flags. It already accepts and freezes the
pair `HERDR_PLUGIN_CONFIG_DIR` / `HERDR_PLUGIN_STATE_DIR` at invocation start.
Use that existing interface; no new CLI flag, active-profile registry, queue
implementation or service is needed to address separate profile directories.

The unified host's existing locator owns the base pair `C`, `S`. Legacy/default
selection passes exactly `C`, `S`, retaining the current paths and file bytes.
For a new verified principal, the file component's path convention is:

```text
key = lowercase hex SHA256(UTF8(provider + NUL + connection_id + NUL
                             + device_id + NUL + instance_id))
HERDR_PLUGIN_CONFIG_DIR = resolve(C, "profiles", key)
HERDR_PLUGIN_STATE_DIR  = resolve(S, "profiles", key)
```

The four values come from the existing verified registration, not model input or
a second identity registry. Provider and UUID values use the registration's
canonical representation; instance bytes are exact UTF-8, without trimming or
case/Unicode normalization. Empty fields or embedded NUL are invalid. For the
agreed native projection both IDs are the registered runtime UUID. Credential
ID/secret, token rotation, connection epoch, Bot mount, attempt and generation
do not enter the storage key. This is a local directory key, not a NAS selector
or authority. The host must derive both paths together in its one locator,
resolve relative layout components there, and pass canonical absolute paths;
the CLI does not resolve relative paths against cwd or fall back to another root.

The existing private local directory/NTFS, owner, no-link and root-identity
checks apply unchanged. Profile directories must exist before invocation; this
component does not change ACLs or provision credentials. Each config directory
owns its own `transfer.json`, `device.credential`, optional network policy and
legacy settings. Each state directory owns its own unchanged `deliveries-v1`
locator, journals, history, locks and publication objects. Identical NAS origin
or HostBinding does not permit exchanging those state directories. A common
`source_root` may reference the same approved originals; this does not copy,
delete or grant either principal another task's metadata or receipts.

**Single selected profile is a host lifecycle obligation, not an existing CLI
global lock.** The host's existing single-instance owner must stop accepting
old-profile wake/admission events, finish/classify its in-flight finite command,
then change the selected pair. Queue worker/job locks protect a queue, not two
different profiles. Keep the old pair untouched and do not schedule it until
explicitly selected again. Startup/network events only summarize/advance the
selected queue; they never re-enqueue or collect old work. Result admissions
must retain the original task's pinned profile and bounded closed metadata;
do not indiscriminately collect all old manifests from a shared source root.

There is one remaining caller integration gap: the legacy envelope in
`internal/agenvo/pi_batch_producer.go` calls unprofiled
`herdr plugin action invoke collect-result --plugin memoh.delivery`. Herdr's
[action invocation](https://github.com/herdrdev/herdr/blob/5bd900d88c80d95f5b145397e45bbeea398b8165/src/app/api/plugins/mod.rs#L177)
uses its [derived plugin path environment](https://github.com/herdrdev/herdr/blob/5bd900d88c80d95f5b145397e45bbeea398b8165/src/app/api/plugins/env.rs#L15);
that pinned action has no arbitrary environment override. The native host/source owner
must route its completion through the existing finite CLI with the pinned pair
and gate the inactive legacy hook (using the existing host/plugin lifecycle).
Changing a global pointer or assuming that action inherits new profile paths
is not a supported solution. No such host/hook change is claimed by this patch.

`TestCLIPathSelectedProfileIsolation` uses fresh real CLI processes, synthetic
credentials and a TLS endpoint returning 503. Two profiles deliberately share
the same transfer.json bytes, logical delivery ID and source original, but have
different tasks and credentials. Advancing only the native profile persists its
retry without changing the old journal/config/original or sending an old-profile
request; two new-process summaries and a read-only switch back do not replay.
These are direct upload queue admissions: absent producer context correctly
remains `feedback_status:unavailable`, not a successful producer handoff. It does
not test actual host selection/locking or the still-unprofiled Herdr hook.

## Incremental isolated regression

`TestCLIPathRuntimeReconnectUpload` reuses the existing small TLS CLI fixture and
its lost Complete/Commit replies. At each persisted backoff, three fresh-process
summary reads leave every fixture file's membership/content/mtime and the exact
deadline unchanged, issue no HTTP requests, and preserve the final original
receipt/task/reference without another Create/Complete/Commit.

`TestCLIPathRuntimeReconnectDownload` uses Windows CLI subprocesses and NTFS. It
disconnects one response after sending 8 MiB, then checks that the client persisted
an 8 MiB checkpoint. Three fresh summary invocations preserve that offset and
backoff before the next request fetches only the tail.
It verifies complete bytes/hash, one association, original task/reference and
two completed-process reopen rounds without additional HTTP. It uses 8,486,912
bytes, not another 1 GiB or throughput qualification. The source and endpoint are
synthetic; the NAS is the existing protocol fixture. Only the existing test
helper receives RootCAs; production EXE/system trust is unchanged.

Reuse the [completed 1 GiB process-kill/real Server/PG/NTFS evidence](testing/transfer-native-large-byte-recovery.md)
for the larger fault scenario. The new cases prove the producer side of the
event boundary, not execution of an unpublished native Runtime connector,
physical power loss, real LAN/WAN or a deployed phone/PC/NAS route. No new
credentials, OS trust changes, deployment, endpoint/Agent modifications or
directory-sync product are part of this increment.
