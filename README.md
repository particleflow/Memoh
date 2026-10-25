# Schedule workspace binding validation

Product head: `0ee7a42c22a5965e9d69ea9471729e6da0574d1f`
Base: `9601ac062a3b65477db8db76bcaa521265ba7ee2`

The product commit changes only two schedule host files and two regression files (+416/-8). This separate evidence tree contains no product changes, secrets, configuration or browser state.

## Automated checks

The unchanged repository aggregate (`node .husky/checks.mjs`) passed with exit 0: golangci-lint 2.10.1 and complete default test runs of `internal/agent/application` and `internal/schedule`. Database/Redis integration environment variables were not configured for that check; no full-repository or real model/device acceptance is claimed. Absolute cloud workspace paths in aggregate.log are replaced by `$REPO`.

New regressions cover stored remote PC/native workspace binding through the real ScheduleGateway/native tool assembly, refusal after revoked permission or failed target/workdir resolution, same-session text persistence, single/concurrent bootstrap registration, no replay when recreating the service, offline error recording, and no creator ComputerBinding retained by a new registered callback. These are isolated synthetic tests, not PC execution.

## Real UI and database

An isolated current-source Server, real Web/Vite and fresh PostgreSQL 17.11 were used because Docker vfs exhausted the cloud root inode budget. The fresh database applied 174 up migration events and reports schema version 175, dirty=false. No model/provider/device/workspace was configured.

Normal UI: login, create one disabled synthetic schedule (POST 201), edit (PUT 200), reload (GET 200), restart only the test Server, verify the same schedule/session/disabled state with current_calls/logs/runs all zero, then confirm deletion (DELETE 204). Database snapshots and safe HTTP method/path/status records accompany the screenshots. The visible workspace warning is expected in this schedule-only fixture.

`06-final-server-restart-preserved.png` shows the same disabled schedule and selected existing session after Server restart. `08-deleted-empty-list.png` shows the list after normal UI deletion. The browser's initial hidden/visible duplicate-selector error was corrected without another create request.

This evidence does not establish an enabled scheduled fire on a real PC, native Herdr transport, live provider execution, or attachment delivery. The test services were stopped afterward; original QA instances were untouched. Human QA has not been claimed.
