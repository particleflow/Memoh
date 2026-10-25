# Native registry and pinned file producer combination

This is an isolated Linux protocol integration test, not a production deployment
or Windows native host acceptance. No product, Agent, credential purpose, queue,
protocol, configuration root or Windows endpoint code changes are included.

## Original verification on 2030c2d5

### Fixed sources

- Server: `2030c2d522695f52a227f12421800fe075760813` (PR84, native migration 0176).
- CLI source checkout: `c50342868f99cfb5f78f1f7724d0272bf43906c7` (PR77).
  Its production CLI, transferclient and deliverymeta blobs are unchanged from
  `0894a6eb8ff69c8796e2da5450fa172d4cd5ef81`, component 0.10.1.
- The server source does not itself contain the 0.10.1 inbound-feedback fix.
  The test builds the CLI from the separately pinned checkout explicitly; no
  producer product file is overlaid onto the server tree.

## Executed evidence

The existing full-migration PostgreSQL fixture creates and drops its own database.
The request path uses the real Server routes, registry verifier, NOBYPASSRLS role,
PostgreSQL delivery transactions and filesystem Media storage. Only registration
records, credentials, source files and the loopback TLS CA are synthetic. The CLI
trusts this CA in its process via SSL_CERT_FILE; no system trust store is changed.

[Raw successful output](evidence/native-transfer-producer/accepted.log): one test
PASS, zero FAIL/SKIP, 10.364s including race instrumentation (test body 9.29s).
This is not a throughput measurement.

- 9,800,163 actual upload bytes, successful PATCH response deliberately lost;
  recovery keeps the same operation/attempt/delivery/receipt. Create, Complete and
  Commit each occur once. Original file contents and identity are retained.
- Two fresh CLI inspect processes return the same committed reference and
  `feedback_status:recorded`, preserve the PG proof and perform zero HTTP requests.
  Repeated enqueue returns the original association; conflicting content fails.
- Valid native and legacy Agenvo mft credentials are enrolled for the same owner
  and Bot. Each works for its own operation; both cross-provider combinations are
  denied with 403. A Runtime mrk-shaped token is rejected with 401.
- The same real native route downloads the uploaded original through the
  production transport library, resumes a dropped response at byte 8,394,902
  using Range, and verifies the entire file hash. Wrong attempt and revoked mft
  cannot read the completed reference.
- The exact configured config/state pair is reused by every CLI process. There
  is no runtime/task replay endpoint, HostFS path or additional queue implementation.

The initial fixture incorrectly copied native-only selector fields into its
legacy sample. The production decoder rejected enrollment as designed; the test
now clears those fields. [Original failure output](evidence/native-transfer-producer/first-fixture-failure.log)
is retained. No server fix was needed.

## Reproduction

Use the server checkout plus this test-only delta, the fixed producer checkout
above, and the existing isolated PostgreSQL integration prerequisites. Do not
point TEST_POSTGRES_DSN at production: the fixture creates a synthetic database.

```sh
export MEMOH_NATIVE_PRODUCER_SOURCE=/absolute/path/to/pinned-producer-checkout
# TEST_POSTGRES_DSN is the existing isolated test database connection.
go test -v -race -tags=integration -count=1 -timeout=3m \
  -run '^TestNativeTransferPinnedProducerCombination$' ./internal/db
```

The test requires both explicit environment entries. It does not discover user
configuration, request credentials, download binaries or install a service.
Peak payload size remains the existing small round-trip fixture, not a repeat of
the 1 GiB run. Available /tmp capacity was about 4.1 GiB before setup and 4.0 GiB
after cleanup. No user data or shared caches were deleted.

## Confirmed missing native completion hook

The round-trip test deliberately invokes the existing stdin `enqueue-result`
entry point. It does **not** prove automatic collection on native task exit.
A separate execution of the production-generated Bash completion wrapper on
`2030c2d5`, with synthetic Pi and a disabled legacy Herdr plugin, reproduced the
missing connection: the result manifest closes and the business receipt returns,
Pi runs once, but the only hook calls the disabled plugin; the producer collector
is never invoked. This is an executed synthetic hook reproduction, not a Windows
host run. No product correction is included here.

[Raw red output](evidence/native-transfer-producer/completion-hook-red.log) and
[the exact red test source](evidence/native-transfer-producer/native_completion_hook_test.go.txt)
are retained separately, rather than weakening the assertion or inserting a
known-failing native-source test into unrelated producer CI. Reproduce by copying
the source to `internal/agenvo/native_completion_hook_test.go` on `2030c2d5` and
running `go test -v -count=1 -timeout=1m -run
'^TestNativeCompletionRegistersWithoutLegacyPlugin$' ./internal/agenvo`.

On the original 2030c2d5 source, the native source owner still needed to fix the two existing collection hooks and provide
the fixed executable/config/state binding. The follow-up below now verifies the fixed generator. A combined test must start
from a newly produced result through that actual entry point. A pre-enqueued job
plus `advance-results` is insufficient to close this requirement.

## Boundaries and next dependency

Linux has no NativePublication binding. The real CLI correctly fails publication
before issuing an inbound GET; its queued identity remains intact. Therefore the
successful library download is **not** an inbound CLI/NTFS committed-publication
claim. The prior 1 GiB NTFS/default-CLI proof is retained separately and does not
prove this newly combined native registry path on Windows.

