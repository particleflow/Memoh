# Native Runtime cutover from an Agenvo connection

This is an operator cutover procedure, not a deployment script. It does not enroll a Runtime, create credentials, revoke a grant, change an ACL, run a task, or switch a live installation. Record the actual approved Bot, Runtime, mount and connection IDs before applying it. A native Remote Runtime has provider `memoh`; its mount is still a remote workspace target. The literal target ID `native` means the server workspace, **not** the Windows Runtime.

## Preserve evidence and stop the retired automatic route

Use the existing MCP connection's **Enabled** switch to set `is_active=false`. Keep the connection, OAuth row, Agenvo runtime registration, transfer credential record, old mount, operation rows, queues, and history. Disabling is sufficient to stop the ordinary paths below before any old MCP/OAuth transport call. It does not revoke the third party's authorization or terminate work already accepted by Herdr.

Do not delete the old MCP connection: its OAuth row has an `ON DELETE CASCADE` foreign key. Do not detach the old mount or revoke the Runtime as a retirement shortcut either. Runtime revocation deletes its mounts; `bot_workdirs.remote_binding_id` cascades on mount deletion, and deleted workdirs clear `bot_sessions.workdir_id` and `schedule.workdir_id`. That changes historical bindings. The owner-computer, Bot-target and account-grant list queries exclude Agenvo registrations whose referenced MCP connection is explicitly inactive. This removes the retired computer from these selectors while retaining every underlying row. An unavailable but still active connection keeps its existing visibility and diagnosis.

Inventory every old registration/connection used by the retiring path, including aliases and mounts granted to other Bots. Disable the specifically retired connection IDs in their owning source Bots. Do not disable other OAuth clients or connections merely because they share a provider name. Retain the identifier mapping for review; do not copy or log token/credential values.

| Existing interface | Cutover use |
| --- | --- |
| `GET /users/me/runtimes` | Inspect registered Runtime IDs/provider metadata. Retain the old registration. |
| `GET /users/me/computer-access` | Inspect existing account computer grants. |
| `GET /bots/{bot_id}/workspace-targets` | Confirm old/new mount IDs. The new target has provider `memoh` and the expected Runtime ID. |
| `PUT /bots/{source_bot_id}/mcp/{connection_id}` | Set `is_active:false` through the existing Enabled switch. This is a full `UpsertRequest`, not PATCH: preserve the saved `name`, URL/transport, headers and other connection fields. Its fields are flat (`url`, `headers`, `transport`), not a nested `config`. Keep the same OAuth resource and do not change `auth_type`; the service preserves the existing OAuth row on a same-resource toggle. The UI switch performs this using its saved snapshot without probing. |
| `PUT /bots/{bot_id}/workspace-targets/remotes/{runtime_id}` | Grant/mount the already enrolled, configured native Runtime where needed, within the approved Bot scope. |
| `PUT /bots/{bot_id}/workspace-targets/primary` with `{"target_id":"<actual new mount ID>"}` | Set the default for future unbound work. This does not migrate an existing session's pinned computer or workdir. |
| `PUT /bots/{bot_id}/schedule/{schedule_id}` with `{"enabled":false}` | Temporarily stop only schedules that still target an old session/workdir, until their future execution target has been explicitly selected and validated. Existing-session schedules inherit that session's binding; changing the Bot primary is insufficient. Keep their records and run history. |
| `DELETE /bots/{bot_id}/workdirs/{workdir_id}` with no body | This existing endpoint **archives** only the selected workdir metadata (`archived_at`); it does not delete the directory, row or session binding. It removes the retired directory from new-session selectors and refuses new bindings. Confirm the exact old workdir IDs first; never substitute the mount-delete or Runtime-revoke endpoints. |
| `GET /bots/{bot_id}/workdirs?include_archived=true` | Read preserved workdir metadata, including archived paths and original target bindings. |

Apply the cutover in this order: inventory the actual IDs and affected schedules; pause those schedules; disable their old MCP connections; explicitly select the verified Windows native mount as primary; archive only the old workdirs; then verify the three selection lists no longer contain the retired IDs. Do not infer retirement from a changed label or an offline badge. The MCP management record remains disabled for configuration/history inspection.

For each schedule being continued, read `GET /bots/{bot_id}/schedule`, then update that same schedule with `enabled:false` and its complete replacement `execution` block. A `new_session` schedule uses the new native `workdir_id`. An `existing_session` schedule must use a new session bound to that workdir (`target_session_id`); omit `workdir_id`, `runtime_type`, `bot_agent_id` and `acp_agent_id` in that mode. Preserve the required model, reasoning and time-budget fields: `execution` is replaced as a block. Re-enable only after checking the new target. The schedule ID, counters and old run logs remain intact. Existing-session history can still be selected explicitly; archiving does not rewrite it, and inactive-connection authorization still prevents old PC execution. No automatic schedule or historical-session migration is performed.

