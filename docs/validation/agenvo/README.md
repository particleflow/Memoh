# Agenvo integration validation — 2026-10-21

Cloud development environment: PostgreSQL 18 (isolated synthetic database), Go
1.25.7 server, Vite on `http://127.0.0.1:18082`, Chromium. No production secrets,
NAS data, personal endpoints or real Windows tasks were used.

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
- The new Windows batch and CI for this correction remain pending at this
  checkpoint. Reuse the already observed live acceptance pane; do not repeat
  the old unknown command, close the workspace or resume IDs after a restart.

## Limits and failures

The Agenvo MCP and model endpoints are controlled protocol fixtures, not a real
Windows machine. The completion wrapper still requires Windows Git Bash
acceptance for actual commands, Unicode/large output, nonzero exit and cancel.
ARM64 execution has not been exercised.

[Bash acceptance instructions](bash-acceptance.md) and
[exact generated commands](bash-vectors.json) provide one-time synthetic
vectors for the existing Windows execution thread. They are preparation
artifacts; their presence does not mean Windows acceptance passed.

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
- [Completed synthetic command](agenvo-bash-chat-completed.png): output and retained
  workspace notice in the actual chat UI.
- [Completion boundary regression](agenvo-bash-boundary-completed.png): current
  application completes a synthetic result preceded by terminal residue and
  the wrapper's explicit CRLF boundary.
- [Chinese/dark/narrow](agenvo-bash-mobile-zh-dark.png): shared scrollable dialog.

Agent screenshots and automated checks are not human QA. Keep the PR draft and
production release unchanged pending review and controlled Windows acceptance.
