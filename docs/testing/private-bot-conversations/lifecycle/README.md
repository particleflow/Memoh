# Contact lifecycle revision

Base: `5c45c97f3c47c56788ad7a137b4627c4fd66052c`. This is an incremental revision
of the independent structure draft, not the confirmed bug-fix release candidate.
No deployment, bulk history migration, specialist-provider adaptation, navigation
redesign, model invocation or user-device access is part of this change.

## Resulting behavior

- Contact resolution locks Bot (the existing shared parent lock), then the complete
  existing Session row, including a tombstone, and only then Route. Deletion and
  reset writers retain their original SQL and lock order. The new SQL query is
  additive; sqlc was regenerated with the repository's pinned version 1.31.1.
- A concurrent first creator can win while another request's first lookup sees
  no route. That loser rolls back and reads the same key again in lock order,
  with at most three transaction attempts. Only that explicit binding-change
  condition permits an internal retry. Database errors and COMMIT-unknown never do.
- Reuse validates owner, Bot, original route, native `local` session surface,
  visibility and absence of a parent, including for deleted records. A known,
  correctly bound soft-deletion may create one replacement. An empty stored
  pointer, foreign tombstone, missing row or transient read failure does not
  authorize a new chat. After a concurrent destructive lifecycle wins, the
  current request fails closed; a subsequent request reconciles the same key.
- Mode/runtime are mutable descriptors, not contact identity. A legal change
  keeps the same contact ID. HTTP reuses normal session read authorization after
  resolution, including workspace-exec permission for external runtimes and
  fresh revocation/ownership checks. No permissions are expanded.
- A staged notification is guarded by `sync.Once`. Known-successful creation
  invokes the ordinary session-created publication and session-start hook once;
  reuse, rollback and unknown COMMIT do not invoke them. A later read of the
  committed row after unknown COMMIT does not replay either callback.

The hook guarantee is **one invocation after a known successful commit**, not
durable exactly-once external side effects. A crash after commit but before the
callback, or lost commit acknowledgement, may omit it. Hook failures remain
failures. No new outbox, hook replay mechanism or execution service is introduced.
Opening a newly created contact still invokes the original session-start hook;
this revision does not silently change that lifecycle/product choice.

## Directed verification

Real PostgreSQL 18.6 at the existing local development endpoint was healthy. The
repository helper created, migrated and dropped separate test databases. Existing
development data and the old source owner's recovery copy were not used or changed.

- [PostgreSQL race output](postgres-race.jsonl): 14 test/subtest PASS events,
  two top-level tests, zero FAIL/SKIP. This includes eight concurrent independent
  coordinators, distinct accounts/Bots/channels, unchanged historical rows,
  actual ordinary-role cross-team RLS, typed activation failure and actual commit
  injection counters, rollback, uncertain-commit readback, notification/hook
  invocation counts, legal descriptor changes, owned/foreign tombstones, and
  route-delete/runtime-clear in both interleavings plus Bot deletion.
- Lock-order assertions pause a real transaction after its Session lock, prove a
  separate backend can still lock Route with NOWAIT, observe the deleter's real
  PostgreSQL lock wait, then release and join both operations. The inverse
  interleaving verifies a stale binding fails without new creation.
- [HTTP/route race output](http-race.jsonl): 19 test/subtest PASS events across
  two packages, zero FAIL/SKIP. Changed-descriptor cases cover model/discuss/Codex,
  revoked external permission, foreign ownership and revocation during resolution.
  The affected eight original authenticated HTTP cases and three native route
  coordinator cases remain covered because this revision changes their boundary.
- [Official core boundary](core-boundary.txt) passed against `bf0318c5`; runtime
  and contextview are unchanged. The original shared Web/Harmony/SDK and native
  specialist planning/authority source files are unchanged by this revision.
- The new lifecycle test is required by the existing PG CI step's no-skip PASS
  assertion. Remote exact-head CI and human acceptance are separate; no passing
  remote run is claimed here.
