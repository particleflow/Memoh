# Bounded classification of the 19 non-noctx diagnostics

Product checkpoint remains `1369312252d0f55cd6e35a058927f921a80a753b`.
This is evidence-only work following `618ba5b9183e5647d0516f02b714aade666e4f99`.
No new linter release, rule exclusion, custom linter or product implementation is
introduced. The 187 noctx findings are deliberately outside this investigation;
that does not turn the whole-repository failed check into a pass.

## Provenance and new-check distinction

[The complete classification](classification.json) gives each of the 19 locations,
its `git blame` commit, ancestry against official `bf0318c5`, whether the full file
matches that baseline, evidence level and the bounded next action.

- **13 locations are on original upstream lines**, including the one protected
  core finding at `internal/agent/runtime/grok/server.go:92`.
- **Six locations are on our integration lines**: two Pi history fixture writes,
  one OAuth diagnostic fixture, the upload-root initializer, the memory projection
  fixture, and the originals cancellation fixture.
- These are newly reported under the newer tools on unchanged product code, not
  19 newly introduced behavior regressions. G118/G120/G122 are new rules in the
  upgrade; G602/G703/G115 and govet are changed analysis results of existing code.
  The prior recorded 2.10.1 full check is reused, not rerun.

| Finding group | Count | Evidence and classification |
| --- | ---: | --- |
| G118 Grok `sync.Once` ownership | 1 | Reproduced analyzer false positive with the actual ownership pattern; core remains unchanged. |
| G118 cancel in map / value receiver | 2 | Both patterns reproduce despite explicit invocation. Originals test can independently strengthen cleanup and callback coverage. |
| G118 workspace dependency shutdown | 1 | Store and `Shutdown()`/host `OnStop` call are present; the small direct-method sample does not reproduce the full-program report. Unresolved analysis, not a confirmed leak. |
| G602 nested ranges/equal lengths | 5 | Source contains bounds invariants. Small equivalents pass; deliberate out-of-bounds control is caught. Exact whole-program warning remains unresolved. |
| G115 rune conversion / wire bits | 2 | Both diagnostics reproduce. Rune range values are nonnegative and checked against 255; negative wire bits are intentional test input. No accidental overflow established. |
| G703 controlled fixture paths | 3 | Two Pi fixture writes and one repository-template test; no production user-to-path chain. |
| G703 configured originals root | 1 | Startup caller passes `Config.Files.OriginalsRoot`; operator-chosen root is intentional. This is not a general filesystem security proof. |
| G120 form parsing | 2 | Both are synthetic `httptest.NewServer` endpoints. Request caps can strengthen fixtures independently; no deployed endpoint defect is demonstrated. |
| G122 WalkDir helper | 1 | Only a synthetic integration-test projection reader; no concurrent adversarial tree actor in this fixture. |
| govet nilness | 1 | Original upstream interface-error check. Reduced example does not reproduce it; do not delete the check or alter error behavior. |

## Exact core-pattern reproduction

The [initial sample](basic-probe.go.txt) with direct `Close()` ownership reports
only the intentionally lost cancel: [raw output](basic-ownership.log).
Adding `Close()` cancellation through `sync.Once.Do` produces a second G118 at
`NewOnceOwner`, although `UseOnceOwner` defers `Close()`:
[raw output](once-ownership.log). This matches Grok's stored field and
`server.Close()` closure, without importing or modifying the Agent.

The final [probe](probe.go.txt), [indirect ownership cases](indirect.go.txt), and
[bounds/conversion cases](bounds.go.txt) are analyzed with the **entire unchanged
repository lint configuration**, not a filtered check set. [Raw complete output](complete-probe.log)
is kept. A deliberately discarded cancel, definite out-of-bounds index and unsafe
integer narrowing remain detected. None of these Go functions is executed; the
probe performs no network request or database/file business operation.

The official analyzer source at
[`619ce2117e08/analyzers/context_propagation.go`](https://github.com/securego/gosec/blob/619ce2117e08/analyzers/context_propagation.go#L789-L830)
searches same-receiver method instructions for field loads/calls. The cancellation
inside `sync.Once.Do` lives in a separate closure body. This source inspection
supports the observed failure; it does not certify every G118 report false.

Run `python3 verify.py /path/to/official/golangci-lint-2.11.4` to copy only these
small samples to a temporary module and assert the complete expected diagnostics.
The linter is expected to exit 1 because the fixture contains unsafe controls;
the assertion harness must exit 0. The [actual harness run](harness.log)
completed with exit 0 in 2.139 seconds, retaining all eight expected diagnostics. No modified/custom analyzer is compiled.

## Safe scope and remaining blocker

No production behavior defect was confirmed from these 19 diagnostics. Possible
independent fixture improvements are (1) always release the originals test's
cancel and explicitly verify its reader caused cancellation, and (2) cap synthetic
OAuth form requests and test oversized input. Those are test-quality work, not a
fix for the protected-core analyzer error, and are not implemented in this
classification checkpoint. Other unproven findings are left visible.

There is still no verified compatible whole-repository green upgrade under the
requirements of an unchanged official core and unchanged check policy. The pins
stay at 2.10.1; no CI, PR, release or deployment is triggered. The blocked legacy
media P1 is untouched.
