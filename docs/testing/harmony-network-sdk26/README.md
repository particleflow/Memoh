# Harmony SDK26 same-authority routing capability

Contract inspected: `2d03f6cbabad67b242d33180cc36bf02d423e6d5`, whose product
source is unchanged from 9bb. This report extends the existing
[owner handoff](../harmony-unified-9bb6fc3/network-owner-handoff.md). It does not
add a transport protocol, configuration API, production module or HAP build.

## Findings by consumer

| Consumer | Supported local SDK surface | What is and is not established |
| --- | --- | --- |
| Native upload and download (`@kit.RemoteCommunicationKit` RCP) | `Configuration.dns`, `StaticDnsRule {host,port,ipAddresses}`, dynamic rules, `SessionConfiguration.requestConfiguration`, system CA validation and typed connect/TLS/send/receive errors. | A DNS-hostname request can be constructed with its canonical HTTPS URL, original port, headers/body and system trust intact, while supplying a connection-address hint. These are useful primitives, not a proven equivalent of the Windows TCP dialer. Both native classes can consume the same eventual policy at their existing RCP session/request creation sites. |
| Shared SDK inside ArkWeb | `WebviewController.setHostIP/clearHostIP` provides an engine host-IP cache; `setHttpDns` selects one HTTPS DNS service. | A host-cache hook exists; there is no exposed per-request TCP dial callback in the inspected WebView API. Cache state is engine-level and host-only, not a host/port ordered candidate list with connect-stage errors. Ordinary Fetch still cannot implement strict TCP fallback or safely replay an uncertain POST. No new DNS/proxy service is proposed. |
| Alternative native `@ohos.net.http` | DNS servers/DoH and `addressFamily`. | Family selection alone does not identify LAN or implement explicit address order. Moving the current RCP/file stream implementation to another HTTP stack would not establish the required TCP hook and was not done. |

Evidence is the **installed official SDK26.0.0.105**, not recollection of method
names. [sdk-evidence.json](sdk-evidence.json) records four actual declaration
file hashes and exact symbol/error line anchors. SDK files/binaries and signing
material are not copied into the repository.

### Why the complete strict policy is not wired yet

1. RCP documents that multiple DNS-rule IPs use only the last address by default.
   Enabling `happyEyeballOnDnsRule` enables racing, not a documented strict LAN
   order. Thus supplying a list and claiming ordered fallback would be wrong.
2. `Session.fetch` distinguishes connect failure **1007900007**, TLS handshake
   **1007900035**, certificate **1007900060**, send/receive **1007900055/56**,
   empty reply **1007900052**, and generic timeout **1007900028**. Only a proven
   direct pre-connect failure is even a candidate for selecting another address.
   TLS/HTTP/timeout/unknown write outcomes are not permission to retry. The SDK
   has separate timeout settings but does not document a separate connect-timeout
   error or a guaranteed failed-stage/unsent flag. `TimeInfo` is telemetry; its
   declaration does not define missing-phase values as proof that no bytes went
   out. A simulated code cannot establish those native-engine guarantees.
3. DNS rules and WebView cache methods are documented in terms of host names.
   Their application to an IP-literal canonical URL is not established. Do not
   change an existing IP authority into a new domain or IP URL to make a test
   pass. Whether the existing private canonical authority is a hostname or IP
   can be reported as a non-secret classification by its existing config owner;
   the actual address is unnecessary in this public evidence.
4. Proxy routing must remain owned by the existing system/proxy. The SDK's
   read-only `connection.getDefaultHttpProxy()` can inspect that state; disabling
   a proxy is not an allowed LAN strategy. The probe leaves proxy options unset.
5. The fixed central contract defines a Windows private policy but **no phone
   delivery field**. Current phone config validators deliberately reject extra
   fields. Do not ship an inactive DTO, expose IP lists through media limits, or
   independently invent an endpoint.

Consequently this task establishes API feasibility for explicit address hints
and a conservative isolated boundary probe. It does **not** claim strict native
LAN/IPv6 failover is implemented. Configuration agreement alone would not close
the timeout, literal-authority and WebView dial-hook gaps above.

## Precise configuration proposal for the unified owner

Reuse the existing Windows policy payload without a new wire protocol:

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["version", "lan_addresses"],
  "properties": {
    "version": {"const": 1},
    "lan_addresses": {"type": "array", "minItems": 1, "items": {"type": "string"}}
  }
}
```

Semantic validation must match existing `HostConfig.readNetworkPolicy`:
explicit IP literals in order, IPv4-mapped normalization, no duplicates,
unspecified or multicast addresses; IP family does not classify LAN. Keep the
original canonical HTTPS port; do not add per-address URLs, ports or credentials.
The Windows private file has its existing 8192-byte bound.

**Phone container proposal, not an accepted or implemented field:** optional
`networkPolicy` carrying that exact payload in the existing privately signed
`memoh_client.json` resource. Omission preserves current platform behavior;
present but invalid input must fail closed. The entire existing deployment seed
must still fit the reserved 8192-byte slot. The unified owner must confirm the
container/delivery choice before production parser changes. No new NAS route,
public address advertisement or separate configuration store is proposed.

Consumption points after that agreement:

- `client_config.py` existing allowlist/injector: validate the optional payload
  before signing, without changing legacy binding/hash or adding credentials.
- `harmony-service-config.ts` / `harmony-entry.ts`: read the signed policy only
  for the exact active canonical API base. A user-configured different base
  must not inherit another deployment's hints. Preserve `apiOrigin`, token,
  existing user configuration and HTTPS migration semantics.
- Trusted `client.configure` → `MemohClientConfig.ets`: one in-memory copy of
  the approved policy under the existing configuration/lifecycle gates. No
  arbitrary page, remote response or untrusted frame may select an address.
- `NativeResumableUpload.ets` and `NativeMediaDownload.ets`: same RCP seam,
  canonical URL/port, system trust and disabled redirects. Preserve healthy
  connections, all original journal identities and unknown-result fences.
- Shared WebView: remain platform-routed until an actual supported mechanism
  satisfies the full contract. `setHostIP` must not be labelled a Fetch dialer,
  automatically rewritten after a Fetch error, or applied globally without an
  agreed lifecycle. Do not route shared authenticated requests through a new
  native byte bridge merely to hide this limitation.

These are review inputs to the existing unified owner. They are not changes to
current public/private config or a request to modify phone system networking.

## Executed isolated verification and remaining field work

[probe.ts](probe.ts) was typechecked against the actual installed declarations;
own code is strict, with `skipLibCheck` only for vendor dependency declarations.
Product compiler settings were untouched. [The empty log](types.txt) has exit0.
[run-probe.cjs](run-probe.cjs) executes the probe with Node Kit seams. It verifies
unchanged canonical URL/port/header/body, explicit system validation, disabled
redirects, no proxy override, refusal to assume literal-authority support,
original-host IPv6 filtering, and exclusion of TLS/HTTP/timeout/unknown errors
from the candidate connect-failure classification. [Result](probe-result.json).

The probe does not contain a retry loop and is not imported by the product. It
does not perform a native request, TLS handshake, DNS change, WebView cache write,
phone operation or model call. No new permission was requested. Existing 1.0.12
artifacts, mobile source, SDK/compiler and configuration were left unchanged.

Actual RCP behavior (including proxy, IP-literal handling, pool reuse, connect
timeout stages, unchanged Host/SNI and refusal after TLS/403) and real handover
still require official-engine/device evidence. Ordered LAN TCP failure followed
by original-authority IPv6, original receipt recovery and journal identity must
be proven together before field acceptance. A Node mock is not that evidence.
