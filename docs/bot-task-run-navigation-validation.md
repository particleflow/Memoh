# Original task execution navigation

This slice is based on `453a1178c25213772dbead9f428f1dca83e2a027`. It closes the task-directory link gap: selecting an older task in a session previously opened that session at its latest messages. The directory still shows only name, status, and the existing execution-context link.

## Trust and history boundaries

After existing Bot, user, session-ancestry and task-specific authorization, the directory resolves the saved run against the team-scoped native run ledger using the exact Bot/session/run tuple. Native admissions also match `subagent:<task_id>`, schedule occurrences match `schedule:<schedule_id>:<log_id>`, and delegations match `bot-delegation:<delegation_id>`. Incoming and outgoing delegations resolve the same original target execution. Historical runs do not inherit the current execution fence: a later run must not make an earlier execution disappear. Explicit history-reset records remain non-navigable.

Computer operation IDs, schedule log IDs, delegation IDs, native admission IDs, and legacy native run occurrence IDs remain distinct. Computer receipt run IDs are TEXT: only an exact match to an authorized native execution provides a link. Opaque provider IDs, unstarted admissions, missing executions and invalid associations keep their task/status without inventing a session/latest-run destination. Read failures remain errors. Unknown is never promoted to success or re-executed by navigation.

`destination.turn_id` is additive. The Web deep link preserves the original directory Bot, typed task ID, occurrence ID, session ID, and run ID. Route admission and restored-panel activation re-fetch the original task without cache; URL and local layout values do not establish ownership. The server-resolved turn is located through existing history pagination and message scrolling. Missing content shows an unavailable state. The full task transcript and tool history remain intact. No schema migration, backend history rewrite, new management panel, or Agent core change is involved.

Same-session run changes replace the panel anchor; ordinary history navigation clears it. Late route resolutions cannot overwrite a newer anchor. An actual KeepAlive lifecycle test confirms cached-pane reactivation reauthorizes even when visibility props are unchanged. Authorization hides content without collapsing Dockview dimensions; collapsing the whole workspace during same-session navigation reproduced a real zero-size layout failure and is now avoided. Once located, live messages do not pull the reader away from the historical turn.

## Local evidence, 2026-10-28

- Actual PostgreSQL transaction tests: two original discovery/reference cases, rollback only. Multiple runs in one session, a reused original task ID across historical sessions, incoming/outgoing delegation, exact schedule occurrence, native and opaque computer run IDs, advanced session fences, wrong Bot/session/run, and explicit history reset. Seven directory tests (including the two PostgreSQL cases) and two HTTP authorization/read-failure tests passed. The added native-computer anchor assertion also passed separately after refinement.
- 139 relevant Web tests passed across task-link resolution, history anchoring, directory interaction, workspace reuse and route navigation. The 13 link/anchor cases were repeated after type corrections; this is not an additional 13 unique cases.
- Web typecheck, affected ESLint, UI contract, and backend build passed. The UI contract reports three pre-existing unrelated warnings. Exact final exit results are included in the handoff evidence.
- Actual Chromium at `http://127.0.0.1:28082/bot/local-dialog`, current Vite source and native Server, isolated PostgreSQL `memoh_bot_dialog_test_20261028`. Baseline `453a1178` opened the older task at the later execution. Current source loads older history pages and lands the original prompt/tool/result, including browser refresh.
- Browser checks cover same-session different runs, repeating the same directory selection after scrolling away, cross-session history, browser back, contact return, 390px refresh, and a forged run link rejected without switching to another context. No browser exceptions were observed.
- Live check uses two real browser contexts and one loopback scripted model request per run of this check (two executions, before and after lifecycle refinement). One context remains anchored on the old run while the other submits and completes a new real native run. Original prompt Y position stays 103px before, during, after, and after refresh. Task reactivation suppresses generic remembered-position restoration so it cannot overwrite the exact turn. New content is received through the real stream; no paid model or external tool action is used.

The additional persisted later-run fixture is synthetic and explicitly labeled as such; the historical original task and the later live native run used the real local runtime. Tests do not establish production model semantics, human QA, or device acceptance.

## Reviewable screenshots and logs

All screenshots are actual locally captured application screens and were inspected. They remain local, outside the Library archive:

- `/workspace/bot-dialog-handoff/run-anchor-red.png`: old task selection lands at later-run content on baseline.
- `/workspace/bot-dialog-handoff/run-anchor-green.png` and `run-anchor-refresh.png`: original prompt, tool and result are visible at the target turn.
- `/workspace/bot-dialog-handoff/run-anchor-wrong-run.png`: forged run link rejected.
- `/workspace/bot-dialog-handoff/run-anchor-same-session-new.png` and `run-anchor-cross-session.png`: distinct run/history selection.
- `/workspace/bot-dialog-handoff/run-anchor-return-contact.png`: returns to the fixed contact.
- `/workspace/bot-dialog-handoff/run-anchor-narrow-refresh.png`: original execution at 390px.
- `/workspace/bot-dialog-handoff/run-anchor-live-preserved.png`: old position retained while another real native run completes.

Machine-readable evidence is in `run-anchor-{red,green,navigation,live}.json`, `run-anchor-postgres.log`, the focused Vitest logs, and explicit lint/typecheck exit files. An environment reconnect interrupted an early navigation attempt and a typecheck invocation; interrupted results were not counted and the affected checks were rerun. A root-level pnpm filter initially matched another checkout as well; only this checkout's explicit Web typecheck is accepted.

## Integration limits

Deliver the independent increment for review/cherry-pick onto the coordinated line. No deployment, signing, production migration, paid call, or subagent dispatch occurred. Web is the shared HAP source; physical HAP back-button behavior and packaged-device acceptance remain with the HAP owner. The original full-product compatibility and semantic acceptance limits remain unchanged. No attention management UI is added as a new gate.
