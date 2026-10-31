# Lifecycle wake adapter follow-up

The complete Home fix at `7f2a3475ed910ce657df57851a8e1412f968b053`
finished its first normal CI attempt. Native delivery `40792889922`, large-byte
recovery `40792889867`, ESLint `40792889865`, Docker PR `40792889933`, and policy
`40792889854` succeeded. Governance was skipped and is not counted as validation.

Go run `40792895852` passed six of its seven jobs, including the full Test job,
PostgreSQL fencing, Redis, Valkey, pgvector bootstrap and Agent core boundary.
Only lint job `123182187013` failed, with one remaining diagnostic:

```text
cmd/internal/core/task_result_delivery.go:59:2:
Non-inherited new context, use function like `context.WithXXX` instead (contextcheck)
go func(lifecycleCtx context.Context) {
```

The earlier repair made the worker's lifecycle context explicit at the closure
boundary, but the notification callback still had an unrelated context input.
The upstream contextcheck v1.1.6 analysis used by golangci-lint v2.10.1 checks
that callback boundary too. The existing callback is a notification, not an
inline scan: it must not inherit a short request's cancellation, while shutdown
must cancel the worker. A context-free `signalResultDelivery` method now owns
the unchanged scheduling body, and the existing callback forwards only the wake.
This matches the existing WorkspaceIntents Wake-to-Kick adapter pattern.

The body, mutex/dirty coalescing, lifecycle context, 30-second scan timeout,
shutdown wait and authorization/reconciliation paths are unchanged. No linter
disable, exclusion, timeout, test assertion or CI configuration was changed.
The old failure remains evidence; a normal source push validates this changed
head instead of rerunning the unchanged old head.

The affected core package was compiled with the existing Go 1.25.7 toolchain
and its `TestLegacyUnknownCannotEnterDeliveryOrQueuedAdmission` guard ran once
with `-race -count=1`: PASS, package 1.070 seconds. This does not claim a new
PostgreSQL or full-suite run; the unchanged six Go jobs and five other workflow
successes above remain their original evidence.

This follow-up changes no Web, SDK, schema, producer, route or grant contract.
The complete source-consumer map remains byte-for-byte valid. No HAP build,
formal Docker publication, PR approval/metadata update or field deployment is
performed by this source integration.
