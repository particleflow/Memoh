# Delegation return route and execution deadline

This incremental PR79 change starts at `caa2b3ed7891ca55afab050cc2853bf975ddedad`.
It changes only host delegation routing, its existing durable payload, and the
normal TOML/core/service configuration path. No runtime/contextview, file
transport, model/provider protocol or database schema change is included.

## Contract

- The host resolves the source root's persisted channel route after the existing
  user/Bot/session authorization. The route is stored in the existing delegation
  payload, never accepted from model or HTTP input, and never projected to clients.
  Delivery and queued-turn admission compare it with current owned route facts.
  A changed/deleted route does not silently redirect an old result. The original
  queue identity and business deduplication key are unchanged.
- Legacy records without a snapshot still deliver to owned local conversations:
  the official REST path permits omitted `channel_type`, and thread creation
  canonicalizes Web/CLI to `local`. A declared external channel or route ID must
  resolve through the route store. Legacy external records remain readable but
  cannot infer a historical recipient; delivery is unavailable. No history is
  rewritten and no specialist is rerun.
- `[bot_delegation].execution_timeout` uses a non-negative Go duration. Empty or
  omitted means `30m`; `2h` is an example longer deadline; explicit `0s` disables
  only the outer host deadline. Invalid values fail config load. Agent/provider
  deadlines and shutdown remain effective. Timeout/cancellation records unknown,
  clears final summary/output projections, and does not deliver or rerun work.

## Executed checks

All commands used the existing cloud checkout and Go 1.25.7 scratch caches.
Public logs below are selected byte-for-byte copies; the manifest includes their
hashes. No credentials, private server log/config, database or original files are
published. Exact known fixture-secret scanning found no match; this is not a
universal secret detection guarantee.

| Command / check | Result |
| --- | --- |
| Initial focused config/delegation/core tests | 42 test/subtest PASS, exit 0 |
| Targeted `go test -race` across config/delegation/core/handlers/application | 51 PASS, 0 FAIL/SKIP, exit 0; `race.jsonl` |
| Route compatibility and real application continuation selection | 45 PASS, 0 FAIL/SKIP, exit 0; `route-final.jsonl` |
| Final `go test -race ./internal/agent/application -run '^TestAuthorizedDelivery'` | 5 PASS, 0 FAIL/SKIP, exit 0; `continuation-final.jsonl` |
| `go test -tags=integration ./internal/db -run '^TestBotDelegationPostgresClaimIsolationAndDescendantAssets$'` | 1 named test PASS, 0 SKIP; actual isolated PostgreSQL/full migrations, route payload roundtrip, CAS and RLS; `pg.jsonl` |
| `go build -o <scratch>/server ./cmd/agent` | exit 0 |
| `go vet ./internal/botdelegation ./internal/config ./cmd/internal/core` | exit 0 |
| `scripts/check-agent-core-boundary.sh`, `git diff --check` | exit 0 |

The counts overlap; do not add them as distinct tests. The route/deadline tests
use fixtures for model/channel execution. They cover external channel fields,
legacy local versus external records, route/owner/config changes, caller-forged
route fields, queued continuation/restart and explicit long/disabled deadline.
The real PG test does not send a real IM message.

## Real Web/API/PG observation and incomplete business probe

The updated Server ran at loopback port 18100 and existing Vite at 18102, with
only the existing synthetic PG, files and credentials. A single new HTTP
submission included forged route fields. The real host ignored them, persisted
Web routing, and attempted the specialist once. `host-pg-proof.json` records the
actual database outcome. No real PC, paid model or production data was used.

The existing fixture's workspace bind paths and bridge socket no longer exist.
Both specialist and supervisor hit `runtime_run_failed` before any synthetic
model request: business execution **failed**, model calls **0**. The configured
5-second timeout business probe was therefore not submitted. No unknown or
failed business request was resubmitted. Successful real-model/channel delivery
and a timeout through the complete runtime remain **unverified**. No fixture
originals/volumes were deleted or recreated to conceal this limitation.

The actual browser opened the new supervisor session, Tasks, Specialist Bot
handoffs, then this result. [Screenshot](browser-failed.png) shows execution
failed, return delivery accepted, the explicit accepted-is-not-completion note,
and no final reply. The screenshot was inspected. `browser-proof.json` contains
the real GET response statuses and no page errors; this is a failure-state
observation, not a successful business flow or human QA.

Harness failures retained: the first formatting command ran before sourcing the
Go environment (`gofmt` not found), then succeeded with the established env;
the initial health probe used GET and got 405 (the official health endpoint uses
HEAD, which returned 200); two browser selectors incorrectly assumed a standalone
`Failed` label. Correcting them to the existing `Execution failed` label passed,
using only reads of the same failed task. Raw browser and business probe failures
are retained. No product behavior was changed to satisfy these harness errors.

## Followup history: separate proposed contract, not implemented

The native PC followup limit remains 64 in this patch. A safe removal cannot
just enlarge the number or drop old request IDs from operation JSON. The minimal
next storage change is append-only followup rows within the existing operations
PG module: parent operation ownership/RLS and CAS remain authoritative, with a
unique `(operation_id, request_id)` and immutable fingerprint. Persisted history
must be paginated; admission should depend on the one unresolved followup, not
historical count. Existing JSON records need versioned, verified compatibility
without duplicate dispatch or deleting evidence. This requires a separately
reviewed schema/write-version transition and rollback boundary; it is not a new
task system or part of this release. Acceptance must cover >64 settled requests,
old request replay/conflict, unresolved blocking, restart and current scope checks.

PR79 remains Draft. No main merge, image publish or production deployment is
part of this change. Existing legacy-media P1 and other real-device release holds
remain. Transfer's independent total-capacity redesign is not included.
