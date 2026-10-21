# Agenvo execution target

This integration registers a command-only Windows target in the existing
account computer registry. It reuses `user_runtimes`, Bot mounts and session
`workspace_target_id`; it does not implement `memoh.runtime.v1`, a scheduler, or
a new credential owner. Production deployment requires separate Windows
acceptance of the Bash completion wrapper.

## Release status and terminology

This document describes unmerged PR #4 on `feat/agenvo-execution-target`.
Controlled candidate `383d23c` was deployed for acceptance with schema 169;
its first account/Bot adapter command failed on explicit cwd normalization.
The follow-up below fixes that routing contract. A replacement candidate still
requires deployment acceptance; merging and main promotion remain blocked.
Main remains `5900e4c1`, and native workspace storage remains active. The
existing direct Bot MCP channel and earlier Windows wrapper tests do not prove
that the new account/Bot adapter has completed a production turn.

The existing API paths and database names containing `runtime` or `workspace`
are shared registry/routing identifiers. For `provider: "agenvo"`, both account
and Bot target responses advertise only `capabilities: ["exec_foreground"]`.
They do not advertise native `fs`, `exec` or `hostfs` protocol support. The
Computers page and session selector label it **Agenvo · commands only**.
`online` means the authorized Herdr session answered `workspace.list`; it is
not proof that the configured Bash executable, full filesystem protocol or a
Herdr AI agent is running. Bash availability is checked at command execution.

Unsupported file APIs do not prohibit a Bash command from reading or writing
files under the Windows account's permissions. Likewise, rejecting managed
background-job options is not an OS sandbox that prevents a shell from creating
child processes. This adapter does not manage a Herdr AI-agent lifecycle.

## Registration and authorization

1. Configure an Agenvo HTTP MCP connection through the existing MCP settings.
2. In **Computers → Connect Agenvo**, select its source Bot, connection, Herdr
   instance, existing session and Windows working directory and the verified absolute Git Bash executable path.
3. `POST /users/me/runtimes/agenvo/discover` returns only Herdr identity/display
   fields. Search advertisements alone never establish runtime readiness.
4. `POST /users/me/runtimes` with `provider: "agenvo"` probes `workspace.list`
   before registering. The database stores an existing MCP connection ID and
   target selectors; no endpoint, OAuth token or reusable Memoh runtime key is
   returned or duplicated. Identical registrations reuse the existing record.
5. Select individual Bots in the existing computer-access dialog. Registration
   creates **zero grants**. `PUT /bots/:bot_id/workspace-targets/remotes/:runtime_id`
   and the existing session target selector remain the only assignment paths.

The source connection must be active, HTTP, in the current team and owned by
that account. Removing/disabling it, transferring its Bot, revoking the target,
or deactivating its owner invalidates access. Each native operation rechecks the
Bot mount, owner/team and authenticated caller's `workspace_exec` permission.
Another Bot's mount ID is never accepted. This is authorization and terminal
routing isolation; authorized Bots share the Windows account's OS permissions,
not separate filesystem or process sandboxes.

## Working-directory contract

Business tasks should use root-relative `work_dir` and relative file references
inside Bash. The base is the **currently authorized registered Windows target
root**, not the Memoh server cwd, a prior pane cwd, or an inferred drive.

| `work_dir` | Behavior |
| --- | --- |
| Omitted | Use the session's bound directory if present, otherwise `.`. |
| `.` | Resolve to the current registered root. |
| `child` or `child/subdir` | Resolve below that root; Windows separators also work. |
| `child/../other` | Normalize within the root. |
| `../outside`, drive-relative or slash-rooted paths | Reject before workspace creation/command submission. |
| Legacy absolute Windows path | Accept only if its normalized volume and components are within the current root; compare Windows components case-insensitively. |
| Cross-drive, other UNC share or absolute path outside the root | Reject explicitly; never redirect to native execution. |

The tool forwards cwd intent unchanged to the Agenvo executor. The executor
loads the current authorized target configuration, resolves the root-relative
reference and passes an absolute Windows cwd only to `workspace.create`.
A relative task reference keeps working when that configured root changes;
legacy absolute references to an old root are rejected. Tool descriptions use
`.` rather than embedding the machine's root. Existing bound sessions retain
their stored path; this change does not rewrite historical tasks or messages.
The account UI currently registers/lists/deletes targets; editing a registered
root in that UI is **not implemented**. The configuration-resolution tests do
not claim such an edit UI exists or change a deployed registration.

