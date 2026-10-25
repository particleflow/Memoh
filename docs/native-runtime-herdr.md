# Native Remote Runtime to Herdr contract

This adapter replaces the interactive OAuth transport for one explicitly configured Windows computer. It reuses the existing Memoh Remote Runtime identity, Bot mount and computer operation ledger. It does not change other OAuth clients, create credentials, enroll a device, start Herdr, or deploy anything.

## Routing and owner configuration

| Target | `kind` | `provider` | Identifier |
| --- | --- | --- | --- |
| NAS local workspace | `native` | existing local provider | `target_id = native` |
| Windows native Remote Runtime | `remote` | `memoh` | Bot mount `target_id`; registry `runtime_id` |
| Existing Agenvo target | `remote` | `agenvo` | Existing Bot mount and MCP reference |

Do not infer Windows routing from the word “native.” The selected Bot mount is resolved on every call. The adapter requires a ready Windows (`windows` or `win32`) remote connection; it never selects the NAS bridge or the Bot's current default as a fallback. Workspace target listing retains the existing connection-status path, with no OAuth probe.

`PUT /users/me/runtimes/{id}/native-herdr` accepts an already activated, non-revoked Runtime owned by the authenticated account. It stores the following non-secret profile under `user_runtimes.provider_config.herdr`, using a compare-and-swap. Existing unrelated provider configuration is preserved. The response is the existing Runtime DTO with no key reissued. It creates no Bot grant.

```json
{
  "instance_id": "herdr-main",
  "session": "default",
  "work_dir": "C:\\Memoh\\work",
  "bash_path": "C:\\Program Files\\Git\\bin\\bash.exe",
  "herdr_cli": "C:\\Memoh\\herdr\\herdr.exe",
  "herdr_config_root": "C:\\Memoh\\config\\herdr",
  "producer": {
    "program_path": "C:\\Memoh\\bin\\transferqueue.exe",
    "config_dir": "C:\\Memoh\\producer\\native-config",
    "state_dir": "C:\\Memoh\\producer\\native-state"
  },
  "agent": {
    "execution_mode": "pi-json-v1",
    "provider": "deployment-provider",
    "model": "deployment-model",
    "config_dir": "C:\\Memoh\\pi"
  }
}
```

These example paths and names are not discovered production settings. `agent` is optional; without it only `foreground_bash` is advertised. Native Agent tasks require the existing `pi-json-v1` protocol. `.cmd` wrappers, relative program/config paths and legacy TUI startup are not supported. `herdr_config_root` must end in `herdr`, matching the official CLI's XDG directory convention. The existing Agent `path` override is optional. Credentials must remain in the existing host-owned protected configuration, never in this profile, headers or model arguments.

`producer` is optional for tasks without file declarations, and required before a native task with declared files can be claimed. It references the existing finite producer EXE and existing `HERDR_PLUGIN_CONFIG_DIR` / `HERDR_PLUGIN_STATE_DIR` pair. All three values are required together, use canonical local Windows paths with backslashes and no redundant/trailing separator, and are pinned by the existing target fingerprint. The existing producer configuration's `source_root` must match the task work directory. No new directory, profile manager, queue or credential is created by this configuration. Updating these references cannot silently redirect an old task.

On native task completion, and on a permitted bounded delivery wakeup, Memoh invokes that exact EXE with `-action collect-result` and explicitly sets both directory variables for that process. It does not invoke the retired `herdr plugin action` hook, inherit its directory pair, or substitute `advance-results` for first registration. Agenvo tasks retain their existing plugin hook.

The returned Runtime DTO and owner Runtime list expose optional `native_herdr = {connection_id, device_id, instance_id, session, target_fingerprint}` for a valid configured profile, including while offline. The first two fields both equal the DTO's `id`; the fingerprint identifies the current full profile. This is a read-only binding for host/transfer consistency checks, not a third selector or a credential. Bot task requests continue to use their existing mount `target_id`. Invalid or unconfigured profiles and Agenvo records omit this native binding.

Status codes: 200 updated; 400 invalid profile; 403 foreign, pending or revoked Runtime; 409 conflicting configuration or a paused-instance mapping change. Authentication and owner identity use the existing account boundary.

The ledger's `connection_id` and `device_id` both equal the actual native Runtime UUID; they do not name an MCP connection. `instance_id` names the owner's explicit local Herdr instance. The target fingerprint includes Runtime identity, executable, configuration root, session, work directory, Bash and Agent profile. Existing Agenvo fingerprints retain their original serialized fields.

