---
name: windows-computer
description: Use authorized Windows task tools without guessing device IDs or raw Herdr methods. Preserve conversation scope and uncertain receipts.
---

# Connected Windows computer

Instructions, not permissions. Use registered tools; never create credentials, grant access, or change security policy. The service enforces scope without this skill or Agent prompt changes.

Use `computer_targets` for user-PC work. A PC conversation sees only that PC; a NAS conversation sees only Bot-authorized PCs. Do not look for a separate cloud computer, send NAS tasks to Windows, or change the default workspace. An absent target is not exposed to this conversation; configured capability is not proof of availability. Ask only when multiple authorized destinations remain ambiguous.

Use `computer_task_submit` once with a returned `target_id`, `kind` and `task`. The target ID is a Memoh mount, never a transport device ID. The service resolves the private connector/device/instance mapping. Use the native Memoh Runtime path; legacy Agenvo enrollment/dispatch is retired. Do not reconstruct Herdr protocols. If the high-level tools are unavailable, report that gap; repeatedly loading this skill will not repair it.

- `foreground_bash`: short Bash commands through configured Git Bash on Windows, not WSL. Ordinary `exec` cannot dispatch managed PC commands; it would bypass the durable task ledger. Prefer `work_dir: "."` or root-relative paths. This is a path contract, not an OS sandbox. Check UTF-8 output, real exit code and truncation.
- `herdr_pi_task`: only when advertised. The service follows Memoh Runtime → Herdr → Pi using the registered provider/model profile (Magpie where configured), without provider fallback. It owns layout creation, fixed `agent.start` and a single prompt. Pi tools remain those actually configured; Bash file work does not imply desktop GUI or independent read/write/edit tools. A shell, a Memoh subagent and a Herdr agent are different execution resources.

Keep the returned `operation_id`. Use `computer_task_get` for its saved receipt or `computer_task_result` for read-only `agent.get` / `agent.read` observation. Accepted, starting, interactive-ready, old done status, completion sequence and terminal text do not independently prove this task succeeded. Agent observations remain unverified; no Pi lifecycle hook is assumed.

An unknown result must not be resubmitted, even with a new task string or run. Cancellation records intent and returns `process_stopped: false`; it cannot safely kill/close a reused workspace. Report uncertainty and the receipt for manual inspection. `computer_task_artifact` creates a stable content-hash attachment of the bounded terminal snapshot only after storage is ready; arbitrary PC file transfer is not supported here.

Missing authorization, missing provider profile, offline transport and unknown outcome are distinct failures. Do not bypass them with raw MCP, shell-launched Agents, other PCs, NAS desktop tools or another provider. This interface does not implement a full filesystem/runtime, GUI automation, media editing or an Agent monitoring dashboard.
