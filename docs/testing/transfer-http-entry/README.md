# Existing HTTP Web entry: real dispatch and persistence limits

This follow-up starts from `68c01296981f2d0c173226c5a3590ca4ad59e0fa`.
It tests the parent's concrete alternative: serve the existing Web over HTTP
and use its existing same-origin HTTP API, keeping the logical service identity.
There are no product changes in this increment, no new bypass service, browser
security exception, native SDK rebuild, NAS configuration write or device test.

## Minimal existing configuration, with a bounded result

The existing Web API base option can stay bound to the original logical API,
independently of the document URL. The existing v2 policy can map file dispatch
to the HTTP origin that serves that page and its existing `/api` reverse proxy:

```sh
# Existing Web build/bootstrap option; keep the original logical service base.
VITE_API_URL=https://logical.fixture.test/api
```

```toml
# Existing NAS file policy returned through the original authenticated profile.
# Illustrative isolated values only; no current NAS setting was changed.
[files.network_policy]
version = 2
[files.network_policy.endpoint]
logical_origin = "https://logical.fixture.test"
physical_origin = "http://fast.fixture.test:18683"
```

The page is opened at `http://fast.fixture.test:18683`; its file API path remains
`/api`. A same-origin file dispatch needs no cross-origin CORS permission. The
logical HTTPS metadata/control API still has its own CORS/transport needs; the
controlled metadata adapter below does not qualify those on the NAS. A hosted
deployment uses its existing Web/reverse-proxy configuration, not a new service.
The fixture reuses the existing Vite `/api` proxy and Go Server.

This configuration works for ordinary Files upload/download and raw preview.
It is **not compatible with the full original persistent browser transfer**:
ordinary non-loopback HTTP lacks the required Web Locks and OPFS. Replacing the
logical API base with the physical base would also change identity. No such
replacement, storage fallback, queue migration or algorithm rewrite was made.

## Executed actual-browser observations

Current Vite Web, Go Server, PostgreSQL schema 180 and the existing synthetic
workspace ran on the original isolated services, Web 18683 and Server 18680.
Chromium resolved the fixture HTTP hostname to loopback, with no proxy for
that browser fixture. Its page origin was a **non-loopback hostname**, and the
actual browser reported `isSecureContext=false`. File API requests went through
the actual HTTP network engine and existing Vite proxy, not Playwright response
fulfillment or the earlier separate physical adapter. All credentials had
synthetic fixture authority only; none were NAS/provider/device credentials.

Only logical HTTPS metadata/control requests and the old-origin storage probe
were fulfilled by a controlled test adapter. They used the existing fixture
server/profile, with the explicit v2 mapping inserted into its response. This
does not establish actual HTTPS transport, NAS CORS or public DNS reachability.
No browser mixed-content/TLS enforcement was disabled. The first run completed
all transfer assertions but a pending control-SSE fixture callback raised an
error during browser shutdown. Fixture route cleanup was corrected with
`unrouteAll({behavior: 'ignoreErrors'})`; the final complete run exited 0.
`browser-initial-sanitized.txt` preserves that first cleanup failure without
credential values. No product or streaming implementation was changed.

`proof.json` and `physical-requests.json` record:

- The actual Files picker uploaded one original 65,573-byte File using the
  existing multipart API. Reverse download returned HTTP 200 and identical
  SHA256 `7e5381cf6023e0c5129f2e10a337b58c5c6812ebf9849369173d20a950ab980d`.
  Observed download TTFB 107.250ms and end-to-end fetch/body duration 127.500ms
  are small isolated-payload observations, not a public throughput benchmark.
- Original authenticated media GET returned the 95-byte stored PNG, SHA256
  `5eb802f8c45fdd95c61a40e9aedf8bf6e42964902307bcd7f7c331dfac32b26f`.
  Its raw same-origin HTML image decoded at width 8. Mixed-content messages and
  page errors were zero. The preview probe uses an existing fixture original,
  not an invented model response or a new transfer implementation.
