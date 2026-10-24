# Completed batch recovery after control-terminal replacement

Base: `893eb55b50d46e5fd4d64ffb4a2769c4e6b6f75e`. This is a test/evidence checkpoint only. Product source, the PR79 frozen head and published `91af53f4` images are unchanged. It does not claim the recovery gap is fixed.

## Reproduced boundary

`TestBatchCompletedControlRestartRecoveryBoundary` exercises the real standard MCP client → computer-task service → fixed Agenvo relay path with the existing synthetic Herdr/Pi transport. The fixture has already exited with a valid receipt and a parsed `succeeded` small result; file bytes, task identifiers and their hashes remain identical. It then removes old scrollback and changes only the reported control terminal incarnation while preserving pane/workspace IDs. It also reconstructs the Memoh service from the original store.

| Durable stage before peer restart | Observed result |
| --- | --- |
| Receipt not yet collected | Result read fails; unchanged valid files are still on the synthetic PC. No terminal command or Pi replay. |
| Exit receipt saved, result.json not yet collected | Operation remains completed/result-pending; result read fails. Completed alone is not readiness for a lossless shutdown. |
| Small semantic result saved, diagnostics not collected | Cached summary succeeds without a peer call. Explicit diagnostic collection fails; the known summary is not overwritten. |
| Summary and complete diagnostic references saved | Both remain available without a peer call. |

Each case asserts one original Pi dispatch, no control command after terminal replacement, byte-identical original progress/receipt/result/stdout/stderr, unchanged operation revision/details and unchanged stored assets. This is a **known-gap assertion**, not a green test of desired recovery. No official Herdr/Pi binary, physical Windows restart, new network service or production data is used. A prior official Herdr download denial was not retried or bypassed.

Command (exit 0; four subtests plus parent PASS; zero SKIP):

```sh
go test -count=1 -v ./internal/computertask \
  -run '^TestBatchCompletedControlRestartRecoveryBoundary$'
```

[Actual output](regression.txt). Go formatter and golangci-lint 2.10.1 with all configured rules on `./internal/computertask` also completed with exit 0. `evidence.json` records the three unmodified local output hashes. Existing synthetic fixture code is in `internal/computertask/pi_batch_test.go`; this test adds no production override or weakened identity check. No Web source changed, and the existing UI verification at the frozen base is not presented as a new recovery UI test.

## Why the current receipt is insufficient

- `internal/agenvo/pi_batch.go`: `BatchReceipt.Validate` binds the original attempt, invocation nonce, request hash, Pi session and execution workspace/pane/terminal. File names, sizes, digests, semantic result and history remain validated. These are invocation/result evidence, not permission to send text into a replacement shell.
- `internal/agenvo/pi_batch_service.go`: `ObserveBatch` falls back from evicted execution-pane scrollback to `receipt.json`; `ReadBatchPage` reads the fixed result/log files through `readBatchFile`. That helper must call `pane.run` on the dedicated control shell. Its `pane.get` projection contains only pane ID, workspace ID and terminal ID. A different terminal is rejected before input.
- `internal/agenvo/pi_batch_producer.go`: the optional producer wake-up uses the same control-terminal guard. Current declared artifact manifests are not a generic authenticated reader for the invocation's receipt or compact result.
- `internal/computertask/pi_batch.go`: a persisted small result or complete diagnostic refs can be returned from NAS state. Uncollected data still requires the original read channel; no configuration switch establishes a replacement channel's ownership.

Do not replace `ControlTerminal` with whatever `pane.get` returns, rewrite the original receipt, infer authority from an idle prompt or disable terminal matching. A valid receipt can establish which bytes to accept **after** reading, but it cannot establish that typing the read command into an unrelated/reused terminal is safe. The current invocation nonce is not an exposed Herdr server generation.

## Minimum cross-line decision before product changes

The existing official `workspace.create`/`tab.create` operations can create a fresh reader; no new Herdr protocol is needed for creation itself. What is missing is the durable association and recovery ownership for that reader. The current foreground `Executor.Execute` is not a drop-in solution: it creates and retains a workspace on each invocation, and does not associate that workspace with the original batch's recovery progress. Repeating it per file page would introduce untracked resources.

The smallest candidate is an optional `BatchDetails.recovery_reader` on the **existing operation**, separate from immutable execution progress: `{request_id, phase, workspace_id, pane_id, terminal_id}`. Server would CAS-reserve creation before one existing native create call, validate/save the returned reader identity, then reuse only that channel for fixed reads. Unknown creation/association would not authorize another create or a Pi retry. Old records without this field would retain their original behavior. This is a proposed persistence contract, not implemented code or proof of a deployed capability.

Before adding it, align with the Windows/producer owner on who owns that reader during endpoint upgrade/drain and what event makes it safe to use. No additional Windows HTTP API is asserted necessary at this stage. At minimum, the agreement must identify:

1. The existing authorized target registration and original operation/attempt/generation that the recovery belongs to. No model-selected native IDs, new token or new authorization scope.
2. An independently owned replacement **reader** workspace/pane/terminal. It must be created or restored by the trusted host, not inferred from a reused pane ID; the original execution identity remains immutable.
3. The point at which that binding is durably associated with the existing operation using CAS before any input. An uncertain creation/association must remain unknown and must not repeatedly create readers or rerun Pi.
4. Reads restricted to the existing receipt/result/log names and persisted offsets, with all current ownership, registration pin, identity, hash, size and semantic checks unchanged. Changed/missing/corrupt data remains rejected.

If Windows already supplies this ownership via an existing producer recovery contract, reuse it. Otherwise agree the above binding and failure semantics with that owner before adding fields or calls. A fresh reader is a transport resource, not a new business attempt; it must not silently start an Agent, replace history or close a user workspace. No second ledger, file service or Agent-core change is proposed.

The product repair is pending that contract clarification; this is not an access-denial workaround or a request to loosen authorization. Existing completed/cached tasks are usable within the boundaries demonstrated above.
