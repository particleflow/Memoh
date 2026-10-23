# Unified transfer and Web management candidate

This is a source integration checkpoint, not a release or deployment approval.
The original `integrate/unified-server-web-candidate` stays at
`dfc7cf5104c382ecc622061ce43c3dcd733b7998`.
The new branch is `integrate/unified-server-web-management`.

Final source merge: `3ce1f27cb4ea5c5c1be3be92245ef4008625b518`.
Source tree: `9c913cf6406bc17d623526e834f0442a841fae13`.
Later commits containing only this integration evidence do not change product
code. No existing source PR was rebased or force-pushed.

## Fixed inputs and merge order

| Input | Fixed SHA | Scope | Merge checkpoint |
| --- | --- | --- | --- |
| PR #69 | `3ec5d309a8c8e6dd8917fd4599d6d526d397037d` | Real-route upload/download tests and original logs | `1e725692ea898bcedb524a57a80b1e2e4eafe938` |
| PR #71 | `91149ad7ab5c0d24a57f10e7d9ff1df4b71cf51c` | API-base validation, vectors and prefix upload test; contains #69 | `de6b554e7348bff0a8cb306bea86bc3bc4dd946a` |
| PR #70 | `6ace867cd87936347ea8f65f007030fdd6965249` | Web downstream task navigation and real application evidence | `0236d3ab868ba9562e4ea9d6bb571c02bfaa2d6a` |
| PR #72 | `7a79c48cce892811b92d9b3396cc8118869795a3` | Own-child state/text, scoped downloads, polling; contains #70 | Included below |
| PR #72 wording | `6ccbafa225d7d2988f68b40152bacced13e21785` | Correct SQL comment and core-directory documentation only | `0c54f98f2462d214de40d465b1abb7f3083bdb02` |
| PR #73 | `24ee5414b4d277e1462212655d6c2655ee36083e` | Prefix download/Range/query tests and evidence; contains #71 | `3ce1f27cb4ea5c5c1be3be92245ef4008625b518` |

Every input is an ancestor of the final candidate. Each normal merge has its
own commit and no source conflict. No `ours`/`theirs` replacement, manual SDK
rewrite, API deletion or product fix was needed. PR #72's wording now explicitly
says hidden messages are filtered before selecting the latest visible message
within one run; there is no cross-run fallback. sqlc regenerated the matching Go
comment. No tests were rerun for that wording-only change.

[Merge checkpoints](unified-management-checkpoints.json) retain the intermediate
trees. [Source audit](unified-management-source-audit.json) independently checks:

- Web, SDK, OpenAPI and task projection are byte-identical to PR #72's corrected
  head; transfer client/CLI and combined route tests are byte-identical to #73.
- All 360 OpenAPI operations remain; none were added or removed by combining.
- Migrations are identical to `dfc`; no change to schemas 171/172/173.
- The separate mandatory operation CAS/RLS and Pi latest-history PG gates remain,
  together with the child-run PASS guard. This is workflow-source verification,
  not a statement that remote checks executed.
- Official `bf0318c5` core trees match exactly: runtime
  `b21040a32c089642c2d0fdd312b19635e72fb38a`, `internal/contextview`
  `7ef779cf08a7117978de05ab75425897ad61928e`, and `packages/runtime`
  `49f09a39168dd07e2759f0a3b6449eb8684b125b`.

## Combination checks actually executed

All results in this table are local cloud checks, not GitHub Actions. Tests
before #73 were not rerun merely for its test/docs-only addition. The final
integration-tag compile includes #73's new test sources.

| Command/check | Source point | Result | Output |
| --- | --- | --- | --- |
| `go test -race -v -count=1 -run '^TestAPIBaseContractVectors$' ./internal/transferclient` | `0c54f98f` | exit 0, 30 vectors; package 1.019 s | [Vectors](unified-management-api-base.log) |
| `pnpm --filter @memohai/web typecheck` | `0c54f98f` | exit 0 | [Web types](unified-management-web-typecheck.log) |
| `pnpm --filter @memohai/sdk typecheck` | `0c54f98f` | exit 0 | [SDK types](unified-management-sdk-typecheck.log) |
| `go test -tags=integration -count=1 -timeout 90s -v -run '^TestSessionTasksPostgresLatestChildRunAndMessage$' ./internal/db` | `0c54f98f` | exit 0, real PG, no skip, 0.051 s | [PG read](unified-management-child-pg.log) |
| `go test -count=1 -v -run 'Test(Subagent\|SessionTask\|TaskHTTP\|BotDelegationJWTSubmitObserveOriginalAndRevocation)' ./internal/sessiontask ./internal/handlers` | `3ce1f27c` | exit 0; 5 top-level tests plus 5 nested auth cases | [Task/HTTP](unified-management-task-http.log) |
| `go test -tags=integration -run '^$' ./internal/db` | `3ce1f27c` | exit 0; **compilation only**, no tests run | [Integration compile](unified-management-db-compile.log) |
| Ancestry, exact component identity, API/migration/core/gate checks | `3ce1f27c` | passed | [Audit](unified-management-source-audit.json) |
| Actual Chromium observation of the existing synthetic task | `3ce1f27c` Web checkout | passed; no business dispatch | [Output](unified-management-observe.log), [proof](unified-management-browser-proof.json) |

