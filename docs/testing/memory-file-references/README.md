# Synthetic memory file-reference regression

Branch base: `851e8da4284c1f01e7e95f9547b16e7250bf553b` (`main`). This change is
independent of PR55; it does not include or modify that PR's deletion lifecycle
work. No production, NAS, Windows, real user chats, paid models or new credentials
were used. The official Agent core boundary remains unchanged.

The initial regression uploaded real synthetic bytes and retained the returned
canonical hash in a memory. Before the change, `search_memory` omitted that
reference. The older ad hoc `file-synthetic-001` test had never registered a file;
it is not evidence of a broken media resolver.

Run the committed PostgreSQL regression against a disposable database:

```sh
# Supply a local, disposable PostgreSQL DSN; no cloud model credentials needed.
TEST_POSTGRES_BOOTSTRAP_SCHEMA=1 MEMOH_TEST_MEMORY_FILES_REQUIRED=1 \
  go test -race -count=1 -run '^TestPostgresMemoryFileReferenceLifecycle$' \
  ./internal/memory/adapters/builtin
```

`TEST_POSTGRES_DSN` is required. Bootstrap uses `dbtest.MigratePostgresUp`, the
server's full migration chain. It does not load only the historical initial
snapshot or hand-edit the schema. The CI step reuses the migrated PostgreSQL
fencing job and requires an actual PASS event, so a skipped or absent test fails.

The test uses real PG, canonical original bytes and a real Markdown store over a
local bridge. It checks rebuild, new connection/runtime/resolver instances,
search/list/tool output, exact bytes and another Bot claiming ownership. Unit
regressions cover malformed hashes, paths, URLs, extra fields, missing files,
provider failures, unavailable resolvers, forged projections and plain memories.
Availability errors never carry their original diagnostics into the projection.

Local validation: 226 memory tests/subtests passed with `-race`, zero skips or
failures on the official migrated schema (170, dirty=false, no `bots.type`). No
fixture or source overlay was used for this suite. Three UI component regressions
and the existing API/SSE error suites passed: 126 tests. A separate external
read-only probe checked the actual UI fixture's tool output after stopping and
restarting the isolated PG and server processes; it added a test only, without
replacing production code or schema.

The actual application ran at `127.0.0.1:18090` with Vite at `127.0.0.1:18092`,
using the existing local PostgreSQL and synthetic Bot workspace. The native files
configuration pointed at the existing canonical-originals directory. After a
server and PG restart, Rebuild retained all four synthetic records, with no
restored entries. REST and tool output supplied the validated reference, and the
media endpoint returned the exact 30 bytes (SHA-256
`36b3189ddd3a7fc47549adaf85c8ce9248cadd45523dea7d0f783222bc9dd046`).

Actual browser interactions verified the memory attachment opening the existing
preview, unavailable references without a link, ordinary memory search/edit,
and dark/narrow Chinese display and preview. These are agent checks, not human QA.

- [Before: summary without an attachment action](before.png)
- [After: available action, unavailable message and ordinary memories](after.png)
- [Existing preview displays the original synthetic bytes](preview.png)
- [Dark, narrow Chinese memory page](narrow.png)

The SDK typecheck, targeted Go lint, full ESLint (zero errors; 18 existing warnings),
UI contract guard, server build and Agent boundary check passed. Full Go lint
reports one pre-existing `unused-parameter` diagnostic in the unchanged official
Agent file `internal/agent/tool/background.go:260`; it is not a passing full lint
run, and the upstream Agent core was not edited to silence it.

Whole-web typecheck is **not green**. Against an isolated unchanged main worktree,
its 89 distinct normalized TypeScript diagnostics are identical (zero new or
resolved diagnostics). Do not call this a passing whole-web typecheck. No unrelated
type errors were changed.

All language decisions are explicit synthetic input or local stubs; this does not
measure LLM/embedding accuracy, automatic attachment extraction, complete message
provenance traversal, or cross-Bot delegation. No original document is copied into
memory. Resource scope is one small local PG (1 CPU / 512 MiB limit), one existing
synthetic workspace, existing cached build tools and tiny files; no image pull or
new memory service is required.
