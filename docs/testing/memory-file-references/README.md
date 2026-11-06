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

## Follow-up audit of 63c4bb and locator cost

The original local suite count **226** is separate from the one named PG gate in
CI. At fixed head `63c4bb2592a73e2ce76e39f23999cdb6cf6a8f82`, all five workflows
finished successfully; the PG gate in run `39850551575`, job `120075218950`, step
6 also succeeded. [Recorded API metadata](ci-63c4bb.json) is terminal evidence
for that head only, not any later revision. No historical denied log was fetched.

The raw Web typecheck logs each contain **145 diagnostic first lines**, across
**17 TS codes**. Removing only `(line,column)` and grouping by the remaining
complete first line (file path + code + message) gives **89 distinct entries**.
This is not “89 TypeScript error-code categories.” Continuation prose is not
counted as another diagnostic. Both normalized multisets, including duplicate
multiplicities, are equal. The raw safe logs, SHA-256s, exact definitions and all
counts are in [typecheck/](typecheck/). Reproduce with:

```sh
python3 docs/testing/memory-file-references/typecheck/compare.py
```

The 30-byte functional fixture did not test large-file recall costs. A bounded
Linux probe used real 1 MiB and 16 MiB originals behind the actual durable provider,
with two references to the same file per query and three successive queries.
Setup/ingestion was excluded. `/proc/self/io` `rchar` measures bytes returned by
read syscalls, **not physical disk traffic**; warm-cache timings are illustrative,
not a throughput or huge-file accuracy claim.

| Payload | Old projection rchar, 3 queries | Metadata locator rchar, 3 queries |
| --- | ---: | ---: |
| 1 MiB | 3,145,832 bytes | 104 bytes |
| 16 MiB | 50,331,757 bytes | 109 bytes |

The small locator remainder is reading the measurement's own `/proc/self/io`.
Directory enumeration still does metadata I/O and may contact a workspace for a
legacy locator; it is not O(1) over an arbitrarily large prefix directory. There
is no new memory index or cross-request cache. The bounded inputs require at most
about 32 MiB transient payload storage during the larger setup, not a huge image.

[Probe source](io/probe.go.txt), raw output and JSON measurements are in [io/](io/).
To reproduce, copy the probe to a temporary `.go` file and use a Go overlay to add
it as `internal/memory/adapters/builtin/io_probe_test.go`; no source is replaced.
Set `MEMORY_IO_EVIDENCE` to a temporary JSON path and run
`go test -overlay <overlay.json> -count=1 -run '^TestBoundedMemoryProjectionIO$' -v ./internal/memory/adapters/builtin`.
The same probe runs at old head 63c4bb and the locator revision.

The shared `media.Locate` uses existing metadata listing only; the legacy `Stat`
function can still open content outside persistence mode, so it was not substituted
blindly. New real-file counter tests forbid Open/Put during Locate, retain legacy
migration at actual read time, reject out-of-scope/malformed locators, and prove
that a corrupt-but-locatable file still fails actual verified delivery. The output
status is now `located`, not a claim of verified readability or committed storage.

The locator follow-up was also tested locally on 2026-10-22: the same official
schema memory suite passed 226 tests/subtests with race detection and zero skips;
`internal/media` and the durable/local storage suites passed with race detection,
including the three new locator regressions. Three memory UI tests, SDK typecheck,
targeted Go/ESLint checks, server build, UI contract and Agent boundary checks
passed. A fresh whole-Web typecheck still failed with the same 145 raw / 89
distinct normalized diagnostics as main, with equal duplicate multiplicities.
The live server was rebuilt with Locate and the actual browser repeated
attachment preview, unavailable/plain memories, search/edit, and dark/narrow
Chinese preview; the screenshots above were recaptured and inspected. Preview
and narrow images were byte-identical to the previous capture. These checks do
not turn the earlier five-workflow evidence into a result for this later head.
