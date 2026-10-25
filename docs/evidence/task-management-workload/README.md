# Task management: real local workload and Server restart acceptance

Frozen product: `cbcee15a17dc87fd02eb4c198fba0aa45eb3fa45`.
Independent evidence branch: `test/task-management-workload-recovery`.
No application, configuration schema, Agent core, migration, generated SDK,
workflow, upload/download or image-terminal code changes. The original
publication branch is unchanged. Its denied image publication is not retried.

## Previously missing combination

Earlier [native-content acceptance](../../testing/native-task-content/README.md)
proved real Web/HTTP/PG routing but its peer only returned synthetic prompt
receipts. [Parent reconnect acceptance](../../testing/task-parent-reconnect/README.md)
proved polling composition, not a live workload surviving a Server restart.
This increment tests those boundaries together, without repeating the prior
full Web, hierarchy, batch-reader or CI suites.

The current interactive follow-up capability belongs to an existing interactive
Agent. `NativeHerdrProfile.target` currently permits an Agent profile only with
`pi-json-v1`; `Source.NativeContent` and `Service.followup` reject batch
interactive follow-up. These are distinct paths. This test **does not** prove
interactive follow-up on the new native Runtime batch path, and does not enable
that unsupported feature.

## Environment and actual execution

Reused the owned cloud development Web application at `127.0.0.1:18402`, Server
at `127.0.0.1:18400` and isolated PostgreSQL `integration_web_types`, schema 176.
No production data, model, OAuth, field device, credential creation or external
network request was used. The already-running Server build was
`384414933c8734b5e5bca663b99d9d7e292b4389`; the exercised `computertask`, `agenvo`,
`sessiontask`, native task handlers, auth and server source trees are identical
to cbcee. The later retirement-list SQL is not reverified by this older binary.
Vite served the frozen cbcee Web source.

The fixture reused an existing synthetic, uncredentialed, loopback MCP
connection. It temporarily selected its interactive profile and seeded only
one new parent, child and operation. A synthetic peer implements the existing
`workspace.list`, `pane.get`, `agent.get/read/prompt` method shapes. It is **not
the official Herdr executable, native Runtime Exec, Pi or Windows**. On the
two exact allowlisted `agent.prompt` texts it really launched the bundled
finite Python workload. No user-provided text is executed as a command.

The two processes emit known SHA-256 values and exit 0. The first takes three
seconds; the second stays alive until the test releases it (hard bound 60
seconds). The second peer response deliberately reports unknown **after** the
process starts. This models lost confirmation without assuming that unknown
means nothing executed.

| Combined scenario | Actual observation |
| --- | --- |
| Two immediate Web submit clicks | Real authenticated HTTP and PG CAS accepted one request; exactly one local process launched. UI continued to say input accepted, not work completed. |
| Local output | Existing bounded `agent.read` sample displayed actual process START/hash/EXIT output, with `terminal_snapshot_unverified`. No full-log or per-instruction evidence claim. |
| Lost confirmation after execution | Second process started once; operation follow-up and browser request UUID remained unknown. |
| Browser offline + actual Memoh Server process restart | Restarted only the owned Server with the same binary/configuration/PG. The original workload PID stayed alive. Reload reopened the same operation/request, disabled resubmission and read the original output. Prompt count remained two. |
| Workload exits successfully | Terminal sample showed EXIT 0; operation stayed `submitted`, business/process outcome unknown, follow-ups `accepted` and `unknown`, revision 6. Sampled exit did not manufacture a verified receipt. |
| Actual synthetic Runtime revocation | Real handler returned 404/no-store, cleared visible output, stopped automatic reads. An explicit unauthorized follow-up reached no peer and launched no process. Restoring the same synthetic authorization only read the original task. |
| Anonymous/wrong parent/wrong Bot reads | 401/404/no-store and zero peer calls. No mocked permission responses. |
| Native reference readback | Existing operation target, workspace and pane IDs matched; final peer metadata was online and reference-match observed. No name/time-based inference or task reassignment. |
| Existing persisted Pi batch and foreground task | Real content HTTP returns supported=false/followup_available=false. No prompt, task mutation, or unsupported control button was added. |

All temporary connection/profile/revocation fields were restored. The complete
serialized **pre-existing operation rows** have the same SHA-256 before and
after. The newly seeded synthetic operation and its two receipts are retained
for review. Both workloads exited; the fixture peer stopped; the owned dev
Server remains healthy. No database, original file or Git history was removed.

The unrelated workspace-selector request was replaced with an empty fixture to
avoid device discovery. Task list/detail/content/follow-up/observation calls
used the actual Server and PG; they were not replaced with browser responses.
The empty selector/footer is not evidence of workspace provisioning or device
offline status.

