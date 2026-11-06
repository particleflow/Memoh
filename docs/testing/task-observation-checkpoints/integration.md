# Checkpoint UI integration on the provider security candidate

Security base: `2895ce47feaa982b71fa562a6c84038f2ebabd08`.
Source: `1cacf511d450b7ba4099bd7642114165c4d39a18` (based on `24f65a6c`).
Integrated product: `30860d0818843cfff490b63bce04d54c1f175c7d`.

The eight source files applied without conflict and remain byte-identical to
the fixed source commit. The difference from the security base is seven Web
files and the source validation README. There are no backend, provider, OAuth,
API/SDK, configuration, migration, official Agent-core or polling changes. Both
provider account guards and all 369 API operations remain as in the security
base. No regeneration or backend retest was needed for this display-only merge.

The shared `SessionTaskResults` now renders `ComputerTaskCheckpoints` from the
same observation reader. The component accepts the existing `ComputertaskRecovery`
and optional `receivedAt`; it has no requests or actions and no DialogRoot
dependency. Native HTML-dialog hosts use the same body. This source integration
does not update a phone package or establish a real device test.

## Combined-source validation

All final checks below exited 0:

| Check | Result and evidence |
| --- | --- |
| Five UI test files listed in the source README | [89 tests passed](integration-evidence/ui.log), 10.60s, including Web/native hosts and observation/scope recovery |
| `pnpm --filter @memohai/web typecheck` | [Output](integration-evidence/web-types.log) |
| `pnpm --filter @memohai/sdk typecheck` | [Output](integration-evidence/sdk-types.log) |
| `pnpm --filter @memohai/web build` | [Output](integration-evidence/build.log); existing large-chunk warning retained |
| `node scripts/check-ui-contract.mjs` | [Output](integration-evidence/contract.log); three existing warnings and recorded baselines retained |
| `node .husky/checks.mjs` with the eight source files staged | [Output](integration-evidence/aggregate.log); frontend lint, 4.00s; no Go packages selected |
| `node /tmp/task-checkpoint-integration/browser.mjs` | [Final output](integration-evidence/browser.log), [counters](integration-evidence/browser-proof.json) |

The running Vite Web (`http://127.0.0.1:18402`) loaded these current source
files. The existing isolated Server/PG login and synthetic conversation were
reused; the Server binary was unchanged from the validated security base.
All task HTTP responses were controlled browser fixtures. They do not establish
real server observation, Herdr recovery, PC execution or production access.

The final browser run recorded 17 task GETs, six observation reads for the same
operation, zero mutations, zero device/OAuth requests and zero page errors.
It verified the four groups, exact revision beyond JavaScript's safe integer
range, offline connection distinct from persisted Pi success, unknown business
outcome, zero/recorded bytes without download links, 403 clearing and automatic
read suspension, explicit read recovery, disconnect/reconnect to the same task,
omitted fields remaining unknown, reload, and 390px Chinese dark-mode wrapping.
The browser offline flag was paired with an intercepted network failure because
Playwright fulfill bypasses transport; this is not a real PC disconnection.

Current screenshots were inspected: [receipt](integration-evidence/receipt.png),
[collection](integration-evidence/collection.png),
[denial](integration-evidence/denied.png), and
[narrow Chinese dark view](integration-evidence/narrow-zh.png).

Four initial harness failures are retained: [wrong expected translated label](integration-evidence/browser-first.log),
[offline fulfillment bypass](integration-evidence/browser-network-first.log),
[initialization resetting the locale](integration-evidence/browser-locale-first.log),
and [wrong theme-storage key](integration-evidence/browser-theme-first.log).
Only the test harness was corrected; no product assertions or application
behavior were changed. The final harness also supplies a valid empty delivery
page instead of an incomplete fixture. The [actual runner](integration-evidence/browser.mjs)
is tied to this existing synthetic stack and private local login file; it is
evidence, not a deployment recipe or a new testing service.

Selected logs and runner are byte-preserved. [Hashes and sizes](integration-evidence/checksums.json)
identify them. They and the screenshots contain only synthetic task data; no
login value, production credential, private endpoint or raw database rows were
copied. No build cache cleanup or database modification was required this turn.

Docker run `40172780834` remains fixed to the separate security base `2895ce47`.
This later UI checkpoint is not part of that image. Remote CI on the new head,
publication, deployment, physical phone/PC checks and human QA are separate;
none is implied by this local integration.
