# Configured Images request correlation

Base: `eb1a9634c37097455f0eb055d791bc16598ef56d`. Existing migration177 already
records a best-effort incoming WebSocket abort observation (fixed client-reason
enum, receiving source, exact authorized run). The invocation lookup projects it
without treating client claims as human identity. The shared image/run views
already stop waiting on terminal runs and preserve confirmed receipts. None of
those state, cancellation or UI implementations is duplicated here.

The remaining source gap was in the external `imagecapability` adapter: it
intentionally returned the same safe, non-retryable `unknown` result for local
cancellation, a client deadline and any non200 provider response, without a
structured request record connecting them. A504 alone therefore could not tell
an operator whether a model timed out, a caller canceled, or another boundary
failed. This increment adds diagnostic facts only, using the existing app logger.

At actual transport dispatch, a generated opaque UUID is placed in the standard
`X-Request-ID` header and logged with Bot/session/toolcall and configured provider
ID. The run ID is taken only from the existing matching host-owned binding;
absent/foreign bindings do not acquire a guessed latest run. No internal run ID
or user identity is sent to the provider. The original request correlation stays
with the existing logging context. A bounded provider `X-Request-ID` response is
hashed, not copied verbatim, because that header is controlled by the provider.

The finish record separately names the observed HTTP status, transport return,
request context state, owning tool context state and actual returned tool state.
Request context is sampled when headers/error return, and updated on a canceled
body read; a normal response-body Close does not become a spurious cancellation.
A504 with active contexts remains a504, while a504 and canceled context can both
be recorded. Neither implies causal order, model timeout, a human cancel, image
existence or successful delivery. HTTP200 with invalid image bytes stays unknown.
Prompt, arguments, request/response bodies, credentials, URLs and raw errors are
not logged. Existing single-POST, redirect refusal and unknown/no-retry behavior
are unchanged. No timeout, permission, protocol encoding, SDK/Agent core, database
schema, task ledger, recovery worker or user-facing field is changed.

## Validation and limits

- Full `internal/imagecapability` package tests passed, then the full same package
  passed with `-race`. The new scenarios cover pre-dispatch cancellation, local
  cancel after dispatch,504, cancellation plus504, run deadline, client/header
  timeout, body-read timeout, invalid200, confirmed bytes and missing/foreign run
  identity. Assertions retain one dispatch, original provider credential, unknown
  non-retryability/no hash, exact run/tool/provider/request association and zero
  secret markers in diagnostic logs. Selected synthetic records are readable in
  `scenarios.txt`; none are historical/field records.
- Pinned golangci-lint2.10.1 over imagecapability and core composition passed.
  Initial import-name collision with the existing test helper and three lint
  findings are retained with the later passing results. No rule was suppressed.
- Existing terminal image-card and run-view evidence is reused: the output map
  and UI contract did not change. No new full application/provider or field UI
  acceptance is claimed. In particular, no paid provider, snow-image replay or
  production device was called. No historical run or media record was written.

The old snow run's toolcall, cancellation origin and upstream response linkage
remain unavailable. This code cannot reconstruct deleted logs, make the original
504 a proven model timeout or recover absent image bytes. It observes only the
explicitly configured Images API adapter; the official legacy/chat fallback is
unchanged. Run identity can still be absent on an unbound path. Start logs show
an attempted dispatch, not proof that a provider received it; a process crash may
leave only the start record. Existing log retention and best-effort abort storage
remain limits, not a new durable execution ledger or exactly-once guarantee.

This is an independently traceable source checkpoint. It must not delay the
separate complex-PC submit fix, and is not a deployment or new image publication.
