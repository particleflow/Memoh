# Existing Harmony endpoint and recovery seams

This is an input to the existing unified network-policy owner, not a new phone
protocol or implementation. LAN-first/IPv6 fallback is still absent in Harmony.
No new domain, configuration field, credential, transport service or discovery
mechanism was added in this update.

| Existing seam | Concrete contract to preserve |
| --- | --- |
| `mobile/memoh/harmony-service-config.ts` | The existing `apiOrigin` configuration key is authoritative. A valid persisted configuration wins over an ordinary package seed. The explicitly signed legacy HTTP-to-HTTPS exception only accepts the attested original base/hash, same host/path and explicit valid ports. It must not be reused as permission to migrate a token to an arbitrary LAN/IPv6 origin. |
| `mobile/memoh/harmony-entry.ts` boot and builder `setupApiClient` adaptation | The same API base, including `/api`, configures both shared SDK and `client.configure` before authenticated app imports. `hapDownloadUrl` remains a separate installer destination and never becomes an API credential target. |
| Native `MemohClientConfig.ets` / `NativeClient.ets` | `MEMOH_ORIGIN` is an in-memory copy, not another persistent configuration. `client.configuration` stops voice/upload/download I/O and resets it; ready-state reconfiguration is refused. Only the trusted `begonia-client://app/index.html` frame can invoke these commands. A future transport rebind must respect this lifecycle. |
| `NativeResumableUpload.ets` owner/call/reconcile | Owner hash includes API origin and freshly authenticated user; local journal also binds Bot and session/draft scope. A different origin currently fails ownership rather than adopting the journal. Preserve original upload ID, offset, expiry, size/hash and original NAS storage. Missing/unknown Create, PATCH or complete response must not become another POST because a route changed. GET404 preserves the original receipt and local snapshot. Generic receipt TTL remains unchanged. |
| `NativeMediaDownload.ets` bind/reopen | Journal key hashes API origin, current user, Bot, exact ordinary/delegation path, hash and size. Do not drop those checks to reuse cached bytes across an unverified origin. Fresh scoped access and final full size/hash remain required; 401/403 cannot trigger a broader Bot/hash fallback. |
| `harmony-download.ts` / `harmony-resumable.ts` | Keep current auth-generation/token gates, original operation identity and lifecycle pause/release behavior. Do not put file bytes in conversation transport or substitute PC runtime credentials. |
| Existing platform HTTPS requests | System certificate validation remains enabled; native authenticated requests disable redirects. Preserve the intended TLS hostname/SNI, certificate trust and API path when choosing a route. A syntactically valid LAN/IPv6 URL is not proof of the same NAS or a trusted certificate. |

The unified owner must specify how its already defined policy retains one
authenticated NAS identity across the allowed network routes. If its solution
uses existing system networking with the same canonical HTTPS origin, these
current origin-bound identities need not be changed merely to select a network
path. If the chosen contract changes the API origin, explicit same-NAS and
account binding plus durable journal migration must be specified before phone
adoption; current arbitrary-origin cleanup and ownership checks must not be
silently bypassed. This report does not prescribe another mapping/config layer.

Required acceptance remains actual LAN availability/preference, IPv6 fallback
after route loss, intact TLS/identity, continuation of the original receipt and
scoped download, and refusal on a different NAS/account or denied delegation.
No model, phone network, NAS or production configuration was exercised here.
