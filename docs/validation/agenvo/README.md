# Agenvo integration validation — 2026-10-21

Cloud development environment: PostgreSQL 18 (isolated synthetic database), Go
1.25.7 server, Vite on `http://127.0.0.1:18082`, Chromium. No production secrets,
NAS data, personal endpoints or real Windows tasks were used in cloud tests.
Separate runtime-side Windows wrapper evidence is recorded below.

## Verified

- Fresh schema migrations through 0169, with `dirty=false`.
- Real PostgreSQL account/team/connection ownership, duplicate registration,
  zero automatic grants, cross-Bot mount denial, connection disable, source Bot
  transfer and inactive membership tests; Agenvo package passed `-race`.
- Backend packages: agenvo, userruntime, workspace, bridge, hooks, handlers,
  server, apperror and core passed. Targeted tool routing tests passed.
- Browser login; existing MCP selection; Herdr discovery; registration;
  both Bot grants initially off; grant A only; A target selection; B cannot see it.
- Browser revocation removes the grant and target; restoring A works.
- Backend restart retains registration/grant. Explicit unavailable target rejects
  a message; unsupported filesystem request returns
  `501 workspace.operation_unsupported`. No endpoint/token is returned for the
  Agenvo registration.
- Actual browser → application → synthetic model tool call → HTTP MCP fixture →
  same-session `completed`. The latest captured run submitted one command and
  read once, retained its workspace and made zero close calls. A native Exec
  sentinel did not fire. Result details display merged output and retention.
- Web error and tool rendering tests: 149 passed. SDK typecheck passed.
- ESLint: zero errors, 18 existing warnings. UI contract guard passed with three
  existing warnings.
- Go lint for changed backend packages against `origin/main` passed with no
  reported issues after formatting and private error-message cleanup.
- Narrow Chinese/dark dialog has no horizontal page overflow. Form body scrolls
  within the shared dialog; its footer remains reachable.

## Bash checkpoint

- The requested Bash replacement requires an explicit verified Windows Git Bash
  executable path. No PATH/WSL fallback, installation or global shell change.
- The actual Bash script passed Linux execution tests for normal UTF-8 output,
  stderr without failure, nonzero exits, one-million-byte output draining,
  truncated UTF-8, syntax errors and rejection of a Linux host. Only the uname
  host identity was synthetic for script tests; these are not Windows tests.
- Real Chromium repeated registration with the new Bash field (zero grants),
  A-only assignment, A target selection and a synthetic model/MCP command. One
  command was submitted, one snapshot read, zero workspaces closed. B could not
  see the target. Chinese dark narrow layout remained scrollable without
  horizontal overflow. New screenshots are linked below.
- CI on `84e46dbe` reached terminal results: Docker PR, Electron, ESLint and
  Go lint passed; migrations and Go tests failed. Both newly introduced defects
  have targeted passing local regressions: 0169 rollback now uses per-team
  context and restores the teams policy in the same transaction; unsupported
  local targets no longer reclassify native gRPC Unimplemented protocol errors.
- PostgreSQL authorization/race and SDK typecheck passed for the Bash changes.
  Generated Swagger/SDK and sqlc v1.31.1 outputs were regenerated.
