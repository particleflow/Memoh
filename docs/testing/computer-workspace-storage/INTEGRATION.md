# Post-4d integration checkpoint

The integration preserves the frozen `4d23244c683aca4c436efec4db375921fdf60472`
checkpoint and merges these fixed sources without replacing generated trees:

| Source | Scope |
| --- | --- |
| `56b38d39fadda858963b36e597dcabd3a0fb0a1d` | Account-owned NAS computer directory configuration, shared settings and three generated API operations |
| `274b171c3903d4d47944cbe0841c0c0df598e9a6` | Existing delegation-scoped image reference loader for `edit_image` |
| `83b027e935dde846cf73af2216b1f8cd617460e3` | Internal safe result-read diagnostics; no execution or retry change |

The merge product checkpoint is `a1914ea447d347ad3a886e9d49ab8f2fbc5c9539`
(tree `001b05a9c0452a7a669788e2ce2b34077d2acce4`). A separate fixture-only
commit removes `Target.DeviceID` from a temporary value whose NativeHerdrProfile
serialization never reads that field. Assertions and production identity checks
are unchanged. This addresses the same baseline unusedwrite diagnostic reported
by both source tasks; no lint rule or exclusion is added.

## Integration validation

Before the diagnostics merge, the combined core selection passed:

```sh
go test -p=1 -ldflags='-s -w' ./cmd/internal/core \
  -run '^Test(ImageCapability|DelegatedImage|ComputerTask)' -count=1 -v
```

After the diagnostics merge, the following exact selection passed in
`internal/agenvo` (0.008s) and `internal/computertask` (0.639s), including the
logging-off/on comparison and all nine result-read cases:

```sh
go test -p=1 -ldflags='-s -w' ./internal/agenvo ./internal/computertask ./cmd/internal/core \
  -run '^Test(BatchReadTracePreservesSentinelIdentity|ResultReadTracePreservesErrorsAndDispatch|ImageCapability|DelegatedImage|ComputerTask)' \
  -count=1 -v
```

The latter command returned **1**: the subsequent core test binary could not
link because the local temporary filesystem ran out of space. No core test ran
in that attempt. It does not establish a full final-head test pass. The original
output is retained in `evidence/integration-go.txt`; final CI must independently
verify the candidate, including full lint. Previous source evidence is retained
without rerunning its 128 Web tests, real PG/HTTP, browser flows or Windows tests.
Raw historical logs retain their original whitespace.

The generated API operation set grows from 368 to 371, with no removed operation.
The existing required PG set gains the storage HTTP case. Native CI keeps the
previously validated 10-minute Windows aggregate and 35-minute job budgets.
There is no migration, official Agent core change, dependency upgrade or token
protocol change. Schema stays177. No model or real-device call was made.

## Consumer and deployment handoff

The shared settings and SDK contract are documented in `README.md`. Port only
the shared settings route/sidebar, directory-picker callback, error translations
and generated SDK additions to the mobile source; do not replace its adaptation
tree or rebuild a previously accepted HAP. The separate Harmony timing patch
`b0849bc832cbe82f6c5a5ce4d155857fb9d7ae35` remains mobile-owned and is neither
included here nor proof of an image performance remedy.

This configuration slice does not redirect existing Herdr work_dir, Runtime
sourceRoot, receipts or task fingerprints. Windows fresh setup/profile consumption
of the saved UNC and actual private share/mapping/permissions still require joint
acceptance. Cross-Bot incident inputs and ready-before-trigger behavior likewise
remain separately unverified; the loader fix adds no attachment grant.

After final CI, use only the existing `docker.yml` dispatch on
`integrate/unified-7b-transfer-observation` with `validated_sha` equal to that
exact verified candidate. Check for an existing run before dispatch. This task
does not dispatch, publish or deploy an image. The sole field owner must preserve
the current private configuration, mounts, originals, database and previous image
digests. Application rollback uses the same schema177 database and files; do not
invoke generic migrate-down or delete volumes. Old binaries ignore the added
storage metadata; a rollback is not a workspace-root migration.

Only after the diagnostic source and logger are deployed may the authorized
field owner plan one correlated original-operation read. No new result call,
recollection, Pi replay or historical record rewrite is performed here.

Human QA and physical-client acceptance remain incomplete.
