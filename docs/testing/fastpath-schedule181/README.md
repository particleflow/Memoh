# Qualified file routing combined with deployed schedule181

This is a source integration record. No production image, NAS operation, HTTP-token
activation, Windows change or HAP rebuild is performed by this combination.

## Exact sources and merge

- Original review branch parent: `7a5f2bb2f7eda2a00c3af6ef6f64281c0b45475f`, tree
  `3be3719bdd9d355dbb0ffd0b13705c97eade9fad`.
- Deployed schedule source: `e2b7a43b9a41716af11bd7aa374aad23814d4914`, tree
  `ab21ab5fc87440936557f4857fa0a89d3c5cf079`.
- Common qualified baseline: `370922014a2435b83d4a2c33cd0b0611bc85de11`.

The89 fast-path paths and35 schedule181 paths relative to that baseline have no
intersection. The actual merge is conflict-free and retains both original parent
histories. Every changed source blob equals its corresponding qualified parent;
the only new file is this integration record. Nothing is rewritten to reconstruct
the other branch from an older snapshot.

The complete migration tree, schedule/task-result implementation, official Agent
core, computer-task/tool context and transferqueue trees equal e2b7. The Web, SDK
and spec trees equal7a5. Schedule181, its exact177-to181 regression and the corrected
CI selector/required-PASS entry all remain. This is not an old7a5 deployment over181.

## Necessary integration check and reused evidence

The existing Go1.25.7 toolchain compiled the media, transferclient, schedule,
taskresult and core packages together with `go test ... -run '^$'`: exit0. This is
a five-package compilation seam, not a claim that their existing test cases were
rerun. A source/blob guard verified all124 imported paths, protected trees and
mobile-consumer trees. Its initial audit-script attempt addressed a Git index
directory using `:path`; the corrected version resolves the written index tree.
That audit-script error did not change product code or fail a product test.

Reuse7a5's six successful first-attempt CI suites, original routing/browser/byte
evidence and its two prior transport/timing/history seams. Reuse e2b7 Go CI
40928461916 (seven successful jobs), Docker PR40928461556, policy40928461648 and
the actual isolated schedule181 forward/preservation evidence. The original37e3
stale180-name failure remains disclosed; no unchanged run was rerun. New combined
head qualification must come from its normal PR79 CI, not either parent result.

## Consumers, activation and recovery

Parent reports e2b7/schema181 actually deployed and HAP31 signed/distributed with
the shared7a5 source. This merge changes none of that shared source or SDK; do not
rebuild the same HAP. This record does not independently inspect the signed package
or establish phone acceptance. Harmony native bridge/background source remains
the original consumer owner's qualified composition.

Windows production57 remains unchanged. Producer212ae and its paired consumer are
not incorporated here, and shared7a5 is not a substitute producer. Receipt,
HostBinding, queue/journal, authorization and recovery implementations are retained.
Real HTTP credential transmission is not enabled. Browser mixed-content and ordinary
HTTP secure-storage restrictions remain; no guard or TLS policy is relaxed.

The current target is schema181 with no migration beyond the already-deployed181
source. Future application by the unique writer must preserve that latest schema,
schedule/fire/run/publication receipts, unknown records, files and configuration.
Before fast-path activation, unmatched consumers must retain their original safe
route or refuse explicitly; never rewrite an existing queue or binding to make it
fit. No live activation or compatibility downgrade is tested by this merge.

The compatible application recovery baseline is e2b7 on the current181 database,
not370 or standalone7a5. Preserve newest data and181-compatible scheduler writers;
do not down/force, restore an old dump over new history or replay unknown work.
Publishing a workspace image does not authorize recreating current Bot workspaces.
