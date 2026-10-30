# Delivery compatibility incident register

Last reconciled: 2026-10-30 UTC. This is a reusable index for the existing
delivery handoffs, not a replacement deployment procedure or a claim that all
compatibility faults are fixed. Read the matching entry before retrying an
operation. A failure blocks that operation; continue independent authorized work.

## Use and maintenance

- Classify the failed action first: text handoff, file materialization, execution
  channel, approval, source/CI, or deployment. Similar wording is not a shared
  root cause. Preserve the original error and distinguish denied, not executed,
  pending and uncertain outcomes. Read back an uncertain outcome before retrying.
- Reuse the recorded successful path. Repeat a failed path only with the new
  condition stated in its entry; this is not a new global retry budget or an
  extra approval requirement. Existing authorization remains applicable within
  its scope. A real denial still requires relevant new evidence and normal review.
- The original owner updates the relevant entry and handoff when a cause or
  remedy is actually verified. Keep the prior failure, exact source/version,
  result, unresolved boundary and date. Unknown causes remain unknown. Do not
  delete history or weaken identity, permission or validation checks to recover.
- Record only non-sensitive error signatures, public source/CI links and opaque
  evidence identifiers. Exclude credentials, private addresses, signed download
  URLs, user content and full configuration dumps. A Library ID identifies
  evidence; it neither grants access nor proves materialization.

Each entry below uses the same six fields. Add new incidents with these fields
rather than creating another parallel troubleshooting manual.

## Evidence provenance

**P-20261030** is the parent integration handoff received on 2026-10-30 in thread
`01a17240-b602-730d-8e96-6e16279a60c6`, following user message
`Sentinel_50e78ef4a62c8191ab8f2a9b8e41700f` at 09:15:54 UTC. Field and tool
observations attributed to P-20261030 below are parent-reported; their raw
transcripts/field files are not independently attached to this repository.
Short turn IDs and package aliases are preserved exactly as supplied, not
expanded into invented full IDs or links. Times below are UTC; a date not supplied
by that handoff is not inferred from the reporting date.

