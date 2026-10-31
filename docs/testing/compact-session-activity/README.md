# Compact active session entries

Baseline: `e6149a454c0677a80ad765b03615c63befaaea98`. This follow-up is local, not pushed to the frozen publication ref.

The existing desktop session sidebar and mobile navigation sheet now show only active native runs, child tasks and professional Bot delegations. Rows contain the existing title and status and open the recorded conversation. Terminal and unknown records stay out of this primary list. The existing task dialog remains available as Task records; its latest-run details start collapsed. Its management, result, history and resume behavior remains present.

No backend, SDK, schema, execution, file transfer, credential, permission or native phone change is included. The list reads the existing task/delegation endpoints and uses their original continuation cursors. It makes no device observation, submission or collection request. Specialist navigation reuses the existing relation and session permission checks. Missing recorded session/run references are not turned into links. Existing native run admitting/aborting and projected delegation states are retained; unknown is never converted to running.

## Actual coverage

This is a projection of synchronized runs already known to the current Bot's chat store plus the current conversation's authorized task/delegation lists. It is not a new global all-Bot discovery API. No inference from titles, time, UUID similarity or a parent's completion creates an identity or status.

The actual modified Vite application was exercised in Chromium against the existing isolated Server/PG at loopback ports 18402/18400. Synthetic intercepted metadata supplied submitted, waiting-decision, aborted and unknown rows. The specialist relation and target-session navigation used actual existing isolated API records and returned HTTP 200. Zero business requests, device observations and page exceptions occurred. Metadata 503 followed by recovery retained the same source identities. This is not live PC/provider execution, production acceptance, ArkWeb or device QA.

Screenshots inspected locally: desktop-active.png, compact-rows.png, mobile-active.png, mobile-reconnected.png and history-collapsed.png. The original supplied reference images are not redistributed. The synthetic workspace has no ready cloud workspace and can display its pre-existing workspace-unavailable toast; that unrelated condition is not represented as fixed. The first mobile animation frame was retained separately and replaced for visual evidence only after the original sheet transition settled.

## Checks

- `pnpm exec vitest run .../session-activity.test.ts .../session-tasks.test.ts`: 60 passed, exit 0. Then two existing projected specialist state cases (accepted/finishing) were added: activity fixture 20 passed, exit 0. Unique affected total: 62 tests; this is not one combined 62-test run.
- Final full Web vue-tsc: exit 0. An intermediate attempt incorrectly compared the projected SDK state with raw admitting and failed TS2367; that attempt was removed rather than widening the SDK. Final cases use the declared accepted/finishing projection. The initial exit 137 was recovered by restarting only this task's verified Vite process; source, PG and data were preserved. A subsequent five-error fixture diagnostic was corrected with complete typed runtime messages, without any/ignore.
- Targeted ESLint: exit 0, two existing one-component-per-file warnings in the task fixture. Final activity lint: exit 0.
- Web production build: exit 0; existing chunk-size warning retained.
- UI contract: exit 0, three existing warnings retained. The initial arbitrary 11px padding was replaced with the existing spacing token rather than adding an exemption.
- The old task dialog initially had 42 selector failures after its trigger label changed; only the three trigger lookups were updated, preserving all behavioral assertions. Final 42 pass.
- Independent review found and verified two corrected seams: opening an original active session from a new draft, and cancelling in-flight specialist navigation after source access denial.

Console copies remove trailing whitespace only; log-processing.json retains original byte hashes and counts. Raw originals and supplied references stay outside this source patch; readable early failure copies are included and not relabelled as passing runs.

Human QA has not passed and must remain unchecked. The initial review checkpoint stopped before commit by applying an inherited UI skill as a human-review prerequisite. The user subsequently explicitly directed continuation of this pending submission under the existing direct-submit authorization; the parent relayed that instruction for this exact visual change. The user then explicitly requested permanent removal of that project prerequisite. Root and Web AGENTS now make the host override explicit; the third-party UI submodule and Human QA status are unchanged. The source remains the reviewed 0550d0e021b25726474f683313f677ac464161f9 tree; only project guidance and evidence disclosures changed after that reviewed source checkpoint. The remote e614 publication ref remains frozen: no push, publication, deployment or new HAP is performed by this local submission.

## Provenance of the removed submission prerequisite

Read-only `git blame`, `git log --follow -S`, and `git show` were run against the existing full upstream history and the existing UI submodule history. Author timestamps below are recorded Git metadata, not inferred approval times.

| Stage | Exact commit and evidence | Recorded author and time |
| --- | --- | --- |
| First upstream Memoh addition of the human-before-commit rule | [2d0083d53c3203a9f408bbaeb597233349e3bfc1](https://github.com/felinics/Memoh/commit/2d0083d53c3203a9f408bbaeb597233349e3bfc1), `.agents/skills/memoh-web/SKILL.md`, rule 10 added in PR707 | qqqqqf, 2026-06-29 17:17:58 +08:00 |
| Same original rule in extracted UI history | [3cd83ace984119c35c71fe5f11a1fac979e905a2](https://github.com/felinics/ui/commit/3cd83ace984119c35c71fe5f11a1fac979e905a2), `skills/memoh-web/SKILL.md` | qqqqqf, same author time; this is the extracted history hash, not a second independent addition |
| Extraction into UI submodule | [c0c667b6c14fa5e4ed8e2737a66540c9dca2cb11](https://github.com/felinics/Memoh/commit/c0c667b6c14fa5e4ed8e2737a66540c9dca2cb11), PR758 | qqqqqf, 2026-07-10 11:45:02 +08:00 |
| UI skill rename and host guidance | [a390b9fcfda75edc3ffc5013f4ddbecd09a4d849](https://github.com/felinics/ui/commit/a390b9fcfda75edc3ffc5013f4ddbecd09a4d849), `memoh-web` to `web`, PR3 | qqqqqf, 2026-07-12 22:41:25 +08:00 |
| Current Web host-to-submodule guidance | [c56c5af5e356667aa34ae28946fe5cc0ca2c1db0](https://github.com/felinics/Memoh/commit/c56c5af5e356667aa34ae28946fe5cc0ca2c1db0), `apps/web/AGENTS.md:9-19`, PR773 | qqqqqf, 2026-07-12 22:47:33 +08:00 |

The project's explicitly selected official baseline [bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046](https://github.com/felinics/Memoh/commit/bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046) already points at UI gitlink `f69402a55dd87a59c10722fe11507ad8fd7048f5` and contains the same Web AGENTS routing blob `fe7daffa541127a0a9eace9c8f099eb8f2caea56`. The local candidate's earliest available boundary 40f0a69 is shallow, so its blame cannot identify the fork's original import author. It must not be presented as Xuanting Mu adding this rule. A remote metadata read for that boundary failed with RemoteProtocolError before providing metadata; it was not treated as evidence of origin. The existing full official and UI histories suffice to establish that the rule predates this project's customization and was inherited with the official source.

Git history cannot establish whether the upstream author had this user's approval, nor whether a person intended to obstruct this project. The delay in this turn came from this agent applying the inherited prerequisite to the new UI change and asking again; that execution decision is distinct from the upstream author adding the text. The user has now expressly removed its application in this project. Normal automated verification, actual UI checks, tool/security approvals and truthful Human QA disclosures remain in force.
