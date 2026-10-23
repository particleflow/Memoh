# Native Agent observation and export history

Native `computer_task_result` previously replaced the operation's latest
`observation` / `output`; exporting then replaced `artifact_hash`. The operation
now also retains distinct bounded samples and their export associations inside
its existing `details` JSON. The existing revision CAS protects these additions.
There is no SQL migration, new service, upstream Agent modification, or change
to initial/follow-up prompt dispatch. Pi batch and foreground execution keep
their existing behavior.

## Meaning of the evidence

A sample records the read start, first observation and recording timestamps,
Agent status/readiness, optional official `completion_seq` (a decimal string
preserving the integer value received by Go), observed official session metadata, terminal text
fidelity/truncation, and the input request IDs/states saved **before** polling.
Raw session paths/IDs and historical terminal text stay private in the operation
record. Public history exposes a session-observed flag and relation to the
original binding; it never rebinds the original Agent/session.

This does not establish end-to-end uint64 precision. Official Agenvo
`apps/herdr/src/herdr.ts` parses CLI stdout with `JSON.parse(stdout)`, so a
sequence above JavaScript's safe integer range can already be rounded before
Go receives it. The decimal string avoids additional numeric rounding at this
boundary; it cannot recover upstream rounding. The synthetic Go transport's
`9007199254740993` case tests only this local preservation of the received value.

`association: sample_only` and `verified_result_ownership: unknown` are explicit.
Herdr `agent.prompt` returns AgentInfo, not a per-input execution/result ID.
`completion_seq` and `--wait` do not attribute a completion to one prompt. A
`done` sample followed by `working`, several accepted inputs, or a larger
sequence number cannot establish whose output was read or whether the user's
objective succeeded. A backward/gapped sequence is retained as observed rather
than reordered or repaired. No Agenvo event stream is consumed: there is no
resume cursor, loss recovery or reconstruction of unobserved events.

The session metadata comes from `agent.get`; terminal text comes from a later
`agent.read`. Neither the two reads nor the preceding input-receipt snapshot is
an atomic upstream observation. A session/instance can change between calls.
The recorded relation is a sampled fact, not proof that every output byte belongs
to that session, or that a Pi conversation was restored. Unknown input outcomes
remain unresolved, and these APIs never send prompts or recreate an Agent.
The original operation's state and accepted input state are not round outcomes.

## Tool contract

- `computer_task_result {operation_id}` polls the existing native Agent once,
  then persists the sample. The compatible latest `observation`, `output` and
  `truncated` fields remain. `native_evidence` adds counts, capacity, latest
  observation/export IDs, and the current sample's compact history page.
- `computer_task_get {operation_id}` reads the stored latest view without a
  provider call. Add `history_offset: 0` to obtain one compact stored sample
  and its export references without raw terminal text or the full follow-up
  list. Follow `next_history_offset` until absent. Offsets index append-only
  host records; they are not upstream execution IDs or event cursors. A page's
  `history_partial` reports that other samples exist. Concurrent appends can
  increase `count`, but cannot move an existing offset. Page 0 also includes
  any unassigned legacy export.
- `computer_task_artifact {operation_id, observation_id?}` exports the saved
  sample, defaulting to the latest saved observation. The optional local
  `observation_id` selects an older sample from the same operation. The returned
  history page describes that selected sample; the compatible latest terminal
  view remains the latest observation. `artifact_hash` and
  `latest_export_observation_id` refer to the most recently selected export.
  Export does not poll a PC or collect arbitrary files.

The two optional inputs are necessary to retrieve/export older evidence and to
avoid coupling history lookup to large terminal output. Ordinary tool results
remain subject to the existing gateway output budget; if a large latest view
is pruned, use the compact `history_offset` form. Configuring a smaller global
tool budget can still prune a history page. This does not remove stored data.
No REST endpoint, frontend implementation, or new permission mapping is added.
All reads/exports use the existing operation owner/Bot/conversation checks,
selected-PC restriction, current registration and grants, including the existing
delegation restrictions. Authority is checked again after provider/media work
and on CAS retries before releasing the result.

## Deduplication, export verification and capacity

Identical sampled contents, official metadata and input-receipt snapshots reuse
the same local record ID. The ID is content-derived solely for host lookup; it
is not sent upstream and is not an atomic incarnation/round identifier.
`first_observed_at` and the record remain immutable; `last_sampled_at` on the
ledger updates on another successful read. This is a history of distinct
samples, not a complete log of every poll or every Agent state transition.

