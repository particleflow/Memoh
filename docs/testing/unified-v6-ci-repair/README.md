# First Go CI failure on unified 28ac

Exact source: `28ac9393c7e20db34b804ad3ece63680eecded8c`.
Original run: https://github.com/particleflow/Memoh/actions/runs/40789342050.
The Test job `123170373082` failed only
`TestRetryDeliveryNeverRunsSpecialistAgain` in `internal/botdelegation`.
The Lint job `123170373211` reported the nine diagnostics retained alongside
this note. The other five Go jobs passed; this was not a download/runner error.

## Test contract correction

The existing fixture submits a Web/model request, which now explicitly opts in
to durable publication reconciliation. Its old assertion instead expected the
legacy behavior: after delivery is accepted, do not call the host again and
reject unknown retries. A host reconciliation call is not a specialist execution
or a published user message. The durable publication layer owns receipt dedup.

The original test now explicitly uses the supported ACP source runtime to stay
on the legacy path, verifies that the persisted opt-in version is zero, and keeps
all original assertions: one created specialist session, one execution, two host
calls across the initial uncertain response and three retries, and unknown
rejected. No assertion or production authorization is removed.

A separate opted-in test asserts version 1, the original uncertain delivery,
three successful retries of the same record, unchanged original run and identity,
one specialist session/execution, and four total host reconciliation entries.
It does not relabel those entries as four user replies. The existing actual
PostgreSQL publication/CAS evidence remains the distinct exactly-once proof.

Executed once with the existing Go 1.25.7 toolchain:

```sh
go test -race ./internal/botdelegation \
  -run '^(TestRetryDeliveryNeverRunsSpecialistAgain|TestRetryOptedInDeliveryReconcilesOriginalRecord|TestUnknownDeliveryRequiresPersistedOptIn)$' \
  -count=1 -v
```

All three top-level tests and both opt-in subcases passed, exit 0, package time
1.050 seconds. No old broad suite, production model or device was invoked.

## Nine narrow lint corrections

- Pass the existing host-lifecycle context explicitly into the wake goroutine;
  preserve its existing timeout and lifecycle ownership, not the short terminal
  request's cancellation scope.
- Check the PostgreSQL count query's Scan error in the publication test.
- Rewrite the existing result lookup if/else chain as an equivalent switch.
- Remove the tautological nil test for the channel just allocated in StartTurn;
  the result-only no-steer condition remains.
- Use errors.New for three unchanged constant error strings.
- Remove an unused test-fixture receiver name.
- Apply the equivalent De Morgan condition to the unknown opt-in gate.

No linter configuration, assertion skip, runtime core, schema, permissions,
provider, Web, SDK or production configuration is changed. Full final-head lint
is not claimed: the cached linter executable from the earlier temporary
environment is absent, and no new tool was downloaded. The next combined head
must run the existing normal CI after the independent URL speech fix arrives.

Native run `40789342201` ultimately succeeded on its first attempt. ESLint,
Policy, Large-byte and Docker PR also retain their first-attempt successes.
The failing 28ac run was not rerun. No source push or publication is performed
for this Go-only checkpoint, and the separate URL-admission speech owner keeps
its source-file boundary.
