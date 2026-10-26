# Source verification

Base: `50d69c7b77be0dda884e2708db6167a1d561bba0`.
Branch: `fix/retire-agenvo-project-wide`.
Status: source fixes and isolated checks complete; not deployed or device/UI accepted.

| Check | Result |
| --- | --- |
| `go test -p 2 ./internal/agenvo ./internal/userruntime ./internal/workspace ./internal/computertask ./internal/handlers ./cmd/internal/core ./internal/agent/tool ./internal/nativeherdr ./internal/sessiontask` | PASS, all nine packages. See `go-packages.txt`. The native-only wiring, legacy registration/list/transfer rejection, retained primary/no fallback, HTTP retired-route 404 and native transport regressions are included. |
| `go test -p 2 -tags integration ./internal/db -run 'TestNativeRuntimeCutover\|TestNativeHerdr\|TestProjectWideAgenvoRetirement\|TestComputerDeliveryPostgresAuthorizationCASAndPreservation' -count=1 -timeout=8m` | PASS in 202.983 seconds. Ten matching top-level tests plus subtests, including active/inactive/malformed legacy inventory, no old OAuth/dispatch, row/unknown-history preservation, denied legacy grant/transfer, native admission/RLS/CAS and native delivery/receipt recovery. |
| `vitest run apps/web/src/utils/workspace-target.test.ts apps/web/src/pages/home/components/tool-call-inline.test.ts` | PASS: 2 files, 47 tests. Includes retired online snapshot denial and preserved unknown/retained-workspace receipt rendering. |
| `tsc --noEmit -p packages/sdk/tsconfig.json`; `vue-tsc --noEmit -p apps/web/tsconfig.json` | PASS, no diagnostics. |
| ESLint on changed Web Vue/TS files | PASS, no diagnostics. |
| `node scripts/check-ui-contract.mjs` | PASS, existing three warning categories only. No baseline changed. |
| `go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate` | PASS; only the two changed query output files regenerated. Toolchain auto-selected Go 1.26.10 as required by pinned sqlc. |
| `go generate` in `internal/handlers`; `openapi-ts` | PASS. Old discovery/agent/endpoint operations and registration payload are absent from SDK/schema. Native/shared `Agenvo*` type names intentionally remain. |
| `bash scripts/check-agent-core-boundary.sh` | PASS: Agent runtime/contextview match official baseline `bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046`. |
| `git diff --check`; patch apply on clean exact base checkout | PASS. |

The PostgreSQL fixture was a new local `postgres:17-alpine` container bound only to `127.0.0.1:15439`, with disposable databases created/dropped by repository tests. It was stopped after checks. No production database, private deployment configuration, device, OAuth service, live model or browser was contacted. Inherited proxy variables were empty for the final Go test process so test-owned image fixtures could use their configured local dialer; product URL validation and image code were unchanged.

Earlier failures exposed old test assumptions: active Agenvo inventory/probes and legacy delivery configuration. Those fixtures now test the native provider and explicitly assert retired-provider rejection. No product behavior was weakened to preserve those old assumptions.

No real Web interaction, screenshot, Windows PowerShell execution, PC/NAS device behavior, independently packaged Harmony client, live old-schedule status or deployment is claimed. These remain with the designated unified/onsite writer. The Windows delivery fixture setup was updated to native registration, but Windows integration execution was not run here. This independently tested source branch is for unified composition and is not a deployment or device acceptance ref.
