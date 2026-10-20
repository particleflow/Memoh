# Attachment originals: runtime evidence

Verified on 2026-10-20 using only generated test content and synthetic accounts.
The application used the modified Go Server binary, the source Vite Web UI at
`http://127.0.0.1:38082`, PostgreSQL 18 and a real Docker workspace. A local HTTP
model fixture produced the response shown below; no external model was called.
The Server binary ran in a lightweight test container. This is source-runtime
verification, not verification of a newly published production image.

## Observed behavior

1. Logged in through the browser, attached a generated text file in the composer,
   sent it, and received the local fixture response.
2. Read the attachment through its authenticated Bot/hash URL and compared the
   bytes with the file on disk.
3. Deleted this synthetic Bot's workspace, read the original while the workspace
   was absent, recreated the workspace with a different container ID, and read
   the same original again.
4. Restarted Server, read the original again, reopened the conversation in a
   fresh browser session, and opened the attachment preview.
5. Temporarily made this synthetic Bot's originals directory unavailable. A real
   WebSocket submission returned the generic `internal` error, emitted no
   attachment, and caused zero new model requests. Restored the directory and
   verified the previous original again.

All successful reads contained 1,917 bytes with SHA256
`e268fe4ba82c18bbeccf91618dc0bb3dfd7866293b6b6e8b7cd14c30d51b140c`.
Unauthenticated access returned 401, another account without Bot access returned
403, and looking up this hash under another Bot returned 404. The traversal
request was rejected. Browser verification recorded no JavaScript page errors.

The following screenshots show the actual application, with synthetic data only:

- [Uploaded attachment and local fixture response](originals-reference.png)
- [History attachment preview after workspace recreation and Server restart](originals-after-restart.png)

## Automated checks

Passed:

```sh
go test ./internal/storage/... ./internal/media ./internal/config \
  ./cmd/internal/core ./cmd/channel ./internal/handlers/... \
  ./internal/channel/inbound/... ./internal/rpc/storageruntime \
  ./internal/apperror/... ./internal/server/...
go build ./cmd/agent
cd apps/web
pnpm vitest run src/utils/api-error.test.ts src/composables/api/sse-error.test.ts
```

The Web command passed 123 tests. Storage tests cover atomic publication,
cancellation, hash mismatch/corruption, traversal and cross-Bot symlinks, legacy
copying without removal, and workspace-independent hash reads. Admission tests
cover persistence failure, cancellation and foreign-Bot references. Compose
rendering with synthetic paths confirmed distinct config/data/originals mounts
and consistent Server/Channel policy.

After the directory-sync fix, the upload, workspace deletion/recreation,
Server restart, browser history preview, write-failure and authorization checks
above were repeated with the new payload. Focused tests record the filesystem
sync order and simulate errors while syncing the configured root's ancestors,
the originals root and the Bot directory. They verify that failed parent syncs
reject publication and that retries resync entries left by a failed attempt.

## Limits

- Cancellation and split Channel admission were tested in Go; external channel
  integrations and a real external model were not exercised.
- No real NAS, private deployment data or production configuration was accessed.
- No image was published or promoted for this change. The complete repository
  test suite and existing unrelated Web/typecheck failures were not addressed.
- First-read legacy copying is not a bulk migration. Originals must be backed
  up independently; an older binary may not read originals-only attachments.
- No physical power-loss or storage-device fault injection was performed. Normal
  restart tests and unit-level sync-error simulation do not establish that coverage.
- Agent verification does not establish human QA.
