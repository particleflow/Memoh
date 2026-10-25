# Actual-byte transfer acceptance

This test-only follow-up uses production source from
`a5fe835225c00409687da4aa93e269bf8de1e1a0` (implementation unchanged from
`0f4a4ae8b615098f8a4f34cf187160c37c689996`). It reuses the earlier local resource
acceptance harness, adapting only its driver to the existing metadata registration
and per-delivery ledger APIs. It adds no product feature, service, protocol,
configuration requirement or queue limit.

## Scope and invocation

The previous 200 MiB + 256-file result is retained, not repeated as new evidence.
The new fixture writes one **1,073,741,824-byte (1 GiB)** source and **512 sources
of 65,536 bytes**, totaling **1,107,296,256 bytes in 513 files**. Every byte is
written through a bounded 64 KiB buffer; each source is synced and its allocated
blocks must cover its logical size. No truncate-only sparse source is used.
The 512 files run as 64 existing eight-artifact task batches; this is not a
single 512-artifact Pi declaration or a simultaneous 10,000-file transfer.

The fixture uses a separate fully migrated PostgreSQL database, a NOBYPASSRLS
role, the real reusable-device verifier, Server routes under `/api`, operation
CAS/ledger, Media upload store and local filesystem originals. It builds the
existing production `cmd/transferqueue` locally and runs its finite actions in
new processes. This Linux build is only a cloud test executable, not a new
product or service. A fixture CA is trusted only by those child processes;
TLS chain and hostname verification remain enabled. No OS trust or real host
configuration is changed.

Only the isolated Media service receives `SetMaxUploadBytes(1 << 30)`, the same
existing setting exposed by `[files].max_upload_bytes`. No production setting
or default changes. No `queue-budget.json` is created; metadata-based upload
uses the installed CLI's existing defaults and bounded indexing logic.

```sh
# TEST_POSTGRES_DSN must target an authorized isolated test PostgreSQL instance.
# The existing fixture creates/drops its own database and role.
MEMOH_TEST_LARGE_TRANSFER=1 go test -v -tags=integration -count=1 \
  -timeout=20m -run '^TestComputerLargeByteResourceAcceptance$' ./internal/db
```

Opt-in is test resource protection, not a product limit. The test requires
4 GiB + 64 MiB free temporary space before writing, samples available space
every 100 ms, and cancels if less than 1 GiB remains. Its own regenerated
TempDir data and test database are cleaned up by their existing fixtures.
No global cleanup or user-file deletion is performed.

## Assertions

- Interrupt the CLI after the real upload handler has durably applied 256 MiB,
  withholding that PATCH response. The client checkpoint is exactly 8 MiB
  behind. Resume through fresh CLI processes with the original delivery,
  operation, attempt and upload receipt.
- Check every PATCH offset against the server's already accepted prefix. Count
  accepted bytes separately from rejected request attempts; confirmed data may
  not be uploaded twice or skipped.
- Inject one pre-apply HTTP 503 in each eighth small batch (eight rejections),
  let the existing retry/due policy continue, and complete all original files.
- Require exactly one Create, Complete and Commit per file, a durable committed
  ledger entry with its original receipt/hash/size, and unchanged ledger bytes
  after repeated enqueue and reading the result.
- Download each committed reference through the real production download
  client/routes to one reusable staging file. Re-read and hash all downloaded
  bytes and all original source bytes. Source identity and modification time
  must remain unchanged. This uses the download library; it does not add a
  Linux native publication binding or claim a Windows destination publish.
- Check that already committed temporary upload receipts/payloads are safely
  retired between batches, while retaining all source files and NAS originals
  until fixture cleanup.

## Evidence and limits

Local run: **PASS**, test 433.22s, package 433.293s, no skips.
Data-phase wall time: 426.832s. All 65 rounds passed.

| Measurement | Observed |
| --- | ---: |
| Files / real bytes in each direction | 513 / 1,107,296,256 B |
| Source bytes written / allocated | 1,107,296,256 / 1,107,296,256 B |
| Accepted upload / verified download bytes | 1,107,296,256 / 1,107,296,256 B |
| PATCH attempts / pre-apply 503 rejections | 648 / 8 |
| Confirmed PATCH bytes retransmitted | 0 |
| Create / Complete / Commit per file | 1 / 1 / 1 |
| 1 GiB upload phase | 31.851s; 32.150 MiB/s |
| 1 GiB download + source verification + replay | 5.161s |
| 512-file phases including verification/replay | 381.993s; 1.340 files/s |
| Test/Server rusage peak RSS | 129,277,952 B |
| Maximum child rusage peak RSS | 129,277,952 B |
| Sampled peak allocated / logical fixture bytes | 3,232,440,320 / 3,232,336,450 B |
| Minimum temporary filesystem free space | 1,247,502,336 B |
| PostgreSQL database before / after | 16,135,871 / 17,913,535 B |

The successful raw output is [readable here](evidence/transfer-large-bytes-passed.txt),
SHA-256 `f6f3507b77d6e924def6475a26bf7fe339ab5ec3de3e0a4fd1ea58e33cc2c22e`. The first driver failure is
[retained here](evidence/transfer-large-bytes-driver-failed.txt),
SHA-256 `b1774516fcc701191060efe90049ffc4f47bde543339f3a32641f3fee74a1a31`. Logs contain only synthetic fixture counters and diagnostics;
no endpoint, credential, user file or private database connection is included.
Request counters measure attempts and accepted payload bytes, not total NIC bytes:
a rejected request can have transport buffering, and TLS/HTTP overhead is not counted.
After fixture cleanup temporary free space returned to approximately 4.2 GiB;
workspace free space remained approximately 2.1 GiB.

The first attempt used the old harness assumption that one finite advance
would start upload. It failed after 97.20s waiting for the kill barrier: the
current metadata path had correctly checkpointed its first indexing prefix
and returned `admission_pending`. The fixture was corrected to call subsequent
finite actions until the barrier. No product implementation was changed.
The failed output remains included; it is not a successful product scenario.

The real-byte run was compiled before final formatter/linter-only cleanup.
Executed test-source SHA-256:
`6afaaa11a7fd923977e36fe5abd98e29e9dffd24a6cb4f8ed2d9886d4899478c`.
The [reconstruction patch](evidence/large-byte-executed-source.patch), applied
on this commit's test file, restores those exact executed bytes. The final
changes are formatting, narrow existing-style lint annotations, an unused
callback parameter, equivalent HTTP constant and error wrapping in the space
failure diagnostic. Test sizes, requests, assertions and production bytes are
unchanged. Final incremental integration lint passed. No second large run was
performed only to repeat formatting verification.

These are Linux loopback TLS/PG/filesystem results. Temporary file storage is
**tmpfs**, so allocated bytes are actual filesystem blocks but not physical
media writes. RSS is the OS rusage high-water value for the test/Server process
and each CLI child; it excludes PostgreSQL, build processes and tmpfs/page-cache
storage, and child rusage can include the pre-exec process baseline. The 100 ms
filesystem peak is sampled, not an exact cumulative physical-I/O counter.
Upload duration includes indexing, child restarts, protocol and commit; small
batch timing also includes verification/replay. These are not NIC bandwidth or
pure disk-throughput benchmarks. No claim is made of multi-GiB, 10 TiB, phone,
physical Windows/NAS, WAN/LAN switching, server/PG crash or physical power-loss
acceptance. The interruption here kills the CLI after PATCH; it does not lose
Complete/Commit responses or restart Server. Those earlier separate fixtures
must not be silently merged into this run's scope.
