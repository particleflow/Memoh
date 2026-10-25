# Native Runtime adapter scope review

The implementation is a provider adapter on base `9601ac062a3b65477db8db76bcaa521265ba7ee2`. Existing configuration is preferred wherever it supplies the required behavior. No production deployment, enrollment, credential creation, network change or old-connection disable is part of this source delivery.

## Existing configuration and interfaces retained

| Capability | Existing interface used; no replacement implementation |
| --- | --- |
| Runtime identity, activation, reconnect and service lifecycle | `packages/runtime` enrollment/configuration, the existing registry and reverse RPC. This package is unchanged. |
| Windows process/service setup | Official Herdr CLI, its existing configuration/environment and independently supervised service. No Herdr, Pi or Magpie core modification. |
| File producer directories, state and queues | Existing producer `HERDR_PLUGIN_CONFIG_DIR` / `HERDR_PLUGIN_STATE_DIR`, managed by the Windows host lane. No producer profile manager, new directory scheme or queue migration is implemented here. |
| Bot grants and selected PC | Existing account computer access, remote mount and primary-target APIs. No new selector or ACL model. |
| Schedules and heartbeat | Existing schedule/session routing and admission. No scheduler, timer, service or queue is added. |
| Retiring automatic OAuth calls | Existing MCP `is_active=false`, retaining the same resource and historical bindings. The [cutover procedure](native-runtime-cutover.md) and isolated database test cover this; no production implementation change is needed. |
| File transfer and delivery | Existing dedicated `mft_` credential, resumable producer protocol, manifests, checksums, receipts and committed NAS references. Runtime `mrk_` and generic host file access do not replace these. |

## Source gaps that configuration cannot fill

| Gap and evidence at the base | Minimal implementation files |
| --- | --- |
| `computertask.Service` holds `*agenvo.Service`; its target resolver rejects non-Agenvo providers. Existing Runtime Exec returns transport events, not durable Herdr/Pi result evidence. Configuring a Runtime cannot change those Go branches. | `internal/agenvo/{service,client}.go`, `internal/nativeherdr/{commands,exec}.go`, `cmd/internal/core/agenvo.go`, the one-line computer-task source decoder. Same operation/run/admission/result ledger; typed CLI mapping with no retry or new task API. |
| Existing Runtime create/list/revoke APIs have no owner-managed Herdr locator update. `HandshakeInfo` contains machine metadata only; `ParseHandshakeMetadata` rejects unknown fields and the metadata is not persisted. Adding Herdr to it would require modifying both official Runtime and server protocols. Using generic client environment would not bind owner-approved program/instance references to an operation fingerprint. | `internal/agenvo/native_profile.go`, owner PUT in `internal/handlers/user_runtime.go`, small userruntime/store wrappers and existing `provider_config` CAS query. No new table, client registration, credential or profile manager. |
| Existing transfer lookup/CAS SQL filters `provider='agenvo'` and requires an MCP connection. The native Runtime has no MCP identity. | Native branch in `runtime_transfer_credentials.sql`, generated sqlc, existing store record's provider, and `internal/userruntime/transfer.go`. Connection/device equal the actual Runtime ID; existing credential format and producer validation remain. |
| Admission owner lookup and database instance mapping only recognize Agenvo. A configuration-only transport change would fail native pause/resume or omit the database's concurrent pause checks. | Native owner/CAS queries, `internal/agenvo/admission.go`, paired 0176 migration and canonical snapshot. Existing request/revision/pause semantics retained; no remote probe. |
| Generic bridge consumers can otherwise execute on a configured Herdr computer through direct client/Hook paths, bypassing the durable task admission boundary; raw attachment ingestion can otherwise bypass producer authority. Existing `fs`, `exec`, `host_fs` flags cannot distinguish task transport from those consumers. | Configured-Herdr-only guard in `workspace/remote.go`, non-owning file-only bridge view and central container media guard. Ordinary authorized file tools and unconfigured Runtime behavior remain. Regression tests verify rejected RPCs never reach the remote client. |
| Consumers need to compare the stored native principal after configuration and restart. Existing Runtime DTO has no instance/session/fingerprint fields. | Optional read-only `native_herdr` DTO binding plus generated schema/SDK. It contains five non-secret fields, not a new selector; no client-supplied identity override. |

`NativeHerdrProfile` is one non-secret value in an existing Runtime row, not a new host profile framework. `instance_id` and `session` identify the Herdr context; `work_dir` and `bash_path` retain the existing task execution contract; `herdr_cli` and `herdr_config_root` locate the already-installed official executable and existing configuration. The optional `agent` reuses `AgentProfile`. These values are pinned in the existing operation fingerprint. They neither create a directory nor change producer profile selection. The root field is translated to official `HERDR_CONFIG_PATH`, `XDG_CONFIG_HOME`, `HERDR_SOCKET_PATH` and `HERDR_SESSION`; it does not introduce an upstream environment variable.

OpenAPI/SDK/sqlc changes are generated consequences of these seams. Remaining changes are tests, the official CLI probe and reviewable evidence. The initial OAuth-refresh and custom Agenvo-machine-credential investigations remain separate checkpoints and are absent from this implementation.

## Verification and limits

The [evidence directory](evidence/native-herdr) records isolated official Herdr CLI observations, native task/transport tests, PostgreSQL admission and producer checks, and the current synthetic Go/Vite application's 23 HTTP/identity checks and real UI screenshots. The retired-route combination test adds zero-old-OAuth-call and historical-data preservation assertions using only synthetic records.

The WebSocket timing investigation found the same short-budget failure mechanism on the unchanged baseline and candidate. Both full handlers race comparisons passed three repetitions; controlled scheduling pauses reproduced the old mechanism in both. The initial natural failures' unique cause remains unproven. No WebSocket implementation/test change or test skip is included.

These results do not establish production Windows execution, real Runtime enrollment, TLS trust, live producer configuration or old-queue handling. Those remain parent-coordinated integration and acceptance work. A source delivery requires a verified remote tree and parent commit plus the independent draft PR; unreferenced Git blobs alone are not a delivered branch.
