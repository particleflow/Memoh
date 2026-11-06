# Unified candidate: paged delivery ledger and task HTTP projection

## Sources and scope

The preserved merge checkpoint `a36d89d5` joins candidate
`58c624cf0a4db56afc9d3e5415135e56606aa958` and fixed PR77
`0f4a4ae8b615098f8a4f34cf187160c37c689996`. No conflicts; all 40 incoming
paths match the source exactly. Channel return routing, configurable delegation
deadline, SDK Images integration, schema 173, Web and official Agent core were
unchanged by that merge. This follow-on adds only a bounded read projection to
existing task HTTP routes. No dispatch, credential creation, second ledger or
new transfer service.

## Shared Web / phone contract

`GET /bots/{bot_id}/sessions/{session_id}/tasks/computer_operation/{task_id}/deliveries?after=...`
uses the existing JWT/API base (normally the Web proxy's `/api`, stripped before
the server). The SDK export is
`getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdDeliveries`.
Omit `after` for the first page, pass `next_after` verbatim for subsequent pages,
and continue while `has_more` is true. Each page contains at most 16 `files`,
`version`, `operation_id`, string `revision`, string `next_after`, and `has_more`.
Each file contains `delivery_id`, `state` and, **only when committed**, `reference`
(`version`, `direction`, `content_hash`, `size_bytes`, `mime`). No file path,
transfer lease/token, raw output or download URL is exposed. A reference is not
permission to download through a broader Bot/hash route.

Every request rechecks the current conversation owner, parent/child membership,
Bot chat permission, PC scope, target fingerprint and execution grant. Unknown
or unrelated tasks remain 404; invalid continuation is 400; anonymous is 401;
revoked chat permission is 403. These responses retain `no-store` before JWT.
Reading never polls a device or changes an operation. `has_more=false` is the
current end, not completion or a snapshot. Continue from it for late inserts;
refresh from the first page to see changes to earlier entries. Do not load all
pages into memory automatically. Old servers without this route return 404;
clients must distinguish unsupported from an empty successful page.

The previous Web Tasks panel did **not** consume ledger pages and still only
shows safe process/result summaries. This change supplies the shared generated
HTTP contract, not a new per-file Web/phone browser or download button. Native
Windows auto-paging belongs to its separate client change. Neither the storage
scale test nor this endpoint proves arbitrary file browsing on every client.

## Executed checks

All tests use synthetic data and existing cloud dependencies. No paid provider,
real PC, phone or production database is involved. Passing counts below are Go
named test/subtest nodes, not leaf-only counts.

| Check | Exit / result | Evidence |
| --- | --- | --- |
| `go test -race -p=2 -count=1 -timeout=180s -skip '^TestRegistrationTenThousand' -json ./internal/computertask ./internal/transferclient ./internal/deliverymeta ./cmd/transferqueue ./internal/botdelegation ./cmd/internal/core ./internal/sessiontask` on merge | 0; 820 PASS, 0 SKIP | `affected-race.txt` |
| `go test -race -p=2 -tags=integration -count=1 -timeout=5m -run '^TestComputerDeliveryPostgres\|^TestBotDelegationPostgresClaimIsolationAndDescendantAssets$' -json ./internal/db` on merge | 0; 32 PASS, 0 SKIP | `postgres.txt` |
| `go test -race -p=2 -count=1 -timeout=180s -run TestSessionTask -json ./internal/handlers` after HTTP addition | 0; 8 PASS, 0 SKIP, JWT/revocation/pending-reference/late-insert/no-store checks | `http-final.txt` |
| `go test -race -p=2 -tags=integration -count=1 -timeout=5m -run '^TestComputerDeliveryPostgresAuthorizationCASAndPreservation$/entry-pages' -json ./internal/db` after HTTP addition | 0; 3 PASS, 0 SKIP; real Server/JWT/PG without BYPASSRLS, 257 metadata entries across 17 HTTP pages, no dispatch or revision changes | `http-postgres-routed.txt` |
| Two existing Tasks UI test files, with CI's `NODE_OPTIONS=--no-experimental-webstorage` | 0; 56 PASS | `ui-node22.log`, `ui-success.json` |
| Handler `go generate`, `pnpm run generate-sdk`, SDK/Web typecheck | each 0; all existing 275 OpenAPI path values unchanged, one added path | generated spec/SDK; `sdk-type.log`, `web-type.log` |
| `pnpm --filter @memohai/web build` | 0; existing chunk warnings retained | `web-build.log` |
| `go build -p=2 -o <scratch>/server ./cmd/agent`; `go vet -p=2 ./internal/computertask ./internal/sessiontask ./internal/handlers` | each 0 | `build.log`, `vet.log` |
| `scripts/check-agent-core-boundary.sh`, `git diff --check` | each 0 | official `bf0318c5` runtime/contextview unchanged |

The unchanged 10,000-entry test was not rerun locally; the new HTTP fixture uses
257 metadata records referring to one small real synthetic asset, not bulk file
traffic. Original failures remain: first UI attempt had 30 Node experimental
webstorage setup failures; applying the existing CI runtime option passed.
Initial HTTP fixture builds exposed an import cycle and incorrect store adapter,
both corrected without changing core. Initial real-Server test used the Web
proxy prefix against the direct backend (401 without task cache policy); using
the existing direct `/bots` route passed. No application proxy behavior changed.

## Current application observation

Only the existing synthetic cloud application was restarted: no active session
runs/delegations, old writer stopped, retained complete SQL backup, migration
173 clean -> 174 clean, current built server healthy. Original records and
private backup remain. Existing Web dev server uses current source at loopback
port 18102; backend is loopback 18100. No new business request was sent.

Reused read-only `../delegation-route-config/browser.mjs`: open the existing
synthetic supervisor conversation, Tasks, Specialist Bot handoffs, previous
failed delegation detail. Two real delegation GETs return 200, zero browser
errors. [Screenshot](browser-failed.png) shows **Execution failed** and separately
**Return delivery accepted**, with no false completion. It is a current UI
regression check, not a per-file browser or a successful business execution.
The previously recorded fixture workspace/bridge failure remains unresolved;
this task did not resubmit it.

Published Go text is decoded JSONL `Output` fields with checkout prefixes
normalized, not byte-identical raw JSONL. `evidence-manifest.json` retains source
and published hashes and processing rules. Private config, tokens, complete
server logs, SQL backup, files and original JSONL remain outside Git.

## Deployment and remaining holds

This is an integration candidate, not main promotion or production acceptance.
Follow [migration contract](../transfer-ledger-pages.md): stop old writers and
back up before 174. Retain all history/objects. Lossy down is refused; do not
force it to run an old executable. Queue v5 from the preceding candidate is
incompatible with old executables. Images `omit`/`multipart` policies still fail
before the request with an explicit configuration migration error; they are not
silently converted. No new scope or credential is created.

Legacy-media P1 remains on hold; no denied fix was retried. Real PC throughput,
physical interruption, all-client rendering and actual provider acceptance are
not established. This work neither changes production nor approves its risks.
