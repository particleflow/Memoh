# Shared file API base and reference contract

This contract describes one logical NAS file entry in the existing Memoh Server.
It is not a new service, configuration file, endpoint registry or file protocol.
It applies to the existing Windows transfer component and provides shared test
vectors for endpoint and mobile configuration owners. Those other clients have
not yet adopted the vectors in this change.

## Existing owners and the independent change

| Concern | Existing source of truth | This change / remaining work |
| --- | --- | --- |
| NAS file routes | `MessageHandler`, reused by the device task adapter | No new routes. Ordinary attachments and device transfers use the same Media service and storage provider. |
| NAS original location | `Config.Files.OriginalsRoot` -> core providers -> Media/storage | No path or mount changes. `.uploads` derives from this root. Clients use references, never construct the NAS filesystem layout. |
| Windows transfer API address | Private `transfer.json.origin`, resolved by `OpenHostConfig` | Shared HTTPS API-base validation; existing config version and raw-byte binding remain unchanged. |
| Windows endpoint configuration | Existing unified `transfer.nasOrigin` and config/state locator | Endpoint owner must allow the API path prefix and align validation with these vectors. No separate transfer settings root or installer is introduced here. |
| Mobile API address | Existing `apiOrigin` configuration owner | Explicit HTTPS LAN support is present in the mobile increment linked below; automatic route selection and full shared syntax alignment remain with that owner. |
| Credentials | Existing account/session authorization for mobile; stable device registry and private `device.credential` for PC | No new credential type, per-file pairing, signing or persistent identity. Device requests retain operation/device/attempt/file authorization. |
| Task results | Existing `computer_task_result` -> `ObserveBatch` / `ReadBatchPage`, operation delivery ledger | No new result collection or task service. |

The endpoint source inspected at
[`e2f61151`](https://github.com/particleflow/windows-endpoint/blob/e2f61151c88557e8846e5778691d72f33e7bfa7b/endpoint.mjs)
requires `nasOrigin` to equal a URL's origin, excluding `/api`. Memoh's
[default app gateway](../docker/nginx-app.conf) serves the UI at the root and
strips `/api/` before proxying to Server. Therefore, that endpoint validator and
this default gateway need explicit alignment. This is a source-level integration
gap, not a claim about a particular production reverse proxy.

The mobile follow-up at
[`14bb9b85`](https://github.com/particleflow/Memoh/blob/14bb9b85004477b8a6628572e1dfbee480a9bf5c/docs/harmony-endpoint-native-scope.md)
already admits explicitly configured HTTPS private-network addresses and supports
the existing `url` download field with delegated scope. Its missing overlay and
native bridge inputs still prevent complete caller/build verification. It has a
single API base and no automatic LAN/IPv6 selector. That client also retains
legacy public HTTP configuration compatibility; this is not encrypted transport
or a policy adopted by the HTTPS-only PC transfer client. Converging that legacy
policy requires the mobile owner's explicit compatibility/migration work.

## API base syntax

The existing fields above mean **HTTPS API base**, not necessarily URL origin.
Examples using synthetic addresses are `https://nas.example.test/api` and
`https://[2001:db8::10]/api`. A direct Server root or a nested gateway prefix is
also supported. A single trailing slash remains compatible. The client appends
its existing route namespace without losing the configured prefix.

`transferclient.ValidateAPIBase`, `New` and `NewTask` enforce the same syntax:

- HTTPS, nonempty host, optional port in 1..65535, no userinfo.
- No query or fragment, including empty `?` or `#` delimiters; no whitespace,
  control characters, backslashes, percent escapes or IPv6 zone identifiers.
- Path segments contain ASCII letters/digits or `-._~`; no `.` or `..` segment
  and no doubled separator. The parsed client prefix is bounded to 2048 bytes,
  including the task namespace when constructing a task client.
- Explicitly configured private IPv4, private names and ULA IPv6 are accepted.
  Acceptance performs no DNS lookup, probe or authorization. TLS still verifies
  the configured hostname/IP against the server certificate. Redirects remain
  disabled; certificate validation cannot be disabled through this API.

The language-independent
[`memoh.file-api-base.v1` vectors](../internal/transferclient/testdata/api-base-v1.json)
are the common acceptance/rejection examples. This is a syntax-contract version,
not a new host-config, queue, database or upload-protocol version.
These vectors describe the transfer layer; they do not remove each endpoint's
additional address allowlist or claim that existing phone/Windows validators
already accept every example. Their configuration owners must explicitly adopt
the shared syntax while retaining their platform's destination restrictions.

Valid existing `transfer.json` version 1 files remain compatible. The config's
original bytes still determine the host binding: this change neither normalizes
the saved value nor silently migrates a queue to a different server. Previously
accepted ambiguous URLs now fail before credentials or requests are obtained
(`ErrInvalid`, or `ErrBinding` through `OpenHostConfig`). Existing queue payloads
are retained. Correcting a bound configuration requires deliberate configuration
migration/reconciliation by its owner; changing an address must not silently
recreate a delivery or discard an unknown outcome.

## Same NAS identity across network paths

The minimal shared rule is to keep one canonical HTTPS API base, TLS identity,
authorization scope and persisted owner binding while choosing a network path
to that same NAS. The endpoint's existing network/configuration owner should
prefer an approved reachable LAN path and otherwise use the approved external
IPv6 path. File code consumes the resulting connection; it must not maintain
its own root directory, alternate login or replacement task ID.

**This change implements no LAN probe, DNS priority or route selector.** System
DNS or IPv6 support alone does not establish LAN preference. A future selector
needs platform-specific reachability and failover tests. If multiple URL aliases
are necessary, their common server identity, approved scope and migration of
existing config bindings must be explicit. Merely cycling `origin` values would
change the Windows config binding and the mobile upload owner and is not a
transparent resume mechanism.

401/403, invalid certificates and an unknown write result are not reasons to
create new business work or weaken authorization. Network interruption continues
through the existing receipt/offset and bounded retry rules. Actual certificate,
trust-store, DNS and network changes are separate deployment actions; none are
performed by this contract or its tests.

## References, access and storage

Ordinary files retain Bot ID, content hash, byte size and MIME metadata; these
values identify content but do not grant access. A delegated attachment retains
its server-issued scoped `delegation_url` and associated context. Clients must
not replace that reference with a broader Bot/hash download when scoped access
fails. The mobile follow-up supports that scoped field in the adapter/native
module; its missing caller overlay still needs to forward the original scope.
URLs are derived from the selected API base and existing authorized routes, not
from a NAS filesystem path.

PC transfers additionally retain the original operation, device, attempt,
delivery ID and upload receipt. Existing Commit/Resolve proofs, cancel fences,
Range/hash verification and unknown-state retention are unchanged. A shared NAS
identity does not mean all clients share one credential or acquire each other's
Bot/task permissions.

The unified locator owns locations; originals, private queue snapshots,
publication records, configuration, credentials and logs retain separate
ownership. The existing empty-root legacy behavior and Media temporary-spool
placement are outside this increment. No original, queue evidence or production
directory is moved or deleted.

## Verification and remaining acceptance

See [recorded tests](testing/file-api-base-contract.md). This increment adds
real Server/PG/file-storage verification of `/api` upload/association paths and
shared syntax tests. It does not implement mobile persistence, Windows routing
UI, LAN/IPv6 failover, a new Linux product, or a complete phone/PC/NAS acceptance.
The existing Windows queue and publication implementation remain the endpoint
components; no Linux publication binding is needed to establish this contract.
