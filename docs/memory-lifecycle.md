# Native memory deletion and recovery

The native graph runtime uses PostgreSQL nodes and edges as its authority.
Markdown under `/data/memory` and the optional semantic index are derived views.
This contract applies to the native graph runtime, not to arbitrary file contents,
chat history, backups, or the legacy file runtime.

## Operations

- **Delete / batch delete / delete all:** commit removal from PostgreSQL, invalidate
  the local graph cache and discard queued embedding retries, then clean derived
  Markdown and semantic entries. Known cleanup errors are returned to the caller;
  the database deletion may already have committed. Retry deletion or run rebuild
  after the failing dependency recovers. A failed batch may have deleted a prefix.
- **Rebuild:** regenerate projections and the semantic index from current database
  nodes. It does not import Markdown. A stale projection left by failed cleanup
  must not restore a deleted node, including after a new runtime or database restart.
- **Explicit ingest:** `POST /bots/:bot_id/memory/ingest` imports eligible Markdown
  into the database. It can restore content whose indexed node was deleted while
  its original document was retained. Re-importing unchanged content with the same
  effective ID is idempotent; this is not a universal content deduplication promise.
- **Original documents:** deleting indexed memories does not recursively delete
  the memory directory. Rebuild preserves documents that the existing projection
  classifier does not recognize as database projections. It preserves an unreadable
  file and returns an error rather than guessing ownership or reporting completion.
- **Unavailable authority:** search/list must not fall back to unverified Markdown
  after a database graph failure. This trades fallback availability for avoiding
  recall from a stale deleted projection. Existing in-process graph caching remains;
  this change does not add distributed cache consistency.

A projection is currently recognized by parsed entry IDs all beginning with the
Bot ID plus `:`. A caller-authored file that uses that reserved namespace is not
reliably distinguishable from a projection. No stronger file-ownership scheme is
introduced here.

## Boundaries requiring a separate product decision

Deleting an indexed node is not permanent erasure of all sources. Explicit ingest,
restoring an old database backup, or future extraction from retained chat history
can recreate a fact. Whether those operations must obey durable deletion markers,
and how a user explicitly overrides such markers during restoration, needs a
separate retention and recovery contract. This patch does not delete original
user documents, add tombstones, change Bot sharing, or change the Agent core.

A successful cleanup only covers the operations performed by this request. It is
not a transaction across PostgreSQL, files and embeddings, nor a guarantee against
concurrent writes, other instances' cached graphs, or old backups.

## Regression coverage

`internal/memory/adapters/builtin/lifecycle_test.go` covers returned errors, cache
invalidation, retries, stale projection recovery, and refusal of file fallback.
`lifecycle_postgres_integration_test.go` repeats healthy deletion, injected sync
failure, unreadable projection, preserved originals and explicit repeated ingest
against real PostgreSQL and a real filesystem through a local gRPC bridge.

Set `TEST_POSTGRES_DSN` to a disposable database initialized with the current
schema, then run:

```sh
go test -race -count=1 ./internal/memory/adapters/builtin -run 'Lifecycle|Projection|AuthorityIsUnavailable|DeleteAllPreservesOriginals'
```

Initialize the database using the server's **full migration chain**, not by
loading `0001_init.up.sql` alone. Migration 0033 removes `bots.type`; fixtures
must match that migrated schema and must not insert the obsolete column. The
existing `wikistore` fixtures already match it and need no correction or overlay.

The `Session Runtime (PostgreSQL fencing)` Go CI job bootstraps the official
schema, then runs `go test -json -race -count=1 -timeout 90s ./internal/memory/...`
against the same disposable database. Its memory step explicitly rejects a
missing `TEST_POSTGRES_DSN` and checks PASS events for all four lifecycle and
both wikistore PG tests. Missing or skipped cases fail the step. Running the
ordinary unit-test job without a database still skips the optional PG tests.

No model credentials are required. These are deterministic lifecycle tests, not
measurements of natural-language forgetting or embedding relevance.
