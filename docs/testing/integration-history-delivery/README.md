# Terminal history and shared result-delivery integration

This is a source-only integration checkpoint. No pull request, remote CI,
candidate image, queue migration, production change or deployment was performed
for this checkpoint. All checks below ran in the existing cloud development
environment on 2026-10-22 UTC, with synthetic data and no configured model or
provider in the browser fixture.

## Frozen inputs and merge decisions

| Role | Commit |
| --- | --- |
| Previous reviewed evidence checkpoint | `18b599e2a878c119142d8016b73e7ef5385b0f7b` |
| Previous integrated product | `47434be1505c68290bab9e06e266422b1d395ccf` |
| PR61 terminal history | `6e477b42280f19d57b883ee9267703f25f27bb15` |
| Shared result-delivery UI | `65c7c11a00dd48eeea6ee1bbbb4cc7317c57e466` |
| First merge, history | `b9336cd09d282562b4aafe4fcb6a53f308ac14f8` |
| Second merge, integrated product checkpoint | `acd9f21122a99efd66f8af98e8abd48122084545` |
| Integrated product checkpoint tree | `22fab01d81508cb54551101f5b040f7167643137` |

Both fixed inputs are ancestors of the product checkpoint. Their source branches
were not changed. History merged cleanly. The UI merge had one generated conflict
in `packages/sdk/src/index.ts`; it was resolved by running
`go generate ./internal/handlers` and `pnpm run generate-sdk`, not by selecting one
side. The resulting entire `spec/` and `packages/sdk/` trees are byte-identical to
18b599e2: 275 OpenAPI paths and 360 HTTP operations. Generator warnings are retained
in the [Swagger log](evidence/integration-history-swagger.log).

The incremental Web source change is exactly ten files. Existing combined type
repairs remain in place. `internal/transferclient`, `cmd/transferqueue` and
`contrib/herdr/memoh-delivery` exactly match the fixed PR61 input. No additional
NativePublication capacity, Pi history or Agent follow-up work is included.

All database migrations, including 171/172/173, and all workflows remain identical
to 18b599e2. `internal/agent/runtime`, `internal/contextview` and `packages/runtime`
match the official pinned `bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046` trees.
See [boundary commands and output](evidence/boundary.log). This is a precise core
boundary, not a claim that the whole fork is unmodified or every Agent integration
has been exercised.

## Local checks and exact scope

[results.json](results.json) records the actual command arrays, exit codes and
available timing information. Complete test output is linked below; Go counts
are named test/subtest pass events, not package counts. No named test skipped or
failed in these Go selections.

| Check | Exit | Actual result | Readable output |
| --- | --- | --- | --- |
| Full transferclient + transferqueue, race | 0 | 215 named test/subtest passes | [Go output](evidence/go-transfer.output.txt) |
| Full botdelegation package, race | 0 | 45 named test/subtest passes | [Go output](evidence/go-botdelegation.output.txt) |
| Application/handler delegation selection, race | 0 | 7 named test/subtest passes | [Go output](evidence/go-delivery.output.txt) |
| Real PG delivery/lifecycle/task selection, race | 0 | 26 named test/subtest passes | [Go output](evidence/pg-delivery.output.txt) |
| Targeted shared UI/API errors | 0 | 8 files, 244 tests | [Vitest output](evidence/web-targeted.log) |
| Complete Web project | 0 | 259 files, 2,498 tests | [Vitest output](evidence/web-full.log) |
| Web typecheck | 0 | Zero diagnostics | [Output](evidence/web-type.log) |
| SDK typecheck | 0 | Zero diagnostics | [Output](evidence/sdk-type.log) |
| Desktop node and Web typecheck | 0 | Zero diagnostics; runtime preparation included | [Output](evidence/desktop-type.log) |
| Web ESLint | 0 | 0 errors, 18 warnings | [Output](evidence/web-eslint.log) |
| UI contract | 0 | 3 warnings; grandfathered baselines unchanged | [Output](evidence/ui-contract.log) |
| Web production build | 0 | Existing large-chunk warning retained | [Output](evidence/web-build.log) |
| Server build | 0 | Current development binary rebuilt | [Command/exit](evidence/server-build.result.json) |
| Windows/amd64 transferclient test compilation | 0 | Compile only, no Windows execution | [Command/exit](evidence/windows-compile.result.json) |