Cloud release evidence was also read from the unified task's retained records:
source [9270da0252128a296d9a641235cdfd32ab44cfda](https://github.com/particleflow/Memoh/commit/9270da0252128a296d9a641235cdfd32ab44cfda),
tree `79d8385c58a05d9a372af5ea5e38ab839d5172f5`, and
[Docker #82 / 41028335536](https://github.com/particleflow/Memoh/actions/runs/41028335536).
That run completed successfully at 08:48:01 UTC on 2026-10-30. Publication,
field deployment and product acceptance remain separate evidence classes.

## Incident index

| ID | Signature / scope | Current boundary |
| --- | --- | --- |
| C01 | Windows helper: unavailable `os.setxattr` | Text handoff recovered; binary materialization unresolved |
| C02 | Library `download failed` | Cause unknown; no verified package bytes |
| C03 | Library 403 `scope_violation` | Access denied for the selected scope |
| C04 | Git safe-directory check | Specific checkout action restricted |
| C05 | OS errno 13 on process metadata | Read denied; detached-process cleanup unverified |
| C06 | Executor disconnected / recovery timeout | Same-executor actual read later succeeded |
| C07 | `Selected model is at capacity` | Agent turn failed; existing CI continued |
| C08 | Approval timeout / authorization-state change | Technical review failure, not an unsafe finding |
| C09 | R-to-C replacement/reload denied | Targeted approval used; default-off application verified |
| C10 | Original-event repeat POST denied | One approved request consumed; 409, no new run |
| C11 | 9270 deployment versus first message load | Deployment verified by writer; product defect remains open |
| C12 | Task-local Go build cache: no space left on device | Disposable task cache cleaned; scoped lint recovered |

## C01 — Windows helper calls unavailable os.setxattr

- **Platform / signature:** Windows official handoff/materialization helper calls
  unavailable `os.setxattr`; the helper operation fails.
- **Cause:** The helper uses an OS API unavailable in that Windows environment.
  No official helper compatibility fix has been verified.
- **Validated handling:** The original author outputs the complete non-sensitive
  steps, API contract and recovery instructions in the original task. The parent
  relays that text to the same field owner. Preserve binary identity requirements;
  do not skip identity marking or patch the helper's security mechanisms.
- **Result / unresolved boundary:** Text handoff enabled the parallel A/B
  execution and, separately, v37's 9270 deployment. It did not repair binary
  downloading or Library materialization. For binaries, use only an already
  supported original GitHub artifact flow and verify actual bytes, size and hash;
  no unverified alternative route is promised.
- **Original evidence:** P-20261030: parallel acceptance package supplied only as
  alias `libfile2bbb` (full ID unavailable); v37
  `libfile_598d489937848191912dd0bed9de5159`, ZIP 7,692,550 bytes, SHA256
  `1d5caacea61535e669389c8f4d082b2ae62d13bd0c0de8d79f252bb0a9d7c213`.
  Writer deployment result is recorded separately in C11. These package
  identifiers/hashes do not assert successful Windows materialization.
- **New condition before another attempt:** An official Windows-compatible
  helper correction and a supported verification path. Until then, do not repeat
  this helper call unchanged. Successful text delivery is not that condition.

## C02 — Download failed without verified bytes

- **Platform / signature:** Windows Library materialization returns
  `download failed` for the audit v3 package.
- **Cause:** Unknown. The error does not prove that the file is absent and does
  not establish C01 as its cause.
- **Validated handling:** Retain the failed materialization result. Align the
  original-source counterexamples with the unified task's already-held verified
  admission matrix, recording which owner actually read which evidence.
- **Result / unresolved boundary:** Source review progressed; the Windows owner
  did not independently obtain the v3 contents or verify its SHA. Binary
  materialization remains unresolved.
- **Original evidence:** P-20261030; audit
  `libfile_34f4bd2b7acc8191b0bb1fa7d506eb4f` v3, reported 235,962 bytes/SHA256
  `666292bf234e4a00cb307f28219936944c8bf3783d41ad107614acf9508c7829`.
  That advertised hash is not a consumer verification result. The unified task
  held the later v4 matrix independently; do not substitute that for a v3 read.
- **New condition before another attempt:** A supported technical correction or
  new diagnostic evidence identifying a recoverable failure. Claim receipt only
  after consumer-local bytes exist and their size/hash are verified.

## C03 — Library scope denial

- **Platform / signature:** Parent Library read at 07:42 returned HTTP 403,
  `scope_violation`: `outside Library folders selected for this user turn`.
- **Cause:** An explicit access-scope restriction, not a transport failure.
- **Validated handling:** Stop the denied read. Do not use another account,
  connector, materialization call or subtask to read the same out-of-scope file.
- **Result / unresolved boundary:** The parent's HTTPS review-package read is
  still blocked. Other independently authorized files and work are unaffected.
- **Original evidence:** P-20261030; exact target
  `libfile_529906009d348191893ba22510648d96` and the 07:42 error above. The raw
  response link was not supplied.
- **New condition before another attempt:** The user selects the file's Library
  scope for the current work, or an official correction resolves that restriction.
  A different download mechanism does not establish access.

## C04 — Git safe-directory restriction

- **Platform / signature:** Windows Git safe-directory check rejects an action
  on the project checkout; the machine-specific checkout path is omitted here.
- **Cause:** That checkout action hit Git's ownership/safe-directory protection.
  The underlying ownership cause and complete raw error were not supplied.
- **Validated handling:** Preserve the exact failed action in its original task;
  do not change `safe.directory`, ownership or permissions to make it pass.
- **Result / unresolved boundary:** No correction of this restriction is proven.
  It is not evidence of a failed download, missing file or stopped remote CI.
- **Original evidence:** P-20261030, Windows project-checkout safe-directory
  report; no full diagnostic or independently readable receipt link supplied.
- **New condition before another attempt:** A supported correction for that
  checkout and the action's normal authorization. Do not route around the check.

## C05 — Process metadata denied and cleanup not established

- **Platform / signature:** OS errno 13 reading `/proc/13496/ns/net`; separately,
  the navigation test owner was denied reading `/proc/29353/cwd`.
- **Cause:** Those specific process-metadata reads were denied. Their underlying
  permission cause and current process liveness are not established.
- **Validated handling:** Stop the denied reads without changing permissions or
  probing the same protected metadata through another route. Retain original
  startup receipts and the environment-maintenance cleanup handoff.
- **Result / unresolved boundary:** Navigation Server PID 29353 and Vite PID
  30927 were detached `Popen` children without a verifiable exec session; current
  liveness is unknown and they were not stopped. PostgreSQL was stopped with data
  retained. Do not describe all test cleanup as complete.
  A separate input-source workstream was denied reading `/proc/27592/cwd`;
  its old Vite launcher/child 27589/27592 was not cleaned up and current liveness
  is unverified. Later, distinct source-test Server/Vite/provider processes were
  identified from their own startup receipts and unique arguments, then stopped
  with normal SIGTERM. Managed terminal Ctrl-C did not reliably stop those
  detached children. Those later shutdowns do not resolve the denied old cleanup.
  See the [original workstream's exact evidence](https://github.com/particleflow/Memoh/blob/65649801a58946993d2824f3963255750661eb2a/docs/testing/session-input-source-20261030/README.md).
- **Original evidence:** P-20261030, original navigation-owner startup/cleanup
  report; raw startup receipt links were not supplied. The two denied paths are
  distinct observations, not proof that either process exited.
- **New condition before another attempt:** A supported environment-maintenance
  path with trustworthy process ownership/liveness evidence and authority for
  the specific cleanup. Do not kill an unverified process or widen read access.

## C06 — Execution-channel timeout, subsequently recovered

- **Platform / signature:** `exec-server transport disconnected; recovery timed
  out after25s` in the field executor.
- **Cause:** Transport/recovery failure; the underlying cause is unknown. In the
  original failure, no process was created and no POST was sent.
- **Validated handling:** Read back operation state, retain the same executor
  and writer, and use the supported recovery path. The original owner later
  completed an actual side-effect-free exec read on that same executor.
- **Result / unresolved boundary:** Execution-channel recovery is proven by that
  successful execution, not by a connected list or cloud thread read. The
  spaces 51/5 scripts were cleared. Earlier drafts saying the channel remained
  broken or those scripts remained are superseded. No general repair/reset
  guarantee or product recovery follows from this result.
- **Original evidence:** P-20261030: failed turn `01a18bb6`, later successful
  original-owner turn `01a18bc4`; full turn IDs/raw links were not supplied.
- **New condition before another attempt:** For a new timeout, first establish
  the original operation's outcome. Resume through the supported executor only
  when its state allows it; do not invent a restart or replace the field writer.

## C07 — Model capacity does not stop external jobs

- **Platform / signature:** Agent turn fails with `Selected model is at capacity`.
- **Cause:** Model capacity rejection. It does not report GitHub job cancellation.
- **Validated handling:** Read the original remote head and existing run states,
  then resume the original task with the prescribed model to collect results.
  The user's current assignment is generally Sol/high and Astra/high for the
  original parallel Bot owner; follow later explicit assignments if changed.
- **Result / unresolved boundary:** The 9270 push had already occurred and its
  six CI classes kept running. By 09:02 the unified task had collected CI,
  formal Docker #82 and text handoff results. No duplicate push/rerun or new task
  was needed. This is orchestration recovery, not a model-capacity fix.
- **Original evidence:** P-20261030, unified turn `01a18bf3`; public
  [Go](https://github.com/particleflow/Memoh/actions/runs/41026380212),
  [Native](https://github.com/particleflow/Memoh/actions/runs/41026380339) and
  [formal publication](https://github.com/particleflow/Memoh/actions/runs/41028335536)
  results, retained in the unified task.
- **New condition before another attempt:** First check what the original turn
  already submitted. Use supported resumption with available prescribed capacity;
  do not independently switch models, recreate tasks or repeatedly probe capacity.

## C08 — Technical approval failure versus a denial

- **Platform / signature:** Approval `review timeout`, or
  `authorization changed before approval could be applied`; the latter tool
  result explicitly states not executed and not a determination of unsafe action.
- **Cause:** Technical approval-state failure. The result is not a safety denial
  and does not itself revoke the existing task authorization.
- **Validated handling:** Preserve authorization/context and inspect the original
  task/operation for queueing or execution. If known not executed and the tool
  permits one retry, use that supported retry. An uncertain outcome requires readback.
- **Result / unresolved boundary:** The original tasks subsequently resumed
  successfully through the normal interface. This is not permission for endless
  retries, bypassing approval, or treating an actual denial the same way.
- **Original evidence:** P-20261030, original approval-error/resumption reports;
  individual complete response links were not supplied. Preserve the error
  classification above rather than inventing a new unsafe-action finding.
- **New condition before another attempt:** Confirm no queued/executed action and
  satisfy the tool's supported retry/recovery conditions. If actually denied,
  stop that action and use relevant new evidence under normal review (C09/C10).

## C09 — Targeted approval of R-to-C replacement/reload

- **Platform / signature:** Actual review denial of the original R-to-C
  replacement/reload action.
- **Cause:** The specific action was denied; technical timeout recovery was not
  applicable. A broader product or network root cause was not established.
- **Validated handling:** After the user's targeted 06:52 approval, the original
  writer applied the same scoped action through normal review, with the feature
  default off and a smooth reload.
- **Result / unresolved boundary:** Application and strict TLS HTTP 200 were
  reported. HTTPS performance remains unresolved; this approval does not
  authorize another sampling operation or wider network/security changes.
- **Original evidence:** P-20261030, 06:52 targeted user approval and original
  writer's default-off/TLS-200 receipt; raw message/receipt links not supplied.
- **New condition before another attempt:** Check current state first. This
  application is complete; no unchanged replay is needed. A different denied
  action needs evidence relevant to its own scope.

## C10 — One original-event POST, already consumed

- **Platform / signature:** Actual review denial of repeating the original
  result-event POST.
- **Cause:** A specific write action was denied; it was not a download error.
- **Validated handling:** The user explicitly approved at 07:27; the original
  event's read-only DB evidence was supplied at 08:04. At 08:10 the original
  writer sent the one approved original request through the normal path.
- **Result / unresolved boundary:** HTTP 409, no additional run, receipt count 1.
  The approval has been consumed. Do not repeat the POST, rerun the expert or
  extend that approval to another event. A conflict is not proof of a new reply.
- **Original evidence:** P-20261030, the 07:27/08:04/08:10 approval, readback and
  request sequence; no full event identifier or raw receipt link supplied here.
- **New condition before another attempt:** None for this completed request.
  A genuinely new required action needs its own current-state evidence and normal
  authorization; do not use this entry as a reusable write grant.

## C11 — Deployment success is distinct from a product read defect

- **Platform / signature:** 9270 deployment succeeded; on the recipient task page
  the first message-body load did not converge, while refresh showed two messages.
- **Cause:** First-load product cause unknown, under investigation by original
  navigation owner `01a18c1a`. It is not evidence that the handoff failed, messages
  were deleted or a tool failed to execute.
- **Validated handling:** Text handoff (C01) supplied the original writer's
  deployment instructions. The writer checked actual healthy/revision/digest,
  schema and preservation evidence, then exercised the task button/original row
  and route identity. Keep the first-load failure as a separate product issue.
- **Result / unresolved boundary:** P-20261030's 2026-10-30 09:17 field receipt
  confirms Server/Web 9270 healthy, schema 181 clean, unchanged 17-table summaries,
  54 original-file hashes and unknown count 2. Button/row navigation identity
  passed; initial message load did not. This does not prove all business/client
  acceptance, repair binary materialization or justify another deployment.
- **Original evidence:** [Exact source](https://github.com/particleflow/Memoh/commit/9270da0252128a296d9a641235cdfd32ab44cfda),
  [Docker #82](https://github.com/particleflow/Memoh/actions/runs/41028335536)
  and P-20261030's 09:17 field report. Public release links establish publication;
  they are not field receipts. The raw field-receipt link was not supplied.
- **New condition before another attempt:** Await the navigation owner's new
  root-cause/patch evidence for targeted product verification. Preserve old
  failures and unknowns; no message fabrication, history reset, expert replay or
  unchanged release rebuild is required to document this issue.

### C11 controlled follow-up — cold same-Bot history has no transport

- **New field witness, relayed by parent:**0954's45-second run on read-back
  Server/Webcefdf0 (`index-5VwLPQ4H.js`) had original component messages200 in
  44.94ms/two valid original messages, followed by85 DOM samples of eleven
  skeleton nodes/zero rows after Home admission. Anchor was absent. No target
  subscribe/snapshot was recorded; that does not prove online absence. Screenshot
  failed with the window minimized; no retry. Space65 closed/no in-flight writer.
  Raw Windows JSON was not read by this source owner. Natural resolver binding
  outcomes remain unknown and do not backfill older records.
- **Proved source defect:** persisted `currentBotId` can equal a cold explicit
  history target before bootstrap has started. Its same-Bot shortcut awaits no
  initialization Promise, then starts history/runtime without creating a target
  WebSocket. REST can complete while the real snapshot-gated atomic commit waits
  indefinitely. Real pane-view binding can start that same pending load earlier.
  This defect is independently reproduced; the exact field entry branch is
  still unrecorded, so do not assign the whole field incident from absence alone.
- **Correction:** product `1f021c94d08ac5617a8ae2489103d4036a11d51b` calls the existing
  native `ensureWebSocket` after navigation/Bot fences and before selecting that
  runtime; `chat-list` supplies it. A handshaking/live socket is reused. Snapshot
  atomicity, identities, queue, history, permissions, schema, UI and Agent core
  are preserved. Synchronous setup errors propagate before masking history.
- **Controlled result:** unchangedcefdf0's accepted initial four cases had three
  failures/one pass; final six actual-controller lifecycle cases and fourteen
  existing contact cases pass. Targeted lint/type/copy/contract/diff checks pass.
  Observer8/4/3 suites/full builds were not rerun. This is controller integration,
  not full application/browser, screenshot, human QA, NAS or Harmony acceptance.
- **Remaining boundary:** the source owner did not deploy or take over the sole
  writer. The first corrected real load, screenshot and exact current Harmony
  mirrors/locks remain unverified. Do not remove snapshot waiting, force refresh,
  replay a request, create work or probe protected process metadata as recovery.
  Parent arranges one original-record acceptance after unified consumption.
- **Reusable evidence:** [Cold-history diagnosis, patch and validation](history-runtime-bootstrap/README.md),
  [consumer map](history-runtime-bootstrap/consumer-map.json). Original navigation
  branch/history are preserved; the product branch starts from currentcefdf0.

## C12 — Task-local Go build cache exhausts temporary disk

- **Platform / signature:** Input-source workstream scoped lint exited 1 during
  typechecking: `write /tmp/memoh-input-source-gocache/...: no space left on
  device`. This was not an approval or access denial.
- **Cause:** The disposable task-specific Go cache occupied 5.3 GB on the 8.8 GB
  temporary filesystem. No product/database corruption was observed or inferred.
- **Validated handling:** The author confirmed the failed check had exited and
  that no valid build still used this task cache, then used `go clean -cache`
  with GOCACHE explicitly naming that cache. Source, evidence, configuration,
  database/history and shared or unidentified caches were preserved.
- **Result / unresolved boundary:** The author reported 5.7 GB available after
  cleanup; the affected scoped lint then exited 0 with no new issues. The
  inherited lint finding and independent C05 denial remain recorded. This does
  not establish product/field qualification or permit general cache pruning.
- **Original evidence:** Exact input-source commit
  `65649801a58946993d2824f3963255750661eb2a`:
  [disk-full output](https://github.com/particleflow/Memoh/blob/65649801a58946993d2824f3963255750661eb2a/docs/testing/session-input-source-20261030/go-lint-disk-full.log),
  [checks](https://github.com/particleflow/Memoh/blob/65649801a58946993d2824f3963255750661eb2a/docs/testing/session-input-source-20261030/checks.txt)
  and its [README](https://github.com/particleflow/Memoh/blob/65649801a58946993d2824f3963255750661eb2a/docs/testing/session-input-source-20261030/README.md).
  This register reuses the author's observed result; it did not clean that cache.
- **New condition before another attempt:** Verify the prior command exited,
  the cache's exact task ownership/disposability and sufficient recovered space.
  Retry only the affected check. Do not delete shared/unknown data or repeat
  unrelated builds, tests or paid calls.

## C13 — Vite test navigation used an unmounted router

- **Platform / signature:** Owned full-app Chromium fixture changed the URL but
  Home did not request the selected history. The test timed out waiting for that
  REST request. This was not a field authorization or product failure.
- **Proven cause:** In this Vite/HMR fixture, dynamic import of `src/router.ts`
  produced a different router object from the mounted application's global
  router. Identity comparison was false; its current route was `/` while the
  mounted route contained the actual Bot/session.
- **Verified handling:** Within this owned controlled test, navigate using the
  already mounted application's router. Do not use this as a field injection or
  bypass authorization. The full-app warm check then passed same-Bot reuse,
  cross-Bot connection ownership and404/no-connection rejection. The corrected
  outage/restoration check produced the bounded missing-transport red/green.
- **Evidence / boundaries:** [Full-app review](history-runtime-bootstrap/README.md#unified-mounted-app-review-2026-10-30),
  [router identity and guards](history-runtime-bootstrap/full-app-review/warm-identity.json),
  [outcome](history-runtime-bootstrap/full-app-review/OUTCOME.json). Old failed
  fixture logs are preserved in the unified task evidence. Ordinary cold entry
  passed before and after;0954's same cause and later initializer selection
  preservation remain unproved. This is not deployment or phone acceptance.
- **Before another attempt:** If URL changes without a target read, inspect the
  actual mounted router identity and the recorded request path first. Reuse
  these passing checks; do not repeat the faulty fixture or infer missing data.

## C14 — Package-manager setup failed before a narrow test started

- **Platform / signature:** Unified Node24 review, `pnpm exec vitest`, ENOENT
  creating `/home/agent/.local/share/pnpm` during automatic package-manager
  setup. No Vitest case ran; this was not an approval/scope denial or a product
  assertion failure.
- **Verified handling:** Reused the already installed official Node24 executable
  and repository `node_modules/vitest/vitest.mjs` directly, with the documented
  `--no-experimental-webstorage` test flag. No tool download, system/user path
  change, permission change or dependency installation.
- **Result / boundary:** Narrow controller and real-cache counterexamples then
  executed, each with one control pass and one product failure, exit1. Tool
  execution recovered; product admission did not.
- **Evidence:** [Review commands and source identities](speech-continuity-review-ba44/README.md),
  [setup-only error](speech-continuity-review-ba44/runner-compatibility-error.log.txt),
  [actual test failure](speech-continuity-review-ba44/final-reconciliation-red.log.txt).
- **Before another attempt:** Check the installed toolchain/actual test start
  first. Reuse that working executable; do not repeat unchanged automatic setup
  or infer a passing/failed product test from a pre-test tool failure.

## C15 — PATH go names a board game, not the Go toolchain

- **Platform / signature:** Conversation-presence research environment on
  2026-10-30, fixed source `cefdf0cbb3207d2a07db17ef365e1b8d2d43ca3f`:
  `go test ./internal/reasoning -run 'TestResolveConfigAgreesWithOffReachability|TestResolveConfigHonorsProjectedOffDeclarations|TestAlwaysOnModelIsSupportedWithoutAControl' -count=1 -v`
  returned exit 1, `Go: Unknown option: test`.
- **Cause:** `type -a go` resolved `/usr/bin/go` and `/bin/go`; `go --help`
  identified a local two-player Go board-game application. This is an executable
  name collision, not a Go test failure or a model/permission denial. The
  repository declares Go 1.25.7; no compiler execution was established.
- **Validated handling:** Stopped the unchanged test path, inspected executable
  help and checked the ordinary `/usr/local/go/bin/go` and `/opt/go/bin/go`
  locations (absent). No PATH replacement, installation or elevated action was
  attempted. Independent source/document and JSON fixture checks continued.
- **Result / unresolved boundary:** The three named existing Go tests did not
  run; no product behavior is certified. Research documents and acceptance
  inputs are delivered separately, without claiming this test passed.
- **Original evidence:** [Research report](conversation-presence-20261030/README.md)
  and its `STATIC-CHECK.json` retain the attempted command and outcome. No
  inference, TTS or production request was sent.
- **New condition before another attempt:** An identified, authorized Go
  compiler satisfying the repository toolchain requirement. Invoke its exact
  path or verified project toolchain; do not retry the board-game executable or
  install tooling solely to disguise this documentation-only task's boundary.

### C13 follow-up — recovered prepared compiler

- **New evidence:** Existing project setup and prior repository evidence identify
  `<prepared-toolchain>/bin/go` as official Go 1.25.7. Sourcing
  `<prepared-environment>/env.sh` activates that compiler and writable caches.
  No installation or global PATH mutation was necessary. A direct attempt with
  the real compiler first hit read-only default GOCACHE; prepared cache variables
  resolved it. The original board-game failure remains historical evidence.
- **Verified result:** Actual gofmt, compilation and race tests now execute.
  Conversation-presence integration results and exact commands are recorded in
  [the research report, section 11](conversation-presence-20261030/README.md)
  and `RUNTIME-CHECK.json`. Do not continue reporting missing Go as a blocker.

- **Later sqlc wrapper recovery:** During the admission fix, `mise run
  sqlc-generate` attempted unrelated tool installation and exited before the
  task because Rust could not create `~/.rustup` on the read-only filesystem.
  The pinned command already declared by that task,
  `go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate`, succeeded using the
  prepared Go environment (official Go 1.26.10 selected by the module). Rust was
  not retried and system permissions were not changed. The first generated
  projection had an unspecified Go type; an explicit SQL jsonb result cast
  restored the generated byte projection and scoped compilation/tests passed.


## C16 — constrain the durable provider and transport in a local model fixture

- **Signature:** The supplementary native WS fixture changed its fake provider
  address, but replaced the application store with real PostgreSQL. Provider
  resolution therefore read a different persistent row with empty config. Two
  initial runs showed agent_start and retries, zero requests at the intended
  httptest server, and no model result.
- **Cause / exposure boundary:** The SDK default for an absent base URL is
  `https://api.openai.com/v1`; the persistent fixture had no API key. Upstream
  attempts may have left the process. External receipt/response was not captured
  and is **unknown**; zero external traffic is not established. No production
  configuration or production credentials were used. This was a test isolation
  error, not an approved real inference experiment or a bypass of the rejected
  production `/providers` read.
- **Handling:** Stopped the original path. The durable fixture provider now also
  points to httptest. A test-only transport accepts only that exact local host,
  rejecting all other hosts before network I/O. Final success requires a zero
  blocked-host count and captures same-model main/auxiliary request bodies.
- **Prevention:** When substituting a real application store into a fake-model
  harness, inventory every selection/address/credential owner. Restrict the
  actual transport independently; modifying a fake service alone is insufficient.
  Preserve failed and corrected run evidence separately in the task report.


## C17 — Home speech fixture still returned a pre-stream SDK result

- **Signature:** All eight Home read-aloud admission cases failed at
  `api.player.dispatchEvent`: the synthetic Audio was never created. Route-context
  cases passed. The navigation author also observed the same eight failures with
  its Home binding removed; this did not establish a navigation regression.
- **Proved cause:** The fixture returned `{data: Blob}`. The existing production
  hook requests `parseAs:stream`, reads `response.headers`, and validates a
  ReadableStream with an audio MIME before creating the player. Its mock therefore
  failed before reaching the lifecycle assertions.
- **Verified handling:** Update only that fixture to return a fresh stream and
  Response, including the deferred late response. Preserve every original
  identity/admission/cancel/request-count/queue assertion. No production speech,
  SDK, navigation policy or permission was changed to make the tests pass.
- **Result / boundary:** The original eight cases pass, exit0; the two new
  route-context cases pass. Targeted ESLint exits0. This is fixture recovery,
  not a fix for the independently reproduced ba44 tail/cache P1, real audio,
  physical ArkWeb or the field's eleven skeletons. No paid/field request occurred.
- **Evidence:** [Unified convergence](unified-latest-20261030/README.md),
  [preserved failure](unified-latest-20261030/history-route-speech-current.log.txt),
  [eight corrected cases](unified-latest-20261030/history-speech-fixture-followup.log.txt).
- **Before another attempt:** Check the current generated SDK response and the
  actual first failing step. Mirror this tests-only correction into a consumer
  that still has the old Blob fixture; do not recompile an unchanged package,
  delete assertions or modify runtime behavior to mask a mock-contract mismatch.

## Applying these lessons to the existing handoffs

Use the [current convergence checkpoint](unified-latest-20261030/README.md) to
distinguish admitted source, active repairs and field gaps without replacing the
canonical 46-item/seven-gate acceptance Page. Use the version-specific handoff for actual commands, images and compatibility
requirements. The older [delivery rollout](../delivery-integration-rollout.md)
and [unified candidate evidence](unified-server-web-candidate/README.md) are
historical checkpoints; do not apply their old schema numbers to a current
deployment. This register's reporting snapshot is also not a live deployment API.

Keep latest business data, original queues, receipts, cooldown/deadline fields,
HostBinding and consumer-policy fields. Schema equality alone does not prove an
older binary can read the latest retained queue/history. Establish compatible
readers/writers or use a compatible forward correction; do not delete fields,
down a migration or restore an old dump over new data to make a rollback pass.
Windows producer/consumer, Harmony package and Server/Web evidence stay separate.
See also [queue retention](../delivery-queue-retention.md) and the
[producer handoff](../delivery-producer-handoff.md).