## File principal handoff

Runtime authentication (`mrk_...`) and file authentication (`mft_...`) remain separate purposes. The Runtime key must never be sent to a file endpoint. The existing owner-only `GET/POST/DELETE /users/me/runtimes/{runtime_id}/transfer-credential` now supports an explicitly configured native Runtime, with digest-only storage, CAS rotation and revocation. This implementation created and tested only synthetic credentials.

| Binding | Native value |
| --- | --- |
| Owner | Current Runtime owner, active account/team membership |
| Connection and device | Actual native Runtime UUID for both fields |
| Instance | `provider_config.herdr.instance_id` |
| Task target | Existing Bot mount `target_id`, never the Runtime UUID |
| Producer principal | Original `operation_id`, Bot/user/session/target/run, attempt, generation and root hash |
| Herdr context | Explicit profile's CLI/config root/session, pinned by the operation fingerprint |

File authentication revalidates the current owner, team, activated/non-revoked Runtime, credential and instance. The delivery transaction then verifies the same operation, original run/attempt/generation, current grant, target fingerprint and authenticated device triple before committing a NAS reference. An owner configuration change does not rewrite old task evidence.

Old Agenvo registrations, operation identities, file credentials and HostBinding values are not converted or accepted as the new native identity. HostBinding is still the local transfer configuration binding, not Runtime authentication. The host/transfer integration must explicitly select the approved native credential reference and retain or isolate old queue entries under their original identity. Unknown old work is queried in place and is never re-enqueued under a new selector. Host-side transfer 0.10.1 configuration and queue-isolation acceptance remain separate from the server mapping implemented here.

An unknown producer acknowledgement is not permission to replay Pi or create a new upload identity. The existing collector can be invoked again with the same pinned directory pair: its durable journal, original delivery ID and unknown-create fence govern recovery. This can repeat a finite collector process; it does not imply a second business invocation or upload Create. Matching non-retryable feedback suppresses further automatic wakeups. CLI output and process exit remain advisory until the NAS ledger contains a committed reference.

## Task and result contract

Web and Harmony clients retain the existing [session tasks REST contract](session-tasks-api.md): `GET /bots/{bot_id}/sessions/{session_id}/tasks`, detail `/{task_type}/{task_id}`, and `/deliveries`, `/observation`, `/content`. For this adapter, `task_type=computer_operation`, `task_id=operation_id`; keep the returned `run_id`, `target_id` and string `revision`. Do not infer a completed business result from `status`: inspect separate `process_state`, `pi_result`, `business_state` and result/delivery references. No new task endpoint or client-side dispatch loop is needed.

Admission remains `GET/PUT /users/me/runtimes/{id}/admission`. PUT fields are `request_id`, `expected_revision` (string) and `paused` (boolean). Resume retains the original pause request ID and current revision. For native profiles it is an owner registry transaction and works offline without a remote probe; it does not resume or repeat business work. Schedule and heartbeat code must enter the existing selected-target task/admission boundary, not call the CLI or bridge itself.

No new task HTTP API, scheduler, queue, task ID, or tool name is introduced. Reuse these existing computer tools:

| Operation | Contract |
| --- | --- |
| `computer_targets` | `target_id`, name, capabilities, `availability=not_probed`, optional `agent_protocol` |
| `computer_task_submit` | Existing `kind`, task, target/work directory and bounded artifact declarations; returns the durable `operation_id` |
| `computer_task_get` | Read persisted operation, delivery and result facts; no CLI call |
| `computer_task_result` | Collect receipt-bound evidence for the same operation; never submit the business task again |
| `computer_task_recover` | Read current native metadata for stored references; cannot prove restored conversation history |
| `computer_workspaces` | Read supported workspace metadata within the same selected target |
| `computer_task_cancel` | Record cancellation intent; does not prove process termination |
| `computer_task_artifact`, `computer_task_logs` | Existing committed producer references and explicit diagnostic flow |

`computer_task_followup` remains available for its existing supported legacy operations; this adapter does not manufacture asynchronous `agent.start` receipts or promise TUI follow-up for finite Pi invocations. Unsupported operations fail closed.

Keep `operation_id`, original `run_id`, attempt, generation, request hash, target fingerprint, workspace/pane/terminal references and producer task identity. Reconnection queries the original operation. `unknown`, an interrupted write or an incomplete receipt is never permission to submit again. Raw `exec` on a configured Herdr mount is gated through computer tasks, so it cannot bypass unresolved operations or paused admission.