A Windows combination needs the existing 0.10.1 test executable and NTFS fixture
against this native registry setup. It must retain fixture-only RootCAs, full TLS
hostname verification and the original environment directory pair. The actual
native host selection/single-instance/legacy-hook gate also belongs to the host
integration source; this test neither implements nor claims those gates.

Only the server verifier combination is newly established. No real native
Runtime connection, Herdr execution, phone client, LAN/public IPv6 switch,
physical power-loss, system-trusted Windows EXE or production PC/NAS acceptance
was performed. No CI run is claimed for this standalone evidence branch.

## Evidence hashes

- `accepted.log`: SHA-256 `930e6e9be9b411e396784e6bee8ff9822cf7d78751841add8d132ecbd46a729b`
- `completion-hook-red.log`: SHA-256 `03449c4350d295851a05b8e92c74cbf5d8bd73a4cc168266c538b1ff4e9c3ba8`
- `first-fixture-failure.log`: SHA-256 `e94ec65f7a5c9f18ba3d036ee24f28cde050d5439b9a787bb2eec2ba9082103d`
- `native_completion_hook_test.go.txt`: SHA-256 `ee542f9798f0b8292471e1a14c72739018570cf80e96c398f2f582ee712fd3db`

## Follow-up: generated native completion and real NAS commit

Server/generator source is now
`03e4fb6220a4b9f13f2b642e8dd3acaca7545a48`; the CLI remains separately pinned to
`c50342868f99cfb5f78f1f7724d0272bf43906c7` / unchanged 0.10.1 product blobs.
No producer or server product code was changed by this verification delta.
The native source's fixed collector call is exercised directly.

`TestNativeGeneratedCompletionRealServerAndPinnedProducer` reuses the existing
real Server/PG fixture. A test-binary entry point calls the actual private
`batchScript` and `nativeProducerCollectCommand` functions, so the integration
test does not recreate their command-generation logic. The real generated Bash
script runs a synthetic Pi that writes 45 source bytes. The production completion
closure writes the context and closed manifest; its native hook invokes the
existing production `collect-result`. The test neither writes that new manifest
nor manually admits/enqueues the new result. The unrelated retained legacy queue
is deliberately seeded before execution solely to check isolation.

The Linux shim checks the exact registered Windows program/config/state strings,
fixed `-action collect-result` arguments and then maps only those locations to
test-owned Linux paths. Ambient config/state point to the retired legacy pair;
the generated command must override them. To keep local path hashing coherent,
the generator fixture uses its Linux source path for WorkDir/RootHash, as the
native source's existing Linux test does. This does not claim Windows path
resolution, NTFS validation, or a real Runtime transport invocation.

The real HTTP wrapper waits until PostgreSQL says committed, then holds the
acknowledgement. The test kills only the actual producer child process; it does
not kill Pi, the NAS server or a user process. The queue is explicitly checked as
`associated:false`, `association_state:in_flight`. Three subsequent freshly
spawned generated wakes resolve that original job. Consumed manifests are not
recreated. A later idle wake is followed by a durable inspect; idle alone is
never treated as proof of delivery.

Verified final assertions:

- Pi executes once; duplicate original launch is rejected by the existing attempt.
- Create=1, Commit=1, Resolve=1; original operation/run/target fingerprint stays
  completed, and the synthetic Bot still has exactly one business operation.
- All original PG entry bytes, including receipt/reference proof, stay identical
  after recovery. NAS bytes/full hash match the generated source.
- Original source and selected config remain intact; every old configuration,
  queue and source byte stays unchanged; legacy Herdr hook calls=0.
- The first generated collector plus three generated recovery wakes are counted;
  no second queue, admission implementation, HostFS delivery or task ID is added.

[Final raw output](evidence/native-transfer-producer/generated-completion-accepted.log)
has one PASS, zero FAIL/SKIP, 9.346s including race instrumentation (body 8.27s).
The [first test expectation failure](evidence/native-transfer-producer/generated-completion-first-expectation.log)
is retained: it expected each no-manifest wake to return associated status instead
of allowing idle and proving the original association through inspect. Only the
test assertion was corrected; no product change or manifest replay was used.

```sh
# Same isolated PostgreSQL and fixed producer source as above.
go test -v -race -tags=integration -count=1 -timeout=3m \
  -run '^TestNativeGeneratedCompletionRealServerAndPinnedProducer$' ./internal/db
```

The earlier red completion-hook seam is closed in this Linux combined sample.
The prior download protocol and 1 GiB Windows byte evidence are reused, not rerun
or relabeled. Real Windows native-host/Runtime/NTFS combination, system-trusted
EXE TLS and actual PC/NAS acceptance remain outside this run. This branch records
local integration/lint evidence; it has no independent CI or deployment claim.

- `generated-completion-accepted.log` SHA-256: `fd920278627b95499801fe65dadc6ad8bf72a33a125cc2a30946b444966ebeb5`
- `generated-completion-first-expectation.log` SHA-256: `adb875266a655d12608aa6ffd4f354568ab74f12ed2939cedb4a0c634819c4a1`

The unchanged-mode branch of the reused fixture also passed its targeted race regression: [raw output](evidence/native-transfer-producer/original-helper-regression.log), one PASS, zero FAIL/SKIP, 6.793s. SHA-256 `bc8dee7462a198d3c21c480a0de14f8a66b07ed0affb23af1129b3c43d3c3013`. Incremental integration-tag lint for `internal/db` and `internal/agenvo`, and `git diff --check`, passed. No broad byte/stress suite was rerun.
