# Native Herdr resume: receipt-reading UI verification

The current Go binary, including the qualified `Microsoft.PowerShell.Utility\ConvertFrom-Json` lookup, ran with the real Vite application and isolated PostgreSQL. [Summary](verification-summary.json) records the tested source/binary hashes, synthetic identities, full normalized Submit hash, and observed results. Production source hashes remained identical between build and current UI verification.

- [Native target](native-target.png): the existing synthetic native computer remains registered and intentionally offline.
- [Unknown batch task](unknown-task.png): after the existing **Tasks → Refresh computer** action, execution remains unknown, the original attempt/generation remain visible, and workspace/pane references remain unknown. No success is inferred.

The existing fixture contains one clearly synthetic `batch_creating_workspace` / `unknown` database fixture with valid attempt/generation, `history.mode=first`, the complete normalized Submit hash, original run ID, and empty native references. This verification reused it without adding or changing fixture rows and preserved the prior two history rows. All three operation rows and associated sessions remain unchanged after reads; operation count stays three. The scoped task API still reports execution and business outcome unknown. Task write requests and browser page errors are zero.

The running MCP endpoint answered `initialize` and `tools/list` with 200 and exposed optional `computer_task_submit.resume_operation_id`. No `tools/call`, submit, resume or duplicate-submission request was sent.

This pass covers current application startup and receipt reading only. A hand-seeded receipt and offline UI do not prove successful Windows recovery. Resume dispatch and replay behavior have separate synthetic transport tests; admission and CAS have separate real-PostgreSQL tests. Live-environment acceptance remains with the parent. The workspace-unavailable toast is expected for the offline fixture. No real credentials, enrollment, OAuth refresh or production deployment occurred. This is agent verification, not human QA.
