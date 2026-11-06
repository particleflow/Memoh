# Deferred decision replacement checkpoint

Base: `9d91623e1f3325066adddfa8a11db16cef0d46a7`.

A retry/edit run persists intermediate steps outside visible history until its
replacement is selected. The queue coordinator previously returned immediately
when a step parked on a deferred decision. That left its assistant tool-call
row unbound (`turn_id` and `turn_position` absent, `turn_visible=false`). Native
answer continuation subsequently looks up the admitted turn and cannot find it.

The deferred boundary now selects the exact existing replacement through
`FinalizeAgentReplacement`, before the producer releases its continuation
barrier. It retains the original rows and decision/run identity. It does not
claim queued steers, swallow persistence errors, retry tools, call a provider,
or alter Agent runtime/core files. Ordinary final-boundary selection uses the
same helper.

## Validation

- The new `TestDeferredReplacementSelectsVisibleTurnBeforeAnswer` failed on the
  unmodified base (exit 1: zero replacement finalizations).
- Directed tests passed after the change, including persistence-error
  propagation and unchanged deferred/final steer handling.
- A fresh, isolated PostgreSQL 18 database ran
  `TestPostgresDeferredReplacementKeepsDecisionContinuationAddressable` for
  both retry and edit. The actual runtime ledger, fenced step writer, replacement
  transaction and decision store found the admitted turn, accepted the original
  decision and wrote one visible answer tool result. Original history rows
  remained present. No model, image generation or delegation was invoked.
- The directed `go test -race` selection passed seven top-level tests and four
  subtests, with zero failures or skips.
- `bash scripts/check-agent-core-boundary.sh` passed against the repository's
  official baseline. The script is tracked without its executable bit; direct
  invocation returned exit 126 before the normal Bash invocation.
- The normal staged aggregate, `node .husky/checks.mjs`, passed (exit 0):
  application-package lint and package tests, total 162.28 seconds. Integration
  tests requiring external databases were not enabled in that aggregate; the
  directed PostgreSQL/race run above separately enabled this new integration
  test. No full-repository suite was run.

Directed command:

```sh
go test -race -json ./internal/agent/application \
  -run 'TestDeferredReplacement|TestPostgresDeferredReplacementKeepsDecisionContinuationAddressable|TestDeferredStepDoesNotClaimSteerAndContinuationDeliversIt|TestFinalStepClaimsSteerForLoopContinuation|TestStepCommitSeparatesHistoryFailureFromQueueFailure|TestRuntimeUserInputCommandCommitsAndResumesSameRun' \
  -count=1
```

The PostgreSQL test uses the repository's `TEST_POSTGRES_DSN` and
`TEST_POSTGRES_BOOTSTRAP_SCHEMA=1` fixture contract. It was run only against a
new synthetic database.

## Unfinished acceptance and recovery

This verifies the deferred **replacement** defect, not every possible cause of
unbound history. Attribution of an existing failed run additionally requires
its persisted admission kind and original-input provenance. A generic
`runtime_run_failed` error is insufficient for that attribution.

This patch does not reopen a terminal failed run or restore historical rows.
An already-submitted answer must not be submitted as a new answer or wired to
whole-turn `retry_message`. Replaying its original control ID only acknowledges
the accepted decision; it does not restart a failed continuation. Original
failed history must remain intact. Recovery needs the original persisted input
and decision, explicit authorization and an existing admission/fencing boundary;
never substitute the session's latest message. No recovery endpoint is claimed
by this checkpoint.

No current application UI, production deployment, original-request recovery,
or paid-provider acceptance was performed. This is not a Ready declaration.
