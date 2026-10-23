# Unified candidate CI restoration and lint corrections

The frozen candidate remains `8c358cac27922f122315ac9db62d52cc100c8a2a` on
`integrate/unified-server-web-management`. Corrections live on the independent
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
- The embedded `pi_history.cjs` CLI retains CommonJS. The existing ESLint policy
  now explicitly allows only its four Node builtins in that exact file. The rule
  remains enabled elsewhere and still rejects other modules in that file.
  [Nine executable positive/negative probes](ci-import-policy.mjs) verify this.
- Go fixture cleanup failures are checked. Formatting, named HTTP constants,
  discard logger, shadowed builtin naming and unused parameter fixes preserve
  assertions and runtime semantics. The equivalent native evidence predicate
  keeps the same scope/history selection.
- G704 reports the Images API request whose address comes from server-managed
  provider configuration. `parseConfig` validates its HTTP(S) shape; strict tool
  decoding rejects endpoint/model/authority fields; redirects remain disabled.
  A single rule-specific annotation records this established trust boundary,
  following the existing configured-provider adapter convention. No global
  security rule or check is disabled. This is **not** a claim that configured
  private endpoints are blocked or that administrators cannot configure them.
  The exact analyzer is gosec v2.23.0 from golangci-lint v2.10.1; its
  [G704 configuration](https://github.com/securego/gosec/blob/v2.23.0/analyzers/ssrf.go)
  treats the request object as the sink. An exploratory body-construction change
  did not resolve the report and was discarded; final HTTP behavior is unchanged.
  A new real-HTTP test places another server URL in model-controlled prompt text:
  only the configured destination receives one POST, the other receives zero,
  and content length and prompt bytes are preserved. This annotation requires
  review with these boundaries, not a claim of general SSRF sanitization.

The [original Go lint errors](unified-ci-go-lint-original-excerpt.log) remain
readable. No official Agent core, API schema, migration, provider configuration,
credential, storage policy or legacy-media P1 code was changed.

## Local checks on the correction

- `golangci-lint run ./internal/imagecapability ./internal/computertask ./internal/agent/tool ./cmd/internal/core`: exit 0 after corrections. The preceding attempt retained G704 and exited 1; that failure is not counted as success.
- Focused race regressions: [29 top-level PASS](unified-ci-focused-go.log), exit 0, no skips. These include internal spawn using synthetic HTTP models, native evidence and background wrapper behavior.
- Remaining protocol/authorization/storage/timeout regressions: [11 top-level PASS](unified-ci-protocol-boundaries.log), exit 0, no skips. No paid provider was called.
- Repository ESLint: [0 errors, 20 existing warnings](unified-ci-eslint-local.log), exit 0. Warnings remain visible.
- [Nine import-policy probes](unified-ci-import-policy.log), exit 0. Syntax checks passed for all three browser scripts.
- The official runtime/contextview boundary script passed; `packages/runtime` is also unchanged. Frozen branches were not moved.

A fresh `go build -p 2 -o /tmp/web-management-checks/ci-lint-server ./cmd/agent`
exited 0. The isolated server was replaced only after verifying zero active
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

Legacy media P1 remains unresolved and held. OAuth recovery, real PC acceptance,
and the missing original mobile overlay/native shell remain separate blockers.
The Windows source contact is `a6908728` with `/api` support; the mobile source
contact is `b568d81`. Those contact points are parent handoff facts, not new
local device verification. A passing candidate check does not approve release.
