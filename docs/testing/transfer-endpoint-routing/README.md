# Explicit physical endpoint routing

This independent increment starts at `b5e7aff6040d1efce36ee51a0c5d3f8915dbddf3`.
It is source for the existing unified owner to integrate. It has not been
activated on the NAS, distributed in a HAP, or accepted on a physical phone or
Windows machine. No production credential, NAS setting, listener, TLS trust
store, official Agent implementation, or paid model was used or changed.

## Connection-only change

The existing startup `[files.network_policy]`, authenticated metadata GETs,
and private Windows `transfer-network.json` accept an explicit version 2 map:

```toml
[files.network_policy]
version = 2
[files.network_policy.endpoint]
logical_origin = "https://nas.example.invalid:18443"
physical_origin = "http://nas.example.invalid:18068"
```

These are synthetic documentation hostnames and ports, not an activation plan.
Origins contain scheme/host/port only: no path, credentials, query, fragment,
escaped separators or trailing slash. Use canonical ASCII origins; omit default
80/443 ports. The original `/api` and task/file suffix are preserved. Existing
logical Go bases with an explicit default HTTPS port keep their original byte
binding and work with the equivalent canonical policy origin.

The operator must explicitly assert that both origins serve the same NAS.
A mismatched logical origin fails before dispatch. The endpoint is not learned
from a redirect or a failed write. Go rejects a proxy conflict and retains
certificate verification for an HTTPS physical target. Redirects remain refused.
The browser rechecks the original account/token generation before dispatch,
including after preparing mapped request bytes. Cookies are omitted on a mapped
request; the existing Authorization protocol and scoped request paths remain.

Go changes the cloned dispatch request in a RoundTripper. Shared Web changes
only its guarded media fetch. The logical API base, Bot/operation/delivery,
HostBinding, journal keys, receipt, source snapshot, offset, SHA256 and result
reference are retained. No changes were made to upload/download algorithms,
chunk/worker budgets, scheduler, disk queue, recovery log format or permission
handlers. No new anonymous endpoint, credential, ticket or database migration
was added. OpenAPI and SDK changes were produced by their original generators.

Chromium treats a `Request.body` copied as a ReadableStream as a streaming
upload requiring HTTP/2. The dispatch adapter preserves the existing bounded
request body as a Blob so the explicit HTTP/1 path works. It does not buffer a
whole file or change the existing 1 MiB shared-Web chunk size. The account guard
is checked again after that asynchronous preparation and before the single send.

## Compatibility and activation

Omitted policy and version 1 TCP hints keep their previous behavior. Version 2
is opt-in; older strict consumers reject it. Upgrade all consumers before
activation. Keep `transfer.json`, login/saved API origin and existing queue/IDB
identities unchanged. Change only the separate routing policy. Returning to
version 1 or omitting the routing policy returns the physical connection to the
original logical endpoint without migrating the queue. Uncertain mutations
still reconcile their original receipts; switching routes does not authorize a
new Create or an operation replay.

Ordinary browsers retain mixed-content, CORS and private-network restrictions.
No TLS/certificate/mixed-content bypass was added. An HTTPS page cannot be
assumed to fetch an arbitrary public HTTP endpoint. The actual browser run here
used HTTP loopback origins. The production native client HTTPS logical identity
to HTTP physical origin transition was exercised in the isolated Linux Go test.
Neither is a phone-public-network speed result.

The independent Harmony owner must consume the same version 2 mapping below
its existing native identity guard, on its current source. The remote PR82
snapshot `a2f3c9f6c597f71688c5677dccfcfc2ce659e95e` was inspected only as a source
map; it is not asserted to be the current 1.0.28 source. Relevant native files:

- `mobile/client/mobile/harmony/app/entry/src/main/ets/common/ServiceUrl.ets`:
  version 2 validation and logical/physical origin representation.
- `mobile/client/mobile/harmony/app/entry/src/main/ets/common/NativeTransferNetwork.ets`:
  validate the original target/identity, then construct one original RCP request
  at the explicit physical target, retaining method/body/headers and system TLS
  validation for HTTPS. The current v1 implementation changes DNS rules only.
- Current native upload/download/bridge callers must continue to bind journals
  to the original logical base and accept the centrally authenticated policy.
  Do not enable v2 on an old native bridge that rejects the rewritten URL as a
  foreign identity. Do not retry a mutating request to another endpoint.

This increment does not replace that owner's current P1 journal/auth-fetch
work, rebuild/sign a phone package, or change current native source blindly.
Real-credential HTTP activation and actual endpoint/HTTP API/CORS configuration
remain with the parent and the single NAS writer. The original fast static HAP
route alone does not prove that authenticated business routes are connected.
Cloud-produced attachment file downloads consumed through the guarded media/hash
functions use this shared route. Raw thumbnail/image/video/audio `src` builders in
`useMediaGallery.ts` and workspace Files-pane direct fetch/anchor paths still use
the original SDK URL and require their connection consumers to be integrated; see
`connection-coverage.json`. Direct provider/cloud-to-NAS upstream fetches were not
modified or benchmarked by this increment.

## Executed evidence

