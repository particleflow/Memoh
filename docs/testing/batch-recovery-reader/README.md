# Batch file recovery through an operation-owned reader

Product checkpoint: `334c47d29e3cb1bfe7bed392f8d84e64672ce0b4`, based on the independent `ed4a5e69b9e469f4b0b801f5365322b42eab4a31` [known-gap regression](../batch-control-restart/README.md). The published candidate and `integrate/unified-7b-transfer-observation` are unchanged. This increment is not deployed, published or a real Herdr/Windows acceptance.

## Existing contract, narrowly extended

Herdr can retain workspace/pane IDs across a restart while changing `terminal_id`. The original reader correctly refused to type into that replacement terminal, even when the completed invocation's files were unchanged. Result/log collection now retains that refusal and can create one new dedicated shell through the existing official `workspace.create` method.

The Memoh operation coordinator is the only creation owner. The endpoint does not create anything. The optional private `batch.recovery_reader` JSON record has `request_id`, `phase`, `workspace_id`, `pane_id`, and `terminal_id`. It reuses the existing operation store/CAS and needs no migration, API, credential, service, daemon, tool argument or Agent-core change. Original `Progress`, attempt, generation nonce, request hash, Pi session, execution workspace/pane/terminal, receipt and file offsets remain unchanged.

1. Only explicit `computer_task_result` or `computer_task_logs` collection may enter this path. A well-formed `pane.get` must identify the original workspace and control pane but a different terminal. Mapping, authorization, transport, missing-pane or malformed-response errors do not trigger creation.
2. The coordinator persists `creating` with a new UUID using the original operation revision CAS. Only the winner calls `workspace.create` once, with the existing registered root/session. A label verifies this call's response; it is never a discovery or takeover key.
3. The returned workspace/root-pane/terminal must be bounded, distinct from execution/control identities and correlated to that label. The coordinator persists `ready` using CAS before any `pane.run` in the reader.
4. Reads use the original path resolver, filename allowlist, bounded pages, nonce framing and receipt/hash checks. Reader identity is used only for transport. The receipt still validates against original execution identity. Grant/mapping checks occur on every native call; every read checks the reader's pane/terminal/workspace first.
5. Lost or timed-out create responses, malformed responses, failed ownership saves and restart with `creating` remain unknown. They never trigger another creation, label search, Pi resubmission, workspace close or identity rewrite. A changed ready reader is refused, without resetting confirmed offsets.

`computer_task_get` and the existing HTTP/native `Recover` observation path remain read-only and cannot create a reader. Already cached summary/log results return without Herdr calls. Pending producer delivery remains a separate responsibility: this increment does not rebind its feedback/wakeup commands or assert file delivery from Pi completion.

## Verification

All device calls below use the existing synthetic official-method transport. It checks the original owner/connector/device/instance, reservation-before-create, ready-before-run, original immutable files and the existing pane.read schema/nonce/rendered-row paging. No Herdr executable was downloaded, and no production PC or provider was called.

| Check | Actual outcome |
| --- | --- |
| Four prior boundary stages: uncollected receipt, receipt only, summary ready, all cached | Same invocation recovered; semantic result and exact log hashes; no Pi replay; all-cached makes no Herdr call |
| Lost/timeout/malformed creation; reservation/ready save failure; creating left after restart | At most one creation; zero reader input before confirmed persistence; no replacement resource on repeat |
| Concurrent coordinator + stale revision | One durable winner; contender returns unknown; stale CAS rejected |
| Read-only get / unrelated workspace mismatch | Zero creation |
| Revocation before read or between get/run; changed reader identity | Refused; saved partial file offsets/revision unchanged |
| Wrong receipt attempt / corrupted completion / corrupted diagnostics | No forged result or ready artifact; semantic result is not changed by failed optional logs |
| Failed page CAS then same-page reread | Original offsets retained; no duplicate committed chunks; exact final hashes; cached reread performs no remote work |
| Real isolated PG, transaction rolled back | Existing account/Bot authorization and new reader JSON/CAS round-trip passed; original execution state/identity retained; two PASS records, zero SKIP |
| Affected packages | `agenvo` and `computertask` exit 0; 517 PASS records including subtests; one initial opt-in PG skip, subsequently run and passed separately |
| Targeted race | Initial suite: 23 PASS records, zero SKIP, exit 0; added timeout/identity/partial-offset checks: 24 PASS records, zero SKIP, exit 0 |
| golangci-lint 2.10.1 | Both affected packages, all configured rules, no diff filter: exit 0 |
| Official core boundary / diff whitespace | exit 0; no runtime/contextview/toolexec changes |

