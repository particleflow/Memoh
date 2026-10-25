# Native Runtime cutover from an Agenvo connection

This is an operator cutover procedure, not a deployment script. It does not enroll a Runtime, create credentials, revoke a grant, change an ACL, run a task, or switch a live installation. Record the actual approved Bot, Runtime, mount and connection IDs before applying it. A native Remote Runtime has provider `memoh`; its mount is still a remote workspace target. The literal target ID `native` means the server workspace, **not** the Windows Runtime.

## Preserve evidence and stop the retired automatic route

Use the existing MCP connection's **Enabled** switch to set `is_active=false`. Keep the connection, OAuth row, Agenvo runtime registration, transfer credential record, old mount, operation rows, queues, and history. Disabling is sufficient to stop the ordinary paths below before any old MCP/OAuth transport call. It does not revoke the third party's authorization or terminate work already accepted by Herdr.

Do not delete the old MCP connection: its OAuth row has an `ON DELETE CASCADE` foreign key. Do not detach the old mount as a default migration step either. `bot_workdirs.remote_binding_id` cascades on mount deletion, and deleted workdirs clear `bot_sessions.workdir_id` and `schedule.workdir_id`. That can alter historical bindings and future routing. Only consider detach after a separate reference inventory establishes that it will preserve the intended records. The inactive old mount may remain visible as `connection_unavailable`; this is not a request to authorize it again.

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

Do not silently copy an unknown old operation to the new target. Preserve its original task/run/operation, target fingerprint, attempt, generation and queue identities. A new native invocation is separate work and must not be used as a recovery replay. Queued artifacts retain their original producer manifests, receipts and delivery ledger; do not reassociate them with the native Runtime or move them through HostFS as a shortcut. Disabling the old connection can stop its credential-backed queue progress; record that state rather than recreating the work. Native Runtime integration does not transfer ownership of old in-flight work.

Select the new Windows mount explicitly for new conversations and future scheduled work. Keep old sessions and workdirs intact. Verify independent NAS and other-PC work separately; do not stop unrelated schedules. The native profile, Bot permissions, admission state and selected-PC boundary must all pass before allowing new tasks.

## Why ordinary reads do not refresh the disabled connection

- Account `ListRuntimes` and remote target listing still call the `agenvo.Service.Probe` **method** for a retained Agenvo registration. Its resolver first calls `OwnedHTTPMCPConnectionBot`, whose SQL requires `connection.is_active`. An inactive row returns before the SourceCaller, gateway, OAuth token getter or network probe. The projected status is `connection_unavailable`, not `authentication_unavailable`.
- Selected-PC `Manager.ListWorkspaceTargets` reads only the chosen mount. The native provider uses the existing Runtime connection and the typed native caller; it does not fall back to the old MCP connection.
- Federation discovery uses `ListActiveByBot`. An already cached tool route also rechecks the current active connection and update timestamp immediately before a call. A cache or process restart does not reactivate the connection.
- Opening an existing MCP details page sets `probeOnCreate=false`. The Enabled toggle performs only its update. The page's OAuth status read calls `GetStatus`, which reads stored metadata and does not refresh an expired token. Automatic probing is associated with creating a new MCP entry, not opening this existing one.
- Reading stored computer-operation records does not execute or replay them. Public task/result APIs still enforce current mount/connection permissions and may refuse an inactive legacy target; preserving history is not a reason to bypass those checks. A stored `unknown` remains unknown.

`is_active=false` is not a universal ban on explicit management actions. The existing manual MCP **Test** / `POST .../probe`, explicit OAuth authorization, or saving a changed old endpoint can deliberately invoke that connection even while it is inactive. Do not invoke those controls as part of the native cutover or attempt to repair the retired path by authorizing again. This procedure does not change general MCP debugging semantics or add an approval layer.

An already in-flight old request cannot be recalled by flipping the flag. Stop initiating affected work, record any uncertain outcome, and inspect the same operation/queue identity after the current call settles. Never infer non-execution from a lost response and never retry business submission with a fresh ID.

## Isolated acceptance evidence

`TestNativeRuntimeCutoverStopsLegacyAutomaticOAuthPaths` in `internal/db/native_runtime_cutover_integration_test.go` runs against an isolated migrated PostgreSQL database. It uses synthetic Runtime/configuration/token records and a synthetic OAuth HTTP endpoint only. Positive controls prove the old active provider and federation discovery were reachable before disabling.

After the same-resource MCP disable and native-primary selection, the test verifies repeated owner/target listings, explicitly selected native inventory, an expired-token status read, cached and restarted federation paths, and one native metadata call. There are zero old transport calls and zero OAuth HTTP requests. It also checks that an unknown operation returns its original ID without a new claim, and that its revision/details/fingerprint, bound old workdir/session, synthetic OAuth row and transfer credential record remain unchanged.

Run with a disposable local PostgreSQL instance:

```sh
TEST_POSTGRES_DSN='postgres://postgres@127.0.0.1:<isolated-port>/postgres?sslmode=disable' \
  go test -tags=integration ./internal/db \
  -run '^TestNativeRuntimeCutoverStopsLegacyAutomaticOAuthPaths$' -count=1 -v
```

This test does not attest to a particular deployed Windows machine, live queue drain, browser screenshot, or actual production switch. Those require the parent task's approved concrete deployment/acceptance scope. No real credential values are needed for this source-level migration test.
