# Preview and workspace Files connection coverage

This source increment continues `d1f2c0b943423c5899b18e28b3fd5d9bb56143a1`
on `fix/fast-transfer-path`. It closes the two shared-Web consumers identified
in the earlier connection coverage: raw stored-media preview URLs and workspace
Files operations. It does not declare the full phone/Windows/NAS/cloud chain
switched or accepted. No production endpoint/configuration/credential, Agent
core, file protocol, queue, signing process or phone SDK was changed.

## Original paths and connection-only changes

The source check confirmed that `useMediaGallery.ts` produced raw SDK media
URLs for image/video/audio and delegated previews. `files-pane.vue` used SDK
filesystem calls plus direct fetch/anchor downloads. `file-viewer.vue` had a
separate download anchor. These bypassed `MediaIdentity.fetch`, so the earlier
persistent-media routing did not cover them.

The existing authenticated `GET /users/me` now supplies an ephemeral per-tab
file endpoint snapshot. It is bound to the original SDK base, current token,
auth generation and returned identity; it is not persisted. A late response
from an old account is refused. No login, new metadata endpoint, credential
issuer, policy write, queue or per-chunk discovery request was added. The same
version 2 endpoint validator is reused. Omitted policy/version 1 retains the
original physical path.

- `sdkFileApiUrl` resolves only SDK-relative protected file paths through the
  explicit mapping. Attachment metadata, Bot/hash/delegation scope, saved API
  origin and existing journal keys are not rewritten. External attachment URLs
  remain external, and mismatched query credentials/foreign routes are refused.
- Existing SDK workspace `/container/fs` calls map in the original request
  interceptor. The existing serialized BodyInit is retained. In particular,
  FormData keeps the same File values: it is not read into a whole-request Blob
  or converted to a ReadableStream requiring H2. The browser regenerates its
  multipart boundary. Existing methods, file/path fields, conditional headers,
  signals, permissions and the four-worker upload batch remain.
- Direct archive/extract fetches use the same route guard and their original
  JSON bodies. Files-pane and file-viewer download anchors use the mapped URL.
  The existing small automatic text line-count fetch uses the same guard.
- Mapped SDK/direct fetches refuse redirects and recheck identity at dispatch.
  Pending requests after an account switch cannot send the prior credential to
  a mapped endpoint. Other API/auth/SSE routing is unchanged.

Raw HTML image/video/audio tags retain their original browser resource/redirect
semantics; they were not replaced with a new fetch/blob transfer scheme. In the
observed cross-origin image redirect, the foreign server received neither an
Authorization header nor a copied query credential. SDK upload redirects were
refused, with no foreign request or retry. These are distinct observations.

## Executed isolated verification

Actual current Vite Web, Go Server, PostgreSQL schema 180 and existing synthetic
workspace ran at Web18683/API18680 with physical HTTP adapter18684. The same
synthetic fixture account and workspace were reused. There was no chat/model
submission or production/NAS access.

- `final-entry-tests.txt`: 71 relevant entry tests passed: 8 new routing/identity
  cases and 63 existing auth/gallery/Files/viewer regressions. Full Web types,
  targeted source/evidence ESLint, UI copy policy and UI contract passed; the UI
  contract retains its 3 existing warnings. The final platform dispatch-guard
  adjustment then passed the 8 focused cases again (`final-platform-tests.txt`)
  and current types/lint; no large-file baseline was repeated.
- Actual `AttachmentBlock` was mounted with the running app's context and a
  synthetic stored PNG. Its physical-endpoint image decoded at width 8;
  attachment identity was unchanged. Source bytes: 95, SHA256
  `5eb802f8c45fdd95c61a40e9aedf8bf6e42964902307bcd7f7c331dfac32b26f`.
- The actual Files input selected three File objects. Three original multipart
  uploads reached the physical endpoint once each. Reverse downloads contained
  9, 31 and 65,573 bytes and their original SHA256 values. The actual Files-menu
  anchor and separate file-viewer Download button also downloaded identical
  bytes from the physical endpoint. The adapter observed the 65,573-byte
  reverse download with TTFB 12.377ms and duration 12.704ms; preview GET
  TTFB 6.541ms and duration 6.834ms. These are isolated small-payload
  dispatch observations, not public-path throughput claims.
- The original archive POST returned a 805-byte tar.gz. `archive-proof.json`
  verifies that its three files match all original hashes. No archive, transfer
  algorithm, concurrency or recovery implementation was changed.
- Actual mapped SDK 307 upload redirect was refused. An already-resolved URL
  from a previous token and a token switch between SDK routing and final
  dispatch were refused before transport. Page errors were zero.