The second race command excludes the PostgreSQL test and the unchanged page-reread test (the latter passed in the first race run). The final mapping assertion is test-only; its targeted log is included separately. No package-wide or remote CI success is inferred from a test starting.

Commands:

```sh
go test -count=1 -v ./internal/agenvo ./internal/computertask
go test -count=1 -v ./internal/computertask \
  -run '^TestBatch(CompletedControlRestartRecoveryBoundary|Reader)'
go test -race -count=1 -v ./internal/computertask \
  -run '^TestBatch(CompletedControlRestartRecoveryBoundary|Reader)'
go test -race -count=1 -v ./internal/computertask \
  -run '^TestBatch(CompletedControlRestartRecoveryBoundary|Reader[^P])'
# Set the existing isolated test DSN privately; do not use a production DB.
go test -count=1 -v ./internal/agenvo ./internal/computertask \
  -run '^Test(BatchReaderPostgresPersistenceCAS|PostgresAccountAndBotAuthorization)$'
golangci-lint run ./internal/agenvo/... ./internal/computertask/...
bash scripts/check-agent-core-boundary.sh
```

The package-wide/race logs precede the new opt-in PG test; the later real-PG command compiles that test and runs it. The final mapping check runs all recovery cases except the opt-in PG case. No full Web or unchanged UI tests were rerun for this backend-only change.

## Preserved limits and integration conditions

- Herdr exposes no atomic expected-generation guard for `pane.get` then `pane.run`. Metadata checks do not prove a strict same-incarnation operation across that race.
- A known ready reader remains tracked by its operation. No automatic cleanup is attempted: safe atomic closure of a possibly reused resource is not established. A lost creation response may leave an unidentifiable workspace. This is **not** a zero-residue guarantee; unknown resources are never guessed or closed.
- Missing/deleted original control panes and a second restart that changes the ready reader remain refused. This change handles the verified case of the same original control workspace/pane with a changed terminal, not arbitrary device restoration.
- Optional JSON needs no database migration, but mixed-version writers are unsafe: an old executable can deserialize and rewrite Details without preserving `recovery_reader`. Stop old coordinators before enabling this increment, preserve operation data, and do not downgrade a writer over active reader records. No upgrade or data rewrite is performed here.
- Existing bounded collection deadlines, page sizes, content validation, target/session grants and producer delivery behavior remain intact. A verified Pi result is distinct from artifact delivery or user acceptance.
- The real-PG test reconstructs the store and tests stale CAS; it is not a physical DB crash/power-loss experiment. The synthetic tests model interruption/restart at persisted boundaries, not a vendor-binary restart.

`initial.txt` preserves a test-helper name collision. `targeted.txt` preserves an incorrect assertion comparing persisted `batch_submitted` progress with the fixture's earlier `batch_submitting` snapshot; it was corrected to compare the actual pre-recovery persisted Progress. Later logs show the outcomes above. These were test defects, not relaxed product identity checks. `evidence.json` records original/published hashes and explicit normalization. Private DB configuration, credentials, raw server logs and data are not included.

Draft PR/Actions operations through this environment's previously rejected `gh` API are not retried. The independent source branch can be reviewed and integrated through the parent's existing authorized connection. This work does not move the frozen candidate, main/latest tags, or perform a NAS/PC deployment.