## Failures retained, not relabeled as a full green run

1. `node browser.mjs` completed every scenario assertion, then exited **1**:
   a response-body read raced browser closure. [The full log](browser-first-exit1.log)
   is retained. The recorder now drains its pending reads before closing.
   Its `browser-proof.json` `passed` value means assertions reached the end,
   **not** that the command exited successfully. No workload was rerun.
2. Initial peer cleanup exited **1** because its transaction omitted
   `memoh.team_id`, required by the existing registration guard. The transaction
   rolled back. Reusing the existing synthetic team's context restored exactly
   the temporary fields and passed the old-operation digest check. No rule was
   disabled and no new authorization created. The first peer also emitted
   unsupported-method/name assertion diagnostics; its full stderr was not
   saved, so no claim of a complete raw peer log is made.
3. Initial read-only closure exited **0** but proved only persisted reference
   IDs. Its peer rejected the legitimate `agent.get` pane-ID selector, leaving
   native metadata unavailable. This was a fixture defect, not a product fix.
   Final read-only closure accepted that existing selector, required native
   online + original reference match, and passed API/UI checks, exit **0**.
   It launched zero new workloads and ended with successful cleanup, exit **0**.

`evidence.json` lists exact command outcomes and hashes/byte counts for the
selected, unmodified evidence files. The original scripts and corrected
read-only continuation are included. Private fixture IDs/config/token files
and raw Server logs are excluded. Scripts require the already-owned isolated
setup and `/tmp/task-delivery-ui/db.py` helper; they are not a fresh-install or
production test command. The helper uses the existing loopback test PG identity.
Do not point these fixture scripts at production.

## Inspected screenshots

- [Same task after Server restart](same-task-after-server-restart.png): original
  parent/child IDs, live second workload, unknown request retained, no new input.
- [Narrow shared Web](unknown-narrow.png): 390 px layout, terminal sample and
  unknown retained receipt; not a Harmony binary or physical phone.
- [Final native reference readback](readback-with-native-references.png): target,
  workspace and pane IDs; native done explicitly leaves task outcome unverified.

## Page U/W reconciliation and remaining boundaries

| Item | Reused or newly established evidence | Still not accepted here |
| --- | --- | --- |
| U01 login | Existing token used only in isolated Web. | Real Web/mobile app restart/session expiry remains field work. No credential-storage change. |
| U02 queue/parents | Existing [three-level relationship](../../testing/task-direct-parent-reference/README.md) and [specialist drilldown](../../testing/web-management-task-drilldown.md) evidence reused. New workload retained its actual parent, child, operation and target through restart. | Recorded task states are not a scheduler queue-position guarantee. Field same-ID Web/Harmony comparison remains. |
| U03 content/ownership | Actual local process output and original target/workspace/pane read through authenticated task APIs. | Device display-name/work-directory mapping, full logs and real PC output are not established by these synthetic IDs. This read model exposes IDs, not a new device directory. |
| U04 management | Existing interactive instruction really invoked the local workload; duplicate clicks, unknown, revoke and restart were exercised. | No actual PC stop/kill/cancel was attempted. `computer_task_cancel` only records cancellation intent; shared task HTTP has no cancel/stop route. Existing [boundary](../../native-agent-followup-boundary.md) and `TestStandardMCPUnknownIsDurableAndCancellationDoesNotKill` are reused, not rerun or promoted to process termination. Batch interactive continuation remains unsupported. |
| U05 parent run/image | Frozen cbcee [parent-run evidence](../session-bot-run/README.md) reused; no image mutation. | Publication/field original snow-run inspection remains pending. No model retry. |
| W03/W04 disconnected work/tracking | Local finite process survived Memoh Server restart; same receipt/request was read with no replay. | This is not a Windows network interruption, Herdr restart, Pi history retention, native Runtime reconnect or physical power-loss experiment. |
| W05 producer paths | Existing source/producer evidence unchanged. | No file transfer or Windows directory-path acceptance in this test. |
| W06 lifecycle | Only the owned development Server restarted. Existing [batch reader recovery](../../testing/batch-recovery-reader/README.md) and its generation/unknown limitations remain. | No host upgrade/stop permission or safe takeover was created. `task_handoff_unverified` is not bypassed. |

No newly proven application defect was found in these combined scenarios.
Accordingly this branch adds acceptance evidence only; it does not invent a
control action or broaden a supported capability to make the checklist green.
Full types/build/CI and unchanged historical tests were not rerun. Product and
publication refs remain cbcee, while field acceptance and denied publication
stay explicitly pending. Human QA is unconfirmed.
