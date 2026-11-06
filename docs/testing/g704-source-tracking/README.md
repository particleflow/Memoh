# G704 source tracking: official analyzer correction

Base product checkpoint: `1369312252d0f55cd6e35a058927f921a80a753b`.
The base's Go CI run [39940419048](https://github.com/particleflow/Memoh/actions/runs/39940419048)
had six successful jobs and one lint diagnostic at
`internal/imagecapability/protocol.go:89`. Its ESLint run passed. Neither the
base branch nor the older unified candidates is moved by this investigation.

## Actual data and network boundary

- `provider.go:50–75` resolves Bot `ImageModelID`, its stored model/provider and
  `provider.Config.base_url`. Credentials are resolved separately by the existing
  provider service. Tool input is not the destination configuration.
- `config.go:47–55` requires an explicit HTTP(S) host without userinfo, query or
  fragment and appends only the fixed generation/edit path. `provider.go`'s
  strict argument decoder rejects endpoint, model and authority input fields.
- `protocol.go:77–89` builds the POST, disables body replay, sets provider
  credentials and forbids redirects. Response URLs are not fetched as a fallback.
- `models/http_client.go` supplies a two-minute request timeout, a minimum
  30-second TLS handshake timeout and the existing provider User-Agent wrapper.
  It clones the existing default transport. It does not enforce a public-IP-only
  policy; configured LAN/self-hosted providers are intentionally supported.

This is the existing configured-provider boundary, not a claim of general SSRF
protection or a new provider-management permission model. No provider/credential
configuration, real paid request, Agent core, media policy or production data is
changed here.

## Why the old diagnostic is reproducible without untrusted data