- The logical identity and SDK base remained
  `https://logical.fixture.test/api` throughout HTTP file dispatch.
- Under a controlled original secure document, the **original** uploader
  recorded an intent and its OPFS source, then a synthetic pre-dispatch
  interruption prevented Create. Under the HTTP document, the same original
  file/draft/owner/bot/logical origin produced the exact same journal key
  `946d3d62d1eb467c97d4c6a15982a350d38bfdfe066766510f932ca4d1c706b6`.
  Thus the logical task identity did not become a new HTTP identity.
- Nevertheless, the old secure-origin journal was not visible from the new
  HTTP page. Its original journal and source were readable when returning to
  the original secure document. No record was migrated or deleted.
- At the HTTP origin, `navigator.locks` and `navigator.storage.getDirectory`
  were undefined. Calling the unchanged uploader failed at its original lock
  acquisition with TypeError, before a journal write or a new media receipt.
  HTTP-origin journal count stayed 0. IDB availability alone is insufficient.

[Actual Files picker/list](actual-http-files.png) and
[same-origin image and Files](actual-http-preview-and-files.png) were captured
and inspected. They establish those specific controls and bytes. The center
chat uses unqualified fixture control/WebSocket transport and remains under its
loading mask; these screenshots do not assert chat readiness or whole-app QA.

## The distinct native connection gap

