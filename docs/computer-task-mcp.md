# Scoped computer tasks over standard MCP

This branch adds a host service and tool adapter. It is a review candidate,
not a record of production deployment or Windows acceptance. The Agent runtime
and contextview directories still match the official baseline recorded by
`scripts/check-agent-core-boundary.sh`. No Claude Code, Codex or Pi source,
reasoning loop, system composition or RequiredUsage mechanism is changed.

## Trusted connection path

The existing Memoh-hosted Codex/Claude Code tool mount configures the original
Agent through its official MCP configuration. It serves Streamable HTTP tools
on an existing per-thread reverse-HTTP route. The host resolves each request
against the active turn and carries its authenticated user, Bot, conversation,
run and resolved computer selection in `RunContext`. The application layer
creates `toolcontext.ComputerBinding`; `computertask.Source` validates it.

The same MCP source is adapted into native tools. Native Usage and optional
skills are guidance only. The standard-client regression uses the official
Go MCP SDK over real HTTP with a synthetic host-owned active-turn binding;
it calls discovery, submit, saved result and cancellation without system
instructions. It does not run the Claude Code/Codex executable or a real Pi.

General `/bots/{bot_id}/tools` requests do **not** gain computer authority from
an account token plus `X-Memoh-Session-Id`, target headers or an MCP session ID.
They fail closed. Existing authenticated ACP runtime mounts and direct-driver
thread mounts carry the trusted active context; idle mounts cannot execute.
An arbitrary standalone Agent outside that host needs a separately authorized
restricted host connection. This change creates no production token, OAuth
grant or persistent credential and does not make a broad account token safe
for several differently scoped conversations.

An Agenvo command target cannot host the external Agent binary, its filesystem
or credentials. The original drivers' container requirement and the host's
Agenvo rejection remain. Hosting the brain in an existing Server Workspace
and calling a registered PC through these tools is distinct from installing
that brain on the PC or replacing the workspace runtime.

## Tools and authorization

| Tool | Effect |
| --- | --- |
| `computer_targets` | Bot-authorized Agenvo targets within conversation scope; no raw connector/device/instance identifiers. Availability is explicitly not probed. |
| `computer_task_submit` | Fixed `foreground_bash` or configured `herdr_pi_task`, optionally selecting a returned target. |
| `computer_task_get` | Read a durable receipt, no remote effect. |
| `computer_task_result` | Read receipt; for Pi, fixed read-only Herdr get/read observation. |
| `computer_task_cancel` | Record intent; never kill, close or send terminal keys. |
| `computer_task_artifact` | Persist a bounded terminal snapshot through the existing media service and expose its content hash only after opening/validating the stored bytes. |

A selected PC restricts both inventory and calls to that PC, including native
exec, filesystem/desktop rejection and workdir discovery. A NAS conversation
can select only existing Bot grants; an omitted target chooses the sole
matching capability or returns ambiguity. It never changes the default or
falls back. Each remote operation and receipt/result/artifact access rechecks
the grant, caller execution permission, credential-owner relationship and
registered target fingerprint. User, Bot, conversation and team also scope
durable receipt access. These checks do not depend on model compliance.

Foreground execution uses the existing target approval policy. General Pi
tasks additionally require read/write/exec permissions and explicit allow for
all enabled target policy modes. Task prose cannot match command bypass rules.
Ask/deny policies fail before reservation/dispatch; this first interface does
not yet implement a pending-approval continuation for computer tasks.

Raw federation search/execute for registered Agenvo connections and same-URL
aliases is hidden and rejected, including cached routes and revoked registry
entries. All raw federated connections are unavailable in a PC conversation:
their protocol cannot prove a device scope. The aggregate Connect-It source is
likewise unavailable in PC turns until it can enforce device scope. Global
capability management is also unavailable from that conversation; Web administration is unchanged.
Unrelated MCP connections remain available in NAS conversations. This is not
an OS/network sandbox: a high-privilege Bash or Pi process can access resources
available to its operating-system account. Do not interpret target routing as
confinement of arbitrary programs or unknown third-party tools.

## Existing registration and Pi configuration

Registration and per-Bot grants reuse the existing account runtime tables.
`PUT /users/me/runtimes/{id}/agenvo-agent` updates an existing owner registration
with `{provider, model, config_dir, path?}`. An empty profile disables Pi tasks.
Updates use owner checks and compare-and-swap; they do not create a duplicate
connection, credentials, default target or grants. Actual paths and secrets
belong only in private deployment configuration. The response advertises
`herdr_pi_task` only when a valid profile exists. No model/provider is guessed.

The service creates a unique workspace and tab, supplies the non-secret
`PI_CODING_AGENT_DIR` (and optional PATH) through `tab.create.env`, then calls
fixed `agent.start` with kind Pi and configured provider/model arguments.
It reads startup state before submitting one prompt. Magpie is used when the
owner configured that provider; missing configuration never selects OpenAI,
Codex or another provider. Pi's existing deployment must configure Bash for
its business tools; this service does not change a global shell or vendor code.

Business workdirs use `.`/root-relative paths resolved against the current
registered root. Existing in-root absolute compatibility is retained by the
Agenvo adapter. Traversal, cross-drive and other invalid forms are rejected.
Lexical checks do not prevent symlink/junction escapes.

## Receipts, uncertainty and migration

Schema 170 adds `computer_operations` only. Claims and revision checks are
durable, tenant-scoped and protected by RLS. A unique unsettled slot per
Bot/conversation/target prevents an unknown effect from being repeated with a
different task text, tool-call ID, run or restarted server. A stale dispatch
reservation is reported as unknown, not proof a worker is running. A failed
post-dispatch persistence attempt retains the receipt in the error response.

Herdr start acceptance, interactive readiness, a previous prompt snapshot,
completion sequence and terminal output do not prove this task completed.
No Pi lifecycle hook is assumed. Pi observations remain unknown/unverified;
the connector can parse JSON-looking terminal text, so output fidelity is
explicitly unverified. Restarted/reused/moved IDs are not a safe cleanup handle.
`process_stopped` is always false. There is no automatic kill/close, resumed
mutation or administrative unknown-slot resolution in this checkpoint. Such
receipts require investigation rather than further dispatch on that slot.

Only foreground Bash's existing nonce-bound UTF-8 frame can report completed
with a real exit code. Artifacts here are terminal text snapshots, not general
PC file transfer. GUI control, cloud image editing and a management dashboard
are outside this change.

Apply migration 170 explicitly before using this server. Preserve receipts
when rolling application images back; schema 169 applications can ignore this
additive table. Do not run `migrate down` on live data or erase unresolved
receipts to permit a retry. Isolated integration tests alone step 169/170 and
exercise eight concurrent claims, restart idempotency, revision conflicts,
identity isolation and non-superuser RLS.

Local automated and browser evidence is recorded in
[the validation checkpoint](validation/computer-task-mcp/README.md).