The Go delegation name filter selected no tests in `internal/botdelegation` and
printed that warning; the subsequent full-package run above supplies its coverage.
The original filtered log is preserved. The transfer test deliberately exercises
a bad TLS certificate; its loopback handshake error is expected and retained.
Four jsdom `HTMLCanvasElement.getContext()` warnings remain in the full Web output.
The 18 lint warnings are **Web ESLint**, not whole-repository Go lint. The three UI
contract warnings and build chunk warning were not suppressed. Swagger generation
also reported three constant-evaluation warnings.

The new PG selection used the existing disposable database and stack, and tested
connector disconnect finalization, computer delivery, Bot delegation and session
tasks. Both the browser database and the PG integration database were read back as
schema 173, `dirty=false`. Earlier memory-PG 233, service 222 and host 148 records
remain evidence of the previous checkpoint; they are not claimed as new runs.
The earlier opt-in read-volume skip remains unexecuted in this stage.

Environment: Go 1.25.7, Node 25.9.0, pnpm 10.27.0. Web used
`NODE_OPTIONS=--no-experimental-webstorage`; Go checks reused the existing build
and module caches with `-p 1`. Windows compilation additionally used
`GOOS=windows GOARCH=amd64 CGO_ENABLED=0`.

## Actual browser and server observation

The rebuilt server ran on loopback port 18100, with the existing Vite dev server
on `http://127.0.0.1:18102`. The current source was used. The browser logged in to
the existing synthetic account, selected the seeded source conversation, opened
Tasks → Specialist Bot handoffs → an existing completed handoff, and retried only
that result delivery. No API response was fabricated: Playwright held the real
POST response briefly to inspect the disabled pending button, then delivered that
same response to the page.

The final [browser proof](ui/proof.json) and [exit/output](evidence/browser.log)
show:

- One browser delivery POST, with the same logged-in identity and an empty body.
  A second DOM click while pending did not send another POST. The observer then
  waited through a polling interval without another POST.
- HTTP 200, execution state `completed`, delivery state `accepted`, and the
  explicit UI explanation that acceptance does not confirm supervisor processing.
- An unchanged run-ledger fingerprint across the browser POST and subsequent
  polling. The fixture had one **failed supervisor follow-up**, zero specialist
  executions, zero providers and zero models. It is not a model or business
  completion test.
- A native HTML-dialog fixture hosting the same shared body without
  `deliveryAction`: zero buttons and zero API requests. This separate mount tests
  the read-only host contract; it is not a Harmony binary test.
- Zero page exceptions. The fixture has no provisioned workspace, so its unrelated
  container lookup returned one 404 and file-list requests returned three 503s.
  These responses remain in the proof. Source-module GETs recorded by the broad
  `/api/` observer are identifiable by their `/src/` paths.

Screenshots were captured from this run and visually inspected:

| Screenshot | What it establishes |
| --- | --- |
| [Retry available](ui/retry-available.png) | Completed execution and unknown delivery remain separate |
| [Pending](ui/retry-pending.png) | The result-delivery button is disabled during the real held response |
| [Accepted](ui/retry-accepted.png) | Accepted response with the supervisor-processing caveat |
| [Narrow dark Chinese view](ui/retry-dark-narrow-zh.png) | Same state at 390 px, translated and readable |
| [Read-only shared host](ui/readonly-host.png) | No delivery action supplied, no mutation button |

### Fixture errors and recovery, retained rather than hidden