The PG check uses the existing isolated schema-173 database in a rollback-only
transaction; it does not create a production record or replace the separate
fresh-database migration/RLS suites. Sources #69/#71/#73 already contain their
reviewed, readable real-route PG evidence. Those full fresh-database scenarios
were **not rerun in this full-overlay environment** and their older timings are
not relabeled as combination results. Source #73's six named PASS records
include parent/child cases; its private-copy truncation is not a crash/network
failure recovery test. See [#69](delivery-real-route-roundtrip.md),
[#71](file-api-base-contract.md) and [#73](file-api-base-download.md).

## Running application and visible evidence

The current local API remains `http://127.0.0.1:18100`, with Web at
`http://127.0.0.1:18102`. The running server is the previously verified product
binary `ff0f1736`, **not a newly built combination binary**. Reuse was checked:
all changed production Go files outside that product are the transfer client
and a generated comment; `go list -deps ./cmd/agent` confirms transferclient is
not a server dependency. The generated store differs only in comments. Web and
SDK are byte-identical to the already tested #72 source.
[Runtime reuse proof](unified-management-runtime-reuse.json).

The read-only browser pass opened the real existing specialist handoff, downloaded
the same 7,400-byte synthetic original and checked its complete SHA-256, then
opened the child run and verified its own completed state and visible text.
Observation made zero POSTs, the recorded application responses were 200 and
there were no page errors. No model process was started or task reissued. The
source task was created by a synthetic HTTP model during #72 acceptance, not a
real paid model or Windows Agent.

The [exact executed observer script](unified-management-observe.cjs) is included
for review. It expects the existing isolated fixture's state JSON and token file
in `/tmp/web-management-checks`, plus the installed Playwright/Chromium paths.
Those private input files are not published. It performs reads/downloads only;
do not recreate or replay the business task to run it. It is a cloud verification
script, not a new product service or portable production installer.

Fresh screenshots were inspected: [scoped result download](unified-management-download.png)
and [420 px Chinese/dark child view](unified-management-child-dark-zh.png).
Existing 81-test UI, full build, lint and actual dispatch evidence is reused by
byte identity from [#72](web-task-results-and-downloads.md); it was not rerun or
counted again here. Two existing ESLint warnings, three UI contract warnings and
the existing chunk warning remain disclosed in that source evidence.

## Resource failures and retained holds

Root overlay space was already exhausted and scratch had about 84 MiB free.
Only this Web task's superseded generated binary and three generated Web output
directories were removed, recovering 256,962,395 bytes. Current running binary,
source, logs, screenshots, databases, originals and configuration were retained.
No global prune was performed.

Three merges initially failed with `cannot create directory ... No space left
on device`: #69's evidence directory, #71's transfer testdata directory and
#73's evidence directory. No merge commit was created by those failed attempts.
Partial checkout bytes were checked against their exact source commit and
restored before retrying; no user changes were overwritten. Five confirmed empty
directories from the task's inactive Go build cache were renamed to the required
checkout directories, allowing the same fixed commits to merge normally. This
resolved checkout creation only: it did not cure root-disk capacity or prove
fresh-database tests could run. No quota or filesystem permissions were changed.

The [manifest](unified-management-manifest.json) identifies exact selected
outputs, the executed script and screenshots. Copies are not decoded or path
normalized; known-secret checks passed before copying. Local synthetic IDs and
fixture paths remain. Private server logs and credentials are excluded.
The two raw typecheck logs retain pnpm's final blank line; `git diff --check`
reports that formatting in those evidence files. Product changes and the
non-log documentation pass the whitespace check; the raw outputs were not
rewritten merely to hide this observation.

GitHub Actions remain an outstanding hold based on the existing disabled-422
evidence; no dispatch/settings retry was made. OAuth recovery, the blocked legacy
media P1, default workspace bootstrap investigation, missing mobile source/build
evidence and real Windows/mobile/NAS acceptance remain outside this checkpoint.
There is no claim that all endpoints are running this unified version. No new PR,
Ready, main merge, image publication, production migration or deployment is part
of this candidate preparation.