- `endpoint-final-validation.jsonl`: new v2 startup configuration, immutable
  snapshots, malformed destination refusal, central authenticated bootstrap,
  cross-identity refusal, explicit-default-port identity preservation, redirect
  refusal and HTTPS physical certificate refusal passed.
- `endpoint-race.jsonl`: new routing/process/concurrent-file tests passed under
  the race detector. Later changes added canonical/default-port validation and
  more precise performance accounting; those focused tests passed separately.
- `endpoint-large-process.jsonl`: production disk queue/client, isolated
  synthetic-auth HTTP adapter. Three distinct files, each 33,554,465 bytes,
  completed with original SHA256 and one Create/Complete per file. The first
  receipt was created over HTTPS, its first HTTP PATCH response was lost, the
  child process exited without cleanup, and a new process continued the same
  receipt. Only the routing file changed; HostBinding stayed identical. Three
  existing worker slots were used; no byte was re-uploaded after reconciliation.
  The timed interval sent 99,614,819 bytes in 344.271 ms (275.946 MiB/s), excluding
  the first 1 MiB already sent before timing. First-request TTFB is logged per
  job and includes the original metadata/receipt read. The 33,554,465-byte
  reverse download resumed at 1,048,576, took 125.446 ms including verification,
  and recorded a file-request TTFB of 28.781 ms. A revoked complete cache was
  refused. These are loopback/disk fixture numbers, not network guarantees.
- `web-final-tests.log`: 25 shared-Web tests passed, including HTTPS logical
  identity to HTTP dispatch, unchanged chunk bytes, no duplicate Create,
  interrupted recovery and an account change while preparing a mapped body.
- Full Web/SDK type checks, targeted ESLint, changed-package Go lint, UI copy policy and UI contract
  passed (3 existing UI contract warnings). Windows amd64 test compilation
  passed; the resulting executable was not run on Windows.
- Actual current Server/PostgreSQL schema 180 and Vite Web ran at
  API `http://127.0.0.1:18680`, Web `http://127.0.0.1:18683`; the isolated physical
  adapter was `http://127.0.0.1:18684`. No existing application database/config
  was reused or overwritten. The prepared development workflow was reproduced
  with a separate database, configuration, binary and ports.
- `browser-proof.json`: actual application file picker displayed the selected
  synthetic attachment. The actual media module, SDK, IDB and OPFS transferred
  12,582,949 bytes. After a committed PATCH lost its local acknowledgement,
  Chromium was closed and reopened with the original profile. Journal key,
  receipt, logical origin and original source survived. Upload reached ready
  in 2,048.9 ms after reopening; download took 491 ms. SHA256 in both directions:
  `45acb94535abd613b98e13ebf371e652819650ea09808fcd8da81c8a53e5f02f`.
  The filtered physical trace records exactly 13 PATCH calls totaling the
  original 12,582,949 bytes. No repeated confirmed prefix was sent. No chat
  submission or model call occurred; page errors were zero. Upload/download
  were exercised through the real module after selecting the file in the UI;
  screenshots do not claim a chat/model message was submitted.
- `server-restart-browser-proof.json`: after rebuilding/restarting only the
  isolated Server at current source, the original ready receipt/hash remained.
  A browser with fresh local storage downloaded the same 12,582,949 bytes in
  1,344.9 ms, with measured browser file-response TTFB 8.6 ms and matching hash.
  TLS validation was not disabled. No page error occurred.

Screenshots were captured and inspected from the actual application:
[attachment selected](actual-app-selected-file.png),
[after browser process recovery](actual-app-after-process-recovery.png),
[after current-source Server restart](actual-app-after-server-restart.png).
They show the actual app and selected attachment/recovered session; hashes,
receipts and transfer assertions are established by JSON/HTTP evidence.

## Fixture recovery and remaining acceptance

The initial default Go build cache was read-only; the existing workspace cache
was used. The first standalone build failed VCS stamping in the isolated
worktree; the fixture binary was built with `-buildvcs=false`, not as a release.
An initial UI locator hit onboarding/the wrong hidden input; the isolated
synthetic user's normal profile endpoint and the actual composer input were
then used. Initial mapped browser writes exposed the Request streaming/H1
issue described above. The first response-loss assertion incorrectly expected
a local offset ACK after an aborted response; it was corrected to require
`patch-unknown` at offset zero and recovery from the server's existing receipt.
Those isolated unknown/partial journals were retained, not rewritten as
successful sends. No production history was touched.

The 44 pre-change baseline pass events are not presented as new-path evidence.
NAS/public phone/physical Windows throughput, actual native RCP routing,
physical package adoption, direct cloud upstream speed, real-credential HTTP
activation and final once-only device acceptance remain open. Human QA is
unverified. Do not announce the full chain switched or ask for device tests
from this source handoff.

The expanded handler lint reported the existing unused `modelID` parameter at
`internal/handlers/local_channel.go:2717`; the exact same function exists at the
pinned base. That unrelated handler was not edited. Changed media/config/client
package lint passed after fixing the new fixture lint findings. All four affected
Go package test commands passed; they are not counted as new routing test cases.
