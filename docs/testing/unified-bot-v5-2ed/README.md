# Local Bot v5 combination on the independent Bot/UI baseline

## Source identity

The parent is `2edfd88dafec2291df6eee9343a7a2bd494da24a`. The original integration
branch and its remote ref remain at that separately validated release candidate.
This detached worktree contains only the following admitted increments:

1. `b2d6e1c2058e3bddef1c8b3e9ef4c48468282422` (parent `4768679adf01047bff66c148ded37f215c9994be`): durable Web/native result publication.
2. `87b76482e8173907305f6b9303c061a00d9b0d09` (parent b2d6e1c): per-panel native contact transcript projection.
3. `453a1178c25213772dbead9f428f1dca83e2a027` (parent 87b76482e): legacy unknown-delivery boundary correction.

Library handoff version 5 has ZIP SHA256
`56ad8b945ee0268d65d24e4df5754c607912ebf38daf3216e3bff3c28c843804`.
All 116 content hashes passed; the 117th member is the manifest itself. Each
provided patch reconstructed its exact source Git tree. Applying the three
increments to 2ed produced tree `cd83526211164c4091872e43efcb837bfc4bc1f6`
before the six-file formatting correction and this integration note.

## Integration-only checks

The patches applied without conflict. Six Go files received import grouping,
blank-line or constant grouping corrections using the already cached absolute
golangci-lint 2.10.1 executable, with the repository's gci sections and gofumpt
settings. No execution behavior was intentionally changed. A subsequent
`fmt --no-config -E gofumpt --diff` on these six files passed with no output;
`git diff --check` passed. Source-union guards retain all other 2ed files and
require every other admitted source file to match its reviewed source tree.

The gci formatting command exited 0 but logged a local `go env -json GOMOD`
error. There is no usable host `gofmt` at the checked standard paths and the
host `/usr/bin/go` is the previously observed nonstandard command. A probe of
the existing `web-management-workspace:fixture` image failed before container
creation with `no space left on device`. The root overlay had 0 available
bytes; /tmp had 7,063,314,432 available bytes and inodes were not exhausted.
Nothing was downloaded, no system executable was modified, and no Docker
objects or unknown data were deleted. Actual gofmt/toolchain validation and
full lint are not claimed; that environment-dependent check remains pending.

Reuse the reviewed author evidence: 119 Web checks and actual isolated-browser
live/reload/contact-versus-task projection; six legacy unknown boundary tests
and PG/API 409/restart with no new runs/messages; prior result-publication
PostgreSQL C1/C2/C3/concurrent receipt recovery and process interruption tests.
These unchanged suites were not rerun. They are not final-combination CI or
device/production acceptance.

## Compatibility and delivery boundaries

No migration, generated SDK, configuration or Windows producer change is
included. Schema remains 179. The held 1b filename contract and unmerged 3ca
producer recovery are not restored. Existing 2ed history and all Bot v3 work
remain; neither old unknown records nor user history are rewritten/backfilled.

Web source changes require a later matching Harmony source integration. The
separately prepared 2ed/HAP27 release is not replaced by this local candidate.
This worktree does not push, trigger CI, build a HAP/image or operate production.

New result-delivery JSON and internal commands require compatible consumers on
recovery. Retain the database and receipts; do not use migration down, an old
dump or queue deletion as rollback. A blind old-binary retreat does not establish
active-task/result liveness. Prefer a compatible forward repair and retain the
validated 2ed baseline for the separately coordinated release.

Exact run/turn navigation, external-runtime/channel acknowledgements, the full
revision/cancellation matrix, report-state attention recovery, actual Harmony
device behavior and human QA remain unverified or incomplete. No claim of paid
model, production, or full Bot-goal acceptance is made.
