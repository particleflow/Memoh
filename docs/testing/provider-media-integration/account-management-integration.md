# Provider management and catalog account boundary integration

Base: `24f65a6cff9cb12b901be0a14beb06b258aa8cd0` (catalog-only guard).
Source: `723601d9997d1a3f0b01e4591fb6f0674a71536b`, cherry-picked with provenance
as `24416e45b6a8114ab1f26965b31d490eebf072d6`. The cherry-pick had no conflicts.
This document records checks executed on the combined source, separately from
the source author's [earlier validation](../provider-account-boundary.md).

## Behavior and integration correction

The existing provider management group and the four OAuth management routes now
require account credentials. The existing JWT middleware validates that account's
current session. Chat-route credentials receive the existing 403 Problem response
before provider lookup, configuration writes, credential resolution or upstream
requests. The global JWT implementation and Bot chat-token behavior are unchanged.
OAuth callbacks retain their separate state validation and do not use this guard.
The catalog's own guard and its tests from the base remain byte-for-byte unchanged.

The first combined catalog test failed because the group guard returned before
the handler could add `Cache-Control: no-store`. All four chat cases already
returned 403 with zero provider reads and zero upstream requests; the missing
header was the failure. [Original output](account-evidence/scope-first.log), exit 1.
The combined guard now preserves that header, and the management-route matrix
also asserts it. No rule or test expectation was weakened.

## Executed checks

| Command or check | Result | Evidence |
| --- | --- | --- |
| `go test -race -count=1 -v ./internal/handlers -run '^TestProvider(Account\|MediaCatalog)'` | Exit 0; handlers 1.206s | [Output](account-evidence/scope-combined.log) |
| `go test -p=2 -count=1 ./internal/auth/... ./internal/providers/... ./internal/handlers/... ./internal/apperror/... ./internal/server/...` | Exit 0; all five packages pass | [Output](account-evidence/go-packages.log) |
| `go generate ./internal/handlers` and `pnpm run generate-sdk` | Both exit 0; normal generation | [Contract comparison](account-evidence/contract-union.json) |
| `pnpm --filter @memohai/sdk typecheck` and `pnpm --filter @memohai/web typecheck` | Both exit 0 | [SDK](account-evidence/sdk-types.log), [Web](account-evidence/web-types.log) |
| `pnpm exec vitest run apps/web/src/utils/api-error.test.ts apps/web/src/composables/api/sse-error.test.ts` | Exit 0; 123 tests, two files | [Output](account-evidence/web-errors.log) |
| `go build -o /tmp/computer-endpoint/server-next ./cmd/agent` | Exit 0; no output | [Empty build log](account-evidence/build.log) |
| `python3 /tmp/provider-account-integration/runtime-check.py` | Exit 0; actual isolated Server/PG/Web and loopback provider | [Output](account-evidence/runtime.log), [browser counters](account-evidence/browser-proof.json) |
| `node .husky/checks.mjs` with the six integration product/generated files staged | Exit 0; affected lint 106.61s, handlers 4.240s, total 114.72s | [Output](account-evidence/aggregate.log) |

The race-enabled suite includes the 16-route, 160-denial matrix (chat token,
chat token with account identity, anonymous, revoked and expired account;
header/query carriers), the destination-change/probe chain with zero writes and
zero upstream requests, ordinary account management, chat-token pass-through on
its synthetic chat route, OAuth callback state checks, and the existing catalog
scope/boundary tests. The synthetic chat route is a middleware compatibility test,
not a new end-to-end Bot conversation or OAuth provider exchange.

The generated union retains all 369 operations, all request parameters,
successful response schemas and definitions. Only documented 401/403 responses
are added to 15 existing provider operations. No API, callback, owner/admission
endpoint, migration or SDK callable is removed. Official runtime/contextview,
the runtime package, shared auth/server and Server factory have zero diff from
the base; no Agent core changes are part of this integration.

## Isolated runtime evidence

The current Server binary replaced the prior binary only in the existing cloud
synthetic stack: API `http://127.0.0.1:18400`, Web `http://127.0.0.1:18402`, existing
isolated PG at schema 175. The browser exercised the real Provider form: save a
synthetic provider name, reload, test connection, read its media catalog, restore
the name and reload. The masked key was never resubmitted; there were no browser
errors. [Inspected screenshot](account-evidence/account-catalog.png) shows the
synthetic catalog after successful account access, not a real provider catalog.

There was one upstream GET for the connection probe and one for the catalog,
zero upstream POSTs, and no model inference. The browser's one `probePosts`
counter refers to the Memoh test-connection endpoint, not an upstream model POST.
Anonymous 401 and actual foreign-team 404 triggered no upstream request. Full
provider rows were unchanged except the fixture's expected `updated_at`; all
model rows, pre-existing computer operations and the owner-locator response
were unchanged. The script removed only its two synthetic provider records and
synthetic team, and stopped its loopback peer.

Published logs are byte-preserved, including the initial failure and tool
whitespace; [sizes and hashes](account-evidence/checksums.json) identify them.
The selected output and screenshot were reviewed; they contain no session token,
production key, private endpoint, raw DB rows or private Server log. Scripts and
private environment inputs remain local; the committed Go tests supply the
reproducible middleware/HTTP fixtures. Only 647,459,566 bytes of task-owned,
rebuildable Go cache data were reclaimed before these checks; source, evidence,
Git history and databases were preserved.

This is local integration evidence. Final-head remote CI must be checked
separately. No images, main/latest tags or production configuration were changed;
the older deployed provider surface is not mitigated merely by this commit.
No real Magpie, PC, mobile, production OAuth exchange or host-stop handoff was
exercised. The owner reconciliation/admission gate remains distinct from stop
authorization.