- The Bash checkpoint `eeb6c5c` subsequently passed all five CI workflows:
  Go CI, Migrations, Docker PR, ESLint and Electron CI. In particular,
  [Migrations 39550463541](https://github.com/particleflow/Memoh/actions/runs/39550463541)
  completed successfully. No images were published.

## Completion boundary correction

- Windows submitted the first `100...` vector once. Its payload contained the
  expected normal/Unicode/native/path output and exit 0, but `U+01B0 U+006D`
  terminal residue preceded its frame on the same line. Strict whole-line
  parsing found no completion: one submitted, zero strict passes; the other
  four vectors were not submitted. Payload inspection is not a passing result.
- The wrapper now emits explicit CRLF before and after all success and failure
  frames. The parser is unchanged. Regression tests reproduce the original
  rejected line, execute the actual fixed Bash wrapper after terminal residue,
  reject duplicate frames and launcher echoes, and keep frame-shaped business
  output encoded inside the actual result (with its real exit 7).
- Agenvo/Hook race tests, including isolated PostgreSQL authorization checks,
  server build and Go lint passed. The five vectors were regenerated from the
  actual implementation with new `200...` nonces; the old batch is superseded.
- The rebuilt Go server and real Chromium on `http://127.0.0.1:18082` repeated
  target selection and chat execution. The controlled MCP fixture inspected
  the submitted wrapper for the new boundary, then returned terminal residue
  followed by that boundary and its synthetic completion. One submission,
  one read, zero closes; the UI displayed completed output and retention.
  This validates application wiring, not the Windows terminal implementation.
- The new Windows batch was handed to the runtime thread without repeating
  the old unknown command or creating another acceptance workspace. Its
  completed observations are recorded below; the original failed batch remains
  failed evidence.

The follow-up contract check kept the same wrapper and fixed vectors. Account
and Bot target responses advertise only `exec_foreground`; account rows and the
session selector identify the provider as command-only. Registration copy now
says file/HostFS **APIs** are unavailable, since Bash commands still have the
Windows account's ordinary file access. The design document separates this
unmerged candidate from the deployed main release and retained native storage.
Current browser captures verify the clarified form and Chinese dark narrow
scrolling; Bot B still cannot select the ungranted target. Locale duplicate-key
checks and the UI contract guard passed (three existing guard warnings).

## Real Windows wrapper acceptance

The runtime thread executed the five `200...` vectors generated at
`e9ce9f7014c4fb2acc552f71fbc4c6afd627ddae` through the existing Agenvo MCP
tool connection. Each command was submitted exactly once in the same live
acceptance pane. Completion frames were parsed with the strict whole-line
nonce/base64 rules; no parser relaxation was used. No new workspace, close,
kill or focus takeover was performed. The pane remains retained for inspection.

| Vector | Observed result |
| --- | --- |
| Normal/Unicode/native/path | Git Bash 5.2.26, `msys`, result 42, Chinese/✓, native `Windows_NT`, successful `cygpath` round trip; exit 0. |
| Native stderr | `MEMOH_STDERR` followed by `MEMOH_AFTER`; exit 0. |
| Native nonzero | `MEMOH_NONZERO\n`; exit 7. |
| Large output | 4096 bytes, exit 0, truncated true: the 12-byte `MEMOH_LARGE\|` prefix plus 4084 ASCII `x` characters. |
| Canceled observation | At 0.228 seconds the early read contained no complete frame. Stopping observation remained unknown with no exit code. A later read at 06:11:27 UTC independently observed `MEMOH_DELAYED\|42\n`, exit 0; it did not change the canceled observation into success. |

The old large-output expectation incorrectly said 4085 `x` characters. The
original comparison therefore recorded three passes and one documentation
mismatch. Only that arithmetic expectation is corrected: the submitted command,
encoded wrapper, nonce and command hash are unchanged, and no command was rerun.

These are real Windows observations of the wrapper through the existing tool
channel. They do not establish that this PR is deployed, that the account/Bot
adapter has completed a production turn, or that a Go context cancellation was
propagated through that adapter to the real machine. Cloud cancellation tests
and the runtime-side stop-waiting observation remain separate evidence. No
claim is made that Windows execution was terminated or the pane safely closed.

Independent review reported no new P0/P1 findings for `eeb6c5c` and `e9ce9f70`.
The execution code in `022b56c2` is identical to `e9ce9f70`; all five CI workflows
for `022b56c2` completed successfully. Candidate image publication and deployed
account/grant/session integration acceptance are separate release steps.

## Tool-to-executor cwd correction

Controlled candidate `383d23c` reached schema 169 and application health checks,
but the runtime thread reported its first actual adapter command rejected an
explicit cwd. This was not a passing production execution. Merge and main
promotion are blocked until replacement-candidate acceptance.

The cloud reproduced the failure with the old binary through the real Chromium
chat UI: a synthetic model passed `work_dir: C:\Synthetic`, which the bridge
path normalizer reduced to `.` before the absolute-only executor rejected it.
The MCP fixture recorded zero workspace creations and zero command submissions.

The replacement forwards command-target cwd intent without bridge normalization
and resolves it against the current authorized Windows root. The actual
`Tool.Execute` decoder → target router → Agenvo executor → MCP fixture regression
covers omitted/default/dot, relative child, legacy same-root absolute paths,
bound sessions, root changes despite stale tool metadata, UNC roots, denied
callers, unavailable targets and invalid/escaping paths. Rejected inputs perform
zero executor MCP calls and never use the native client. A resolver readiness
probe may still occur before the executor, but no workspace or command is
created for an invalid cwd. Native Linux/Windows bridge path and target-pinning
regressions pass unchanged.

On the rebuilt server, real Chromium selected the existing A-only target and
sent three synthetic commands: legacy absolute root, relative `child`, and `.`.
The fixture observed `C:\Synthetic`, `C:\Synthetic\child`, and `C:\Synthetic`
respectively, three submissions/reads, zero closes. A fourth request with
`../outside` was rejected without adding a workspace or submission. The native
Exec sentinel remained absent. These are cloud application/transport tests,
not real Windows filesystem execution. The synthetic model's fixed final text
is not used as a success assertion; tool receipts and transport counts are.

Agenvo/Hook race tests, isolated PostgreSQL permissions, targeted tool race tests,
server build and changed-package Go lint passed. The first tool race link failed
because the temporary filesystem was full; after removing this task's unused
downloaded browser/build artifacts, the same command passed. No user data was
removed. The Bash wrapper and strict completion parser are unchanged; existing
fixed-vector source hashes describe their original generation commit, not this
cwd follow-up's modified `exec.go`.

Screenshots from the actual localhost app:

- [Old explicit-cwd failure](agenvo-cwd-before.png).
- [Relative child completes](agenvo-cwd-relative.png).
- [Dot resolves to registered root](agenvo-cwd-dot.png).
- [Parent traversal rejects](agenvo-cwd-rejected.png).

No production registration/grant was created or changed by these cloud tests.
Schema remains 169; this follow-up adds no migration. The configured-root change
is covered at the executor boundary; a registered-root edit UI/API is not added.

## Limits and failures

The cloud Agenvo MCP and model endpoints are controlled protocol fixtures. The
runtime-side wrapper acceptance above uses a real Windows machine through the
existing tool channel, not the deployed PR adapter. ARM64 execution has not
been exercised.

[Bash acceptance instructions](bash-acceptance.md) and
[exact generated commands](bash-vectors.json) provide one-time synthetic
vectors for the existing Windows execution thread. Their generation and the
separate runtime-side result record must not be conflated.

The cloud Docker daemon uses vfs. Creating a full native workspace from the
existing image exhausted its 32 GiB filesystem. Derived caches were moved to
`/tmp`; no existing deployment or user data was deleted. For the end-to-end
protocol test, a synthetic native configuration bridge supplied an empty Hook
policy and rejected every Exec call. This does not verify a full native
workspace deployment. Hook policy loading and refusal to execute on the policy
storage provider also have dedicated regression tests.

The wider tool package has seven image download tests blocked by the existing
proxy/restricted-host guard. Web typecheck retains pre-existing errors (442 at
the initial post-change check; none in the new registration/selection code).
The original comparison had 443; correcting the MCP list path annotation removes
one. These checks are not described as passing.

## Screenshots

Only synthetic labels, paths and output appear in these current-build captures.

- [Registration](agenvo-bash-registration.png): existing connection, explicit target
  selection and capability limits.
- [Zero default grants](agenvo-bash-no-grants.png): both Bots off after registration.
- [A only](agenvo-bash-grant-a.png): A on, B off.
- [Candidate gate recheck](agenvo-release-grants.png): current running Web,
  command-only target, A still enabled and B disabled; read-only inspection
  performed without submitting another command or modifying grants.
- [Completed synthetic command](agenvo-bash-chat-completed.png): output and retained
  workspace notice in the actual chat UI.
- [Completion boundary regression](agenvo-bash-boundary-completed.png): current
  application completes a synthetic result preceded by terminal residue and
  the wrapper's explicit CRLF boundary.
- [Chinese/dark/narrow](agenvo-bash-mobile-zh-dark.png): shared scrollable dialog.

Agent screenshots and automated checks are not human QA. The owner has released
the review gate for a controlled candidate publication. The first controlled deployment and its cwd failure are recorded above; a
replacement candidate and main promotion remain separate release gates.