The official [gosec v2.23.0 SSRF source list](https://github.com/securego/gosec/blob/v2.23.0/analyzers/ssrf.go)
includes `*http.Request`. Its [taint implementation](https://github.com/securego/gosec/blob/v2.23.0/taint/taint.go#L279-L299)
checks the type and immediately returns true before tracing construction.
`Client.Do` checks all arguments. A locally constructed request with a constant
URL therefore triggers G704 even without an external URL source.

The official [golangci-lint v2.11.0 module](https://github.com/golangci/golangci-lint/blob/v2.11.0/go.mod)
requires Go 1.25.0 and includes gosec v2.24.7. The project's Go module and local
Go toolchain remain 1.25.7. The downloaded release binary identifies itself as
v2.11.0 built with Go 1.26.1; [its SHA-256 matches the official release checksum](tool-download-proof.json).
The [corrected taint implementation](https://github.com/securego/gosec/blob/v2.24.7/taint/taint.go#L499-L541)
distinguishes locally constructed values from external parameters rather than
marking every value of a source type tainted.

## Static differential probe

[The exact Go source](probe.go.txt) has a constant example.invalid destination
and an intentionally unsafe external request query destination. Neither function
is executed: **zero network requests**. Run the [probe harness](verify-probe.py)
with paths to the two official binaries, sequentially and with no other lint
process running. It uses `--enable-only gosec` only for this small diagnostic
probe; that is not the full repository check or its CI configuration.

| Binary | Constant destination | External query destination | Probe lint exit |
| --- | --- | --- | --- |
| 2.10.1 | G704 false positive | G704 | 1 |
| 2.11.0 | No G704 | G704 retained | 1 (expected unsafe control) |

[Machine-readable results](probe-results.json), [old output](probe-golangci-2.10.1.log),
[new output](probe-golangci-2.11.0.log). The probe assertion harness exits 0 only
when the old false positive disappears **and** the unsafe control still fails.
This does not claim all possible SSRF patterns are covered.

## Earlier failed validation attempts retained

The first parallel probe attempt hit golangci's process lock: [old](probe-golangci-2.10.1.parallel-blocked.log),
[new](probe-golangci-2.11.0.parallel-blocked.log). They did not analyze the probe.
The probes were then run sequentially. The first new-version full lint attempt
hit a missing Go compiled-source cache entry for `templates/workspace.go`:
[original failure](full-lint-2.11.0.log). A direct `go list -compiled -export`
verified that package, and the next full check uses a separate analyzer cache.
No source or check policy was changed to conceal either failure.

The separate-cache attempt was terminated with exit 137 before producing a lint
diagnostic. The local cgroup reported OOM kills; the empty stdout/stderr file is
preserved as [the attempt log](full-lint-2.11.0-fresh-analysis-cache.log), with
[resource/retry metadata](local-resource-retry.json). The next local invocation
keeps every rule and package, using `GOMEMLIMIT=2GiB GOGC=50 GOMAXPROCS=2` and
`run --concurrency 2 ./...`. These are local resource controls, not a CI rule or
coverage change. A terminated process is not a passing check.

## Unchanged application validation

The product tree is the already-tested `1369312252d0f55cd6e35a058927f921a80a753b`.
Its [runtime/browser evidence](../unified-ci-restoration.md) and
[inspected child-result screenshot](../unified-ci-child.png) remain the application
checkpoint. This investigation does not rerun that unchanged business flow or
claim a new paid-provider/PC/production test. The relevant new validation is the
analyzer probe and full configured Go lint, followed by the existing Go workflow.

## Full v2.11.0 result: not compatible yet

The bounded-memory full check completed with **exit 1**, with no G704 diagnostics.
The [complete output](full-lint-2.11.0-bounded-memory.log) and
[parsed diagnostic summary](full-lint-analysis.json) contain **259 diagnostics**:
187 noctx, 58 G118, five G602, four G703, two G115, two G120 and one G122.
Thirteen diagnostics are inside the protected official runtime subtree. None of
these new findings is suppressed or silently classified as passed.

The official release also adds checks; it is not a one-rule replacement.
Some G118 patterns have confirmed upstream analyzer bugs, including
[returning cancellation ownership](https://github.com/securego/gosec/pull/1585),
[returning cancellation in a struct](https://github.com/securego/gosec/pull/1593),
and [cancellation in a goroutine closure](https://github.com/securego/gosec/pull/1592).
These references do not establish that every one of the 58 findings is false.
The latest patch in the same 2.11 series, 2.11.4, includes the follow-up gosec
fixes and noctx 0.5.1. Its unchanged-source evaluation is separate; no CI tool
version is updated merely because the original G704 disappeared.

The [published differential harness output](published-probe-harness.log) has
also been collected from the checked-in script: harness exit 0, both deliberate
unsafe probes exit 1. No tested Go function makes a network request.

## Same-series patched release: still not a green upgrade

Official [2.11.4](https://github.com/golangci/golangci-lint/releases/tag/v2.11.4)
was downloaded, [checksum verified](tool-2.11.4-download-proof.json), and run on
the same product tree, same rules, same packages and same local resource limits.
Its [module](https://github.com/golangci/golangci-lint/blob/v2.11.4/go.mod) still
requires Go 1.25.0 and includes gosec `v2.24.8-0.20260309165252-619ce2117e08`
and noctx 0.5.1. No repository dependencies were updated for this experiment.

The full check exited **1**: [raw output](full-lint-2.11.4-bounded-memory.log),
[diagnostic summary](full-lint-2.11.4-analysis.json). It reports **206** items:
187 noctx, four G118, five G602, four G703, two G115, two G120, one G122 and
one govet nilness. G704 remains absent. The
[2.11.4 differential probe](published-probe-harness-2.11.4.log) also exits 0,
retaining the unsafe-input detection. Invoke the same harness with
`--new-version 2.11.4` to reproduce that probe.

One remaining finding is in the protected official core:
`internal/agent/runtime/grok/server.go:92` (G118). The cancellation function is
stored as `server.mountCancel` at line 103 and invoked by `server.Close` at line
201. That ownership path needs analyzer-specific investigation, not an edit to
the protected core or a rule exception. The other G118 locations are
`internal/agent/tool/subagent_step_persist_test.go:401`,
`internal/storage/providers/localfs/originals_test.go:59`, and
`internal/workspacedeps/service.go:141`. The govet finding is
`internal/agent/application/service_run_lifecycle.go:378`.
No blanket claim that all remaining diagnostics are false positives is made.

**Decision:** the original G704 defect is experimentally confirmed, and the Go
version requirement is compatible, but a whole-repository green tool upgrade is
not established. Both existing version pins remain 2.10.1. No workflow was
triggered, no application code/check rule was changed, and no CI failure is
relabelled as success. Before upgrading the pin, the added-check migration and
remaining analyzer findings must be resolved while preserving the official core.
This evidence checkpoint is not a release candidate or a merge/deploy approval.

[Recorded execution outcomes](executions.json) distinguish successful probes
from failed full checks. The [core boundary check](core-boundary.log) exits 0
against official `bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046`. The unchanged
product/workflow/config diff against 13693122 is empty.

All included lint logs are static diagnostics and public-source excerpts, copied
without redaction or output rewriting. Temporary paths identify only this test
workspace. Intentional secret-shaped strings, if any, come from existing
synthetic repository fixtures; no deployment configuration, credentials, private
conversation text or private addresses were loaded into these logs.
