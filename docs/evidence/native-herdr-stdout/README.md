# Native Herdr stdout hotfix: local UI verification

The current Go binary containing the two `[void]` fixes ran with the real Vite UI and isolated PostgreSQL. [Verification summary](verification-summary.json) records source and binary hashes, exact task identity, checks, and limitations.

- [Native target](native-target.png): the existing synthetic native computer remains registered and intentionally offline.
- [Unknown task](unknown-task.png): the real Tasks detail remains **Execution status unknown** after **Refresh computer**. Connection, process and Pi result remain unknown/unavailable; no success is inferred.

The same saved operation was read again through the scoped API: execution and business result stayed unknown. The owned session and operation rows remained byte-for-byte equivalent, operation count stayed at two (one prior legacy history row plus this synthetic unknown fixture), and the browser made no task write request. No OAuth token row was created. Browser errors were zero.

The visible workspace-unavailable toast is expected for this deliberately offline fixture. No credentials, real enrollment, business dispatch, or production deployment occurred. This local UI pass does not execute the Windows PowerShell wrapper; the dedicated regression test provides that evidence separately. This is agent verification, not human QA. Detailed local records remain outside the repository under `/workspace/native-herdr-verification/stdout-hotfix/`.
