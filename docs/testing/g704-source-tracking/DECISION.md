# Narrow disposition of the local-request G704 false positive

This decision follows the recorded investigation at `618ba5b9` and bounded
classification at `41a2f2fc`. Those failed upgrade results remain historical
evidence; they are not relabelled as passing checks.

## Scope

Keep golangci-lint **2.10.1**, its complete existing configuration, and every
workflow unchanged. Register exactly one `#nosec G704` diagnostic annotation on
`internal/imagecapability/protocol.go`'s `oneShot.Do(req)` call. Do not suppress
other gosec rules, a whole function/file/package, or an ESLint diagnostic.

The destination is derived from the selected Bot image model's stored provider
configuration. Strict tool arguments cannot override the endpoint, provider,
credentials or authority. Prompt text is data and may contain URLs; it does not
select this HTTP destination. The code uses fixed Images API paths, no body replay,
and `CheckRedirect = http.ErrUseLastResponse`. This remains the configured-provider
boundary, including intentionally supported LAN/self-hosted endpoints; it is not
a new public-IP-only filter or a claim that arbitrary caller-supplied URLs are safe.
No provider permission, request implementation, transport or retry behavior changes.

## Evidence and difference from the withdrawn attempt

- Official gosec v2.23.0 type-based taint marks a locally constructed
  `*http.Request` as external. The [recorded static differential probe](README.md#static-differential-probe)
  proves that even a constant destination is diagnosed by the pinned tool.
- The unchanged unsafe external-input control remains detected by the official
  corrected tools. Probe Go functions are never executed and make no network
  requests. Both controls and complete unfiltered probe logs remain available.
- Full checks with official 2.11.0/2.11.4 exposed added checks and other analyzer
  compatibility findings, including a reproduced protected-core ownership false
  positive. Upgrading the entire check set is not required to register this one
  demonstrated false positive, and those other results are not hidden.
- `88211612` previously added this destination annotation before the differential
  evidence, together with an ESLint allowance for four Node builtin `require`
  imports. Both were removed by `13693122`. This change restores **only** the
  specifically evidenced G704 registration with a removal condition. The ESLint
  allowance stays removed, and the corrected dynamic imports remain intact.

## Removal and invalidation

Remove this annotation when the pinned official analyzer correctly distinguishes
locally constructed requests from external request sources and the compatible
repository checks pass. Re-evaluate it immediately if destination ownership,
argument validation, provider selection or redirect handling changes. If tool
arguments ever acquire destination control, this reasoning no longer applies:
the new data flow needs its own security review rather than carrying the annotation
forward. Never generalize this annotation to other HTTP calls.

## Validation boundary

Run the complete configured lint using the existing 2.10.1 binary. Exercise the
existing image-capability package against synthetic loopback HTTP providers and
isolated original storage: explicit/legacy configuration, endpoint and authority
argument rejection, generation/edit parameters, redirect refusal, single POST,
unknown/no retry, persistence failure, original-byte/hash preservation, and the
native supervisor/subagent fixture. The additional cancellation test checks both
zero-dispatch pre-cancellation and single-dispatch uncertain cancellation.
No paid provider, production file, real PC or new credential is involved.

The application implementation is unchanged apart from comments. Existing real
runtime/browser evidence at `13693122` remains the product checkpoint; no new UI
or live-provider acceptance is inferred from these synthetic tests. Record local
results and the exact subsequent Go CI head/run separately, and retain failures.

## Completed local checks and full-check environment failure

- `go test -p 2 -count=1 -v ./internal/imagecapability`: exit 0, 17 top-level
  tests and 44 subtests passed, zero failures/skips. See the complete
  [synthetic test output](call-site-disposition/image-tests.log).
- `bash scripts/check-agent-core-boundary.sh`: exit 0; see the
  [official-baseline check](call-site-disposition/core-boundary.log).
- The [scope audit](call-site-disposition/scope-audit.json) confirms unchanged
  workflow/lint policy and no production executable-statement change.
- The first complete pinned-linter invocation exited 1 on a missing compiled
  Go source cache index for `templates/workspace.go`. This is an environment
  failure, **not** a complete source-check pass. The original
  [failure output](call-site-disposition/full-lint.log) is retained. A bounded
  `go list -a -p 2 -compiled -export -json ./templates` cache rebuild exited 0;
  the missing prior index remained absent. One complete lint retry with a fresh
  analyzer cache was started; no further cache-repair loop is authorized.

The source and regression checkpoint can be submitted while that bounded retry
finishes. Complete verification uses the existing Go workflow at the submitted
SHA, without changing its checks or triggering unrelated workflows. A started
local command or remote workflow is not a successful check. Its actual terminal
result and run URL must accompany the handoff. The copied evidence files are
unaltered; their [manifest](call-site-disposition/manifest.json) records sizes and
hashes. They contain synthetic data and non-secret cloud cache diagnostics only.
