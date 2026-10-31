# Specialist native-run observation in the unified candidate

Product merge: `29fd18fdd81dca8e747206b4d5344e3d0a6338dc`.
Tree: `0461380b0099fd0a6c6cfd0e8bcd5779ed786c24`, exactly equal to the reviewed
`82691d889c2fed40df50f72d1b32232028dfaf93` source tree. The merge preserves both
`150219908c316d75272b77c65148436fac3b7074` and the source branch ancestry:

- `fcbe5c2941bff9def230189b49499daf91f1049d`: exact size exceptions for the three
  generated Swagger outputs; ordinary 1 MiB checks and language checks remain.
- `82691d889c2fed40df50f72d1b32232028dfaf93`: persisted native-run projection and
  ordinary specialist-session history navigation.

There were no conflicts and no additional product edits. The prior shared Web
401/session-generation fix remains in the ancestry and in the regression run.
No image publication, main merge, production deployment or configuration change
was performed by this integration.

## Contract and scope

The existing delegation response gains four optional fields:
`target_run_id`, `target_run_state`, `target_run_created_at`, and
`target_run_updated_at`. All 279 existing Swagger path entries, existing required
fields and other definitions are unchanged. There is no migration in this diff;
`contract-proof.json` records the check.

These fields describe the exact persisted native run, subject to the existing
caller authority and Bot/session/team/invocation/visibility fences. They are
not a live connection check, a new runtime, a queue position, or confirmation
that results were delivered. The UI keeps native-run state, delegation state,
return-delivery state and descendant tasks separate. Missing optional fields
remain explicitly unavailable rather than inferred from a session UUID.

Opening history revalidates the source relation and the target's ordinary
session permission, then reuses existing Bot selection and workspace-tab
navigation. The native session history may contain other turns. The shared
component's history action is optional; a host without that action stays read
only. No Harmony or Windows source was changed.

## Actual combined checks

The commands, exit codes and unchanged published output hashes are in
`evidence.json`. No previous branch's green CI is substituted for this head.

| Check | Observed result |
| --- | --- |
| Real staged aggregate | exit 0, 146.60 seconds |
| Hook regressions within aggregate | 42 passed, zero skipped |
| Aggregate Web lint and affected Go lint/test | exit 0; botdelegation, db, handlers passed; spec has no test files |
| Affected Web tests including prior 401 and parent reconnect fixes | 124 passed in 5 matched files, exit 0 |
| Complete Web and SDK typechecks | both exit 0 |
| Web build and current-source Server build | both exit 0 |
| Official runtime/contextview boundary | exit 0 against `bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046` |
| UI contract | exit 0, existing 3 warnings retained |
| Real PostgreSQL delegation regression | PASS, zero skip; fresh isolated migration chain at 174, dirty=false |
| Real browser + current Server + existing synthetic PG | PASS, described below |

The test command included one nonexistent `useBotDelegations.reconnect.test.ts`
selector; it matched nothing. The result is explicitly five executed files,
not six. The Web build retains its existing large-chunk warning. The PG test
covers the exact admitted run after reconstructing the reader, wrong Bot,
session, user, team and invocation, an admission placeholder, later unrelated
turns, fencing invalidation, deleted sessions and history reset. It creates and
cleans only its uniquely named test database using the existing test identity.

## Browser and persistence

The current combined Server was built and started on the existing isolated
loopback API listener `http://127.0.0.1:18400`; the current-source Web remained at
`http://127.0.0.1:18402`. The existing synthetic config and
`integration_web_types` PG were reused. The previous owned fixture Server was
stopped normally; its binary and configuration were retained. No explicit migration command was issued against that existing fixture
database; its schema remained 174, dirty=false.

The run reused an already-completed synthetic supervisor-to-specialist handoff
and its existing child/history. It did not create a model sample or submit a
new task. Actual authenticated HTTP returned the exact stored run ID, state and
timestamps while retaining the original delegation/delivery state. Anonymous
HTTP returned 401 with `no-store`.

- [Supervisor detail](specialist-run.png): native run and return delivery are
  distinct, with the persistent-state limitation visible.
- [Root run and children](root-and-children.png): the root run remains visible
  above the separate existing subagent entry.
- [Ordinary specialist history](specialist-native-history.png): the existing
  session and its final visible text opened through the original chat UI.

Only one detail response was fault-injected as 503. It removed the unconfirmed
run; explicit Reload re-read the same delegation/run from the real API.
Navigation then returned real target-session and target-history HTTP 200s.
`browser-proof.json` records zero API mutations and zero page JavaScript errors.
`persistence-proof.json` verifies all 25 pre-existing delegation rows were
unchanged, the selected run ID/state were unchanged, and active runs remained
zero. Screenshots were inspected.

The fixture still reports unavailable workspaces: the browser recorded three
other 404s and six 503s, and the history screenshot retains the workspace toast.
Its existing workspace bind-source setup is incomplete. This run proves stored
observation/navigation; it does not prove workspace execution, live Herdr/Pi,
PC reconnect, real model/provider calls, or a native phone binary.

## Failures and bounded environment recovery

All relevant earlier failures are retained instead of overwritten:

1. An explicit 600-second hook budget leaked into hook-test subprocesses whose
   assertions expect the default 180 seconds. Removing this invocation override
   restored the intended test environment; no assertion was changed.
2. The first Go invocation omitted the existing module-cache override. The
   configured override then proved to be a dangling symlink. Its original
   task-owned `/tmp` target was recreated and normal public dependencies restored;
   no alternate credential, dependency version or network route was used.
3. Compiler and later linker output hit ENOSPC. Three bounded cleanups removed
   only enumerated task-owned regenerable Go cache entries: 952,343,602;
   1,143,230,522; and 699,193,182 bytes. The exact lists are included. No global
   prune, source/history/log/database/original deletion or data migration occurred.
   The final aggregate and Server build passed after recovery.
4. The first existing handoff fixture referenced a missing media original and
   returned HTTP 500 (`media asset not found` in the private local server log).
   No original was invented or business task replayed. The final run used another
   already-existing completed handoff without that unrelated artifact dependency.
5. The first history probe expected a session-nested messages URL. The native SDK
   uses `/bots/{bot_id}/messages?session_id=...`. The probe was corrected to that
   real contract, then the full browser sequence passed. Product code did not change.

An optional process-environment read was denied by the OS and was not retried.
The fixture was started using the already-known, permitted test configuration
and documented `CONFIG_PATH`; no process credentials were recovered. Private
server logs and fixture inputs remain local, outside the published evidence.

## Next acceptance

The exact pushed integration head still requires its own remote CI verification.
The expired `gh` API identity was not retried. The original draft PR is retained;
this document is not publication/deployment approval. The next onsite check is
reading the already-existing supervisor/specialist handoffs with this UI, without
creating another model task. Human QA and the real PC chain remain unclaimed.