General bridge consumers, including Hooks and metadata-derived clients, receive a file-only view with no Exec or tunnels. Ordinary authorized workspace file tools remain available. Generic media storage and attachment-path ingestion cannot use that view: a managed PC cannot be silently used as a NAS media backend or turn a raw HostFS path into a delivered attachment. Producer-backed committed references retain the existing delivery route.

The existing state fields remain authoritative and separate: `state` / `phase`, `process_stopped`, `cancel_requested`, `pi_result`, `continuity` and `delivery`. A CLI exit 0 is only a transport acknowledgement. A verified invocation receipt can establish process exit; the correlated official Pi JSON completion establishes `pi_result`. Only a committed NAS delivery reference proves file delivery. Neither establishes independent acceptance of the user's objective.

## Windows host interface

The host lane owns the existing Windows bundle and independently supervised Herdr service. Runtime owns and terminates its short-lived Exec child tree on disconnect or timeout; therefore CLI execution must not start the service. No new listener or service is introduced by Memoh.

The tested official baseline is Herdr v0.9.5, source [`2ecf19a92fc3a7fff75b6ce3655ef04f4625ec01`](https://github.com/herdrdev/herdr/tree/2ecf19a92fc3a7fff75b6ce3655ef04f4625ec01). The official [Windows release ZIP](https://github.com/herdrdev/herdr/releases/download/v0.9.5/herdr-windows-x86_64.zip), SHA256 `d2c6c9c971c43a0bfdde8c692a9a1a7a45c3e4807cd01ae45662b4734fac1cbf`, includes `herdr.exe`, ConPTY dependencies and license files. Do not distribute only the executable as if it were the complete release.

The host service and CLI must use the same profile:

- `HERDR_CONFIG_PATH = <herdr_config_root>\config.toml`
- `XDG_CONFIG_HOME = parent(<herdr_config_root>)`
- Default session: `HERDR_SESSION` absent, `HERDR_SOCKET_PATH = <root>\herdr.sock`
- Named session: `HERDR_SESSION = <session>`, `HERDR_SOCKET_PATH = <root>\sessions\<session>\herdr.sock`

There is no official `HERDR_CONFIG_ROOT` environment variable. The Memoh profile field is converted to the official variables above. The service's configured shell must accept the existing Windows Bash launcher and Pi/provider must already be configured locally. Windows host producer integration remains a separate bundle responsibility; no HostFS import substitutes for producer proof.

The adapter uses the `packages/runtime` Exec contract at Memoh base `9601ac062a3b65477db8db76bcaa521265ba7ee2` (Runtime package 0.22.0), with the existing `fs`, `exec`, `host_fs` capabilities. It requires neither PTY nor GUI support from Runtime. Fixed PowerShell source receives JSON over stdin, launches the absolute `.exe` without shell evaluation, and returns bounded output. Complete EXIT plus clean EOF is required. Calls have a 30-second transport limit; there is no automatic retry. Windows arguments are bounded to 24,000 UTF-16 units; oversized requests are rejected, never truncated.

## Integration and verification

Minimal server changes are `internal/nativeherdr`, provider resolution/configuration in `internal/agenvo`, core wiring, owner handler/userruntime wrappers, the existing workspace Exec gate, SQL/store native identity branches and migration 0176. OpenAPI and SDK are regenerated. The Windows bundle needs the existing Runtime, complete official Herdr release, matching local profile and producer; no Pi/Magpie core change or new OAuth client is required.

Reproducible official CLI probe (Linux, isolated synthetic server, no provider credentials):

```sh
python3 scripts/probe-herdr-contract.py --binary /path/to/official-v0.9.5/herdr --output-dir /tmp/herdr-contract
go test -race ./internal/nativeherdr
go test ./internal/agenvo ./internal/computertask ./internal/userruntime ./internal/workspace
```

The probe pins the Linux binary SHA256 and refuses a mismatch. Its normalized observations are committed in `internal/nativeherdr/testdata/herdr-v0.9.5.json`. Tests also cover interrupted Exec streams, stale fingerprints, scope/revocation, unknown dispatch without replay, persisted operation reconstruction and producer authority. Isolated PostgreSQL tests cover migration/rollback, RLS, admission concurrency, configuration CAS and transfer credentials.

These checks do not establish deployed Windows compatibility. Final acceptance still requires the parent-approved Windows bundle: short-lived CLI against the independent service, Runtime disconnect/reconnect and process restart, one business dispatch, correlated Pi result and committed producer delivery, plus credential revocation and target isolation. No production credential, enrollment or cutover was performed for this change.
