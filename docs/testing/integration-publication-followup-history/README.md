# Publication metadata / native follow-up / Pi history integration

This is a source-only combined checkpoint. No PR, remote workflow dispatch, ready
transition, main merge, release, production migration or deployment was performed.

## Frozen inputs and checkpoints

- Original combined base: `be6862497a58a01ef8c6f4168f02d5624cd44061`.
- PR62: `d353ea3c282eafdfb79384a2cc0fe9fbd52bbccf`.
- PR64: `2c2f6f0c386aea8ee6905f0cfa2494cff2c048fa`.
- Verified 62+64 product: `35fa2513d89deb1044777db7e98e2cc9cd6c270d`;
  tree `60488c5bdb75a9b61814b1b571ec82866ba6d206`.
- Its separately preserved evidence: `3ed68ea25bd33172676e55e16073a0a85e4d83f1`.
- Fixed PR63: `4c880e07ecdff1b6caadd86348779603fce9e8aa`.
- Final product merge: `f679723e2612a486811ac65b41ab0b4339be5b22`;
  tree `dc9c229951425e32077ec5431ae1722a19ccdf25`.

The PR63 merge was clean. All fourteen changed paths equal the fixed PR63 input
byte-for-byte. No additional product edits were needed. The previous 62+64
conflict choices and independent regression are retained; see the
[preceding checkpoint](../integration-native-followup-history/README.md).
The old failing PR63 head was not selected as the integration input. Commit
history and all source branches are preserved.

## Production-code differences reviewed

PR63 is not just a fixture update. Within one finite, locked publication metadata
operation, the catalog is validated and held open once. Each bucket still passes
its own directory check. Nothing caches task authorization or directory identity
across invocations. A brand-new empty store may seed usage only before its first
locator commit. Existing state and the explicit migration continue to use the
dirty-witness protocol; fresh initialization cannot repair an existing catalog.

The plugin moves to 0.7 and retains **both** `migrate-history` and
`migrate-publications`. Queue locator v3 remains a prerequisite; publication
locator v1 transitions through closed v0 to v2. These are distinct layouts.
Migration remains finite, requires stopped old writers (including idle handles)
and a complete raw backup, and preserves objects and original IDs. Bucket or
object-byte capacity causes backpressure, not history/object eviction. See the
[fixed source contract](../../delivery-publication-metadata.md).

[Boundary verification](evidence/boundary.log) confirms all earlier CI steps,
the independent mandatory CAS/RLS and latest-history PostgreSQL gates, the native
five-minute execution budget, Windows+PG gate and packaged verification are
retained. Agent runtime/contextview and packages/runtime match the official
`bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046` trees. Schema 171/172/173, API/SDK,
Web, computer-task/Agent adapters and database queries did not change in this
PR63 increment. No official Agent core modification was made.

## Executed checks on the combined source

These are local checks on the final product merge, not remote CI results.
Commands, exits and timings: [results.json](results.json).

| Check | Exit | Evidence |
| --- | --- | --- |
| Full transferclient + transferqueue, race, original five-minute timeout | 0 | [240 + 6 named PASS; zero SKIP](evidence/transferclient-race.output.txt) |
| Windows amd64 bundle cross-build | 0 | [build log](evidence/windows-bundle.log) |
| Seven bundle file hashes and three PE headers | 0 | [verification and hashes](evidence/bundle-verification.json) |
| Affected Go vet | 0 | [command/result](evidence/transfer-vet.result.json) |
| Combined server build | 0 | [command/result](evidence/server-build.result.json) |
| Current combined development server, HEAD health | 0 | [health](evidence/runtime-health.json) |
| Real browser, existing synthetic task observations | 0 | [browser output](evidence/browser.log), [HTTP proof](ui/proof.json) |

The transfer tests cover both old terminal-history migration/complete original-ID
replay and publication metadata beyond 128 records, finite migration, conflict
preservation, interrupted copies, missing durable evidence, byte limits, dirty
recovery, unknown/corruption, missing/untracked journals and rejecting fresh
initialization over existing state. CLI tests use the real command dispatcher
with isolated fixtures. Synthetic process exits are not physical power cuts.

The Windows package was compiled and its hashes checked here; Windows executable
fixtures, NTFS behavior and Windows+PG were **not executed in this Linux cloud
environment**. The coordinator reported fixed-source PR63 workflows
[Go 39874114092](https://github.com/particleflow/Memoh/actions/runs/39874114092),
[Native 39874113841](https://github.com/particleflow/Memoh/actions/runs/39874113841)
and [Docker 39874113957](https://github.com/particleflow/Memoh/actions/runs/39874113957)
green, including 224.493 seconds for the native package. That is source-head
evidence, not a Windows execution of this combined head or a new CI run here.

The 62+64 package race tests, both mandatory PG gates and 31 affected UI tests
were already run at the preceding checkpoint and remain available there. The
PR63 increment leaves their code and gates unchanged, so they were not rerun.
Neither the complete 2,498 Web suite nor whole-repository Go tests/lint were
rerun. The source PR's seven unresolved media failures remain un-attributed.

## Runtime, resource and evidence boundaries

The server was rebuilt from the final product merge and restarted normally using
the existing isolated configuration/database. Existing Vite was reused. No new
fixture rows, credentials or business submissions were created in this stage.
Browser login, task list and both native/batch detail views use real HTTP and PG,
not mocked responses. The operation rows remain unchanged. Native submission is
shown as recorded, the batch fixture as not started, and both preserve the
business-unverified warning with no invented follow-up/resume action.

Inspected screenshots: [task list](ui/tasks.png),
[native detail](ui/native-detail.png), [batch detail](ui/batch-detail.png).
There were zero page exceptions. Two unrelated container 404 and four workspace
503 responses from the unprovisioned fixture remain in the HTTP proof. This
observation does not establish actual Agent execution or delivery consumption.

Disk space remains constrained. Only two identified old generated Go cache
archives (320,781,234 bytes total) were removed in this stage; the exact records
are in [cache-cleanup.json](evidence/cache-cleanup.json). No database, queue,
history, object, volume or original data was removed. The preceding stage's
no-space PG failure and successful recovery remain published there. No global
prune or timeout increase was used. A helper's full-YAML parse failed on a tab in
an unchanged job; the corrected inspection checks trigger headers only, as
[recorded](evidence/fixture-check-note.txt). No workflow was edited for it.

[Evidence manifest](evidence-manifest.json) includes original/published hashes and
byte counts. Logs are complete; only listed local path substitutions are applied.
Decoded Go Output text is explicitly separate from JSONL. Private server logs,
configuration, login details, seed SQL and executable binaries are not published.
The integration branch matches none of the repository push-triggered workflows;
see [trigger inspection](evidence/push-trigger-check.json). No PR is created.

## Remaining limits

The native follow-up limit is 64 cumulative records, not a rolling active limit.
Clients must save their UUID before submission; accepted is not per-round result
collection. Immutable per-round result association remains incomplete. Pi history
continuity evidence does not guarantee full old model context. Older records do
not silently gain new capability.

The blocked legacy media P1 is still open and was not attempted through this
integration. Real PC throughput, physical power loss and final PC workflow
acceptance remain unverified. Prior Bot-create/workspace readiness and profile
navigation findings are not fixed by this merge. This source checkpoint is for
further review/acceptance, not authorization to migrate or deploy.
