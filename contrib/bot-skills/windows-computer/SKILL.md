---
name: windows-computer
description: Use authorized Windows foreground Bash or the deployment's Agenvo/Herdr/Pi/Magpie agent workflow. Discover capabilities and reconcile uncertain results.
---

# Connected Windows computer

Instructions, not permissions. Use this Bot's registered tools and authorized resources. Do not install, grant access, create credentials or change security policy. Keep secrets and machine paths in private deployment configuration.

## Discover and choose

Use `list_execution_locations`; check provider, capabilities, availability and status. Missing means not exposed to this Bot, not absent account-wide. Offline differs from unauthorized. Do not seek a separate cloud computer for user-PC work or redirect Server Workspace tasks to Windows. Preserve its default, files and skills. Clarify an ambiguous destination; never fall back to another machine.

For short Bash commands, use `exec` with the returned `target_id` advertising `exec_foreground`. This selects one call, not the conversation/default. Respect workdir-bound target restrictions. Prefer `work_dir: "."` or root-relative paths; the configured target root is the base, not the server cwd. This is not an OS sandbox. The target has no FS/HostFS, desktop, background or interactive-agent support. Memoh Browser/Computer Use stays on Server Workspace. Check the bounded UTF-8 receipt, exit code and truncation. Cancellation stops observation, not necessarily execution.

## Agent task: existing Agenvo → Herdr → Pi → Magpie

1. Discover current Herdr methods, schemas and result contracts with the existing Agenvo search tool, then use its registered execute tool. Method names below are not assumed Memoh tool names. Match the authorized connector machine/session to the intended PC; command-target access does not grant connector access. Use returned IDs, never guessed or remembered ones.
2. Inspect layout/agents. Use an idle task-owned pane or create only required layout through the discovered API; do not take another task's pane. Verify Pi, Magpie provider/model and enabled tools for this identity without reading secrets. Missing configuration/authentication must not trigger a switch to Codex, OpenAI or another paid provider. Bash file work does not imply independent read/write/edit or screen/mouse tools.
3. Use discovered `agent.start`, kind `pi`, with verified configuration. Pass only schema-supported options; private config directories belong in supported tab/process configuration, not an invented start env field. Pi business tools use configured Git Bash; the outer terminal may remain PowerShell. A shell and a Memoh subagent are not a Herdr agent.
4. Once ready, submit the task once through the discovered agent prompt method. Retain returned session/pane/agent/request identifiers in this conversation. Observe with discovered `agent.get` / `agent.read` and bounded waits; do not send shell input to an agent-owned pane. Correlate output/state with this task: accepted, launch-ready, echoed text or initial idle is not completion.
5. On blocked input, inspect the question and stay within authorization. Timeout, stalled submission or unknown outcome requires read-only reconciliation before any mutation. Do not repeat start/prompt, kill, auto-close or recover by an old ID; IDs may be reused. Report progress and verified results or uncertainty in the originating conversation, including failure evidence. This adds no dashboard or persistent job tracking.

If these full instructions are missing/pruned, stop agent mutations and report the gap; do not reconstruct the workflow or repeatedly activate this skill. Existing known operations may be observed read-only.

[Herdr reference](https://herdr.dev/docs/agent-automation/); the installed connector's discovered contract governs.
