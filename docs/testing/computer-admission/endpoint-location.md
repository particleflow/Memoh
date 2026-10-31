> Historical record: this describes an earlier Agenvo implementation. Its MCP probe and endpoint configuration routes have been retired and are not current operating instructions.

# Owner endpoint location (configuration only)

Base checkpoint: `8d583a14f355630a5f4d71976c8265ec1575846c`. This increment does
not implement a stop command, acknowledgement message, Windows sender or a new
control identity. The [admission gate](README.md) is still not a stop permit.

## Fixed owner API and persistence

`GET /users/me/runtimes/{id}/agenvo-endpoint` uses the existing authenticated
account owner and owned, unrevoked Agenvo registration/MCP connection. It makes
no native call. Its fields are:

- `connection_id`, `device_id`, `instance_id`: the current stored execution mapping.
- `target_fingerprint`: the existing execution Target fingerprint.
- `configuration_fingerprint`: SHA256 of the complete canonical registration JSON
  (including this locator); exact JSON integers are retained. It is an optimistic
  concurrency token, not an authentication credential or native identity proof.
- `locator`, when configured: `{program_path, host_root}`. Absence means unconfigured;
  there is no fallback to task cwd, Agent config directory, PATH or a fixed layout.

`PUT` on the same path accepts only:

```json
{
  "expected_configuration_fingerprint": "<value returned by GET>",
  "locator": {
    "program_path": "D:\\Endpoint\\endpoint.mjs",
    "host_root": "E:\\Host data\\owned"
  }
}
```

An explicit `locator: {}` clears the configuration; omitted/null locator is
invalid. Unknown keys (including route/owner/command/credential fields) are
rejected. Both nonempty locations must be explicit Windows absolute paths,
lexically valid under the existing Windows path parser, with no dot traversal,
ADS, control characters or command-line quoting. Forward slashes, spaces and
Unicode are supported. Values are preserved as supplied; no remote path is
expanded, resolved, created, opened or executed. A UNC location is not proof that
its filesystem belongs to the approved local host.

PUT uses the existing owner-authorized `workspace.list` read-only native probe,
then the existing registration compare-and-swap update. Only the independent
`provider_config.owner_endpoint` field changes. No migration is added. Migration
0175 still rejects configuration changes under a live pause. Configure the
location before pausing; a stale full configuration is HTTP 409 `http.conflict`.
All responses, including anonymous/foreign-owner failures, retain `no-store`.

Execution Target does **not** include this owner-only field. Existing operation
fingerprints, original receipts and the native call comparison remain unchanged.
The existing Agent-profile updater now preserves other registration fields rather
than rebuilding the complete document from Target. Concurrent changes still use
the same full JSON CAS; native mapping/profile changes cannot be silently merged.

After an unknown PUT result, GET reconciles the stored location/fingerprint.
Neither call changes an operation, claims another task, starts Pi, creates a
reader or authorizes shutdown. A stale update fails instead of guessing whether
the prior write succeeded.

## Windows alignment still required

The future owner-to-host call must retain the existing authenticated transport;
file-transfer credentials and a local JSON file must not substitute for it. This
API only supplies a registration-scoped location. It does not prove coverage of
different connections, other owners, differently identified instances or all
processes on a physical machine. Those cases cannot receive a stop permission
without an authoritative aligned ownership contract; names, addresses and equal
path strings are insufficient.

Fields already available for that next contract are the runtime ID from the
route; owner from authentication; connection/device/instance; configuration and
target fingerprints; admission request/revision; and the existing reconciliation
set of operation ID/revision, attempt/generation/request hash/Pi session, original
pane/terminal and receipt/history/file collection facts. They must be revalidated
before use, not treated as a lasting proof obtained while editing configuration.

The Windows investigation confirms only `status --root` and the original running
host's stdin `status/stop` commands. A Herdr pane does not own that stdin handle;
there is no official `host.stop`. Its execution envelope also does not convey a
Memoh principal independently verifiable by that host. The caller's authorized
Agenvo execution is not proof embedded in a local JSON request. Consequently the
automatic step from a paused/reconciled owner to authenticated host stop is not
available under the present interfaces; `task_handoff_unverified` remains.

