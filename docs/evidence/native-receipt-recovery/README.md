# Recover the original immutable receipt after a corrupt terminal frame

Base: `50d69c7b77be0dda884e2708db6167a1d561bba0`. Only `ObserveBatchWithReader` changes in product code. No configuration can fix this control-flow defect, and no new API, schema, service, Agent runtime or device action is introduced.

## Proven isolated defect

A terminal snapshot containing a frame for the original generation can fail base64/JSON/identity validation. Previously that error returned immediately, even when the original attempt's immutable receipt file was complete and valid. The baseline regression records `state=submitted`, zero control-file reads and the existing unknown error. This is a synthetic reproduction, not telemetry identifying the cause of any field operation.

The fix uses the existing original-file fallback for a corrupt snapshot as well as an absent one. It retains the original operation/attempt/generation, registration pin, current grant, control pane/terminal checks, fixed `receipt.json` path, complete-receipt size bound and every `BatchReceipt.Validate` condition. A file must independently prove the original invocation's exit before the existing CAS can persist completion. It does not trust the damaged frame, an unrelated business receipt, an exit code alone, current pane names or a local producer acknowledgement.

If no valid canonical file is available, the corrupt snapshot remains unknown. Missing/corrupt files, seven mismatched identity fields and revocation before or during the read cannot change the stored row or dispatch Pi again. Transport/protocol/authentication errors before frame parsing retain their existing handling; they do not become permission to switch targets or retry a business call.

## Verification

- Baseline: [expected regression failure](baseline-red.txt).
- Fixed source: [commands, exits and new cases](validation.json); selected receipt/reader/native-result contracts have 151 pass events, zero failures and one explicitly skipped existing PostgreSQL reader-CAS test. Its isolated database URL was not provided; the store/CAS implementation is unchanged, and that skip is not counted as a pass.
- The new positive case recovers the same stored operation from its valid immutable file, keeps original run/target identity and performs one synthetic Pi invocation total. Eleven negative cases retain the original row without completion or replay.
- `go vet ./internal/agenvo ./internal/computertask`, repository-configured golangci-lint 2.10.1 on those packages, and `git diff --check` pass. The task does not repeat image generation, ASR, cancellation, launcher acceptance or complete-repository builds.

All peers and data are synthetic. No real model, device, field browser, production API or production database was used. Real deployed application UI and field recovery remain unverified; no UI screenshot or human QA is claimed. This source checkpoint is not a release-ready or field-success claim.

## Field distinction and minimum next read

A declared business artifact named receipt is distinct from `.memoh-computer/attempts/<original-attempt>/receipt.json`. NAS commitment of the former does not prove that the native execution receipt was recovered. Likewise `awaiting_nas_ledger` is per-file local association feedback, not proof that every declared artifact is committed in NAS.

The existing field owner should read only: internal receipt existence/size/hash/JSON validity; equality of attempt, generation, request hash, Pi session, workspace, pane and terminal against original persisted progress; history and stdout/stderr/completion metadata; observation `result_reason`, `native.reason`, `receipt_state`; original/current control-reference equality and any existing recovery-reader phase. Share comparison booleans and safe metadata, not prompt/log bodies, absolute host paths, provider configuration or credentials. For delivery, retain per-file delivery ID/state/code/retryable and actual NAS-ledger outcome separately.

This patch can help only when the original immutable receipt exists and validates. If it is missing or mismatched, the field operation remains unresolved; this code cannot reconstruct or fake its evidence. Never rerun Pi or replace the historical operation to make recovery appear successful.