- Affected-package normal lint passed. An additional whole-DB integration-tagged
  lint reported 25 diagnostics, including three revision formatting/naming issues
  which were fixed. The other 22 diagnostics are in ten files byte-identical to
  the base and remain outside this revision. Integration lint restricted to the
  actual delta against `5c45c97f` passed. See [summary](lint-summary.json),
  [initial full output](integration-lint-initial.txt) and
  [delta output](integration-lint-delta.txt). This is not a claim of clean full
  integration-tagged lint. No linter configuration or suppression was changed.

Commands (TEST_POSTGRES_DSN must reference the normal local disposable-test setup):

```sh
go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate
go test -race -json -tags=integration -count=1 -timeout=120s ./internal/db \
  -run '^TestPostgresPrivateChat(IsolationAndConcurrentReuse|Lifecycle)$'
go test -race -json -count=1 -timeout=120s ./internal/handlers ./internal/channel/route \
  -run '^Test(PrivateChatHTTP|ThreadCoordinator)'
bash scripts/check-agent-core-boundary.sh
golangci-lint run ./internal/channel/route/... ./internal/chat/thread/... ./internal/handlers/... ./internal/db/...
golangci-lint run --build-tags=integration --new-from-rev=5c45c97f3c47c56788ad7a137b4627c4fd66052c ./internal/db/...
```

The initial new run failed because the review implementation incorrectly compared
the native stored session surface with `web` instead of `local`. A failed assertion
also exposed a test cleanup race: the caller released a connection still used by
its deletion goroutine. The surface check and goroutine-owned release were fixed.
The added RLS fixture initially needed an explicit UUID cast. The linked final
outputs include these corrections; failed earlier runs are not labeled passing.
The initial `mise run sqlc-generate` stopped while installing unrelated Rust in a
read-only home; the documented task's exact pinned Go command above succeeded
without permission changes. No generated file was edited manually.

## Actual application evidence

The affected source was built as the real Server and run at
`http://127.0.0.1:18180`; the current source Web development server ran at
`http://127.0.0.1:18182`. It used dedicated synthetic application/vector databases,
normal development authentication and one synthetic Bot. No API/WebSocket
responses were intercepted. The initial development proxy targeted the default
port; only launch parameters were corrected to target this Server.

The [browser harness](browser.py) and [request/subscription proof](browser-proof.json)
confirm two independent browser contexts resolve and subscribe to the same saved
contact, a real authenticated PATCH changes its mode to discuss, and refresh
continues to resolve and subscribe to that original ID. Desktop and 390px views
were captured and inspected, with zero page errors and zero business messages.
An independent [database readback](application-state.json) found exactly one
contact route and session, with zero session runs, computer operations or
delegations in this dedicated synthetic application database.

- [Real Server contact](contact-real-server.png): the saved contact is open with
  the normal empty composer and secondary debug entries.
- [After mode change and refresh](contact-after-mode-change.png): the conversation
  stays open; mode/ID continuity is established by the accompanying response and
  WebSocket proof, not by inferring it from the visually unchanged composer.
- [390px second client](contact-real-server-narrow.png): the same real contact
  remains usable at the narrow viewport.

No workspace/model was configured for this synthetic Bot. Its first session-start
hook attempted the normal workspace boundary and logged unavailable bridge.sock;
that is not a successful command execution. The PG tests count native hook
invocations using a synthetic provider that fails before workspace access. These
are not claims of field-device, specialist result-return or successful hook-action
acceptance. No full Web/Harmony build or unchanged UI test matrix was repeated.

## Remaining boundaries

The existing draft's navigation/legacy-contact adoption decisions and old task
presentation still require separate product review. Agenvo-only specialist
registration/planning is a separate known capability gap and remains untouched.
There is no data migration or schema change. Preserve additive contact rows on
application rollback; never run generic migrate-down or replay unknown work.
