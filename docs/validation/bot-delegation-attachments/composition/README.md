# Reviewed transfer and CrossBot composition

This normal merge combines `cec5f22d0931f63b97d8d27a1eccf0873c51b734`
(CrossBot attachment chain and process-recovery evidence) with
`83483850061f4c908c28ff5ef340209cf9e718d5` (reviewed transfer delivery).
Both histories are retained. This work resolves compatibility conflicts; it does
not extend the CrossBot product surface or change the official Agent runtime.

## Evidence availability correction

The tested product remains `9bd82f57d66846a6d6c36c3515f0344acf16a9f7`.
This documentation-only follow-up publishes previously retained evidence; no
product test or business execution was rerun for this follow-up. The repository's
`*.log` ignore rule had omitted the eight linked log files from the fixed tree.
The reviewed files now use `.txt` names and are added individually; the related
local image-fixture retry output is also linked.

[The evidence manifest](evidence-manifest.json) records each published hash,
retained source hash, derivation and redaction status. Eight files preserve the
retained text-log bytes. Go JSON-derived files contain decoded `Output` fields,
not the original JSON stream bytes; the combined Range file deliberately selects
the two named tests. The corresponding JSON stream hashes and selection rules
are recorded. The SDK transcript is a **redacted copy**, replacing one local
repository-root path, adding a notice and removing one terminal blank line.
Its original is retained privately,
and its original and published hashes differ as documented. No credentials,
private network addresses, real user text or private server logs are included.

## Six conflict resolutions

| File | Resolution |
| --- | --- |
| `db/postgres/migrations/0001_init.up.sql` | Keep the 171 delivery guard, 172 transfer credential fields/index, and 173 delegation ledger with forced RLS. No incremental migration is edited. |
| `internal/db/session_tasks_postgres_integration_test.go` | Preserve the assertion that read/list operations leave the starting schema version unchanged, without hardcoding 170. |
| `internal/handlers/message.go` | Preserve transfer authority, owner/hash/size and response-time revalidation before shared original-file serving. Delegation reads retain their authorized reader. Both scoped paths retain Range and no-store, including 416. |
| `packages/sdk/src/@pinia/colada.gen.ts` | Regenerate from the combined OpenAPI contract. |
| `packages/sdk/src/index.ts` | Regenerate from the combined OpenAPI contract. |
| `packages/sdk/src/sdk.gen.ts` | Regenerate from the combined OpenAPI contract. |

SQLC was regenerated with pinned `sqlc@v1.31.1`; Swagger used the repository's
`go generate` directive; SDK used `pnpm run generate-sdk`. No generated conflict
was resolved by selecting one parent's file. [Contract evidence](api-contract.json)
asserts that all 360 operations and all 360 SDK exports from the union of the two
parents remain. [Migration evidence](migration-preservation.json) compares every
171/172/173 incremental file byte-for-byte against its reviewed parent.

## Executed checks

- `go test -p 3 -run '^$' ./...`: all 218 package output lines completed, exit 0.
  This is a compile check, not execution of every repository test.
  [Raw output](go-compile-raw.txt).
- Actual PG, integration build tag and nonempty isolated DSN: **20 passed, zero
  skipped**. Pattern: `Test(BotDelegationPostgres|ComputerDeliveryPostgres|ComputerTransfer|SessionTasksPostgres)`.
  The full formal chain, canonical init-only snapshot and 170→171→172→173 path
  execute. New assertions retain exact existing runtime credentials and operation
  JSON across 172→173, plus the legacy-writer guard and delegation forced RLS.
  [Go output decoded from captured JSON](pg-go-output.txt).
- New task-transfer HTTP test: exact Range bytes/Content-Range, 416/no-store,
  size conflict, mismatched hash, revocation and anonymous rejection. Existing
  CrossBot JWT/scoped-original tests pass alongside it.
  [Selected Go test output](range-combined-output.txt), [race output](range-race-raw.txt).
