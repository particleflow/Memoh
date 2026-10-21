# Unmodified Agent core and external computer services

## Enforced source boundary

`scripts/check-agent-core-boundary.sh` compares all of `internal/agent/runtime/`
and `internal/contextview/` byte-for-byte with the official felinics/Memoh commit
recorded in `scripts/agent-core-baseline.txt`:
`bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046`. Go CI runs this check. The optional
revision argument also lets a reviewer demonstrate that the withdrawn
`3700f9bf` implementation fails it. This is the project's verified upstream
baseline, not a claim that upstream has stopped changing. When synchronizing
official updates, verify the new upstream commit and update the baseline in
the reviewed upstream-sync change; do not freeze the product at this version.

The removed PR #5 changes were RequiredUsage consumption in native tool
assembly, Required retention in tool-usage fragments, and legacy context
composition changes. Their supporting interface, provider hook and dedicated
assertions were removed too. Ordinary tool descriptions and optional skill
loading remain. They confer no permissions, deterministic routing, retention
guarantee or security boundary. Historical evidence is
[retained and marked withdrawn](validation/computer-workflow/README.md).

PR #4's extra external-runtime rejection has moved out of the engine to
`cmd/internal/core/container_runtime_bridge.go`, injected into the host's
Codex, Claude Code and Grok driver constructors. It rejects an Agenvo backend
before exposing a bridge client. The original upstream check continues to
reject ordinary remote workspaces. Container information is not relabeled and
failure never selects another workspace. Restoring the engine therefore does
not make an unsupported command-only target eligible to host a full runtime.

This boundary does not claim the whole Memoh repository is unchanged. Host
wiring, tools under `internal/agent/tool`, `internal/agenvo`, account grants and
workspace integration are extension code. No Claude Code, Codex or Pi vendor
source is patched. Official tool configuration/plugins are distinct from
modifying an Agent executable or its reasoning loop.

## Subsequent service and MCP work

The restoration itself did not implement computer task routing. The subsequent independent branch implements the service described in [Scoped computer tasks over standard MCP](computer-task-mcp.md), with its explicit limits and acceptance status.

The existing MCP ToolGatewayService supports additional ToolSource instances
and Streamable HTTP tools. Computer submit/query/result/cancel operations
belong in a separate source backed by the existing Agenvo service. A model
selects a task and an allowed Memoh target reference; it does not supply raw
JavaScript, arbitrary Herdr methods, credentials or device/instance IDs.
The service resolves the registered connector/device/Herdr mapping and fixed
Pi/provider configuration and enforces authorization before transport.

A PC-selected conversation must see and use only that PC. A native/NAS-selected
conversation may choose among its Bot grants; multiple genuinely suitable PCs
require disambiguation. A target ID is a Memoh mount ID, never an Agenvo
device ID. Raw Agenvo search/execute access must be closed at both discovery
and execution boundaries, including stale tool routes, before claiming this
isolation works. No grant is expanded by a skill, default selection or model.

Scope must come from authenticated identity and a trusted host-owned runtime
binding. MCP session IDs, client-provided conversation IDs and scope headers
are not authentication. An original Agent using one broad connection for many
conversations needs host-managed restricted connections/instances to preserve
per-conversation isolation; merely adding a header is insufficient. Use
existing authorized credentials. New persistent credentials or permissions are
outside this restoration.

Standard-client acceptance must work without RequiredUsage or any special
system injection. Submission is once-only; unknown outcomes are reconciled
read-only. Startup readiness, accepted input and a previous completion state
do not establish task completion. Do not assume a Pi lifecycle extension is
installed: without verified task-correlated completion evidence, report unknown.
Cancellation must not imply a process was stopped, and stale native IDs must
not trigger automatic close. Service
correctness must remain independent of the chosen model or Agent's compliance
with prose instructions.

This restoration does not implement that service surface or validate original
Claude Code/Codex interoperability. It also does not implement PC GUI control
or cloud image editing. Those remain separate, explicitly tested capabilities;
local/cloud intent must never silently fall back to another resource.