No new listener, credential, local-file authentication scheme, parent-process
guess, stop permit or acknowledgement parser is added to fill that gap. The
Windows author owns any future finite entrypoint/launch form and durable journal
interface. `program_path` identifies an existing program file, not a shell command
string; this patch does not assume an interpreter or invoke it. Existing task
observation, result collection and identity-preserving recovery remain usable
independently. Native `pane.run` acceptance would only prove input delivery, not
host verification or stop completion.

## Compatibility and rollback

Absent configuration preserves existing task behavior but cannot enable handoff.
An old server has no locator API and may discard this field when rewriting an
Agent profile; that loss must remain unconfigured/fail-closed for handoff, never a
guessed default. It does not invalidate the stored execution fingerprint.

There is no schema rollback for this increment. Migration 0175's live-pause down
prohibition remains intact. The existing migration CLI's `down` invokes `m.Down()`
and rolls back **all** migrations; it is not a command to undo only 0175 and must
not be used as a one-step rollback recipe. No rollback, deployment or production
configuration is performed in this work.

## Verification

All checks use synthetic records in the existing isolated cloud environment; no
production access, Windows process, paid model or new credential is involved.

- `go test -race -v ./internal/agenvo -run 'TestEndpoint|TestAgentProfile' -count=1`:
  exit 0; [output](endpoint-evidence/unit-final.txt). Covers owner/connection access,
  invalid paths, unchanged old execution fingerprints, independent profile fields,
  exact large JSON integers, lost configuration acknowledgements and stale CAS.
- `go test -v ./internal/handlers -run TestOwnerEndpointHTTP -count=1`: exit 0;
  [output](endpoint-evidence/http.txt). Uses the actual global JWT/HTTP middleware
  chain, including anonymous 401/no-store, foreign-owner denial, strict body
  rejection, expected fingerprint and pause errors; only native read-only probes.
- `go test -tags=integration -race -count=1 -v ./internal/db -run '^TestComputerAdmissionEndpoint' -timeout=90s`:
  exit 0, one actual PG test, zero SKIP, 3.057s; [output](endpoint-evidence/postgres.txt).
  A unique disposable database uses the original isolated PG identity. The unknown
  operation/revision/recovery-reader stays byte-equivalent, same claim deduplicates,
  locator persists through a new store instance, foreign/stale/paused writes reject.
  Its name is covered by the existing admission PG gate; no workflow change.
- Affected Go lint 2.10.1: exit 0 [output](endpoint-evidence/lint-final.txt).
  [Initial finding](endpoint-evidence/lint.txt) was a misspell interpretation of an
  invalid-path fixture; changed only that fixture component, without exemptions.
- Real staged `node .husky/checks.mjs`: exit 0, 14.55s, including the affected Go
  packages and generated Web lint; [output](endpoint-evidence/aggregate.txt).
  Normal package runs are not replacements for the explicit PG test above.
- Swagger and SDK regenerated normally, preserving all 366 previous operations
  and adding exactly two. SDK/Web typechecks exit 0:
  [SDK](endpoint-evidence/sdk-types.txt), [Web](endpoint-evidence/web-types.txt).
- Current Server build exit 0 [output](endpoint-evidence/build.txt). Existing
  loopback Server18400/Web18402 and synthetic PG were reused. Real API
  configure/read/conflict/pause rejection, **actual Server restart** with identical
  configuration, then the existing task list/detail/observation UI passed:
  [runtime output](endpoint-evidence/runtime.txt),
  [assertions](endpoint-evidence/browser-proof.json),
  [inspected screenshot](endpoint-evidence/original-task-observation.png).
  Existing task revision/fingerprint/unknown outcome remain visible and the full
  operation snapshot is unchanged. Eight `workspace.list` probes; no process,
  workspace or Agent mutation. The temporary MCP URL and locator were restored.

The screenshot verifies the existing task remains observable; it is not a new
configuration UI, filesystem attestation or proof of task success. Selected
outputs are byte-preserved in the [hash manifest](endpoint-evidence/checksums.json);
private configuration, identity tokens, Server logs and DB snapshots are excluded.
About 734 MB of this task's regenerable Go cache older than two hours was reclaimed
before checks; no source, history, evidence, database or user file was removed.
Human QA remains unconfirmed. Actual Windows program existence, host ownership,
stop/restart safety and end-to-end handoff are not established.
