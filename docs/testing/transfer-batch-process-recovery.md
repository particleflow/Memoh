# Bounded batch upload process recovery

## Source and retained evidence

Base: `7bcbba5ac85a2ba0769b48ed8d4334db4b7806f7` (includes PR #74).
Test source: `4bce6716f09f4507069900e94414229159b01c07`.
This increment changes tests, their CI selection and evidence only. It does not
modify the transfer protocol, CLI, producer, Media, Agent or schema.

Earlier evidence is retained rather than rerun as new progress:

| Existing coverage | New coverage in this increment |
| --- | --- |
| PR #57: CLI killed after successful Complete, before local checkpoint | CLI killed during a three-chunk upload, before the second PATCH response/checkpoint |
| PR #69: first applied PATCH response lost; download Range after truncation | Client offset is 8 MiB behind the server; successful chunks are not resent after a process restart |
| Producer/turnover cases: small files and synthetic history capacity | Three 17,825,915-byte files, with partial batch success while one file gets transient errors |
| PR #63: native Windows publication/metadata crash fixtures | No replacement or rerun of those Windows tests |

## Actual fixture and assertions

The existing full-migration PostgreSQL fixture creates an isolated database and
NOBYPASSRLS role. Real stable-device verification, `server.NewServer`, file and
delivery handlers, operation CAS/ledger, Media and local filesystem storage are
used. An HTTPS listener hosts those routes behind the existing `/api` prefix;
the prefix adapter is a test adapter, not a real Nginx deployment.

The test builds the existing production `cmd/transferqueue` locally and invokes
its finite actions in separate processes. This temporary Linux build is only a
cloud test executable, not another product, service or NAS installation. Its
fixture CA is trusted only in the child's `SSL_CERT_FILE`; TLS verification is
enabled and no OS/PC/NAS trust store is changed. Config/state/source roots,
credentials, task identities and contents are all isolated synthetic fixtures.

1. Register a closed three-artifact plan on the existing operation. Admit the
   first file through the real CLI stdin decoder, immutable snapshot and disk
   queue. Each file is **17,825,915 bytes**, total **53,477,745 bytes**.
2. After the real handler persists the second PATCH, withhold its response and
   kill the CLI child. Require server offset **16,777,216**, local checkpoint
   **8,388,608**, and no associated result. Attempting to open the partial file
   as a final Media original must fail.
3. Close and reopen the real Media/upload store and handler against the same
   files and PG. Require the original upload ID, offset and uploading state.
   This reopens server modules; it does **not** kill the server or PostgreSQL.
4. Resume through a new CLI process. Return synthetic HTTP 503 on subsequent
   PATCH requests for that one delivery. This fault is outside the production
   handler; all successful PATCH/Create/Get/Complete/Commit/Resolve/download
   requests still traverse the real routes. Require a persisted retryable state.
5. Admit the other two files with the same device credential and original task.
   Invoke the existing finite queue actions, respecting persisted due times.
   Both files must commit while the first remains unassociated. Remove the
   transient fault and require recovery of the first file, with no new business
   attempt, delivery or receipt. The observed run rejected two PATCH attempts.
6. Require accepted PATCH offsets **[0, 8388608, 16777216]** for each file.
   Every attempted first-file PATCH after the kill must start at or above
   **16777216**, including failed attempts. Per file, **Create/Complete/Commit
   each equals 1**. The PG committed entry must match the original upload ID,
   content hash and byte count. Repeated enqueue hooks must replay confirmation
   and leave all prior ledger entries unchanged.
7. Download every committed reference through the production download client
   and real HTTPS route. Independently stream SHA-256 and byte-count checks for
   download and source. Source inode and modification time must remain the same.
   Reuse one private staging file; no Linux local-publication implementation is
   invented. The pre-Complete invisibility and full committed-file checks do
   not establish every filesystem power-loss/atomic-rename crash window.

## Reproduction and raw result

Use an isolated PostgreSQL instance with permission to create/drop test
databases/roles. Set `TEST_POSTGRES_DSN` to that instance, never production.

```sh
go test -v -race -tags=integration -count=1 -timeout 90s \
  -run '^TestComputerDeliveryPostgresAuthorizationCASAndPreservation$/^batch-process-recovery$' \
  ./internal/db
```

Local environment: Linux amd64, Go 1.25.7, PostgreSQL 18 isolated container.
Result: **2 named PASS, 0 SKIP, 25.468s** package time; scenario **14.93s**.
The unchanged original log is [focused.txt](evidence/transfer-batch-process/focused.txt)
(SHA-256 `ce90064f37de3cc1c7ee7a1d336faf3763bb1e685ac967621317ea68ef6255a5`).
The complete log was checked for secrets/private endpoints before copying; it
contains only test names, synthetic byte counts/offsets and timings. No
redaction or replacement of original test output was needed.

Incremental integration-tagged golangci-lint against `7bcbba5a` exited 0;
gofmt and `git diff --check` passed. No full old business suite was rerun locally.
Go CI gives this scenario its own 90-second process and excludes only this
scenario from the existing 90-second delivery-contract step. It retains all
assertions and the existing suite; no workflow permission or timeout is widened.

Before testing, `/workspace` had 2.1 GiB and `/tmp` 4.7 GiB available. Test temp
files/builds use `/tmp`, sources total about 51 MiB, and a single download staging
file is reused. `t.TempDir` cleanup removes only this run's regenerable fixtures;
there is no global prune or original/history cleanup. After the run, available
space was approximately 1.7 GiB and 4.6 GiB respectively (build caches also grew).
This is not a measured peak memory/disk bound or a throughput benchmark.

## Remaining acceptance boundaries

- No physical power interruption, server/PG process kill, storage-cache loss,
  actual NAS/PC network or Windows OS run occurs in this test.
- 51 MiB across three files tests multi-chunk recovery, not multi-GiB files,
  thousands of transfers, sustained throughput or the product's ultimate limits.
- The same-reference download/hash path is real, but a Windows finite CLI with
  NTFS publication talking to the unified real server remains a separate joint
  acceptance requirement. Existing native and Linux tests cannot be combined
  into a claim that this whole deployment has passed.
- Real client LAN/IPv6 route choice, network handover, prolonged outage and
  physical durability remain unmeasured. No DNS, credentials, configuration
  roots, installer, daemon or platform support was added to hide these gaps.
- No Memoh runtime/UI was installed or verified; no human QA or screenshot
  evidence is claimed. This cloud test increment remains a Draft.
