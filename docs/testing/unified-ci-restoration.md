# Unified candidate CI restoration and lint corrections

The frozen candidate remains `8c358cac27922f122315ac9db62d52cc100c8a2a` on
`integrate/unified-server-web-management`. The intermediate `882116121d25f4e0395a13b4e276f8beb0da267d` checkpoint is
superseded: its two lint exceptions were removed by an additive commit, not
history rewriting. Corrections live on the independent
`fix/unified-candidate-ci-imports` branch. No release, image publication, main
merge, workflow-permission change or deployment was performed.

## Original candidate: actual remote terminal states

After the owner restored the existing Actions setting, a read found no runs for
this SHA. Each of these existing dispatchable test workflows was started once:

| Workflow | Run | Terminal result |
| --- | --- | --- |
| Go CI | [39937262884](https://github.com/particleflow/Memoh/actions/runs/39937262884) | Test, official-core boundary, PostgreSQL fencing/contracts, Redis, Valkey and pgvector passed. Lint failed. |
| ESLint | [39937281319](https://github.com/particleflow/Memoh/actions/runs/39937281319) | Test passed; Lint failed (17 errors, 20 warnings). |
| Native delivery fixtures | [39937294879](https://github.com/particleflow/Memoh/actions/runs/39937294879) | Windows NTFS fixtures, native PostgreSQL and package verification passed. |
| Electron CI | [39937307889](https://github.com/particleflow/Memoh/actions/runs/39937307889) | macOS, Windows and Linux builds passed. |

[Recorded job identities/states](unified-ci-initial-results.json) refer to this
exact SHA. These are not production or physical-PC throughput tests. The
migration and Docker-PR workflows have no manual dispatch trigger; neither was
forced by changing workflow source or creating a PR. No publishing workflow ran.

## Corrections and security boundaries

- Three published browser scripts now use dynamic imports; an unused import is
  removed. Their assertions are unchanged. Original executed sources remain in
  the frozen candidate; old logs are not relabeled as a new execution.
- The embedded `pi_history.cjs` CLI now loads its four Node builtins using
  dynamic imports inside an async entry point. It remains the same finite `.cjs`
  host helper, not Pi/Agent core. Its history validation and reports are unchanged.
  ESLint configuration is byte-identical to the frozen candidate: no new
  whitelist, ignore or disabled rule. [Thirteen executable probes](ci-import-policy.mjs)
  require nine `require()` inputs to fail and four dynamic imports to pass.
- Go fixture cleanup failures are checked. Formatting, named HTTP constants,
  discard logger, shadowed builtin naming and unused parameter fixes preserve
  assertions and runtime semantics. The equivalent native evidence predicate
  keeps the same scope/history selection.
- **G704 remains unresolved and still fails the unchanged check.** It reports
  `internal/imagecapability/protocol.go:89`, the existing `Do(req)`. This file is
  byte-identical to the frozen candidate. The intermediate narrow suppression
  and exploratory body-construction change were both removed. No linter
  exception or check-policy change remains.
  Source review shows destination selection from server-managed provider config,
  strict tool-argument decoding and redirects disabled. A real-HTTP regression
  verifies that a URL embedded in prompt text cannot select another server.
  This supports analysis, not green lint or a general SSRF protection claim.
  Configured private endpoints are intentionally supported. The remaining
  diagnostic needs a reviewed decision on the configured-provider trust boundary,
  not a blind private-network ban, hidden indirection or rule suppression.
  The exact analyzer is gosec v2.23.0 from golangci-lint v2.10.1; its
  [G704 configuration](https://github.com/securego/gosec/blob/v2.23.0/analyzers/ssrf.go)
  treats the request object as a sink. Investigation used no production
  credentials or paid provider calls.

The [original Go lint errors](unified-ci-go-lint-original-excerpt.log) remain
readable. No official Agent core, API schema, migration, provider configuration,
credential, storage policy or legacy-media P1 code was changed.

## Local checks on the correction

- Final `golangci-lint run ./...`: **exit 1**, only G704 remains. [Current output](unified-ci-go-lint-current.log). Earlier targeted/full passes on the superseded exception-bearing checkpoint are not final acceptance.
- Focused race regressions: [29 top-level PASS](unified-ci-focused-go.log), exit 0, no skips. These include internal spawn using synthetic HTTP models, native evidence and background wrapper behavior.
- Remaining protocol/authorization/storage/timeout regressions: [11 top-level PASS](unified-ci-protocol-boundaries.log), exit 0, no skips. No paid provider was called.
- Repository ESLint: [0 errors, 20 existing warnings](unified-ci-eslint-local.log), exit 0. Warnings remain visible.
- [Thirteen import-policy probes](unified-ci-import-policy.log), exit 0. Syntax checks passed for the host history helper and all three browser scripts.
- Final host-history race regression: [8 top-level PASS](unified-ci-history.log), exit 0, no skips; package 19.692 s. These execute real Node/Bash with synthetic session files.
- Final unchanged-protocol destination regression: [1 PASS](unified-ci-routing.log), exit 0, no skips; second HTTP server receives zero requests.
- The official runtime/contextview boundary script passed; `packages/runtime` is also unchanged. Frozen branches were not moved.

A fresh `go build -p 2 -o /tmp/web-management-checks/ci-no-exemption-server ./cmd/agent`
exited 0. After removing the exceptions, the isolated server was replaced only after verifying zero active
runs; [its binary and source-diff hashes](unified-ci-runtime.json) identify the
build and health HEAD returned 200. The existing Web remained at the unchanged
candidate source. [Current-binary browser observation](unified-ci-browser.log)
passed: [7,400 bytes and full hash](unified-ci-browser-proof.json), own child
completion/text, zero POSTs and no page errors. Fresh inspected screenshots:
[scoped download](unified-ci-download.png), [narrow child view](unified-ci-child.png).
This is the real local application and PostgreSQL with an already-created
synthetic-model task, not a new paid-model or PC execution. The old
unknown-state browser and delivery fault-injection flows were not repeated for
import-only corrections. Only existing authorized synthetic task observations
are eligible for the read-only browser check.

## Storage and remaining handoff constraints

Only 31,010 verified regenerable files in this task's Go build cache were
removed (5,910,357,084 bytes). Cached tools, source, Git history, raw evidence,
originals and databases were preserved. No Docker prune or unknown-directory
cleanup occurred. Scratch regained approximately 5.9 GiB before checks; the
root filesystem remains full. It needs supported environment storage repair,
not deletion of application data. This limits new local database workloads.

The remaining G704 lint failure blocks claiming this correction is fully green. Legacy media P1 remains unresolved and held. OAuth recovery, real PC acceptance,
and the missing original mobile overlay/native shell remain separate blockers.
The Windows source contact is `a6908728` with `/api` support; the mobile source
contact is `b568d81`. Those contact points are parent handoff facts, not new
local device verification. A passing candidate check does not approve release.


## Full root filesystem: checkpoint preservation

Writing an additional selected log copy returned `ENOSPC`. No source, database
or raw evidence was removed. The existing authorized scratch filesystem still
had capacity, so a same-environment shared Git checkout under
`/tmp/web-management-checks/ci-write-checkout` preserves the correction and
reviewable evidence. The original working tree remains in place with its diff
also saved as a patch. No new execution environment, credentials or production
path was introduced. The root filesystem still needs platform storage repair.
