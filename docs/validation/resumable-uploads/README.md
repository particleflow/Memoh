# Resumable uploads: cloud validation

Date: 2026-10-21. Base: `e6f4999dd294a09b4171fdaae0176714405971d5`
(merged PR #6). The implementation and contract are in
[resumable-uploads.md](../../resumable-uploads.md).

## Environment and boundaries

Isolated Linux cloud workspace, Go 1.25.7, PostgreSQL 18 and pgvector 18,
current-source native Server with embedded Channel, Vite Web, system Chromium
through Playwright. The existing prepared cloud startup workflow was used;
PostgreSQL/pgvector health checks, migrations, Server readiness, authentication,
and the Vite API proxy passed. Database schema remains 170, no new migration.
Only synthetic data was used, with an isolated originals root and an explicit
512 MiB per-file policy. No NAS, Windows host, private client project, real
provider credential or model was used. Production limits were not changed.

Server: `http://127.0.0.1:18080`; Web:
`http://127.0.0.1:18082/bot/upload-validation-synthetic` (local validation URLs,
not published previews). A synthetic Bot was created through the real API.
The isolated development user's onboarding flag was set through the existing
profile API after that Bot existed, to reach the attachment UI without adding
an AI provider. Its optional workspace image was unavailable; no workspace or
model execution is claimed. Originals and resumable transfer work independently
of that workspace.

## Real HTTP results

The running Server was rebuilt from this branch before the final measurements.
A Python HTTP client generated zeros in 256 KiB buffers, sent 8 MiB chunks with
length/offset/SHA headers, then streamed the original back and recomputed SHA.
These are server HTTP tests, not client-only large-file fixtures.

| Synthetic file | Final measured elapsed | Server peak RSS | Result |
| --- | ---: | ---: | --- |
| 200 MiB / 209,715,200 bytes | 3.120 s | 88,772 KiB | upload, completion, repeat completion, full readback SHA passed |
| 512 MiB / 536,870,912 bytes | 12.988 s | 88,556 KiB | same, plus actual Server restart after 16 MiB acknowledged; resumed exact offset |

The 512 MiB time includes the owned Server/Web restart helper, its health checks,
and final readback. These cloud-local timings are not NAS performance claims.
Peak RSS is the whole Server process, including unrelated application services;
the restart creates a new process between measurements.

SHA-256:

- 200 MiB: `72abf2ca8f36943ebe2e49ca3a51d409ca5f0bfcffab6c9d25643c17c32889da`
- 512 MiB: `9acca8e8c22201155389f65abbf6bc9723edc7384ead80503839f49dcc56d767`

Also verified against the real server: disabled unauthenticated access (401),
incomplete completion (409), `content_hash` absent before ready, ready receipt
identity across repeated completion, and cancellation of ready upload rejected
(409). A generated SDK Blob test sends non-UTF8 bytes and checks exact readback;
see the captured result in [sdk-binary.txt](sdk-binary.txt).

## Automated integrity, fault and resource checks

Passed targeted Go tests cover:

- Invalid/truncated/overlong chunks and chunk SHA mismatch never advance offset.
- Final SHA mismatch never publishes a ready reference.
- Storage returns an error after writing an original: retry completes through
  existing hash deduplication without deleting the original.
- Journal checkpoint I/O failure retains the old acknowledged offset and resumes
  correctly after reopening the service.
- A real child process is killed midway through its second chunk; a new service
  recovers the first acknowledged chunk and repairs the interrupted tail.
- Two independent store handles concurrently append at the same offset; exactly
  one succeeds. Concurrent complete/cancel has one consistent terminal state.
- Different authenticated identity or Bot cannot query, append, complete or
  cancel another receipt. HTTP auth and Bot permissions gate every operation.
- Path traversal, symlinked staging roots, payload symlinks and hardlinks are
  rejected without modifying the outside fixture.
- Repeated cancellation, expired pending cleanup, orphan journal cleanup,
  staging removal after completion, and preservation of shared originals.
- Per-identity/global reserved byte limits, oversize files/chunks and old upload
  endpoint compatibility. Existing 200 MiB default admission tests remain green.

The standalone 512 MiB test binary processed 64 chunks with final readback SHA
verification. Final measured peak RSS: **10,044 KiB** (Python `getrusage` for the
child process); sampled Go heap peak **3,897,072 bytes**; internal elapsed
**5.820 s**, test duration **6.08 s**. The heap metric is a sample after each chunk,
not a continuously sampled heap maximum. RSS covers the whole test process.
The implementation does not hold the complete file in memory. Temporary disk
can hold staging plus an original during publication; this is bounded-memory,
not zero-copy or zero-extra-disk transfer.

Commands run:

```sh
go test -p 3 ./internal/media ./internal/handlers ./cmd/internal/core ./internal/apperror ./internal/server
go test -race -p 3 ./internal/media ./internal/handlers ./internal/storage/providers/localfs ./internal/storage/providers/fallback
go test -race -p 3 ./internal/media ./internal/handlers -run 'TestUploads|TestResumable' -count=1
go test -race -p 3 ./internal/media -run TestUploads -count=1
go vet ./internal/media ./internal/handlers ./cmd/internal/core
go build -p 3 ./cmd/agent
go generate ./internal/handlers
pnpm run generate-sdk
pnpm --filter @memohai/sdk typecheck
pnpm exec tsc --ignoreConfig --noEmit --moduleResolution bundler --module esnext --target es2022 --skipLibCheck openapi-ts.config.ts
NODE_OPTIONS=--no-experimental-webstorage pnpm exec vitest run --project web apps/web/src/utils/api-error.test.ts apps/web/src/composables/api/sse-error.test.ts --maxWorkers=3
golangci-lint run --new-from-rev=origin/main ./internal/media/... ./internal/handlers/... ./cmd/internal/core/...
```

The selected frontend suites passed **123 tests**. SDK code generation includes
one narrow parser normalization for swag's binary body format location; the
chunk helper accepts `Blob | File` and disables JSON serialization.

One intermediate full Go run failed the `sends_messages` and `answers_pings`
subtests of `TestChatWSKeepsAClientThatShowsSignsOfLife` while concurrent
compilation was active. Targeted rerun, the subsequent full package run, and the
full race run passed. This observation is retained rather than omitted. Initial
static-check findings were fixed; the final check result is recorded alongside
the other compact logs. First browser attempts used a missing Playwright-bundled
browser and a network-idle wait during restarts; system Chromium and readiness
of actual UI elements resolved those attempts.

## Actual UI evidence

Real UI login succeeded. On the synthetic Bot's chat page, the attachment picker
opened and selecting a 1,200,000-byte synthetic text file displayed its pending
attachment preview with 24,001 lines. Both screenshots were captured from the
current running app and visually inspected.

- [attachment-picker.png](attachment-picker.png): existing attachment menu.
- [attachment-selected.png](attachment-selected.png): selected pending file.

The current Web uploads on **Send**, not on file selection. No model was
configured, so Send/model execution was not exercised. These screenshots prove
the relevant UI selection flow, not completed upload or resumable client UI.
The new server protocol and independent byte transfer are verified separately
above; client resumable integration is explicitly outside this PR.

## Core guard and remaining limits

`git diff --exit-code bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046 --
internal/agent/runtime internal/agent/context
internal/agent/application/resolver_runtime_contextview.go
internal/agent/application/resolver_runtime_contextview_test.go` passed with zero
diff. This branch also changes no file anywhere under `internal/agent` relative
to its main base. No existing Media was cleaned, moved or indexed.

No power-loss, NAS NFS/SMB lock/fsync certification, client background process
recovery, generated-result registration, full chat/model workflow or human QA
is claimed. The known optional workspace image gap does not block the file API
checks. The PR remains a draft; production rollout requires review and the
separate authorized deployment validation.