These are lexical checks, **not a filesystem sandbox**. Symlinks/junctions can
resolve outside the lexical root, and arbitrary Bash retains the Windows
account's file/process permissions. No new filesystem containment is claimed.
Bash executable paths and machine mounts remain deployment configuration;
attachment hash references and native Linux workspace semantics are unchanged.

## Protocol subset

The public contract inspected is Agenvo commit
`9ec5643610ab8408503adc79a9e37f42eeb8e6e2`, supplemented by sanitized native receipts.

| Operation | Mapping | Result and limits |
| --- | --- | --- |
| Discovery | MCP `search` | Herdr instance identities, filtered metadata. |
| Readiness | MCP `execute` → `workspace.list` | Actual session response required. |
| Foreground command | `workspace.create` → one `pane.run` | Fresh labeled workspace; bounded encoded Git Bash wrapper. |
| Completion | `pane.read`, `source: recent-unwrapped` | `result.output` contains terminal text. Require the operation's nonce-bound base64 JSON completion and explicit exit code. |
| Cancel/timeout | Stop waiting | State **unknown**, no invented exit code or promise that Windows execution stopped. |
| Cleanup | None automatically | Known workspaces are reported with `cleanup_required: true` and retained for manual inspection. |
| Files, HostFS, PTY, stdin, custom env, persistent/background jobs | Unsupported | Explicit rejection; no native/NAS execution fallback. |

Workspace CLI receipts nest under `result.result`; pane output is directly
`result.output`. `execution: accepted` is submission, not command completion.
PowerShell is only the existing pane launcher; the command body runs in the explicitly configured Git Bash. PATH lookup and WSL are not used. A Windows/MSYS host check rejects Linux/WSL before user execution. Bash exit status is preserved independently of stderr. Output is capped at 4096 bytes while excess is drained to avoid SIGPIPE; an incomplete final UTF-8 character is omitted. Missing Bash or unsupported host receipts are rejected without an exit code.

The wrapper returns bounded UTF-8 text with stderr merged, not byte-preserving
stdout/stderr streams. Prompts, command echoes and PSReadLine errors cannot count
as completion. Malformed/ambiguous responses and lost write responses remain
unknown and are never retried automatically.
Every result frame starts and ends with explicit CRLF. Terminal residue remains
on the preceding line; the parser still requires one entire nonce-matched frame
and rejects duplicates. See the [current validation record](../validation/agenvo/README.md)
for the original Windows framing failure and the new fixed-nonce batch.

Herdr has no atomic expected-generation close. IDs can repeat after restart;
labels, terminal IDs, revision and a separate get/list check do not prove safe
cleanup. No automatic `workspace.close` is issued, including on cancellation.
No workspace is resumed or recovered from an old ID. The connector still cannot
guarantee OS isolation or atomic lifecycle identity across a native restart.

## Native workspace responsibilities

`chat_runtime` remains the model runtime. The native Memoh workspace still owns
Bot configuration, profiles, memory and App/dependency installation. This first
phase replaces foreground command execution only.

For command-only targets, Hook **configuration** is explicitly read from the
Bot's native storage. Hook actions retain the selected execution target: a
configured command action requiring bridge stdin/env is rejected, never executed
on the policy-storage machine. Failure to load policy blocks the turn; policy is
never silently skipped. Selecting Agenvo therefore still requires reachable
native Bot configuration storage. Full native-workspace removal is outside this
adapter's contract.

## Verification and rollout

Synthetic tests cover the observed envelopes, completion/nonzero exit/truncation,
unknown results, cancellation, no automatic close, repeated authorization checks,
unsupported operations and unavailable-target rejection. PostgreSQL integration
checks use an isolated migrated database and rollback every fixture:

```sh
MEMOH_AGENVO_TEST_DATABASE_URL='<isolated test database>' go test -race ./internal/agenvo
```

The account/grant/selection UI must be exercised against the running server with
two synthetic Bots. Keep the release unchanged until the PR is reviewed and a
controlled candidate passes Windows Git Bash execution, Unicode, nonzero
exit, large output and cancellation acceptance. ARM64 execution is not implied
by compilation. Do not send production configuration/data to cloud tests.

Migration 0169 adds provider metadata and an idempotency index. Rollback by
running the previous application with the provider unused, retaining the DB and
known image digests. Do not run the global `migrate down` command as an upgrade
rollback; it may remove unrelated data and this provider's registration history.
