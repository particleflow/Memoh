# Agenvo execution target: implementation boundary

Status: protocol adapter prototype. `internal/agenvo` is not wired into the
server, account registry, workspace resolver, or Web UI. It must not be advertised
as an online Memoh Remote Runtime or deployed as a completed integration.

## Existing integration points

Keep account registration, Bot grants and session selection in their existing
owners:

- `POST /users/me/runtimes`: account-owned computer registration.
- `GET /users/me/computer-access`: account grant view.
- `PUT /bots/:bot_id/workspace-targets/remotes/:runtime_id`: explicit Bot grant.
- `PUT /bots/:bot_id/workspace-targets/primary`: optional Bot default.
- `workspace_target_id`: request/session execution selection.

These currently describe native Memoh reverse-gRPC clients. Agenvo needs a
distinct provider implementation; its MCP endpoint is not a runtime ID. Existing
owner/team checks and per-Bot approval rules must remain authoritative. A target
registration must not grant it to every Bot. The existing new-computer dialog
currently performs bulk grants and cannot be reused unchanged for this provider.

Reuse an existing authorized MCP connection and its OAuth refresh owner. Do not
copy tokens, create a second relay, or couple the provider to a named Bot. Before
wiring persistence, define the connection ownership/lifecycle explicitly: a
reference to a Bot-owned MCP connection must verify the owner's authority and
become unavailable if the source connection is disabled, removed or revoked.
The adapter's `Resolve` function is only a port today, not an implemented database
authorization check.

## Implemented protocol subset

The public Agenvo contract inspected is commit
`9ec5643610ab8408503adc79a9e37f42eeb8e6e2` of
[`Xuanwo/agenvo`](https://github.com/Xuanwo/agenvo).

| Memoh operation | Agenvo/Herdr mapping | Boundary |
| --- | --- | --- |
| Readiness | `execute` calling `workspace.list` for the selected session | Validate an actual native response; search advertisements alone are insufficient. |
| Foreground command | `workspace.create`, then one `pane.run` with a bounded PowerShell wrapper | One fresh labeled workspace per operation; submission is not completion. |
| Result | `pane.read` with `recent-unwrapped` | Require this command's nonce-bound completion record and explicit exit code; retain truncation. |
| Cleanup/cancel | Verify label with `workspace.get`, call `workspace.close`, verify absence with `workspace.list` | Unknown/reused IDs or uncertain responses are not confirmed cancellation. This verifies workspace removal, not every detached OS descendant. |
| File operations | None | Do not advertise `fs` or `host_fs`; the eventual runtime boundary must reject unsupported operations. |
| PTY, stdin, environment injection, persistent/background execution | None | Explicitly rejected by the prototype before remote calls. |

Every native operation resolves the authenticated Bot mount again. Native
rejections inside successful MCP responses are errors. Unknown write outcomes
are never retried. There is no native/NAS fallback. Native workspaces provide
separate terminal routing, not an OS filesystem sandbox between authorized Bots.

The adapter uses bounded PowerShell text output with merged error output. It
does not provide byte-preserving stdout/stderr streams. The wrapper and Herdr
response shapes still require real Windows verification; synthetic tests do not
establish that compatibility. Native session IDs can be reused after restart;
Agenvo's inspected public call interface has no expected-generation argument.
The label check reduces accidental cleanup of reused IDs but is not an atomic
generation check. This limitation must be resolved or explicitly constrained
before enabling automatic cleanup in production.

## Remaining integration and acceptance

1. Verify sanitized native response envelopes from the existing deployment:
   `workspace.create`, `workspace.get`, `workspace.list`, `workspace.close`,
   `pane.run`, `pane.read`. Preserve field names and nesting, replace private
   paths, identifiers and output. Reuse existing receipts where possible.
2. Validate the PowerShell wrapper, Unicode/large output, nonzero exit, timeout
   and cancellation on a synthetic Windows task without installing new services.
3. Wire the provider into persistent account registration and the existing
   grant/selection paths; enforce real permission checks, connection revocation,
   explicit unsupported capabilities and no fallback. Credentials stay in the
   existing MCP/OAuth owner.
4. Exercise the actual application UI with two synthetic Bots, grant only one,
   check the other cannot select/execute, then revoke and disconnect. Capture
   current-build screenshots and verify restart recovery before candidate images.

Apps installation/dependency management and direct external Agent runtimes still
require their native Memoh workspace. Selecting this provider must not silently
change those responsibilities or pretend that MCP replaces `chat_runtime`.
