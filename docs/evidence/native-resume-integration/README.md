# Native recovery integration on the unified candidate

This combines `cbcee15a17dc87fd02eb4c198fba0aa45eb3fa45` with native owner PR84
`0d5e045ca00fb42e031092f00c63a045e4bac98f`. Production and the prior candidate
are unchanged by this source integration. No image publication or field resume
was performed.

## Scope and conflict resolution

The source adds explicit same-operation recovery for the supported pre-prompt
native batch phases, the existing admission transaction/CAS adapter, and three
PowerShell transport corrections. The source evidence is retained under
`../native-herdr-resume` and `../native-herdr-stdout`.

Only `scripts/test-computer-operations-postgres.sh` conflicted. Its resolved form
preserves the original 14 required tests and adds the three admission/CAS tests
in a second bounded process. Both processes reject missing or skipped required
tests. The independent latest-history PG gate remains unchanged. The unified
Windows workflow retains its 10-minute Windows aggregate timeout and 35-minute
job budget; it also runs the source's PowerShell test step.

There are no migration, configuration-schema, HTTP/OpenAPI/SDK, Web, or official
Agent/runtime/contextview changes relative to cbcee. Schema remains 176.

## Executed combination checks

This table records the historical combined-candidate checkpoint above. The raw
logs `targeted-go.log`, `postgres-gate.log`, `server-build.log`,
`core-boundary.log`, `browser.log` and `browser-first-locator-failure.log` were
not published in this repository snapshot and cannot be read back here. The
recorded outcomes below are not newly verified results or current acceptance;
other linked proof, manifest and screenshot files remain separate evidence.

| Check | Result |
| --- | --- |
| `go test -p=1 -count=1 -v -timeout=3m ./internal/nativeherdr ./internal/agenvo ./internal/computertask -run 'Test(PowerShellCLI\|AdoptBatchWorkspace\|ContinueBatch\|BatchResume\|NativeBatchExplicitResume)'` | Exit 0; 20 top-level PASS, 82 including subtests; one explicit Linux PowerShell SKIP. Raw log not published (`targeted-go.log`). |
| `bash scripts/test-computer-operations-postgres.sh` | Exit 1 before tests: race compilation exhausted the local temporary filesystem. The recorded log was truncated at 4096 bytes by that failure; `postgres-gate.log` was not published. This is not a PG pass; the exact merged-head remote gate is required. |
| `go build -p=1 -o <EVIDENCE_ROOT>/server ./cmd/agent` | Exit 0. Recorded output was empty; `server-build.log` was not published. |
| `bash scripts/check-agent-core-boundary.sh` | Exit 0, matches official `bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046`. Raw log not published (`core-boundary.log`). |
| Shell syntax, staged whitespace, required-test union | Passed; the union contains all 17 required PG test names. [Summary](integration-checks.json) |
| Current compiled Server + existing isolated PG + actual Vite Web | Exit 0. Existing unknown operation and owning session stay identical after page reload; zero writes, device reads, replay or browser errors. Raw `browser.log` was not published; [proof](browser-proof.json), [screenshot](combined-unknown-task.png) |

The first browser check selected the nonexistent exact text `Unknown` and timed
out. The UI correctly says `Execution status unknown`; only the test locator
was corrected. The original failure was recorded as
`browser-first-locator-failure.log`; that raw log was not published and cannot be
read back from this snapshot. The screenshot was inspected and continues to show unverified business
completion and no invented attachment result. No product change was needed.

The application was restarted with the combined binary against the same
synthetic database. Whole-table hashes/counts for nine operations, 18 sessions
and 29 messages are equal [before](before-ledger.json) and
[after](after-ledger.json) the restart/read-only verification. Existing scoped
authentication was reused without exporting its values. The unavailable
workspace selector belongs to the offline synthetic fixture.

The local filesystem shortage was handled only by removing this task's unused
rebuildable binaries and failed compilation/cache outputs. Source, Git history,
database, original files and evidence were retained. Full local aggregate lint
and PG race compilation were not repeated under the same capacity limitation.
Remote checks must validate this exact combined commit before it is a candidate.

## Source CI and execution boundary

The exact source head, **not the combination**, passed [Go 40216531770](https://github.com/particleflow/Memoh/actions/runs/40216531770),
[Native 40216531862](https://github.com/particleflow/Memoh/actions/runs/40216531862),
[ESLint 40216531757](https://github.com/particleflow/Memoh/actions/runs/40216531757),
[Docker PR 40216531976](https://github.com/particleflow/Memoh/actions/runs/40216531976)
and [policy 40216531821](https://github.com/particleflow/Memoh/actions/runs/40216531821).
Native includes actual Windows PowerShell byte/exit preservation, CLI, PG,
bundle verification and artifact upload. Governance skips are not validation;
PR Docker builds are not published candidate images.

Field recovery still requires the trusted original conversation's
`computer_task_submit` with the complete original normalized request and only
`resume_operation_id` added. Operation/run/attempt/generation, work directory,
request hash and producer binding must remain original. No fresh workspace,
replayed Pi submission, fabricated MCP identity or automatic retry is permitted.
This narrow continuation does not resume an uncertain submitted prompt or
guarantee native atomic generation checks. See the source contract in
`docs/native-runtime-herdr.md`.

Windows host workspace-base configuration, shared Web file restart recovery and
new Harmony packaging are independent pending work and are not included here.
No real Windows/Pi recovery or production acceptance is claimed. This is agent
verification, not human QA. Evidence copies preserve original hashes and record
path normalization in [the manifest](evidence-manifest.json); private server logs
and configuration are excluded.
