# Windows/NAS transfer: real-route cloud test evidence

## Source and scope

- Server candidate/base: `dfc7cf5104c382ecc622061ce43c3dcd733b7998`.
- Test implementation: `ce8e6bd928cf504f8de5f1e638f65772840df65c`.
- Frozen transfer implementation: PR #63, `4c880e07ecdff1b6caadd86348779603fce9e8aa`.
  The base has identical `internal/transferclient`, `cmd/transferqueue` and
  `contrib/herdr/memoh-delivery` content. This increment changes tests and this
  evidence document only. It does not change the producer or Media implementation.
- No production dispatch, migration, credentials, trust-store changes, artifact
  download, installation, release or deployment occurred.

Earlier evidence covered production CLI upload recovery (PR #57) and Windows
NTFS publication separately (PR #63). The unified candidate's existing evidence
did not cover uploading a file through its real routes and then downloading
that same reference with an interrupted Range transfer.

## What actually runs

The `real-route-roundtrip` subtest reuses the existing full-migration PostgreSQL
fixture, isolated database and NOBYPASSRLS runtime role. It uses the real stable
device credential registry/verifier, `server.NewServer`, transfer handlers,
operation CAS/ledger, Media service and `localfs.Originals`. Only identities,
credentials and file content are synthetic. No in-memory NAS protocol replaces
these services.

An `httptest` HTTPS listener hosts the real Server. The upload subprocess trusts
its generated certificate via that process's `SSL_CERT_FILE`; the download
client uses the fixture's CA-verified TLS transport. TLS verification remains
enabled, and no operating-system or user trust store is modified. The response
fault wrapper preserves the real socket's read-deadline support through
`Unwrap`; a plain response recorder was rejected by the real handler with 503
during fixture development, and was corrected without weakening the handler.

The test temporarily builds the existing `cmd/transferqueue` source for the
Linux cloud test host and invokes that executable from its test-owned directory.
It introduces no Linux product, service or deployment target, and does not
install the binary on a NAS. The product objective remains Windows/NAS transfer;
adding Linux download publication support is not a required follow-up.

The test builds and invokes the production `transferqueue` executable. Existing
closed producer metadata arrives on stdin. It uploads **9,800,163 bytes**, drops
the first successfully applied PATCH response, and starts new finite CLI
invocations using the persisted due time. Assertions require:

- Original operation, attempt, delivery ID and upload receipt survive recovery;
  Create/Complete/Commit each execute once.
- The CLI returns the exact content hash/size/reference and the existing PG
  ledger contains the committed outcome and matching ready proof.
- A duplicate producer hook replays its confirmed result. A conflicting hash
  for the same delivery is rejected without replacing the committed proof.
- The source's bytes, file identity, size and modification time remain unchanged.

This new round-trip scenario drops only the first successful PATCH response.
It does not drop Complete/Commit responses or restart the server. Earlier ready
proof tests in the wider PG selection have separate failure scenarios; their
log entries must not be relabeled as this scenario's coverage.

The same credential and task authorize a download of that uploaded reference.
The **production download library** receives a truncated real route response
after more than 8 MiB, saves a fsynced incomplete offset, closes and reopens its
private staging file, and resumes with the exact persisted Range. The library's
full SHA-256 verification and an independent byte comparison both pass. The
NAS original and upload ledger remain intact. A wrong attempt and subsequent
credential revocation reject the `HEAD` authorization check at `offset == size`
after downloading, while preserving the staged bytes and source. This is not
revocation during an active response stream.

CLI metadata-only admissions also verify the 256 MiB byte budget and 8/9 active
job boundary. These are admission checks, not 256 MiB transfers or nine complete
deliveries. No quota was raised. The single synthetic source is below 10 MiB;
initial free space was approximately 3.0 GiB in `/workspace`
and 4.9 GiB in `/tmp`. No disk-filling stress test was run.

## Exact boundary: this is not full bidirectional CLI publication

On Linux, `DiskQueue.configuredBinding` supports upload only and returns
`ErrBinding` for download. The actual CLI accepts download admission, then
`advance` exits 1 with `state=unavailable` **before any download request**.
The test checks that the queued job stays unassociated. It does not install a
fake LocalPublication or report a staged file as delivered.

Thus upload is a production CLI subprocess test; download is the production
library against the same actual HTTP/PG/filesystem stack. Download reopen is
within the Linux test process, not a killed Windows process or physical power
failure. It is not a Windows PC/NAS acceptance, complete Linux CLI publish flow,
throughput benchmark, mass-file endurance test or proof of physical power-loss
durability.

The smallest remaining combination is the existing Windows CLI/NTFS publication
binding against this real POSIX server/PG fixture using the supported component
distribution and host setup. The production source/bundle need not be forked.
PR #63's already-passed native tests are reused as separate evidence; they are
not relabeled as this combined business scenario. No Windows rerun or artifact
download is required by this test-only increment.

## Reproduction and local results

Use a disposable PostgreSQL admin DSN in `TEST_POSTGRES_DSN`. The existing
fixture creates and drops separate synthetic databases and RLS roles. Missing
DSN causes the existing fixture to skip, which is **not acceptance**; require
the named subtest PASS and zero SKIP in the recorded output.

```sh
go test -p 1 -v -race -tags=integration -count=1 -timeout 90s \
  -run '^TestComputerDeliveryPostgresAuthorizationCASAndPreservation$/^real-route-roundtrip$' ./internal/db

go test -p 1 -v -race -tags=integration -count=1 -timeout 90s \
  -run '^Test(ConnectorDisconnectFinalizationIsAtomicAndScoped|ComputerDeliveryPostgres.*)$' ./internal/db

golangci-lint run --build-tags=integration \
  --new-from-rev=dfc7cf5104c382ecc622061ce43c3dcd733b7998 ./internal/db
```

On the Linux cloud workspace, 2026-10-23 UTC, at the implementation above:

- Focused test: PASS, package 13.041 s; new scenario 6.88 s; zero SKIP.
- Existing CI PostgreSQL selection plus new scenario: 25 named PASS, package
  67.524 s; new scenario 7.85 s; zero SKIP. The full selection checked the
  shared fixture and the existing 90-second package budget.
- Integration-tag incremental lint: exit 0, no findings.
- `git diff --check`: passed.

The two original outputs below are published verbatim, without redaction or
reconstruction. Before copying, their complete contents were reviewed and
checked for credential markers, URLs/DSNs, private keys, email addresses,
absolute private paths and UUIDs. They contain test names, source file/line
references, synthetic byte/count results and elapsed times. No secret or
private endpoint was found. The published bytes match the original local files;
there is no distinct redacted-output hash.

These are readable local execution records, not remote CI or independent-run
claims. Hashes only verify that the published records match the recorded files;
they are not execution proof on their own. The full output's 25 named PASS lines
include parent and child tests, not 25 independent end-to-end transfers.

| Output | SHA-256 |
| --- | --- |
| [Focused race test, original 7 lines](evidence/delivery-real-route-roundtrip/focused.txt) | `d5f30eefc2610994e756e452fe91e5bef37c252f57832a3d318d2d0b3d36848c` |
| [Full PG selection, original 60 lines](evidence/delivery-real-route-roundtrip/pg-suite.txt) | `8426d82c1382a22d4c9565f24fb47baa456317c310ac30385e2263e23d845d27` |

No tests were rerun to publish these outputs. GitHub CI remains unexecuted:
the prior single dispatch was rejected with HTTP 422, `Actions has been disabled
for this repository`. It was not retried for this evidence-only update.

No UI behavior changed or UI/runtime installation was performed in this scoped
cloud test task. Human QA is outstanding. The separately blocked legacy Media
issue was not modified or bypassed.