The authoritative native input remains the owner's
[`a158c56488ed193ec3e9f2d57480ad4f16a58068`](https://github.com/particleflow/Memoh/tree/a158c56488ed193ec3e9f2d57480ad4f16a58068/docs/testing/harmony-rcp-endpoint-20261029).
Its three-file packet uses the same v2 policy and maps at the original RCP seam.
The owner's API26 / 61 compatibility / 29 endpoint / bidirectional 8,388,641-byte
recovery evidence is reused, not rebuilt or rerun here.

The exact native snapshot was also inspected:

- `SharedWebPolicy.ets` retains `begonia-client://app` as the stable, secure
  packaged document origin. Changing a remote Web document is not the same as
  changing that native application origin.
- `SharedWebPolicy.memohRequest` accepts only the configured logical HTTP(S)
  protocol, its corresponding socket protocol, exact host/port and API prefix.
- `SharedChat.intercept` returns 403 for unrecognized remote resource URLs.
  Its existing `.mixedMode(MixedMode.All)` at line 397 was not modified. This
  existing setting does not make a rejected physical URL pass the policy.
- The original native RCP request is outside this Web-resource interceptor.
  RCP support therefore does not establish generic SDK fetch, Files or raw
  image/video/audio resource coverage.

The exact fetched `memohRequest` source and Git blob identity are retained in
`native-guard-source.json`. Five predicate cases passed with a standard-URL Kit
adapter: original HTTPS allowed, same-host HTTP/physical HTTP/foreign/encoded
escape refused. This is source-level diagnostic evidence, **not ArkWeb runtime
or new RCP qualification**. There is no alternate native product patch here.

The concrete remaining connection task belongs with the current native
authenticated-fetch/resource owner: consume the trusted same-identity mapping
below the original logical guard and preserve the stable packaged origin.
The optional `fileRouting:'platform'` seam in 68c must only be enabled after both
existing fetch and resource adapters own that mapping. Merely allowing all HTTP
destinations or changing the saved service URL does not meet this condition.
The current service-config producer sets `resetPending` when `apiOrigin`
changes, and the entry clears existing user-scoped state during that transition;
it is not a route-only migration and was not invoked.

## Cloud upstream path audit

The audit separates file producers from provider/CDN import. In this exact
shared source, ordinary external attachment imports do not automatically pass
through the NAS public Web/API origin:

| Consumer | Actual destination/source | Endpoint-map implication |
| --- | --- | --- |
| `internal/transferclient/producer.go` and `host_action.go` | Existing admitted producer queue invokes `HostConfig.newDeviceTask`; `host_network.go` applies the v2 map to original task/media/receipt HTTP calls | Cloud/Windows producer using this client is in 808cf801's coverage. Do not exclude it as an external CDN pull. |
| `internal/channel/inbound/channel.go:2983` | `openInboundAttachmentURL` creates a GET from the attachment's `rawURL`; platform-key resolution belongs to the channel adapter | External provider/CDN URL goes to that source, not an implicit NAS HTTPS entry. |
| `internal/channel/outbound_prepare.go:378` | `openPreparedAttachmentURL` creates a GET from the supplied attachment URL, then ingests the resulting reader | No shared file endpoint rewrite is injected. |
| `internal/agent/tool/image_gen.go:396` | `fetchGeneratedImageURL` downloads the actual provider-result URL using its original guarded image transport | Provider image/CDN downloads retain their URL, TLS, SSRF and redirect checks. |
| `internal/media/service.go:78`, `internal/storage/providers/containerfs/provider.go:48` | Ingest hashes/spools the reader; configured storage writes local originals or uses the original workspace gRPC stream | This storage write is not an HTTP loop through the public NAS Web endpoint. |

One original channel HTTP-import test passed with a real controlled HTTP source
and exact recovered bytes. Two original DashScope/OpenAI provider-result URL
tests also passed using their existing SDK/dialer test adapters. These tests
make no real provider/model request and measure no public cloud throughput.

The first provider test attempt was stopped by the environment's private egress
proxy being rejected by the existing image SSRF guard. That failure is preserved
with the infrastructure address redacted. Setting `NO_PROXY=198.51.100.10` only
for the original TEST-NET fixture address let the existing test dialer reach its
local fixture; the production guard/transport was not changed. Both focused
tests then passed. This is a fixture correction, not a live proxy reconfigure.

This excludes a blanket rewrite of external provider/CDN traffic to HTTP. It
does not prove every deployed cloud attachment is external: if a specific
attachment URL or redirect itself points to the NAS logical HTTPS origin, the
generic importer is a separate caller and is not automatically covered by
`MediaIdentity.fetch` or the native task client. No such live URL, redirect chain,
egress configuration or deployed cloud-runner source was read here. That case
needs its actual same-NAS destination established before a scoped connection
change; arbitrary upstream URLs must not be silently substituted.

## Unified combination and remaining status

`combination-manifest.json` records the exact shared dependency order and native
packet provenance. For the shared repository, combine 808cf801, the evidence-only
9e968f8cd/d1f2c0b94 commits, then 68c012969. This follow-up adds evidence only.
The native owner's a158c is the **final correction commit**, not a standalone
three-file implementation. Use its reviewed branch prerequisites or the
`native-product.patch` packet from that exact commit against the current native
source. The packet has three before/after blob pairs in the manifest; no old
mobile tree or unrelated diagnostic work should replace current source.

Remaining statuses are specific: ordinary HTTP cannot preserve the existing
browser persistence capabilities; generic native fetch/resource policy adoption
remains with the original owner; actual NAS HTTP API/CORS/site/port activation
is not attempted and belongs to the parent/single writer; public phone/Windows
and deployed cloud throughput remain unmeasured. There was no approval denial
in this increment and no real-credential transmission. No device test was
requested and no full-chain acceptance was declared.

## Final route-only combination handoff

[Final path/file manifest](final-path-combination.json) expands the existing
combination manifest with every changed file, product/evidence separation,
per-route coverage and exact activation conditions. It introduces no new product
change and reruns no prior tests. The shared order remains 808cf801, 9e968f8cd,
d1f2c0b94, 68c012969 and abb2f6970; the native packet remains with its original
owner and current generic bridge work.

The central authenticated profile is also consumed by browser clients. Therefore
a native RCP pass alone does not qualify enabling a global HTTP endpoint mapping
for an existing HTTPS Web page. The HTTP destination must expose the original
authenticated API with the same prefix and backend/receipt identity; a static
package server alone is not sufficient. Real NAS API/site/CORS/port settings
remain solely with the parent/single writer.
