# Unified server/Web candidate: fixed source and bounded verification

For current delivery/tool recovery, read the maintained
[compatibility incident register](../compatibility-incident-register.md) first.
The source, tests and recovery observations below describe this historical
checkpoint; they do not establish the latest deployed version or resolve later
file-materialization and access-scope failures.

This is a source candidate, not a release or proof that every client is on the
same version. It makes no production, provider, credential or Agent-core change.
The only code diff from the reviewed PR68 base is two Go HTTP test files.

## Fixed sources and selective integration

- Base PR68: `a3849eb027d86788780d3ac6b6adc92bbf122db7`.
- Inherited PR65 and the complete frozen combination:
  `f5b162633016ab1e0e594f1fb22217855433791f`.
- PR66 evidence/source checkpoint: `892efee385b8b505b5a8d8a7aaf95fb5bfa384d3`.
- Selected PR66 test increment: `a3386436a764cf1535919ada689b746b50836b0f`,
  restricted to `internal/handlers/bot_delegations_test.go` and
  `internal/handlers/computer_transfers_test.go`.
- Tested candidate: `7e6bc3a892be2c08ec6d3f0c39892768099b96b9`.
- Tested tree: `1dd2eee0cb03cdb44614b27974eb00eaad3cfb20`.
- Branch: `integrate/unified-server-web-candidate`.

The preview fixture already matches f5/PR68 and was not copied again. The current
computer-transfer fixture also has optional `root` support added after PR66's
base. The first exact-file preflight detected that difference before applying
anything; the selected patch preserves that support. No PR66 SDK/spec/Web tree
was copied or merged back. Existing source branches and checkpoints are unchanged.

[Inheritance evidence](evidence/inheritance.json) and
[tree/contract checks](evidence/boundary-contract.json) verify:

- all 360 API operations remain; SDK/spec, Web, Desktop and lockfile are
  byte-identical to PR68;
- migrations, including 171/172/173, and the workflow are unchanged;
- the operation CAS/ownership/RLS and Pi latest-history scripts are still two
  separate required PG gates;
- `internal/agent/runtime`, `internal/contextview` and `packages/runtime` match
  official `bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046` exactly.

The unchanged Images implementation is the external ToolProvider adapter from
PR68. Its explicit protocol/default compatibility and uncertainty limits remain
as documented in [Images capability](../../images-capability.md). This increment
does not add image workflows, a service, automatic retries or vendor-specific
model selection.

## Actual local results

Every row is backed by [commands, source head, exits and durations](results.json).
Named Go PASS counts include subtests; they are not assertion counts. None of the
listed Go selections skipped a test. Remote CI is not represented as executed.

| Check | Final exit | Observed result |
| --- | --- | --- |
| Official core boundary | 0 | [Baseline equality](evidence/boundary.log) |
| SDK / Web / Desktop typecheck | 0 / 0 / 0 | [SDK](evidence/sdk-type.log), [Web](evidence/web-type.log), [Desktop](evidence/desktop-type.log); zero diagnostics |
| Scoped attachment / transfer HTTP, race | 0 | [28 PASS](evidence/http-range.output.txt), including If-Range, Range, no-store and authorization |
| Images / composition root, race | 0 | [119 PASS](evidence/images-core.output.txt): 57 adapter and 62 core tests |
| Computer task routing/history, race | 0 | [295 PASS](evidence/computer-routing.output.txt), including native/batch separation |
| Selected legacy tools, race | 0 | [20 PASS](evidence/legacy-tools.output.txt) |
| Operation CAS/ownership/RLS PG gate | 0 | [Required named test PASS, no SKIP](evidence/operation-pg.output.txt) |
| Pi scope/latest-history PG gate | 0 | [Required named test PASS, no SKIP](evidence/history-pg.output.txt) |
| Full server FX assembly, race | 0 after resource recovery | [4 PASS](evidence/fx-assembly-link-space.output.txt), covering split, embedded and device-transfer shapes |
| Affected Go vet / incremental handler lint | 0 / 0 | [Vet result](evidence/go-vet.result.json), [lint result](evidence/go-lint.result.json) |
| Five affected Web test files | 0 | [44 PASS](evidence/ui-affected.log) |
| Web / server build | 0 / 0 | [Web log](evidence/web-build.log), [server result](evidence/server-build.result.json) |
| Current-source server and browser | 0 | [Health/binary identity](evidence/runtime-health.json), [browser result](ui/proof.json) |

