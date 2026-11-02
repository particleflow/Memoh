# Native follow-up and Pi history integration checkpoint

Source-only integration from `be6862497a58a01ef8c6f4168f02d5624cd44061`:

- Fixed PR62: `d353ea3c282eafdfb79384a2cc0fe9fbd52bbccf`.
- Fixed PR64: `2c2f6f0c386aea8ee6905f0cfa2494cff2c048fa`.
- PR62 merge: `8f107427`.
- PR64 merge: `6c002881`.
- Product checkpoint, including a standard-client combination regression:
  `35fa2513d89deb1044777db7e98e2cc9cd6c270d`.
- Product tree: `60488c5bdb75a9b61814b1b571ec82866ba6d206`.

No PR, remote CI, ready transition, main merge, release or production deployment
was performed. This checkpoint precedes the separately authorized PR63 increment.
It does not contain the old failed `a8ad910` work.

## Conflict resolution

The PR62 merge conflicted only in `go-ci.yml`: retained the complete existing
PostgreSQL delegation/task gate and appended the CAS/ownership/RLS gate.
PR64 conflicted in three files:

- `go-ci.yml`: retained every prior step unchanged and added the independent
  latest-history gate. Both scripts require a DSN and an actual named PASS;
  skipped or absent tests fail their gate.
- `service.go`: kept native identity/follow-up fields and batch `continuity`.
  Native projection remains under `Batch == nil`; batch evidence remains under
  `Batch != nil`. The batch submission history checks are intact.
- `source.go`: retained the follow-up tool and follow-up-aware get description,
  plus PR64's result/recovery continuity cautions. No duplicated tool descriptor.

[Boundary verification](evidence/boundary.log) proves the existing workflow steps
remain value-identical, both independent gates occur once, and official Agent
runtime/contextview, packages/runtime, migrations 171/172/173, SDK/spec, Web and
transferclient are unchanged from the prior combined checkpoint. SQL query and
generated query code match the reviewed PR64 input. No Agent internals were edited.

## Actual local verification

Commands, exits and timings are in [results.json](results.json). Go totals below
count named test/subtest events. These are local results, not new remote CI runs.

| Check | Exit | Result / readable output |
| --- | --- | --- |
| Full Agenvo package, race | 0 | [195 PASS, 1 opt-in PG SKIP](evidence/agenvo.output.txt) |
| Account/Bot PG authorization, explicit DSN follow-up | 0 | [1 PASS](evidence/agenvo-account-pg.output.txt), covers the skipped case |
| Full computer task package, race | 0 | [271 PASS](evidence/computertask.output.txt) |
| Full session task package, race | 0 | [54 PASS](evidence/sessiontask.output.txt) |
| CAS/RLS required PG gate, first attempt | 1 | [Isolated DB creation failed: no space](evidence/operation-pg.output.txt); assertions did not run |
| CAS/RLS required PG gate after storage recovery | 0 | [Named PASS, no SKIP](evidence/operation-pg-retry.output.txt) |
| Latest-history required PG gate | 0 | [Named PASS, no SKIP](evidence/history-pg.output.txt) |
| Affected task UI | 0 | [2 files / 31 tests](evidence/ui-tasks.log) |
| Server build | 0 | [Command/exit](evidence/server-build.result.json) |
| Real browser observation | 0 | [Proof](ui/proof.json), [output](evidence/browser.log) |

The new regression calls the standard MCP client for both routes: native follow-up
gets one `agent.get` / `agent.prompt`, remains business-unknown, and has no batch
continuity/exit evidence; a completed synthetic batch has historical evidence but
rejects native follow-up without changing its ledger or dispatch count. The tool
catalog regression checks unique descriptors and both sets of boundary wording.
Existing tests cover missing/changing session metadata, CAS collisions, cumulative
capacity, unknown no-redispatch, damaged/legacy history, compaction/branch forms and
latest-row scoping. The complete 2,498 Web suite was **not rerun**; Web/SDK source
was unchanged and only the affected task UI tests ran.

The original Agenvo package's opt-in SKIP is retained, not edited into PASS. Its
explicit PostgreSQL rerun passed independently. The seven media failures recorded
by the source PR remain un-attributed; they were neither rerun nor fixed here.
No blocked legacy media P1 repair was attempted. Whole-repository Go tests/lint,
Windows execution and physical power-loss/real-PC acceptance were not performed.

## Browser scope and fixture corrections

Rebuilt server on loopback port 18100, existing Vite on
`http://127.0.0.1:18102`, existing isolated PostgreSQL schema 173. Two synthetic
computer receipts were appended to the fixture, with unusable placeholder runtime
registrations and no OAuth credential or external provider. There are zero models
and zero providers. No existing data was removed or task dispatched.

Actual login → existing conversation → Tasks → two computer details uses real
HTTP/PG, not mocked responses. The native fixture shows **Submission recorded**;
its batch peer shows **Not started**. Both show no final summary and the explicit
business-unverified caveat. Actual HTTP includes the new continuity warning;
the existing UI renders localized status/caveat rather than the service's generic
summary. There is no invented follow-up/resume button. Only task GETs were issued,
operation rows stayed byte-equivalent, and there were no page exceptions.

Inspected screenshots: [list](ui/tasks.png), [native detail](ui/native-detail.png),
[batch detail](ui/batch-detail.png). These are observation fixtures, not native
Agent prompt execution, Pi completion or a restored model conversation. The
unprovisioned workspace's unrelated 404/503 responses remain in the proof.

[Fixture correction record](evidence/fixture-failure.txt) preserves these failures:
SQL seed syntax lacked a delimiter (zero inserts confirmed before correction);
startup health helper incorrectly used GET (405), then HEAD returned 200 without
another restart; browser initially expected a server summary rendered verbatim,
then was corrected to assert actual HTTP and localized UI separately. The failed
browser log/exit are included. No product code was altered for these helpers.

The PG disk failure was recovered without dropping databases or deleting raw
state: an old generated 130,248,888-byte server binary was copied to the separate
scratch filesystem, verified byte-for-byte by SHA-256, and its old path retained
as a symlink. No global prune or queue/history/object deletion. Earlier evidence
and the frozen be686 checkpoint are preserved.

## Limits that remain

PR62's 64 records are a **cumulative hard cap**, including reserved/rejected
requests; callers must persist their UUID before sending and reuse the exact
intent after a lost response. Accepted is prompt acceptance only. Per-round
immutable result association and collection are not implemented. Session/name
observation is not an atomic incarnation guarantee, and older metadata never
silently acquires stronger capability.

PR64 verifies bounded file/history evidence and supported official branch forms;
it does not guarantee all old messages are retained in model context. Query and
recovery remain continuity-unconfirmed. Missing history never silently starts a
fresh conversation. See the unchanged source boundary documents for full limits.

Legacy media P1, Bot-create/workspace readiness and the profile-navigation anomaly
remain open. This integration is input to further review/acceptance, not proof the
real PC workflow is complete.

## Evidence handling

[evidence-manifest.json](evidence-manifest.json) provides source/published byte
counts and hashes. Full logs retain failures/skips; only exact repository,
toolchain and module-cache paths are normalized. `*.output.txt` is a decoded Go
Output projection, not raw JSONL; matching `.log` preserves the JSON events.
Private server/configuration/auth logs, seed SQL and credentials are excluded.
All visible content is synthetic. Captured command whitespace is preserved.
