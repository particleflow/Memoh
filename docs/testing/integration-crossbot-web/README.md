# Combined delegation, transport, memory and Web checkpoint

This is an independent source-integration checkpoint. No PR was created, no
workflow was dispatched, and no image, main ref, production service or credential
was changed. The known legacy media-read P1 remains open; this branch does not
remediate it or establish release eligibility. No private mobile source is included.

## Fixed history and conflict decisions

The [first checkpoint](../integration-memory-files-types/README.md) is preserved at
`e8c8c2e70a0bad03b68a8282b3f33530e133481f` (tree
`020f3128d2f20da11847218cef41d0e82358dd8b`). It merged PR55, PR58 and PR59 in order
into fixed PR60. Its evidence applies only to that first combination.

| Additional fixed input | Merge commit |
| --- | --- |
| CrossBot `9bd82f57d66846a6d6c36c3515f0344acf16a9f7` | `85565d11` |
| Shared observation UI PR54 `8022eca5dd00df20f407451a935269f002256027` | `a2a0e24b` |

Both merges preserve their parents. No input branch/head was updated and no later
moving CrossBot head was used. The SDK index conflict and subsequent generated
SDK/spec conflicts were resolved using `go generate ./internal/handlers` followed
by `pnpm run generate-sdk`, not hand-selected generated exports. All 360 method/path
pairs, including every operation from all six fixed inputs, are retained.
Migrations 0171/0172 are byte-identical to PR60; 0173 is byte-identical to CrossBot.

Integration fixes are saved separately at
`221d2cf9cb1bc2b580ec3d768e8733d47a77ad84`, code tree
`5471cb437d6d364d4c9ac1331530a499e008421c`:

- Pass the required `type: 'file'` to the existing scoped attachment URL resolver;
  complete the test URL builder's typed substitution. No route or authorization
  fallback was added.
- Give four inherited delegation tools explicit display labels and targets.
  `retry_bot_delivery` is **Retry result delivery**, not another specialist run.
  Existing grouping and generic result rendering remain unchanged.
- Preserve all lifecycle, file-reference and transport PG gates; add a separate
  mandatory delegation/task-observation PG selection with two named PASS checks.
  Skipped or missing tests cannot satisfy this new gate. The hard Web typecheck
  remains enabled. No assertion or safety check was deleted.

## Local combined verification

These are local results on the combined source, not input-branch CI or an asserted
GitHub Actions run. Source-branch pushes do not trigger the main/PR-only checks;
no PR or manual CI dispatch was performed. Dependencies/lockfile did not change
after the first checkpoint's frozen install.

| Check | Result |
| --- | --- |
| Full Web Vitest | 258 files / 2,464 tests passed |
| Web / SDK / Desktop typecheck | all exit 0; Web zero diagnostics |
| Web ESLint | exit 0; 0 errors, 18 existing test-component warnings |
| UI contract | exit 0; 3 existing warnings, baselines unchanged |
| Web production / server build | both exit 0; existing Web chunk-size warning |
| Bot-delegation and computer-task service race tests | 222 test/subtest PASS, no skips |
| Related handler / application / tool race selections | 62 + 86 PASS, no skips |
| Memory PG race suite | 233 PASS, no skips |
| Transport / delegation / task-observation PG race selection | 26 PASS, no skips |
| Runtime and contextview | official `bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046` trees unchanged |
| `packages/runtime` | also unchanged from that official baseline |

The PG selection includes connector disconnect finalization, transfer authorization
and CAS preservation, upgrade from 170, delegation claim/descendant asset isolation,
and session task owned-tree/stable-cursor checks. The named PASS requirements were
checked against actual JSON test output. Both isolated test databases ended at
173 clean; no production database or existing application database was migrated.
This was not a full `go test ./...` run.

Initial parallel attempts exhausted temporary disk space. Web suite load failures
included explicit ENOSPC; Go test linking also explicitly failed with ENOSPC.
Initial transfer PG failures included client/store/HTTP errors during the same
exhaustion, without an assertion that every underlying error was proven ENOSPC.
Only inactive task outputs and eight old generated Go cache archives were removed;
no global Docker prune, source reset or user-data deletion occurred. Failed suites
were rerun serially with unchanged assertions. The completed full Web rerun and PG
selection passed; the already-passed handler package was retained while the two
failed Go build packages were rerun. One command typo (`-p1`) selected the empty
repository-root package; the corrected command used `-p 1`. All these unsuccessful
attempts remain in the local log history. [Results and log hashes](results.json)
identify the final evidence and the split Go results.

## Actual application/browser check

The rebuilt combined server ran on loopback port 18100 and the current Vite app on
18102. Its separate synthetic database was migrated to 173. A synthetic local
admin owned two Bots. The existing 35-byte test original remained available through
the real media API; no provider, model or computer workspace was provisioned.

Twenty-two completed handoff records were seeded directly in this disposable PG
database. Their summaries are synthetic text. Browser/API responses were **not
mocked**. This proves actual authenticated observation and integration rendering,
not specialist execution, natural-language routing, result delivery consumption,
paid-provider operation, or production acceptance.

The browser logged in, opened a Bot creation draft without submitting it, opened
Bot settings, viewed located/unavailable memory references and the actual text
preview, then opened the chat Tasks dialog. It read the handoff list, final visible
summary, separate execution and return-delivery states, and second page. Switching
back to Execution tasks used no delegation cursor; switching again reset the
handoff reader to its newest page, as the existing visibility invalidation intends.
An initial browser assertion incorrectly expected second-page retention; the
script was corrected to the existing tested contract, with no product change.

There were 210 observed API responses and zero uncaught page errors. The five
delegation requests were successful GETs; all observed requests after login were
GETs. There was no task submit, delivery retry, model run or computer operation.
The unprovisioned synthetic Bot produced four container 404s and eight workspace
file-list 503s; the screenshot may show its workspace-unavailable toast. This is
not evidence that workspace execution works. The existing two-node graph initial
fit issue remains as documented in the first checkpoint. Attachment downloads in
the handoff view remain unavailable; no raw media fallback was introduced.

All screenshots were captured from this combined app and visually inspected:

- [Bot creation draft](bot-create.png), [Bot settings](bot-settings.png)
- [Memory references](memory-refs.png), [actual original preview](memory-preview.png),
  [narrow/dark memory view](memory-narrow-dark.png)
- [Handoff list](delegation-list.png), [detail and separate delivery state](delegation-detail.png)
- [Second page](delegation-page2.png), [independent execution-task group](execution-independent.png)

This is automated Agent-run verification, not human QA. The open legacy media-read
P1 and production acceptance are not resolved by these passing integration checks.
