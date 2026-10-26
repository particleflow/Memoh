# Direct native file-frame snapshots

Base: `9020c93f7f99bd98f7f8b4e15db1ea685ef27ea2`.

## Confirmed protocol distinction

The pinned Herdr v0.9.5 CLI accepts `pane read --source recent-unwrapped
--lines N --format ansi`. Its default text read is interactive: under certain
known-idle alternate-screen conditions the official server traverses/restores the
screen. The direct ANSI read uses the existing buffer instead. These are official
options, not a new API, private flag or modified Herdr implementation.

Evidence sources:
- [CLI parser](https://github.com/herdrdev/herdr/blob/2ecf19a92fc3a7fff75b6ce3655ef04f4625ec01/src/cli/pane.rs#L451)
- [Official server read selection](https://github.com/herdrdev/herdr/blob/2ecf19a92fc3a7fff75b6ce3655ef04f4625ec01/src/server/headless.rs#L2732)
- [Bounded alternate-screen traversal](https://github.com/herdrdev/herdr/blob/2ecf19a92fc3a7fff75b6ce3655ef04f4625ec01/src/server/alt_screen_read.rs#L11)
- [Immutable v0.9.5 release](https://github.com/herdrdev/herdr/releases/tag/v0.9.5)

The official Linux release binary was SHA256-verified against the repository's
existing pinned contract: `14d9e8bc8b4a9e0118d77c32266f76d461f09b40342eb23752960bd89f59929d`.
CLI/server source blobs were also verified against the fixed upstream commit.

## Minimal adapter change

Only native batch file snapshots request ANSI format. Ordinary Agent reads and
legacy simulator defaults retain their prior contract. The native command adapter
accepts only the official text/ansi enum for pane.read; no arbitrary CLI arguments,
paths, target IDs or raw methods become tool inputs.

The batch reader removes only SGR presentation styling. Other terminal controls
are left untrusted; there is no terminal emulator or cursor reconstruction.
Original nonce, exact page length, whole-file SHA, invocation identity, current
permissions/registration, CAS, timeouts and unknown/no-replay behavior remain.
No fallback to another runtime, task dispatch or file repository was added.

Diagnostics now include snapshot_attempts, snapshot_lines and
snapshot_last_elapsed_ms. The existing read_elapsed_ms is the ENTIRE readBatch
budget, not the last native call duration. This distinction matters when many
short polls consume the original20s context. No secret/body logging is added.

## Isolated official-server comparison

Fresh isolated config/home/session and a model-free synthetic actor were used.
The alternate-screen actor reports a known-agent status label solely to exercise
the official server condition; no Codex or paid model executes.

- Plain shell, text8:4ms, partial frame; text64:2ms, complete frame.
- Synthetic alternate screen, text64:5112ms; direct ANSI64:1-2ms.
- Direct ANSI plain-shell64:2ms, one matching nonce frame, exactly2048 decoded
  original bytes and matching SHA; no terminal/output string is published.
- A separate20s alternate-screen8-line loop returned many fast EMPTY snapshots;
  it did NOT reproduce a single20s CLI hang. It demonstrates why total read time
  alone cannot identify the blocked native call. This negative result is retained.

The adjacent JSON contains sanitized measured facts. This is Linux protocol
verification, NOT a Windows/production reproduction or acceptance of a real task.
The reported field failure remains unconfirmed as the same cause; this patch must
not be described as successful recovery of that operation.

## Project checks and field boundary

The baseline contract test fails on the previous missing format/style handling.
After this patch, direct-format/ordinary-default, SGR-only handling, two-page
hash-verified output and trace invariance tests pass. The normal staged aggregate
runs lint and all tests for the three affected packages without changing rules.
Commands/exits and the remaining Windows/PG/field boundaries are in validation.json.

No producer artifact was recollected, no Pi/model was rerun, no old state/history
was rewritten, and no unrelated receipt fallback was substituted. Runtime process
registration was inspected but not changed: its normal registration branch is
synchronous, so the speculative close-listener explanation was not established.

Pending one field-only comparison, by the unique owner in the ORIGINAL registered
Herdr identity/session: `pane read <original-control> --source recent-unwrapped
--lines 64`, outer limit20s, no pane.run/result/submit. Return only exit/timeout,
elapsed, stdout/stderr byte counts and complete-same-nonce/end-only booleans.
Do not export terminal text or credentials. No field command or deployment is
performed by this source task. Further result collection belongs to the original
authorized workflow after integration/deployment and concrete evidence review.