- `physical-requests.json` retains methods, byte counts, timing and credential
  presence flags; query credential values were removed. `browser-proof.json`,
  `file-viewer-proof.json` and `archive-proof.json` contain the byte/hash results.

Screenshots were captured and inspected from the actual app:
[stored preview](actual-preview-physical-endpoint.png),
[original multi-file picker and Files list](actual-files-multi-upload.png),
[file-viewer Download action](actual-file-viewer-download.png).
The preview uses synthetic attachment data in the real component, not a model
message or claimed business result. The binary viewer's normal unsupported-type
view and Download action are expected, not a new product error state.

### Actual Chromium mixed-content constraint

`mixed-content-proof.json` uses a controlled HTTPS document and current Vite
modules fulfilled locally, then the real Chromium network/security engine.
This isolates mixed-content behavior without adding a trust root, disabling
certificate verification or disabling browser security. It is not an actual
HTTPS server/TLS/public-path benchmark.

The secure context resolved the explicit HTTP physical endpoint. Chromium
blocked active HTTP fetch, automatically upgraded the image request to HTTPS,
and the upgraded image request then failed in this environment
(`ERR_TUNNEL_CONNECTION_FAILED`). The adapter recorded zero
HTTP requests from that secure page. Console/network observations are saved;
no fake successful transfer or plaintext real credential is claimed.
[Constraint screenshot](mixed-content-browser-constraint.png).

Therefore ordinary HTTPS Web cannot be assumed to use a public HTTP physical
route. A matching browser-permitted origin or the existing native transport
adapter is required. The app does not bypass this platform constraint.

## Adopted Harmony owner's RCP increment

The current native owner has independently completed
[`a158c56488ed193ec3e9f2d57480ad4f16a58068`](https://github.com/particleflow/Memoh/commit/a158c56488ed193ec3e9f2d57480ad4f16a58068).
Its [source packet and evidence](https://github.com/particleflow/Memoh/tree/a158c56488ed193ec3e9f2d57480ad4f16a58068/docs/testing/harmony-rcp-endpoint-20261029)
are the native input for unified combination. The owner's README was fetched
and its shared contract confirmed: existing authenticated logical `/users/me`,
`network_policy.version=2`, optional `lan_addresses`, and
`endpoint={logical_origin,physical_origin}`. It retains the original logical
upload owner/download checkpoint identities and maps at the original RCP seam.

The owner reports official API26 narrow compilation, 61 compatibility cases,
29 new endpoint cases and 8,388,641-byte upload/download process-death recovery
across changed physical ports, identical saved hashes and Create/Complete once.
Those are the owner's evidence, not tests rerun here. The source packet changes
three native product files. There is no replacement native patch in this Web
increment. The local stale-base native proposal was withdrawn before commit.
Do not rebuild or combine it as an alternative implementation.

The shared Web exposes the optional host-owned mode
`setupApiClient({ fetch: approvedExistingNativeFetch, fileRouting: 'platform' })`.
It requires an explicit transport, preserves logical URLs for SDK requests,
raw previews and persistent media, retains the authenticated policy and final
account checks, and leaves physical mapping below the original native identity
guard. Default browser routing remains. A response/redirect cannot select this
host mode. This seam was unit-checked and is not enabled in a distributed host.
Enable it only when the current native fetch AND resource adapters actually own
the mapping. The adopted RCP upload/download packet does not by itself establish
generic ArkWeb resource interception or current P1 authenticated-fetch coverage.

The original owner keeps the single final HAP/package workflow. Its cited saved
package predates the source increment. No HAP, SDK, signing, installation,
deployment or phone notification was performed by this task.

## Fixture recovery and remaining chain

The initial physical adapter omitted Content-Length when decoding a chunked
archive response, causing the browser to wait for EOF. The original direct
archive API returned immediately. Adding correct framing to the isolated test
adapter closed the test; no product/archive workaround was added. 

The earlier 100,663,395-byte three-file process recovery and 12,582,949-byte
browser/server restart evidence was reused. The large-file/old 44-case baseline
was not rerun in this increment. This phase used only small payloads needed to
exercise the newly connected consumers.

Still open: unified adoption of the completed native RCP increment and generic
native fetch/resource bridge coverage; actual NAS existing HTTP API/CORS
activation and real-credential handling by the parent/single writer; physical
Windows/Harmony package adoption and public phone/Windows/NAS performance; direct
external cloud/provider-to-NAS upstream speed. The two Web consumers above are connected in isolation. They
are not the entire chain. No device test was requested and no overall delivery
acceptance was marked passed.
