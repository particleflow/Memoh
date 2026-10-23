# Explicit LAN endpoints and native delegated downloads

This follow-up starts from `84ca0e5d764b9944fc0b6c9b8264562288477744`. It changes
the available client modules; it does not recover the missing ZIP inputs,
reconstruct the native shell/builder, or add a transport service.

## Existing fields and ownership

| Existing contract | Meaning and caller |
| --- | --- |
| `ServiceConfig.version` | Existing local schema remains version 1. |
| `ServiceConfig.apiOrigin` | Despite the name, this is the complete API base, including the `/api` suffix. `saveServiceConfig` validates it; `harmony-entry.ts` sends it in `client.configure.apiOrigin`. |
| `ServiceConfig.hapDownloadUrl` | Optional installer location; never an attachment-route candidate. |
| `ServiceConfig.resetPending` | A changed API base invalidates the prior authenticated scope before the app boots. |
| `MEMOH_ORIGIN` | Native in-memory configured API base, set by `MemohClientConfig.ets`. Uploads and downloads use its existing allowlist boundary. |
| `MediaDownloadInput.url` | Existing native download envelope field. It now admits a validated delegation path under the same configured API base as well as ordinary owner/hash media. |
| `transfer.json.version/origin/source_root` | Existing public desktop transfer configuration inspected at `dfc7cf5`; no LAN/IPv6 candidate list is defined there. Device credentials and phone account credentials retain distinct owners. |

The shared transfer work owns the NAS identity and API-base contract, including
the existing Windows `nasOrigin` without a path versus the Web gateway's `/api`
base. No competing `lanOrigin`, fallback list, resolver, discovery service or
credential migration is added here. A shared contract still needs to define the
same NAS identity across routes, canonical API-base path, approved route ordering,
and authentication/upload ownership when connectivity changes.

## Product validation versus transport validation

The old Web/Python validators explicitly rejected RFC1918 addresses, IPv6 ULA and
`.local`/`.lan` names. This was a product restriction, not evidence of an OS or
TLS limitation. They now accept deliberately configured **HTTPS** endpoints in
those categories. Loopback, link-local, unspecified and multicast targets,
noncanonical numeric IPv4, embedded userinfo, queries and fragments remain
rejected by the tested configuration paths. Existing explicit public HTTP
configuration compatibility is retained; newly admitted LAN addresses require
HTTPS.

This does not validate a certificate. The configured hostname/IP must still be
covered by a normally trusted certificate on the actual device. No trust store,
certificate bypass, TLS override, platform permission or network-security file
was changed. Global IPv6 was already accepted; automatic LAN-first/IPv6-fallback
selection remains pending the shared endpoint contract.

Keeping the exact API base preserves the current login. Changing between a LAN
base and a global IPv6 base sets `resetPending` and clears the old token before
network use. This deliberately does not assume that two addresses identify the
same authorized NAS. No token is probed against arbitrary candidate addresses.

## Scoped native download boundary

`saveNativeOriginal` gains an optional sixth argument, `delegationUrl`, carrying
the existing `attachment.metadata.delegation_url` value. Ordinary calls retain
their existing signature and generated SDK path. When the argument is present,
the adapter validates the three UUID path segments and exact content hash before
any bridge request. It resolves the path with the existing SDK API base, sends
authorization only in the header, and passes the same existing `url` field to
`download.fetch`. Invalid, empty or null scope is an error, never a request to
the ordinary Bot/hash API. A scoped 401/403 goes through the existing auth response
handler and never falls back to the ordinary route.

`NativeMediaDownload.ets` rechecks the exact configured API prefix and either the
ordinary path or the full delegation path/hash before file or network work. It
uses the original transfer action, range checks, operation ownership and cancel
logic. Redirects remain disabled. It still performs only a bounded retry within
the current foreground operation; no cross-process resume guarantee is added.

The shared path validator lives in the existing `harmony-service-url.ts`, which
the source architecture identifies as the source for native `ServiceUrl.ets`.
The missing builder and native `ServiceUrl.ets` input must be recovered and
checked before an actual HAP build. Tests inject this actual TS validator at the
native import boundary; they do not claim to have compiled the missing module.

The absent overlay builder and `NativeClient.ets` prevent verifying the complete
UI-to-bridge wiring. The recovered caller must forward the existing metadata
value as the sixth argument; it must not discard the scope and invoke an ordinary
hash download. The adapter and native module support that contract now, but no
claim is made that a newly installed phone already exercises it.

## Validation and remaining blockers

- Configuration/storage tests: 74 checks passed, including explicit HTTPS LAN,
  rejected HTTP LAN, global IPv6, canonical address handling, unchanged-origin
  login retention and changed-origin token clearing.
- Python configuration validator: 3 test methods passed with explicit address
  and embedded-authorization cases.
- Actual TS/ETS routing tests passed with controlled SDK/bridge/RCP boundaries:
  scoped and ordinary envelopes; ten malformed scopes; five invalid native
  URLs; 401/403 and redirect rejection with no wider fallback; account changes;
  and zero transport calls for rejected targets.
- Strict no-emit TypeScript checks passed for service URL/configuration modules;
  ESLint passed for the download adapter and service URL module.
- With the original `84ca0e5` adapter injected into real upstream Web source,
  `vue-tsc` reports 147 errors: the existing 145 plus unavailable
  `createApiFetch`/`authSessionGeneration` exports expected from the missing
  overlay. The new adapter produces the same 147 diagnostics. No generic fixes
  or fabricated exports were made; full Web typecheck still fails.
- The public source manifest/syntax checker passed with original checkpoint
  hashes preserved alongside the newly reviewed hashes.

See `testing/harmony-endpoint-native-scope/results.json` for machine-readable
results. These checks do not cover the unavailable bridge router, native file
integrity helper, real TLS/CA behavior, a Harmony runtime, full Web/HAP build,
phone-to-NAS-to-phone transfer, live server authorization, screenshots or human
QA. No SDK was installed, deployment changed, or Library HTTP 401 bypassed.
