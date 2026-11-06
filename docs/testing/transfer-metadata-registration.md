# Metadata-first upload registration

This is the PR77 follow-up to `37b7464fe062714258ddea5208cce2a6f9ea5f5b`.
The normal upload CLI persists metadata before active indexing/transfer, without
reserving aggregate source sizes or creating a second full source copy. The same
HostConfig, source root, locator, job locks, framed journal, resumable hash state,
block proofs, receipts and NAS association remain the owners of their data.

## Compatibility and bounds

- New upload records use v5. Existing v2/v3/v4 snapshot records are readable; the
  locator and exact `transfer.json` HostBinding do not change. Normal new uploads
  do not require `queue-budget.json` or `migrate-budget`.
- CLI `registered` is additive; `admitted` retains its verified/indexed meaning.
  Producer manifests can be consumed after durable registration. Finite advance
  resumes indexing as well as transmission; lifecycle v1 fields remain unchanged.
- Source identity/size/mtime plus block proofs prevent mixing source versions.
  Changed/missing sources become explicit blocked records without redoing the
  business task, deleting evidence, or creating another receipt. Windows reuses
  native no-write/delete-sharing handles for each finite call.
- This reuses the existing block index; it does not implement or embed BEP.
  The complete hash is still required by Server Create. Producer hashing, index
  verification and subsequent block reads are distinct remaining I/O, not zero cost.
- The installed worker limit bounds simultaneous activity, not queued files.
  Metadata itself still consumes real storage. Selection currently scans journal
  metadata (linear in backlog); this increment does not claim constant-time or
  large-population throughput.
- Download/publication retained-byte limits, Server's default 200 MiB single-file
  limit, task producer 16-file manifests and per-operation ledger constraints are
  unchanged. Thus this is not full 10,000-large-file product acceptance.
- Old executables reject v5 records. Normal coordinated package replacement must
  not roll an old reader over new records. No production update was performed.

## Executed evidence

Cloud Linux, `/tmp` temporary filesystem, synthetic inputs and credentials only.
No user file, PC, NAS, production credential, global trust store or OS security
setting was used. Existing local PostgreSQL fixture created and dropped its own
fresh database through the production full migration helper.

- [Scale raw log](evidence/transfer-metadata-registration/scale.txt): 10,000 distinct
  sparse 1 GiB sources (10 TiB logical only), plus one small helper registration.
  Register 686.831 ms; queue file lengths 12,201,500 B; allocated queue file blocks
  40,968,192 B (directory/inode metadata extra); sampled Go heap baseline 282,344 B,
  peak 4,036,600 B, growth 3,754,256 B; selection 566.071 ms. No snapshot/tree/part
  files created by registration. This is not 10 TiB read/transfer or physical
  disk throughput; sampled heap is not whole-process/host peak RSS.
- [Affected race packages](evidence/transfer-metadata-registration/race.txt):
  transferclient 30.150 s, deliverymeta 1.052 s, CLI 8.461 s. The scale test ran
  separately without race instrumentation; it is not represented by these times.
- [New recovery subset](evidence/transfer-metadata-registration/registration-recovery.txt):
  process exit after registration fsync before response, source change/deletion,
  cancel, worker exclusion, same-ID restart and lost PATCH response. This subset
  preceded the final all-package race run, which also includes block-proof
  tampering with preserved size/mtime.
- [Real Server/PG/CLI log](evidence/transfer-metadata-registration/postgres.txt):
  19.918 s, all selected tests pass. Ready-proof lost response, process killed
  before checkpoint and no-proof unknown retention; multi-file producer; three
  17,825,915-byte files with real routes and file storage. For each large fixture,
  accepted PATCH offsets 0/8388608/16777216 and Create/Complete/Commit each 1;
  source and route download SHA256 match. Kill occurs at Server offset 16777216
  with client checkpoint 8388608; accepted bytes are not resent. Two independent
  files finish before recovery. This is process interruption, not physical power
  loss, and Linux test execution is not native Windows/NAS acceptance.
- Incremental golangci-lint passes. Windows transferclient test cross-compiles;
  native CI must supply actual Windows execution evidence after this commit.

Reproduction uses the existing commands, no separate benchmark service:

```sh
go test -v -count=1 -run '^TestRegistrationTenThousand' ./internal/transferclient
go test -race -count=1 -skip '^TestRegistrationTenThousand' ./internal/transferclient ./internal/deliverymeta ./cmd/transferqueue
# Set TEST_POSTGRES_DSN only to an isolated local test database fixture.
go test -v -tags=integration -count=1 -timeout=5m -run '^TestComputerDeliveryPostgresAuthorizationCASAndPreservation$/(batch-process-recovery|ready-proof-cli|producer-multiple-files)$' ./internal/db
```

The task scope excludes Memoh runtime/UI installation. No real UI screenshots or
human QA are claimed. Existing PR77 remains Draft; no merge, release or deployment.
