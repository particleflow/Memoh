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
