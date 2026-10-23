# Pinned-linter completion and unified-candidate handoff

## Fixed source and evidence boundaries

- Tested product head: `f7479c56f9d9e67be0ddff2ef73bcc6d0b952f75`.
- Tested full tree: `db371bfae38c0f6d2f4e7581cac63a1d211939ca`.
- Existing branch: `fix/golangci-g704-source-tracking`.
- This follow-up changes documentation/evidence only. Its own SHA is the separate
  evidence head reported in the handoff; it is not presented as a second CI-tested
  product head. No executable source, test, dependency, generated API, workflow or
  check configuration changes, and no new CI dispatch accompanies it.

## Historical failure and completed checks

| Check | Terminal result | Evidence |
| --- | --- | --- |
| Initial local full v2.10.1 lint | Exit 1: missing compiled-source cache index | Original [failure log](full-lint.log), unchanged |
| Single bounded cache-repair follow-up, full v2.10.1 lint | Exit 0, 637.016 seconds; `./...`, no changed-line filter | Original [execution record](full-lint-rebuilt-cache.json) and [zero-byte stdout/stderr log](full-lint-rebuilt-cache.log) |
| Synthetic image-capability package | Exit 0; 17 top-level PASS, 44 subtest PASS, zero failures/skips | Existing [complete output](image-tests.log) |
| Official Agent runtime/contextview boundary | Exit 0, baseline `bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046` | Existing [output](core-boundary.log) |
| Original Go CI, exact product SHA, attempt 1 | Completed/success; seven successful jobs | [Run](https://github.com/particleflow/Memoh/actions/runs/39947915759), archived [run](ci-run.json) and [job/step metadata](ci-jobs.json) |

The parent and independent reviewer additionally inspected the remote
[Lint job](https://github.com/particleflow/Memoh/actions/runs/39947915759/job/120395020292)
and confirmed full v2.10.1 lint completed in **392.635 seconds without a diff
filter**. That analyzer duration is their CI-log observation, not the local
637.016-second duration. The archived API metadata confirms the job's successful
terminal state (its whole-job wall time includes setup/cleanup); it does not itself
contain the 392.635-second analyzer measurement. No new log download or test run
was needed for this evidence-only follow-up.

The seven successful jobs were Test, Lint, Agent core boundary, PostgreSQL fencing,
Redis session runtime, Valkey session runtime and pgvector bootstrap. In the
PostgreSQL job, the operation CAS/RLS and Pi history/latest-evidence requirements
remain **two separately named successful steps**, both retaining their no-skip
contract. Runner/Action deprecation notices remain informational warnings; they
were not removed or treated as failures.

The [completion manifest](completion-manifest.json) records raw-copy hashes and
sizes. All original failure files and the earlier manifest remain byte-for-byte
unchanged. A zero-byte lint log proves only that no output was captured; the saved
exit record and the independently completed CI supply the success evidence.

## Next execution contacts and remaining acceptance

The server/Web source checkpoint for the unified candidate is now **f7479c56**,
which retains the integrated `8c358cac` lineage. The prior lint/Actions blockage is
closed; neither another analyzer upgrade nor another full CI run is a prerequisite
for handing this source to the existing integration and acceptance owners.

The next handoff should pin that server/Web source alongside the actual client
build identities, then continue the existing isolated client/API acceptance:

- Windows: the last recorded source contact in this thread is `a6908728`, with
  `/api` support. The operating-side owner must verify its current build identity
  before checking the established upload/download, scoped reference and task-result
  interfaces. These lint/Go results do not establish real-PC throughput, power-loss
  recovery or an actual Pi/provider business result.
- Mobile: the last recorded source contact is `b568d81`; the complete original
  overlay/native shell and reproducible source-build handoff were still missing in
  this thread. Confirm that handoff with the mobile owner before claiming client
  build or end-to-end acceptance. These are recorded contacts, not a fresh claim
  about the latest mobile or Windows deployment.
- Web: retain the existing runtime/browser and attachment evidence linked from
  [the prior restoration report](../../unified-ci-restoration.md). This change
  adds no new UI acceptance and does not turn seeded/synthetic task observations
  into a real specialist-Bot, PC or paid-provider delivery test.

The legacy-media P1 release hold remains unresolved by this work; its previously
denied repair was not retried. Production OAuth recovery and actual PC/mobile
acceptance have no new verification in this follow-up. Those boundaries still
require their existing owners' evidence; they do not block independent source,
build-provenance or isolated fixture work. No merge, image publication, production
deployment, real PC command, paid model request or credential change occurred.

The local root filesystem was still full at handoff, with about 460 MiB free in
`/tmp`. This does not invalidate the completed checks, but future large local
builds need a bounded resource check and safe capacity recovery; this evidence-only
follow-up does not repeat cache cleanup or delete data.
