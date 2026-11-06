# PR79 transfer-budget integration

This integrates the fixed PR77 head
`37b7464fe062714258ddea5208cce2a6f9ea5f5b` into the existing PR79 head
`0a324013f9fe1beec77ac948d0af8f210194ba52`. No new PR, deployment or tag
promotion is part of this checkpoint.

## Exact scope

The automatic merge had no conflicts. All 16 imported files match PR77 exactly:
the same-input producer admission classification, the complementary Windows
test groups, and configurable byte budgets with explicit old-layout migration.
The merged source tree before adding this evidence is
`e19bc0a585e7d7a0e9861481387cfe47be3b3b65`.

The three-file upstream-expression cleanup, existing attachments and memory
changes remain unchanged. Web, SDK, server routes and schemas have no incremental
changes. `internal/agent/runtime`, `internal/contextview` and `packages/runtime`
remain identical to official `bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046`.
The only production importer of `internal/transferclient` is the transferqueue
CLI; the already running server and Vite sources therefore remain current for
the observed Web flow.

## Local execution

Commands ran in the existing cloud checkout, with its existing Go 1.25.7 cache
and task-owned scratch directory. No production data or credentials were used.

| Check | Actual result | Evidence |
| --- | --- | --- |
| `go test -count=1 -json -timeout=5m ./internal/transferclient ./cmd/transferqueue` | exit 0; 305 test/subtest PASS; 0 FAIL, 0 SKIP | [Original JSONL](go-test.jsonl), [stderr](go-test.stderr), [exit](go-test.exit) |
| `go build -o <scratch>/transferqueue ./cmd/transferqueue` | exit 0 | [Exit](build.exit) |
| Built CLI, isolated old default queue -> policy mismatch -> explicit migration -> probe | expected exits 1, 0, 0; original locator budget preserved, version gate 4 | [CLI proof](cli-smoke.json) |
| Current real Web/API/PG, existing supervisor -> specialist -> child detail | exit 0; 34 GET requests, no page errors or business dispatch | [Browser proof](browser-proof.json), [output](browser.log), [screenshot](browser-detail.png) |
| Source boundary, imported-file equality and `git diff --check` | exit 0 | Exact fixed sources above |

The CLI smoke harness initially assumed that a rejected `probe` returned JSON.
That existing command actually emits fixed stderr and exit 1. The harness was
corrected to retain both streams and continued with the same synthetic queue;
no product change or business retry was needed. This failure is recorded in the
CLI proof. No other local test failed.

The browser reused the existing local API at `http://127.0.0.1:18100` and Web at
`http://127.0.0.1:18102`, with real PostgreSQL and earlier synthetic-model task
records. Its observer is the previously committed
`../upstream-style-cleanup/browser-detail.mjs`, with only the scratch output
directory changed. The screenshot was inspected. This is an observation smoke
check, not another model run, new transfer, human QA or real-device acceptance.
The [manifest](manifest.json) records byte-for-byte selected outputs. Private
config, token, binaries and synthetic queue state are excluded. Exact known
private-value checks found no match; this is not universal secret detection.

## Client compatibility and remaining gates

- Plugin manifest is now 0.8.0, minimum Herdr 0.9.5. Missing `queue-budget.json`
  preserves the 256 MiB default. Invalid policy does not fall back.
- A larger existing-queue budget requires stopping all old writers, retaining a
  complete backup, finishing history/publication migrations with the old budget,
  then explicitly running `migrate-budget`. Queue v4/publication v3 reject old
  writers; simply restoring the old policy/binary is not a supported rollback.
  Original IDs, `transfer.json`, objects and receipts remain in place.
- `pending` / `transient` after partial sealing means continue the same
  enqueue/collect identity and metadata. It does not authorize a new task or
  an upload of an unsealed snapshot. The endpoint integration must understand
  that finite handoff; this adds no automatic worker.
- The 8 GiB policy smoke transfers **zero payload bytes**. Tests of larger
  policy values are not multi-GiB throughput tests. NAS `max_upload_bytes`,
  queue reservations, publication accounting and actual free space are separate.
- Linux tests do not execute Windows NTFS branches. The integrated-head native
  workflow, including Windows PostgreSQL and packaged-executable verification,
  must supply that evidence separately; source-PR CI is not integration CI.
- No full Web rerun was needed for unchanged Web code. Existing real PC/mobile,
  TLS/OAuth and legacy-media P1 release holds remain; no production migration,
  main merge, deployment or image publication was performed.
