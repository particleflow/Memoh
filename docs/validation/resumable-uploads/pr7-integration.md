# PR7 integration validation

Date: 2026-10-21. Verified main: `a819c75ed94ae06a3a8196e9416302b18d8dd34f`.
Its tree is `924e0bc57f1845b28801ebe0e4ea138d6709ab1e`; GitHub reports PR7 MERGED
with that merge commit. This integration preserves the PiJSON implementation
already present in that main. It does not include PR9 or runtime migration work.

## Source preservation

The two reviewed PR8 commits rebased without conflicts:

```text
1: 2d67c5ae = 1: fec7036f feat(media): add durable resumable attachment uploads
2: 2bf79bbb = 2: 2472cbba fix(media): repair upload durability barriers and HTTP transport
```

`git range-diff` reports both patches identical. The integration's implementation
commit is `2472cbbaea9308a458a6ee31ae4b7a8e9aa43a0f`; the subsequent evidence commit
only updates documentation, logs and screenshots. Relative to the verified main,
`internal/agent`, `internal/agenvo`, `internal/computertask` and `db/postgres` all
have zero diff. The official Agent runtime/contextview guard also passes against
`bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046`.

Swagger and SDK regeneration leave no tracked diff. All six frozen media paths,
four media schema definitions and PR7's actual changed `agenvo.AgentProfile`
schema definition match their respective reviewed sources exactly. See
[integration-contract.txt](integration-contract.txt). The phone 1.0.5 and Windows
transfer work can continue using the frozen API without a protocol change.

## Shared configuration and ownership check

No new production path setting or duplicate registry is introduced. Existing
`config.Load` resolves `[files].originals_root` and its existing
`MEMOH_ORIGINALS_ROOT` override into `cfg.Files.OriginalsRoot`. The core provider
wiring passes that same resolved value to `localfs.NewOriginals` and
`media.Service.EnableUploads`. The latter owns only the private `.uploads`
staging child. HTTP handlers accept receipt IDs and Bot-scoped stable hashes,
not filesystem roots; publication still goes through `media.Service.persist`
and the existing storage provider. There is no new business-level path builder.
The configuration/override and finite-limit tests pass; see
[integration-config.txt](integration-config.txt).

The `.uploads` JSON is transfer checkpoint state, not a new registry for Agent
tasks, sessions or users. Logs, configuration, user data and scheduler state keep
their existing ownership boundaries. This change does not relocate any root or
volume, and does not implement root relocation/version migration. Moving a root
or changing its physical layout requires a separately coordinated migration and
read/write consistency check; changing one configuration value is not itself a
data migration. Existing receipts are not searched for in an old root.

Without an originals root, resumable upload is explicitly disabled (503).
Initialization errors fail startup; failed publication/fsync cannot return ready.
The existing durable provider has an earlier compatibility rule: only when a
canonical object is absent, it may copy a legacy workspace/spill object into
canonical storage before returning it. Other canonical errors do not take that
path, and copy failure is returned. This is not an old-originals-root search;
however, aligning that existing legacy import policy with any future strict
location migration is a cross-module coordination item, not silently changed
by PR8. No production path, volume or permission was changed in this check.

## Checks executed after rebase

```sh
go test -p 3 ./internal/media ./internal/handlers ./internal/server ./internal/storage/... ./cmd/internal/core ./internal/agenvo ./internal/computertask
go test -race -p 3 ./internal/media ./internal/handlers ./internal/server ./internal/storage/providers/localfs ./internal/storage/providers/fallback ./internal/agenvo ./internal/computertask
go build -p 3 -o /workspace/.memoh-cloud/bin/memoh-server ./cmd/agent
go generate ./internal/handlers
pnpm run generate-sdk
pnpm --filter @memohai/sdk typecheck
bash scripts/check-agent-core-boundary.sh
```

The ordinary and race suites passed, including both the file-module regressions
and PR7's agenvo/computertask cases. Some unchanged ordinary packages used Go's
test cache; the complete related-package race suite executed successfully.
Logs: [ordinary tests](integration-go-tests.txt), [race](integration-race.txt).
The PR records the final lint result and required CI run IDs for its exact head;
the old `2bf79bbb` CI is not used as evidence for the integrated candidate.

## Current-source runtime and UI

The owned isolated cloud Server was rebuilt from the integrated source and
restarted using the prepared cloud startup workflow. PostgreSQL/pgvector, schema
checks, authentication, Server readiness and the Web proxy passed. No production
server, private client project, real provider or existing PC operation was used.

Real Chromium cross-origin Web port 18082 to Server port 18080 OPTIONS and SDK
Blob transfer passed with exact non-UTF8 readback. The explicit CORS header list,
origin and credentials policies are unchanged. See [OPTIONS](integration-cors.txt)
and [browser SDK](integration-sdk.txt).

The running Server used the existing isolated synthetic originals root and an
explicit finite 512 MiB policy. A streaming HTTP client sent 8 MiB chunks from a
256 KiB source buffer and verified whole-file SHA by streaming readback:

| File | Elapsed including validation | Server peak RSS | Result |
| --- | ---: | ---: | --- |
| 200 MiB | 2.846 s | 88,252 KiB | upload, completion, repeat completion and SHA readback passed |
| 512 MiB | 17.438 s | 86,436 KiB | same, with an actual owned Server/Web restart after 16 MiB acknowledged and exact-offset resume |

The 512 MiB timing includes restart/readiness and readback. Existing synthetic
originals from the earlier tests were preserved, so repeated zero-byte-content
files can use canonical deduplication. These are local cloud observations, not
fresh-publication benchmarks or NAS performance claims. Anonymous access,
incomplete completion, absence of early content_hash and rejection of canceling
ready receipts also passed. See [integration-http.txt](integration-http.txt).

Fresh screenshots were captured and inspected from this running version:
[picker](attachment-picker.png) and [pending selection](attachment-selected.png).
The existing UI selected a 1,200,000-byte synthetic text file and displayed 24,001
lines. No Send/model turn occurred; this is pending-preview evidence, not a full
model chain or a resumable-client UI claim. See [UI log](integration-ui.txt).

## Candidate and deployment boundary

The integrated branch includes PR7 before either PR8 patch. A candidate must use
the final reviewed integration SHA and its own successful required CI. The
existing `docker.yml` dispatch guard requires `validated_sha == GITHUB_SHA`;
promotion/deployment is a separate authorized step after review. The old pre-PR7
PR8 base must not replace a PiJSON-capable deployment.

No new database migration, credential, port or production permission is added.
Default admission remains 200 MiB. Originals and completed references remain in
the unified file module; no runtime migration or scheduler-log storage redesign
is part of this integration. NAS mount/lock/fsync acceptance and physical power
loss are still unverified here. Any NAS filesystem probe must run against the
configuration-derived local Server mount, including read-only journal fsync;
a desktop SMB/NFS path or a cloud fixture cannot establish that guarantee.
