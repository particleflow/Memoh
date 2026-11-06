# Directional CLI feedback on candidate 9601

Integration base: `9601ac062a3b65477db8db76bcaa521265ba7ee2`.
Fixed source: `0894a6eb8ff69c8796e2da5450fa172d4cd5ef81`, the single commit after the already integrated `648e4511f35fe2ce187509e9823d03c1d2e1d341`.
Product checkpoint: `e4e97009` (full SHA in `manifest.json`).

Exactly six source files were integrated with no conflicts. Every preimage matched the prior source and every postimage matches 0894a6eb byte-for-byte; see `source-files.json`. Product behavior changes only the six-line direction guard in `cmd/transferqueue/main.go`, plus the component version from 0.10.0 to 0.10.1. The remaining additions are tests and documentation. The observation/OAuth failure handling, direct-parent projection, SDK/OpenAPI, official Agent core and database schema are unchanged from 9601. No new server endpoint, credential, configuration or data migration is introduced.

## Consumer contract

The existing CLI produces `feedback_status`; the unified Windows endpoint consumes it. Only an actual durable queue record with direction `download` returns `not_applicable`. Missing/foreign outbound context and actual feedback write failures remain `unavailable`; valid outbound v1 feedback remains `recorded`. Empty/error results do not establish a download. The source contract is [CLI feedback applicability](../../delivery-producer-feedback.md#cli-feedback-applicability).

`not_applicable` bypasses only the outbound producer-feedback projection. It does not prove delivery, authorization, association, publication or task completion. Consumers still inspect command exit, lifecycle/backoff, phase, original identity and committed reference. Producer and Windows consumer versions must be aligned; do not reinterpret an old producer's `unavailable`. Updating a NAS container alone does not replace the Windows executable. Byte transport, Pi result declarations and UI result browsing remain separate layers.

## Actual combined checks

All checks ran on the combined Linux cloud worktree with Go 1.25.7 and existing caches, without production data or credentials:

| Command | Result |
| --- | --- |
| `go test -race -p=1 -count=1 -v -timeout=120s ./cmd/transferqueue` | Exit 0; 22 named PASS entries including subtests, zero SKIP; 16.125s. Includes inbound no/foreign context, outbound valid/missing/foreign/write-failure cases, repeated registration/inspect, unknown delivery, original TLS restart/lost-response and producer-page recovery fixtures. |
| `go test -race -p=1 -count=1 -v -timeout=120s -run '^(TestProducerFeedbackScopeAndUnchangedBlock\|TestFeedbackStrictOriginalBindingAndBound)$' ./internal/transferclient ./internal/deliverymeta` | Exit 0; 18 named PASS entries including subtests, zero SKIP; packages 1.020s and 1.041s. |
| `golangci-lint run --concurrency=2 ./cmd/transferqueue` | Exit 0; existing v2.10.1; no diagnostics. |
| `go build -o <task-temporary-output> ./cmd/transferqueue` | Exit 0. |
| `bash scripts/check-agent-core-boundary.sh` | Exit 0; official runtime/contextview unchanged. |

The commit command did not emit hook execution. The lint, race tests and build above were executed explicitly; `commit-hook.log` is only the commit output, not an aggregate-hook success claim. `manifest.json` records hashes, sizes and exit codes for the readable, unmodified outputs. They contain synthetic fixture names/public commit metadata only.

No Web/SDK/config/HTTP/DB file changed, so their unchanged suites and previous browser screenshots were not rerun or relabeled as new acceptance. No Windows test was executed on this Linux host. The Windows-only inbound fixture remains in the source and requires final-head Native CI; source-branch success does not establish integration success. Final-head remote CI is tracked separately after push. No phone/PC installation, physical-device transfer, NAS deployment, OAuth request or candidate-image publication was performed for this integration.