- Twelve related Go packages passed, with 3,130 pass events and 65 explicit skips
  (counts include subtests). Most skipped cases require separate application PG
  environment variables; the opt-in media volume benchmark is also skipped.
  The original command additionally named a nonexistent tool package and thus
  exited 1; that invocation failure is retained in the [summary](go-test-summary.json).
  The corrected `internal/agent/tool` run had 415 pass events, one live-search
  skip and seven failures caused by ambient proxy interception of its local image
  fixtures. The seven tests passed after removing proxy variables for that test
  process; their real TCP connection is explicitly redirected to `httptest` by
  the existing fixture. No paid image provider was called.
  [Local retry output](tool-local-retry-output.txt) is decoded from the retained Go JSON.
- SDK `tsc` passed. [Redacted output](sdk-typecheck-redacted.txt).
- Full Web typecheck **failed** with the same 153 diagnostic headers as the saved
  exact a68 baseline: zero added/removed. [Comparison](web-typecheck-comparison.json).
- Normal handler/DB lint passed, as did integration-tag incremental lint
  against 834. [Command results](check-results.json).
- Integration-tag full DB lint **failed** with 18 diagnostics in seven files,
  all byte-identical to 834. [Raw lint](pg-lint-raw.txt),
  [unchanged-file comparison](pg-lint-existing-files.json). The incremental check
  against 834 passed. This does not claim the full integration lint is green.

The first new HTTP fixture attempt reused an upload delivery ID for download
and received the expected immutable-identity conflict. The fixture now gives
independent downloads their own identity. [First attempt](range-fixture-first-attempt-output.txt)
is retained; no product authorization was weakened to make it pass.

## Real application and UI

A new isolated fixture used PostgreSQL 18, AOF Valkey 8, the current combined Go
server and bridge, a tiny cached Node workspace image, and local synthetic SSE
model responses. The committed `scripts/acceptance/delegation_fixture.py` bootstrap
built the server and ran all official migrations on a new database. It did not
reuse a CrossBot-only schema-173 database that had skipped reserved 171/172.

Run `bootstrap(Path(<new-root>))` from `scripts/acceptance/delegation_fixture.py`
with its directory on Python's import path. Then invoke the existing
`bot_delegation.py --mode idle --allow-isolated-fixture-writes` with the private
state's loopback URL, Bot IDs, token-file paths, PG container/user/database,
model port and a new evidence directory. Omit `--restart-fixture` for this bounded
composition check. Private credentials/configuration are never committed.

One fresh synthetic business execution verifies the combined application's new
input → specialist send → final supervisor attachment chain, original owners,
exact attachment counts, reopen identity, terminal/empty-queue observations,
Range/If-Range/HEAD/416 and anonymous rejection. Attachments contain only 31 and
40 bytes. [Raw output](native-combination-raw.txt),
[structured result](native-combination-results.json). A separate actual API
revocation/restoration check denies both scoped originals with 404/no-store and
restores exact bytes under the same explicit grant permissions.
[Revocation proof](native-revocation.json).

The real Vite application at `http://127.0.0.1:18285` used the new fixture server.
Browser automation opened the supervisor conversation, reopened both attachment
previews after reload, verified both download hashes and exact scoped Range
bytes/Content-Range/no-store. Screenshots were captured and visually inspected:
[supervisor final attachments](current-supervisor.png),
[original preview](current-preview.png), [browser assertions](ui-results.json).
This is agent QA; no human-QA approval is claimed.

## Boundaries

The old cec5 SIGTERM/SIGKILL lease evidence and original result files remain
unchanged. This composition run does **not** repeat or claim new restart,
16-concurrent-retry, Windows-native, physical power-loss or production-PC proof.
The generic harness log label “after restart/retry/reopen” includes only the
reopen operation in this run: no restart helper was passed. Its `revocation`
field is UNRUN because that helper was omitted; the separate revocation proof
above records the actual subsequent API test.

No official Agent runtime/context-view files or legacy symlink implementation
were edited. No main-branch merge, production connection, paid model, deployment
or PR metadata action is part of this merge. Only the approved existing source
branch is eligible for push. Full CI status must be checked against the resulting
merge SHA; local checks and an in-progress run are not CI completion.