The original PR66 branch's 151 Web diagnostics were not imported. This candidate
uses PR68's unchanged Web/SDK/lockfile trees and the existing installed
dependencies. No clean dependency installation, full Web suite, full Go suite,
Windows cross-build or unrelated network-image suite was repeated. The previous
seven media-network failures are not silently relabeled as passing. Incremental
lint checks changed handler lines against a384; it is not whole-repository lint.

The Images tests execute actual unmodified native loops and the real internal
`spawn_agent` admission/adapter path with deterministic synthetic chat and image
HTTP providers. They cover generation/edit contracts, legacy provider defaults,
lookup failure, authorization, original-byte references, explicit parent `send`
and unknown outcomes without an automatic second POST. They do not prove real
Magpie/provider compatibility, mobile delivery or new-call durable deduplication.
The PG gates use the existing isolated test database; no production database,
schema downgrade or migration was operated on.

## Runtime and browser evidence

The current source was built into a new server binary and started with the
existing private fixture configuration and isolated database. The development
Web at `http://127.0.0.1:18102` proxies that server on port 18100. HEAD health is
200. Login, existing conversation, Tasks, native detail and batch detail were
exercised through real HTTP, without mocked API responses. Generated SDK
delegation list/detail calls both returned 200. Existing operation row snapshots
were unchanged; business submissions were zero.

Inspected screenshots: [task list](ui/tasks.png),
[native detail](ui/native-detail.png), [batch detail](ui/batch-detail.png).
They show the existing synthetic task records, preserve unverified business
state and do not invent follow-up, retry or attachment actions. They are not
specialist-Bot execution, real-PC completion or image generation screenshots.

There were no page exceptions. One container 404 and two workspace filesystem
503 responses from the unprovisioned synthetic workspace remain in
[request evidence](ui/requests.json). Web build also retains its chunk-size
warning. Neither observation is claimed fixed by these test-only changes.

## Failures, recovery and evidence provenance

The optional full-server FX check first exited 1 with an empty captured log
during disk exhaustion. Its precise compiler diagnostic was not captured.
After the first bounded cleanup, the second attempt failed with the explicit
linker message `mapping output file failed: no space left on device`;
[that log is retained](evidence/fx-assembly-after-space.output.txt).
No assertion failure was observed in either attempt.

Only task-generated old server binaries and enumerated rebuildable Go cache
archives were removed, with hashes and sizes in the cleanup records. A probe for
cached ELF outputs removed zero files. After enough old archives were released,
an explicit scratch `GOTMPDIR` was used and only the failed FX check was rerun;
it passed. Databases, original files, volumes, source, histories and original
evidence were preserved. There was no global prune or increased test timeout.

The cloud environment became unavailable while collecting the final lint result.
On recovery, the existing saved exit 0 and 130.385-second duration were read;
lint and the already passed suites were not restarted. No uncertain push or
duplicate source commit occurred during the interruption.

The initial whole-workflow YAML inspection encountered an existing tab in an
unrelated workflow environment mapping. Only the trigger headers were then
parsed for the source-push precheck; that narrower inspection is explicitly not
a whole-workflow validation. No workflow or Actions setting was changed.

[Evidence manifest](evidence-manifest.json) records source and published hashes,
byte counts, exact path substitutions and decoded Output projections. Original
private logs remain in the cloud workspace. Configurations, credentials, raw
server logs, seed SQL and binaries are excluded from this directory.

## Version/readiness boundary

[Version manifest](version-manifest.json) identifies the server/Web candidate and
the preserved source checkpoints. The following are still separate acceptance
conditions, based on the parent handoff rather than newly verified client runs:

- Windows PR5 still needs its failing CI/log investigation and controlled client
  acceptance. A server-side native/batch fixture does not replace it.
- Harmony PR67 has not delivered its complete source build for this candidate.
- Recovery of the existing OAuth connection remains with the authorized runtime
  operator. This task did not create or modify a credential.
- Legacy media P1 remains a release hold; the previously denied repair was not
  retried or implemented indirectly.
- Actions settings read returned 403 earlier. It was not retried, settings were
  not changed, and no workflow was dispatched for this candidate.
- Real provider/PC/mobile end-to-end behavior, physical restart/failure handling
  and production upgrade acceptance remain outside these local results.

This branch is not a Ready release. No new PR, main merge, image publication,
NAS deployment or production task was performed.
