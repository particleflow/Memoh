# Central file network policy GET integration

This independent source increment starts at PR79
`391adba82a2347123f02f29fdbd289cde84d26cb`. That remote head was rechecked on
2026-10-26. Its `[files]` configuration, authenticated current-user GET and
file limits GET contained no centrally distributed LAN policy. Windows read
only its optional private file; the existing Harmony consumer at
`c530567d2c8f35547e2f1cc550859f3da016b77f` read only the optional signed seed.
The latest Harmony branch `5d0f2df38d57588f313831459dac749d125918a9` retained
that same consumer. No existing centralized implementation was found in those
owner checkpoints. The earlier 2d03f6cb contract is historical, not product
implementation of this gap.

## Existing configuration and authenticated reads

An optional section in the existing NAS configuration uses the original
version 1 policy shape. These documentation addresses are synthetic examples,
not defaults or a field address plan:

```toml
[files.network_policy]
version = 1
lan_addresses = ["192.0.2.1", "2001:db8::1"]
```

Server validates and snapshots IP literals in order at startup, as with the
existing per-file upload configuration. Invalid, duplicate, unspecified or
multicast addresses fail startup; the existing 8192-byte policy bound remains.
Changing the section requires the existing Server restart procedure. No new
service, endpoint, credential, port, origin, storage root or transfer budget is
introduced. No deployment configuration was changed by this task.

The original authenticated GETs `/users/me`,
`/bots/{bot_id}/media/limits`, and
`/bots/{bot_id}/media/uploads/{upload_id}` optionally include
`network_policy: {version:1,lan_addresses:[...]}`. Existing response fields,
authorization and task/device guards remain. Responses are no-store. No
configuration API write, connectivity probe or filesystem path is exposed.
The receipt store and business write responses do not persist this field.
OpenAPI and SDK were regenerated through their existing generators.

## Consumers and retained boundaries

- Configured Windows uploads consume their original limits GET when queued,
  and their original receipt GET when resumed. No extra limits request is
  added for each chunk/scheduler invocation. A configured download reads the
  existing task-authenticated limits once before its file stream. The policy
  remains in that opened client. It never rewrites transfer.json, its byte
  binding, the optional local policy, queue, receipt, offset, hash or reference.
  An omitted central policy retains the original local behavior. A valid NAS
  policy takes precedence for that client. Transport rebuilding starts from
  the validated original transport, preventing stacked old/new dialers and
  discarding the bootstrap pool. The existing LAN order, 30-second LAN phase,
  original-authority IPv6 TCP fallback, proxy refusal and no TLS/HTTP fallback
  remain.
- Shared browser media reads the same current-user GET policy snapshot.
  Fetch remains platform-routed. The implementation never replaces the URL
  with a hint IP, supplies a Host override, or replays a business request.
- The separate Harmony patch reuses NativeTransferNetwork and the existing
  upload/download identity GET. Only a successfully authenticated identity
  supplies the per-transfer policy. An omitted policy preserves the prior
  private-seed/platform path; no seed/default/saved endpoint is modified.
  Existing safe JSON GETs may select/cache a route. Any HTTP response retains
  that connected route, including the old limits-404 compatibility case.
  POST/PATCH/DELETE, HEAD and file-byte GET remain single dispatch.

The bootstrap identity GET must already reach the canonical NAS authority.
No cross-operation policy cache or discovery request was added. In a cold
Harmony download, after learning a policy from that GET, the first HEAD/file
read uses the first candidate once; failure does not cause automatic stream
replay or prove that a request was unsent. Strict all-end LAN/IPv6 handover is
therefore still open. RCP error codes and DNS hints do not establish the
Windows dialer's pre-send TCP guarantee. Proxies and literal authorities keep
the existing platform behavior. Field hostname, addresses, system TLS and
actual engine behavior remain unverified.

## Executed focused validation

- Go 1.25.7: 12 top-level tests and 9 subtests passed across media, config,
  handlers and transferclient. Coverage includes central config validation,
  anonymous/forbidden policy nondisclosure, current-user policy, exact existing
  receipt reuse without extra limits reads, one-time download bootstrap, LAN
  order, IPv6 resume, cancellation, proxy conflict, original queue binding,
  and no TLS/HTTP fallback. TLS fixtures use controlled test roots only.
- Shared Web: one new real-SDK policy/identity regression passed (19 unrelated
  tests in that file were intentionally skipped). Full Web and SDK type
  checks and targeted ESLint passed. UI contract passed with its 3 existing
  warnings. The previous 160/129 matrices were not rerun.
- Harmony: 66 product-source/Kit-boundary assertions passed. With no seed
  policy, 19 controlled 2 MiB upload cases passed; existing metadata GETs
  consumed NAS identity policy, tried the explicit hints in order and then
  original-host IPv6, while uncertain writes retained their original IDs.
  Controlled 2 MiB download passed scoped access, hash, process-death restart
  and exact offset checks. These are Node adapters, real disk and controlled
  local HTTP, not RCP/ArkWeb/system-trust/device evidence. Source manifest:
  131 entries/140 files, no held source; original 19 historical unverified
  changes remain historical. No HAP compilation was performed.
- Current full Server plus PostgreSQL schema 177 and current Vite Web ran in
  an isolated database at API18580/Web18583. Chromium used the original /api
  proxy, synthetic login and Bot. The UI selected a synthetic file; direct
  calls to the actual media module then uploaded/downloaded 65,573 bytes,
  SHA256 `7f38ce5c2014837812bf5e26ae1f70349c074209bdc7468019c308b903bc1699`.
  Create/PATCH/Complete were each one, original hash matched, all three GET
  policy surfaces agreed, and there were zero page errors/model calls. The
  screenshot shows the selected composer file; no chat message was submitted.
  Workspace creation was deliberately absent; its unrelated Files pane can
  report unavailable. This does not verify an actual producer/workspace.

Fixture recovery: initial container Go toolchain/root-store and pnpm version
setup failed before checks; corrected tools ran without weakening TLS or
changing the lockfile. An early download test used an upload-only fixture and
was fixed to serve actual bytes. The full old phone upload runner reached an
unavailable official-context.tar frontend stage; only its relevant 19 native
matrix cases were rerun using its existing matrix-only option. The initial
browser helper incorrectly expected selection to submit a file, then used an
input FileList already cleared by the UI; corrected helper exercised the
actual module separately. A cross-origin fixture could not expose integrity
headers; the final run used the original same-origin /api proxy. No product
workaround for these fixture errors was added.

No remote push, PR update, CI run, NAS deployment, signing, installer build,
phone installation or Windows operation occurred. F01/F02 and device/network
acceptance remain open. Human QA is unverified. The two source patches are
for the existing unified owner to combine; current e614 and PR79 history are
preserved.
