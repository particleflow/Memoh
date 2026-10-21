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

## Limits and failures

The Agenvo MCP and model endpoints are controlled protocol fixtures, not a real
Windows machine. The completion wrapper still requires Windows PowerShell 5.1
acceptance for actual commands, Unicode/large output, nonzero exit and cancel.
ARM64 execution has not been exercised.

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

- [Registration](agenvo-registration.png): existing connection, explicit target
  selection and capability limits.
- [Zero default grants](agenvo-no-grants.png): both Bots off after registration.
- [A only](agenvo-grant-a.png): A on, B off.
- [Completed synthetic command](agenvo-chat-completed.png): output and retained
  workspace notice in the actual chat UI.
- [Chinese/dark/narrow](agenvo-mobile-zh-dark.png): shared scrollable dialog.

Agent screenshots and automated checks are not human QA. Keep the PR draft and
production release unchanged pending review and controlled Windows acceptance.
