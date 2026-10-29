# Harmony logical Web requests through the existing native connection route

Source parent: `a158c56488ed193ec3e9f2d57480ad4f16a58068` on
`review/harmony-image-timing-20261026`. This increment addresses the native
consumer gap documented at `abb2f6970d78cd433e5f055719ad866efcd45fba` in
`docs/testing/transfer-http-entry/combination-manifest.json` and its README.
It includes the complete a158 branch prerequisites; a158 alone is not the
three-file RCP implementation. The earlier packet is retained unchanged at
`../harmony-rcp-endpoint-20261029/native-product.patch`.

## Actual connection seam

`SharedChat.intercept` previously returned `null` for authorized logical API
resources and let ArkWeb dispatch directly; it was not an authenticated native
fetch RPC. Its `WebResourceRequest` has getters, not a URL setter. Simply allowing
the physical HTTP origin would not implement routing, lifetime binding or body
preservation. No such allowlist expansion was made.

`NativeWebNetwork` uses the installed SDK's `WebSchemeHandler` below that
unchanged logical guard. Only the original packaged document's subrequests are
eligible. The original `/users/me` GET is observed through RCP with normal system
TLS, no redirect, and a bounded 64 KiB profile response. Its original response
bytes, status and logical URL are returned to the page. This is the existing GET,
not an additional identity probe. A valid owner and the original v2 policy must
pass `NativeTransferNetwork.adopt` before the matching credential can use a route.
Legacy HTTP logical services are left on their original engine path.

Only existing Bot media, container filesystem and delegated attachment paths
are mapped. Other control, chat, viewer, socket, external and unbound requests
retain their existing path. An existing query-token resource must match the
same bound bearer identity; missing, conflicting or duplicate credentials do not
authorize routing. No bearer is added to query-token-only resources. A direct
physical URL is still rejected by `memohRequest`/`SharedChat.intercept`.

`NativeTransferNetwork.resource` changes only the actual scheme/host/port on
the same RCP request, retaining logical path, query, method, original headers and
body stream. ArkWeb body reads use at most 64 KiB; native response chunks are
forwarded to its original resource handler. The browser sees the original
logical response URL. Cookies are not copied into a second jar; existing token
authentication is retained. The adapter requests identity content encoding and
refuses an unexpected encoded response rather than exposing mismatched bytes.
Redirects and transport errors are returned as failures with no fallback or
automatic replay. RCP system TLS and existing request defaults remain in force;
this is not a new total file budget or a throughput qualification.

Token/owner/policy changes, login/configuration resets, document teardown,
main-document failure, or ArkWeb request cancellation invalidate pending streams.
Checks run again after body initialization, before actual RCP dispatch, and at
each body callback. Old receipt IDs, offsets, owner/Bot/operation identities,
journals, transfer expiry, source files, and originals roots are not rewritten.
The existing persistent native upload/download consumers remain separate and
unchanged except for their already reviewed a158 network helper.

The generated Harmony `main.ts` now supplies the **existing shared**
`fileRouting: 'platform'` setup option. Older shared clients ignore the extra
option. When composing `68c01296981f2d0c173226c5a3590ca4ad59e0fa`, its API client,
Files resource helper and media identity code recognize it and retain logical
requests, preventing double mapping in Web and native. No shared transport
implementation, SDK, Web tree or new configuration schema is copied here.
Across 6,995 generated context files, only `apps/web/src/main.ts` differs from
the existing b5e7 generated checkout. The packaged origin remains
`begonia-client://app`; no HTTP page, storage migration or security exception
is used.

## Verification and retained failures

- Official CLT 26.0.0.851 / SDK 26.0.0.105 / API26
  `hvigorw default@CompileArkTS --mode module -p product=default
  -p module=entry@default -p buildMode=release --no-daemon` passed on the final
  native sources (`api26-admitted.log.txt`). This does **not** assemble a HAP.
- `node mobile/client/tests/stage2/harmony_web_endpoint.cjs`: 23 controlled
  cases passed against the actual ETS classes. Loopback HTTP carried 3,145,767
  deterministic bytes in both directions, with SHA256
  `664555bf1da63a12af71c727333914fe1109b27453b7929ef1a5d94a5efb40d9`.
  The valid multipart wire body is 3,145,924 bytes and is separately hashed in
  `controlled-wire.log.txt`. The test covers original request headers/body/query/Range, scoped delegation
  denial without hash fallback, 401/403/404/503 without replay, redirects,
  uncertain PATCH, changed credentials/owner, revoked identity, invalid policy,
  cancellation before body dispatch and legacy logical HTTP behavior.
- The original 61-case network-policy suite passed, including existing
  NativeClient callers with no additional reset callback.
- The new generated context's full Web `typecheck` passed. Its only generated
  difference and the exact main diff are retained with the results.
- Source inventory passed: 182 text inputs, 198 tracked/declared files and seven
  approved PNG inputs. The 19 unverified historical later changes remain unknown.
- The first new test run failed on encoded `%2e` segments: the original URL guard
  checked after URL parsing, which can normalize those away. The refusal now
  occurs before parsing, preserving the intended boundary. The first failure
  and test-adapter listener warnings remain in `controlled-first.log.txt`; the
  listener lifecycle was corrected, not its assertions. Later intermediate
  successful logs are retained too.

The HTTP fixture uses actual sockets and byte streams, but substitutes ArkWeb
callbacks and RCP at the Kit boundary. The logical HTTPS identity GET is adapted
to a private loopback fixture, not tested over TLS. These are not actual ArkWeb
or RCP-engine wire results, phone URI/storage tests, NAS HTTP activation, or
evidence of public 5 MiB/s throughput. The previous a158 process-kill/restart
upload/download recovery evidence is reused; it is not rerun or relabeled as
this Web resource adapter's process recovery.

No actual device runtime is attached to this cloud executor. Runtime ordering,
scheme-handler response/CORS/cancellation behavior, large/slow-resource behavior
under existing RCP request defaults, and real TLS/physical-path performance must
be verified in the final combined candidate. No replacement UI screenshots or
Web fixture are claimed as Harmony runtime evidence. The parent will coordinate
one final phone validation after all fast-path consumers are combined.

## Packaging boundary

This is source and compile validation only. No version increment, HAP assembly,
signing, NAS/Windows change, private credential use, or new artifact publication
was performed. The existing 1.0.30 package remains tied to source `61355f0d` and
shared `b5e7aff6`; it contains neither a158 nor this adapter. Its existing public
handoff and private copy are retained without changing their identities.
