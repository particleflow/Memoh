# PR66 generated-contract merge verification

This increment resolves the seven generated-file conflicts in the existing
Draft PR66. It does not change product behavior, Agent internals, permissions,
database contents or production configuration. No Actions setting was retried,
no remote workflow was dispatched, and no release/deployment was performed.

## Exact ancestry and regeneration

- Reviewed fix head: `668b214c7efc4f0336151bd75e1561f766c7fb35`.
- Reviewed fix product: `9bd82f57d66846a6d6c36c3515f0344acf16a9f7`.
- Confirmed PR45 base: `885d865874beac21b2371944144d24af08f3bdee`.
- Normal two-parent merge: `aaccb9a220f8b4406498aeb5148b6916ed578b3c`.
- Merge tree: `d18bf008813705f28a96bcd758c98bc846cfd4f0`, **identical to 668b**.
- Test-only follow-up: `a3386436a764cf1535919ada689b746b50836b0f`.
- Frozen combined checkpoint `f5b162633016ab1e0e594f1fb22217855433791f`
  is unchanged; it already includes the 9bd product.

The base's only change after the original common ancestor 096295d2 was API
generation. The merge conflicted in four SDK files and three OpenAPI files.
It was resolved by running the repository's actual generation commands:
`go generate` in `internal/handlers`, then `pnpm run generate-sdk`, followed by
SDK typecheck. No `ours`/`theirs` tree selection or hand deletion of APIs was used.

[Contract proof](evidence/api-contract.json) verifies all **360 operations and
360 SDK operation exports** remain, including every operation from the old base
(356) and the reviewed transport source (354). All seven regenerated files are
byte-identical to 668b. Therefore the merge fixes ancestry/conflicts without
rolling the combined generated contract back to the old base.

The follow-up adds If-Range coverage to the existing JWT delegation and scoped
computer-transfer HTTP fixtures. A matching content-hash ETag returns the exact
206 bytes; a stale ETag returns the complete 200 body. Existing 416/no-store,
anonymous rejection and revoked-access checks remain. Authentication is still
required with a matching ETag. These are test changes only.

The existing panel-preview test had a total `vue-i18n` mock that swallowed
`createI18n`. Its partial typed mock is the same correction already present in
the frozen combined checkpoint. No production UI code or assertion was removed.

## Executed local checks

[Commands, exit codes and timings](results.json) are local evidence, not CI.

| Check | Exit | Result |
| --- | --- | --- |
| Swagger generation | 0 | [Log](evidence/swagger.log) |
| SDK generation, openapi-ts 0.99.0 | 0 | [Log](evidence/sdk-generate.log) |
| SDK typecheck before and after | 0 / 0 | [Before](evidence/sdk-before.log), [after](evidence/sdk-after.log) |
| Scoped attachment / generated-contract Go tests, race | 0 | [25 named PASS, zero SKIP](evidence/go-attachments.output.txt) |
| Affected Go vet | 0 | [Result](evidence/go-vet.result.json) |
| Incremental handler Go lint, after storage recovery | 0 | [Result](evidence/go-lint-after-space.result.json) |
| Gallery/preview tests, first run | 1 | [12 tests passed; one suite failed to load](evidence/ui-gallery.log) |
| Failed preview suite after typed fixture correction | 0 | [One additional test passed](evidence/ui-preview-retry.log) |
| Changed UI fixture ESLint | 0 | [Result](evidence/ui-preview-lint.result.json) |
| Web build | 0 | [Log; chunk-size warning retained](evidence/web-build.log) |
| Server build and current-source HEAD health | 0 | [Build](evidence/server-build.result.json), [health](evidence/runtime-health.json) |
| Real browser and generated SDK list/detail GETs | 0 | [Output](evidence/browser-final.log), [proof](ui/proof.json) |

Full Web typecheck is **not green**: 151 diagnostics occur before generation,
after generation and after the fixture correction, with no added or removed
diagnostic. See [comparison](evidence/web-typecheck-comparison.json),
[original log](evidence/web-before.log), [final log](evidence/web-final.log).
These are this environment's measurements with the same reused dependencies,
not a claim that the earlier historical 153 baseline was fixed. A clean install
and full Web suite were not repeated. SDK/spec and dependency manifests/lockfile
are unchanged from 668b.

The two previously passed gallery files were not rerun after correcting only the
failed preview fixture. The combined evidence is 12 retained PASS plus one
targeted PASS, not a new full-suite run. Whole-repository Go lint/tests and new
PostgreSQL lifecycle tests were not run: production handlers, service and SQL
are byte-identical to the already reviewed source. Earlier PG evidence remains
in the [composition checkpoint](../composition/README.md).

## Browser and fixture boundaries

The rebuilt server uses the existing isolated PG schema 173 and private cloud
fixture configuration. The browser uses real Vite/HTTP with existing synthetic
rows; it does not mock API responses or create new fixture/business records.
Login → existing conversation → task list/detail succeeds. The generated SDK
also calls the existing delegation list and one detail, both HTTP 200. Computer
operation row snapshots remain unchanged and no business POST is issued.

The initial direct SDK probe used the wrong list property `id`, whereas the
actual typed contract is `delegation_id`. Two generic error attempts and a
bounded diagnostic attempt are retained. The diagnostic observed a literal
unsubstituted path parameter and 404 `session.not_found`; fixing the probe's
field reference made the final read succeed. No authorization or API change was
used to make that request pass. This is not the previously fixed production
same-turn attachment issue.

Inspected screenshot: [existing task list](ui/tasks.png). Existing native and
batch detail screenshots and request statuses are included. There are no page
exceptions; one unrelated container 404 and two workspace 503 responses remain
in the final proof. The browser checks read-only observation and actual SDK GETs;
the attachment byte/Range assertions are HTTP fixtures, not a new browser file
download or real specialist business execution. No paid model, production PC,
NAS or external provider was contacted.

## Preserved failures and resources

At entry the root filesystem had zero available bytes. Two bounded cleanups
removed only identified old generated Go archives, recorded in
[first cleanup](evidence/cache-cleanup.json) and
[lint recovery cleanup](evidence/cache-cleanup-lint.json). The lint cache was
copied to scratch, verified, and its old path retained as a symlink. No databases,
volumes, receipts, histories, objects, source or original evidence were removed;
there was no global prune.

The initial tracking checkout failed because the local fetch configuration only
tracks main. HEAD remained on f5 while the index/worktree exactly matched 668b;
both equality checks were verified before creating the local no-track branch at
668b. No reset, discarded modification or remote-ref rewrite was used.
[Checkout note](evidence/checkout-note.txt).

Go lint was first unavailable on PATH (no subprocess started), then the already
installed pinned 2.10.1 binary failed because temporary compiler output exhausted
scratch space. Both outcomes remain recorded. After the second bounded cache
cleanup the same incremental check passed in 129.341 seconds. No tool was newly
installed, budget increased, rule disabled or test removed.

[Boundary evidence](evidence/boundary.json) verifies the official Agent core
gate, unchanged runtime package, migrations, handlers, service, SDK/spec and
dependency manifests. The evidence manifest includes hashes/byte counts and
declares path normalization. Decoded Go output is labeled separately from JSONL.
The missing-tool log is explicitly an authored failure note, not raw subprocess
stdout. Private server logs, configuration, login data, seed SQL and binaries are
excluded.

The existing Actions settings 403 remains a hold: no retry or alternative access
path was used in this increment, and remote checks are not represented as run.
Legacy media P1 and all prior production/physical-PC acceptance limitations remain
open. This Draft is not ready for merging or deployment.
