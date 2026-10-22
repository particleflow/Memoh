# Windows delivery adapter: isolated validation

For the 2026-10-22 merged-server and persistent-queue increment, see
[queue integration evidence and remaining host bindings](queue-integration.md).
The results below describe the original `92b090bb` library checkpoint.

Date: 2026-10-21. Branch: `feat/windows-delivery-contract`.
Base: `a819c75ed94ae06a3a8196e9416302b18d8dd34f`.
Design checkpoint: `6009e761` (before implementation).
Server contract tested: PR #8 `2bf79bbb8a2be4f1f08eefcadde850b2361e29c4`.
The parent subsequently reported integrated PR #8 candidate
`a6149e63bf0790745058ce6b3a2280df0c5fcb41` with unchanged contract; it was not
deployed or modified here. The actual compatibility run below uses the frozen
SHA, not that newer candidate.

## Scope and outcomes

The added Go package is not imported by any application entrypoint. It is an
unconnected adapter with host-owned `Save`, `Snapshot`, authorization and opened
staging-handle interfaces. There is no production Windows worker, durable store,
task-result endpoint, new root configuration, service or credential issuance.
See [the frozen design](../../design/windows-file-delivery.md).

| Check | Observed result |
| --- | --- |
| `go test -race -count=1 -v ./internal/transferclient` | 11 top-level tests and 25 subtests passed; final run 1.557s, including race overhead. |
| Frozen PR #8 real-store test, `-race -tags pr8contract -run TestFrozenPR8` | Passed against actual `media.UploadStore`, `media.Service` and localfs originals through isolated HTTPS routing; reconnect after lost PATCH, close/reopen the store, continue from durable offset, lose complete response, recover ready, repeat complete, Range-download and compare all bytes. |
| `go vet ./internal/transferclient` | Passed. |
| Repository-pinned `golangci-lint 2.10.1 run ./internal/transferclient` | Passed with no findings after local fixes. |
| `golangci-lint fmt --diff ./internal/transferclient` | No formatting difference. |
| `GOOS=windows GOARCH=amd64 go test -c ... ./internal/transferclient` | Cross-compiled successfully; not executed on Windows. |
| `bash scripts/check-agent-core-boundary.sh` | Runtime and contextview match official `bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046`. |
| Diff against task base | No changes under all of `internal/agent`, `internal/contextview`, `conf` or `docker`; no change to PR #8 files/protocol. |

The TLS fixture uses a synthetic header and httptest's ephemeral certificate
trust only. No private production endpoints or credentials were loaded. Tests
do not run Memoh runtime or connect to a real PC/NAS. A temporary Go 1.25.7
compiler and golangci-lint were downloaded with release checksum verification;
Go caches are under `/tmp` because the default home cache was read-only.

## Fault coverage and limits

The fixtures exercise binary chunk headers/checksums, lost create/PATCH/complete/
DELETE replies, save failure before create and after PATCH, stale local offset,
source mismatch, Retry-After/backoff/retry budget, 403, 404, 400, 413, unexpected
receipt fields, pre-admission cancel and cancel-after-publication. They reject
HTTP, insecure TLS, untrusted certificates, redirects, path-segment injection,
oversized JSON and private response diagnostics. Downloads test unacknowledged
tail replacement, corrupted prefixes, wrong ETag/Content-Range, 200 on resume,
truncation, simulated ENOSPC and checkpoint failure. A full local download
checkpoint still performs an authorized HEAD before verification/publication.

The test store serializes JSON between steps to exercise checkpoint ordering;
it is deliberately not a production Windows durability/locking implementation.
Source immutability is simulated. Store restart is a real close/reopen of PR #8
on cloud-local disk, not a server-process kill, physical power failure or NAS
mount certification. The TLS route wraps real storage methods but does not run
the actual JWT/handler authorization stack. No new claim of task-scoped server
authorization follows from these tests.

Initial local iterations found ordinary lint/style issues and tightened TLS
hook validation. One integration invocation accidentally ran its build tag in
the base checkout (which lacks PR #8), so compilation failed with missing upload
symbols; rerunning in the pinned PR #8 worktree passed. A brief executor
transport disconnect occurred before a patch read; file inspection confirmed
no mutation, and the patch was applied once after recovery. No approval was
bypassed or denied, and the environment was not recreated.

## Bounded-memory measurement

Command:

```sh
go test -run '^$' -bench BenchmarkVerify -benchtime=1x -count=3 -benchmem ./internal/transferclient
```

Synthetic zero-stream hashing, Linux amd64, Intel Xeon Platinum 8573C. Each
1 MiB and 512 MiB iteration allocated **262,424 bytes, 8 allocations**. The
512 MiB runs took 0.464–0.710s; the 1 MiB cold run varied substantially. This is
only client streaming-hash allocation/CPU evidence, not end-to-end RSS, network,
disk, snapshot-copy or NAS throughput. Full transfer acceptance must separate
NIC/path, local disk, NAS durability and protocol costs as the design specifies.
No line-rate promise is made.

## Reproduction

Use the repository's Go 1.25.7 and golangci-lint 2.10.1 toolchains. For a
restricted home directory, set `GOCACHE`, `GOPATH` and `GOLANGCI_LINT_CACHE` to
writable temporary directories. All normal package tests run on the base branch.
The tagged compatibility test is intentionally excluded until PR #8 is present:

```sh
git worktree add --detach /tmp/memoh-pr8-contract 2bf79bbb8a2be4f1f08eefcadde850b2361e29c4
cp -a internal/transferclient /tmp/memoh-pr8-contract/internal/
cd /tmp/memoh-pr8-contract
go test -race -tags pr8contract -run TestFrozenPR8 -count=1 -v ./internal/transferclient
```

Use an unused temporary worktree directory if this path already exists. No
change is made to the PR #8 branch or server sources. Synthetic test byte arrays
and local fixture handles are not a second storage configuration/registry.

## Remaining blockers; draft only

1. **Production security: pending, not attempted.** No proven reusable encrypted
   route and scoped Memoh PC identity. The user-described IPv6 HTTP route is not
   encrypted. Do not connect or mint credentials before the concrete approved
   TLS/private-tunnel and revocable task-bound capability plan is authorized.
2. **Windows host integration: pending.** Need the actual existing host/store/
   locator interface, exclusive job lock, immutable handle/ACL implementation,
   disk reservation and queue/outbox admission. The library alone supplies none
   of these production dependencies; no daemon or parallel registry was added.
3. **Generated-result registration: pending parent coordination.** Reuse media
   hashes plus the existing computer operation receipt/revision boundary; add
   scoped, idempotent result association without touching Agent core. Current
   terminal artifact capture is not arbitrary-result registration.
4. **Storage audit: pending separate changes.** Relative originals root handling,
   legacy fallback, system-temp media spools and duplicated Pi attempt paths are
   recorded in the design; no cross-module/production directory changes here.
5. **Required application verification: incomplete.** `AGENTS.md` mandates the
   development environment, real UI and screenshots. The task explicitly says
   not to install/integrate Memoh runtime; `mise` is also absent. No waiver was
   inferred and no runtime was installed. There is no running-app URL, UI
   interaction or screenshot evidence for this branch. Isolated tests are not a
   substitute. Keep this work draft, not ready or production validated.
6. **Real acceptance: pending.** Windows execution, real scoped grants/revocation,
   true process crash/physical power loss, NAS NFS/SMB lock/fsync, sustained
   concurrent throughput/backpressure and end-to-end result association remain
   unverified. No merge, release or production deployment occurred.