Preparation made one setup delivery POST, then its SQL observation failed because
it queried nonexistent `session_runs.status` instead of `state`. The setup POST
had already executed. It was **not repeated**. Read-only reconciliation found
`completed/accepted`, one failed supervisor run and zero specialist runs. The
[setup record](evidence/browser-seed.json) is an orchestration summary, not a raw
stderr transcript. No persistent raw stderr file exists for that first failure;
its executor returned exit 1.

After reconciliation, only that synthetic delivery acknowledgement was changed
to `unknown` to exercise idempotent receipt replay; its task identity, content,
run and permissions were retained. The first browser attempt then failed before
its POST while computing the ledger fingerprint: it queried `id` instead of
`run_id`. That exact [failure output](evidence/browser-before-post-failure.log)
and [exit 1](evidence/browser-before-post-failure.exit) are preserved. A read-only
check confirmed delivery still `unknown` and the same failed run. Correcting the
fixture query allowed the final browser run to finish with
[exit 0](evidence/browser.exit). Across setup and the successful browser run there
were two deliberate result-delivery POSTs, only one of them a browser POST; there
was no new specialist business submission or model invocation.

The temporary host HTML was removed from the source tree after inspection. No
product code was changed to make the browser script pass.

## History compatibility and remaining boundaries

PR61 retains the same immutable locator. Version 2 → 3 is an explicit operator
migration after all old writers stop and a complete raw backup is taken. It only
archives associated, NAS-committed, fully released terminal records; unknown or
unreleased records remain active. Full history produces backpressure without
evicting historical evidence or deleting payload objects. See the fixed
[history contract](../../delivery-terminal-history.md).

The new tests cover 1,152 synthetic terminal records, original-ID replay,
partial/conflicting copies, full buckets, process exits at synchronization/removal
boundaries, migration backup/revision mismatch, busy writers, finite batches and
permanent locks. These are synthetic real-filesystem tests, not 1,152 NAS transfers
or physical power-cut tests. No live locator was migrated or downgraded here.
NativePublication's independent 128-record budget is unchanged.

The known legacy media-read P1 remains an unresolved safety blocker outside this
integration's authorized scope. Nothing here implements or bypasses it. Bot
creation → workspace readiness remains unverified after the earlier disk limit;
the profile-navigation anomaly is still unclassified. Neither is claimed fixed.
No production attachment, paid provider, Windows Agent, fresh specialist execution
or successful supervisor result-consumption loop was exercised.

Available space was bounded: heavy checks were serialized, only task-owned old
build artifacts and three identified old Go cache archives (501,387,132 bytes)
were removed. No global prune or source/queue/data deletion was used. The earlier
checkpoint's disk failures remain in its evidence; they were not rewritten.

Source-branch push does not match the current workflows' main/tag push filters.
No PR or manual workflow dispatch is requested. The prior preflight note remains:
an eventual PR targeting `integrate/**` would not automatically get the existing
ESLint PR gate, which only matches `main`/`feat/**`; workflow scopes were not changed.

## Evidence handling

[evidence-manifest.json](evidence-manifest.json) lists original and published byte
counts and hashes. Exported logs preserve all lines, including warnings and the
failed browser attempt. Exact local repository/toolchain/cache path replacements
are listed there. `*.output.txt` is a decoded Go `Output`-field view; the matching
`.log` contains the complete JSONL with only those path substitutions. Neither is
mislabelled as an unmodified private original when bytes differ.

The final whitespace check flags blank lines at EOF in six exported command logs
and one trailing space emitted by Vite. Those bytes are deliberately retained;
they are evidence-format whitespace, not product-source errors. The product-only
diff has no whitespace errors. No formatter was run over captured command output.

Only selected synthetic check output and screenshots are included. Private server
logs, fixture credentials, request headers, configuration, environment dumps and
raw database contents are excluded. Screenshots contain only seeded synthetic
content. No production identifier, private endpoint or user document was copied.