Exports use the existing media interface. On the first export of bytes, the
returned SHA-256 hash and bounded reopened bytes must match the saved sample
before an association is committed. Later exports reuse that association; equal
bytes in another sample can reference the same previously verified hash with
its original `bytes_verified_at`. A historical hash has
`current_availability: not_rechecked`: replay does not prove the object still
exists or that a user accepted its contents. Concurrent first exports can each
call media ingest before CAS; the content hash and persisted association are
deduplicated. No original object is deleted by this code. A media write followed
by revocation/save failure may leave an unassociated object, but yields no
successful export receipt.

```toml
[computer_tasks]
native_observation_limit = 32
```

The startup configuration accepts 1..128; omitted/0 uses 32. This is a **total
per-operation** limit of distinct samples, not unlimited follow-up retention.
Each sample stores at most 16,384 UTF-8 bytes of terminal text; input references
are bounded by the existing 64-input ledger. There is at most one export
association per sample plus one imported unassigned legacy hash. No history is
automatically evicted. On a new sample at capacity, the tool returns an error
with `native_evidence.recording_state: capacity_reached`, preserves the old latest
view and every prior record, and does not retry/dispatch input. A read may have
occurred, but the rejected new sample is not recorded. Existing samples can
still be queried/exported/deduplicated. Increasing the startup limit permits
new records; lowering it keeps all existing records and rejects new distinct
samples until sufficient capacity is configured. There is no clear-history or
auto-expiry operation in this change.

When first touching a legacy operation, only its surviving latest view can be
imported (`origin: legacy_latest_view`); the original observation time and input
association are unavailable. Its old `artifact_hash` is kept separately as
`legacy_unassigned` with no observation ID or byte-verification timestamp. It
may belong to an already overwritten view, so it is never relabeled as the
surviving text. Already lost observations cannot be reconstructed.

## Validation and remaining acceptance

`internal/computertask/native_evidence_test.go` exercises the actual application
service and standard MCP HTTP client against synthetic interfaces:

- Old `done` and newer `working` samples, multiple accepted inputs, duplicate
  samples, old/new exports and service reconstruction from serialized records.
- Backward/gapped sequence samples, a lost read response, a session change or
  disappearance, preservation of the received integer representation (not
  end-to-end uint64 precision) and UTF-8 byte truncation.
- Repeated/historical exports, immutable legacy hash association, new evidence
  arriving during ingest, corrupted bytes and current authority revocation.
- User/Bot/session/selected-PC/NAS scopes, full-capacity preservation, configuration
  increases/decreases, compact pages with all 64 input receipts.
- CAS conflict preserving follow-up/delivery/unknown JSON metadata and cancel
  intent, committed-save response loss without another provider read, and
  failed writes without false evidence.

The historical four-package race, vet and PostgreSQL records below were made
on the implementation worktree based on `f5b1626`, before the `15845b3`
checkpoint; they are not full reruns of the later history-page correction.
That correction reuses ordinary-get operation facts (including cancellation
and cleanup) while omitting large latest-view fields. Its new JSON regression
first reproduced false flags, then `go test -json -race ./internal/computertask
-run '^TestNativeEvidence' -count=1` passed 10 top-level tests (24 named passes
including subtests, no skips/failures). No four-package, vet, PostgreSQL or
Actions run was repeated for this correction.

Historical local cloud checks:

- `go test -race ./internal/agenvo ./internal/computertask ./internal/config ./cmd/internal/core -count=1`: agenvo/config/core passed; the catalog description contract initially failed, then `go test -race ./internal/computertask -count=1` passed after preserving the existing follow-up-history wording.
- `go vet ./internal/agenvo ./internal/computertask ./internal/config ./cmd/internal/core`: passed.
- `bash scripts/test-computer-operations-postgres.sh`: the required PostgreSQL test passed without skips against the isolated local development database.
- `bash scripts/check-agent-core-boundary.sh`: runtime/contextview match official baseline `bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046`.

Configuration tests verify bounds. The unchanged required PostgreSQL gate runs
`TestComputerOperationsMigrationAndDurableIsolation` with race detection and
rejects a skip/missing test; it covers the reused store CAS and ownership/RLS,
not a new database schema or real-PC lifecycle. The combined workflow retains
its existing memory, file-reference, delegation, transport and Pi history gates.

No real PC/NAS, external model, upstream extension installation, legacy media
P1 repair or production deployment is part of this validation. Real runtime/UI
acceptance and screenshots required by repository guidance remain unverified;
this scoped synthetic change must remain Draft pending coordinated acceptance.
Recorded observations and verified bytes are **not** verified per-input results
or accepted business deliverables. Strict round ownership, automatic recovery,
stop execution, unlimited history and UI wiring remain outside this change.