Do not silently copy an unknown old operation to the new target. Preserve its original task/run/operation, target fingerprint, attempt, generation and queue identities. A new native invocation is separate work and must not be used as a recovery replay. Queued artifacts retain their original producer manifests, receipts and delivery ledger; do not reassociate them with the native Runtime or move them through HostFS as a shortcut. Disabling the old connection can stop its credential-backed queue progress; record that state rather than recreating the work. Native Runtime integration does not transfer ownership of old in-flight work.

Select the new Windows mount explicitly for new conversations and future scheduled work. Keep old sessions and workdirs intact. Verify independent NAS and other-PC work separately; do not stop unrelated schedules. The native profile, Bot permissions, admission state and selected-PC boundary must all pass before allowing new tasks.

## Why ordinary reads do not refresh the disabled connection

- Account `ListRuntimes`, remote target listing and account grants filter the explicitly inactive Agenvo connection in their SQL queries, before a probe or selector entry is created. Direct reads of a known historical mount remain available as metadata. If an old execution target is explicitly supplied, the resolver calls `OwnedHTTPMCPConnectionBot`, whose SQL requires `connection.is_active`, and refuses it before the SourceCaller, gateway, OAuth token getter or network probe.
- Selected-PC `Manager.ListWorkspaceTargets` reads only the chosen mount. The native provider uses the existing Runtime connection and the typed native caller; it does not fall back to the old MCP connection.
- Federation discovery uses `ListActiveByBot`. An already cached tool route also rechecks the current active connection and update timestamp immediately before a call. A cache or process restart does not reactivate the connection.
- Opening an existing MCP details page sets `probeOnCreate=false`. The Enabled toggle performs only its update. The page's OAuth status read calls `GetStatus`, which reads stored metadata and does not refresh an expired token. Automatic probing is associated with creating a new MCP entry, not opening this existing one.
- Reading stored computer-operation records does not execute or replay them. Public task/result APIs still enforce current mount/connection permissions and may refuse an inactive legacy target; preserving history is not a reason to bypass those checks. A stored `unknown` remains unknown.

`is_active=false` is not a universal ban on explicit management actions. The existing manual MCP **Test** / `POST .../probe`, explicit OAuth authorization, or saving a changed old endpoint can deliberately invoke that connection even while it is inactive. Do not invoke those controls as part of the native cutover or attempt to repair the retired path by authorizing again. This procedure does not change general MCP debugging semantics or add an approval layer.

An already in-flight old request or schedule fire cannot be recalled by flipping the flag. Schedule disable removes future cron entries; it does not prove an already-started fire did not run. Stop initiating affected work, record any uncertain outcome, and inspect the same operation/queue identity after the current call settles. Never infer non-execution from a lost response and never retry business submission with a fresh ID.

## Isolated acceptance evidence

`TestNativeRuntimeCutoverStopsLegacyAutomaticOAuthPaths` in `internal/db/native_runtime_cutover_integration_test.go` runs against an isolated migrated PostgreSQL database. It uses synthetic Runtime/configuration/token records and a synthetic OAuth HTTP endpoint only. Positive controls prove the old active provider and federation discovery were reachable before disabling.

After the same-resource MCP disable and native-primary selection, the test verifies that repeated owner/target/grant listings exclude the retired IDs while keeping the native target. It also exercises explicitly selected native inventory, an expired-token status read, cached and restarted federation paths, and one native metadata call. There are zero old transport calls and zero OAuth HTTP requests. An unknown operation returns its original ID without a new claim; its revision/details/fingerprint, bound old workdir/session, synthetic OAuth row and transfer credential record remain unchanged. The selector assertions failed against the prior queries before the minimal filtering change.

Run with a disposable local PostgreSQL instance:

```sh
TEST_POSTGRES_DSN='postgres://postgres@127.0.0.1:<isolated-port>/postgres?sslmode=disable' \
  go test -tags=integration ./internal/db \
  -run '^TestNativeRuntimeCutoverStopsLegacyAutomaticOAuthPaths$' -count=1 -v
```

This test does not attest to a particular deployed Windows machine, live queue drain, browser screenshot, or actual production switch. Those require the parent task's approved concrete deployment/acceptance scope. No real credential values are needed for this source-level migration test.
