# Unified route, SDK and upload-registration checkpoint

Product merge: `ee0394f1e7f0f6149968fd3755af7e2b0531e86a`.
Product tree: `e6938b993c1d7fa368a9d57fabbdc51e2812fc4c`.
This evidence commit adds no product changes. All work remains on Draft PR79;
no main merge, image publication, deployment or production configuration change.

## Fixed inputs and merge decisions

1. `f9a90e5018a8630dc591b1adb012da95c18be037`: trusted delegation return route,
   configurable execution timeout and its evidence. The product is `e37be309`.
   [Route/deadline contract and real-application limits](../delegation-route-config/README.md).
2. `d4a87edfd55cc94624fbb5e2749047dc277af361`: three existing lint diagnostics:
   replace format-free `fmt.Errorf` with `errors.New`, and format two composite
   literals. No rule/config change or new suppression. The original run
   [40094759848](https://github.com/particleflow/Memoh/actions/runs/40094759848)
   ended with six jobs successful and Lint failed; its failure is retained in
   `route-ci-before-fix.json`. Docker PR, ESLint and native delivery fixtures for
   that earlier head passed. Those are not checks of this combined head.
3. Merge `ad0904b2d5fd4ee4ff03880a3bf8edc0e2d85281` (PR76, product `6f9ac071`)
   at `b12458f808dad0f2902a41bfe45bad57e0dc4cd5`. Common ancestor `7bcbba5a`;
   no conflicts. The existing pinned Twilight SDK owns Images JSON encoding and
   parsing. The previous outbound-request lint annotation disappears with the
   removed local encoder; no workaround or replacement suppression is added.
4. Merge `c29253069038886c95a29807ad2a003a214c7593` (PR77) at the product head
   above. It contributes `b4564e7f` and `c2925306` above the already integrated
   `37b7464f`; no conflicts. No moving branch head was selected.

No workflow, SDK dependency, migration, Agent runtime or contextview changes
were made by this integration. The official core boundary script passes against
`bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046`.

## Executed integration checks

Commands used Go 1.25.7, the existing cloud checkout and task scratch caches.
Selected JSONL outputs are byte-for-byte copies, with lengths/hashes in
`manifest.json`. Exact known private fixture token/password scanning found no
match; this is not a universal secret-filter guarantee. The CI metadata file is
an explicit reduced projection, not a raw job-log download.

| Command | Exit / observed result |
| --- | --- |
| `go test -p=2 -count=1 -json ./internal/config ./cmd/internal/core -run 'Test(LoadBotDelegationExecutionTimeout\|Delegation)'` | 0; 42 passing nodes; `lint-fix-tests.jsonl` |
| `go test -race -p=2 -count=1 -timeout=180s -json ./internal/imagecapability ./cmd/internal/core` | 0; 176 passing nodes; `images-combined.jsonl` |
| `go test -race -p=2 -count=1 -timeout=180s -skip '^TestRegistrationTenThousand' -json ./internal/transferclient ./internal/deliverymeta ./cmd/transferqueue` | 0; 332 passing nodes; `transfer-combined.jsonl` |
| `go test -p=2 -tags=integration -count=1 -timeout=5m -json -run '^TestComputerDeliveryPostgresAuthorizationCASAndPreservation$/(batch-process-recovery\|ready-proof-cli\|producer-multiple-files)$' ./internal/db` | 0; seven passing nodes, 19.507s; `transfer-pg-combined.jsonl` |
| `go build -p=2 -o <task-scratch>/server-combined ./cmd/agent` | recorded in `build-vet.txt` |
| `go vet -p=2 ./internal/imagecapability ./internal/botdelegation ./internal/config ./cmd/internal/core ./internal/transferclient ./internal/deliverymeta ./cmd/transferqueue` | recorded in `build-vet.txt` |
| `bash scripts/check-agent-core-boundary.sh`; `git diff --check` | 0 |

The counts include subtests and overlap across commands. Each listed output has
zero FAIL/SKIP events; the explicit scale-test exclusion is not a scale run.
Existing isolated PostgreSQL on loopback supplied the real server/store/CLI
fixtures, which use their own temporary database and synthetic files. No real
PC, paid model, production database or user originals were used.

The SDK tests cover real native spawn with synthetic model/query/storage ports,
configured model and original reference bytes, authorization, redirects, response
limits, cancellation and unknown without automatic re-POST. The PG tests cover
metadata registration, real API upload/download, interrupted CLI recovery and
hash/ready proof. These are not real-provider or Windows/NAS throughput claims.

## Compatibility and remaining acceptance

- Images retains the native Bot model binding and existing provider configuration.
  Old configured-model generation and explicit JSON reference policies remain
  accepted. Omitted-model and multipart policies fail before dispatch with an
  explicit migration error; configuration is not silently rewritten. No actual
  Magpie compatibility or production model binding is claimed here.
- New uploads use v5 registered metadata; legacy v2/v3/v4 queues remain readable.
  Registration neither reserves cumulative source bytes nor copies source bodies.
  Old executables cannot read v5, so package rollback over newly written queues
  is not safe. Retain source files, queue evidence and the current reader. The
  independent remaining producer/ledger/publication bounds are unchanged. The
  source branch's sparse 10,000-file benchmark was not repeated and is not 10 TiB
  actual transfer.
- Delegation snapshots only trusted host routes, revalidates them before delivery
  and queued admission, and keeps legacy local records readable/deliverable.
  Old external records lack a provable original recipient and cannot be guessed.
  `execution_timeout` defaults to 30m; explicit 0s removes only that outer deadline.
  Timeout remains unknown and does not rerun the specialist.
- The earlier real Web/API/PG delegation observation failed before a model call
  because the existing fixture's workspace bind paths/bridge socket were missing.
  Its screenshot and failed outcome remain in the route evidence; no replacement
  success or extra screenshot project was created for this source-only integration.
  Successful full specialist execution and real original-channel delivery remain
  unverified. Acceptance/delivery admission is not business completion.
- Native cumulative followup storage is a separate documented proposal, not
  implemented here. Legacy-media P1 and all existing production release holds
  remain; no previously denied repair or production action was retried.

The final combined head must be assessed by its own PR checks. Individual source
branch green checks and the tests above do not imply deployment or human QA.
