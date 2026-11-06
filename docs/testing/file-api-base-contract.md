# File API-base contract: cloud verification

This independent increment starts at PR #69's evidence-only head
`3ec5d309a8c8e6dd8917fd4599d6d526d397037d`, which contains unified server candidate
`dfc7cf5104c382ecc622061ce43c3dcd733b7998`. It preserves those frozen commits and
does not rerun or relabel PR #69's round-trip evidence.

The [shared contract](../file-service-endpoint-contract.md) introduces no server
route, task ID, credential, storage root or migration. Only transfer-client URL
validation and independent regression tests change executable code.

## Tests actually executed

On the Linux cloud workspace, 2026-10-23 UTC:

| Check | Recorded result |
| --- | --- |
| New syntax vectors against the prior parser | Expected failure: 13 ambiguous/invalid inputs were accepted. The original local red output is retained. |
| All 30 shared vectors against `ValidateAPIBase`, `New` and `NewTask`, with race detection | PASS, 1.023 s; no credential callback or endpoint probe during URL construction. |
| Transfer library and production CLI package tests, with race detection | PASS, 60.663 s and 7.820 s. |
| New `api-base-prefix` test using actual Server routes, full-migration PG, stable-device registry verifier and real file storage | PASS, 8.853 s for the package, 0.68 s for the subtest; zero SKIP. |
| Integration-tag incremental golangci-lint | Exit 0, no findings, after the new validator's boolean expression was simplified as requested by staticcheck. |
| `git diff --check` | Passed. |

The PG and full-package tests ran after the functional changes. The final vector
run and lint ran after the equivalent boolean-expression cleanup; there was no
subsequent executable-code change. The recorded outputs below are copied
verbatim from those existing runs, with no regeneration or redaction. Complete
contents and credential/private-address markers were checked before copying;
they contain only test names, generic fixture observations and durations.

| Readable original output | SHA-256 |
| --- | --- |
| [30 vectors](evidence/file-api-base-contract/vectors.txt) | `588446cf8a358c32e62e6ad062d65d30b7bcbdfec5a0c7b8d6daf8302b487bef` |
| [PG prefix test](evidence/file-api-base-contract/postgres-prefix.txt) | `b3ce61000f16dd711af58c1c973d84b13e0d9b5326fa049b06de42209916ad58` |
| [Library and CLI packages](evidence/file-api-base-contract/package-race.txt) | `e076ab07af1b290666f93119af4b67c793aa16e198a4c6c9fdfd72db75d2d4da` |

Hashes identify these local execution records; they are not an independent CI
result. The full package output is not verbose, so it does not establish that
platform-specific or opt-in tests ran. The explicitly selected PG scenario and
the 30 vector cases do have named PASS lines.

## Actual prefix scenario and limits

An isolated CA-verified HTTPS listener mounts the actual Server at `/api` using
`http.StripPrefix`. Only that path rewrite is a fixture; authentication, handlers,
PostgreSQL authorization/CAS/ledger, uploads and Media storage are real. The test
uses the existing disposable database and NOBYPASSRLS runtime role. Synthetic
identities, credentials and 26-byte content stay within the fixture.

The existing client performs limits, Create, Get, PATCH, Complete, Commit and
Resolve through `/api/internal/computer-transfers/...`. Every expected route is
asserted, including the original upload receipt and the final committed
reference. No in-memory NAS protocol replaces the server and no TLS verification
is disabled. No OS trust store is changed.

This proves API-prefix upload/association compatibility. It does **not** run
Nginx, implement or test LAN/IPv6 path selection, test prefixed download, exercise
Windows/mobile UI, or perform a real PC/phone/NAS acceptance. PR #69 separately
records upload and interrupted download through the real server without this
prefix; its previous evidence is reused, not expanded by inference. There is no
new Linux product or Linux publication binding.

No production requests, installs, credentials, security changes or migration
were performed. No development runtime/UI was installed or screenshots captured
in this scoped cloud transfer task. Human QA remains outstanding. The separately
blocked legacy Media issue and previously denied artifact downloads were not
touched.

GitHub Actions were previously rejected with HTTP 422, `Actions has been
disabled for this repository`. No dispatch was retried for this increment and
no workflow/security setting was changed. Native/remote CI is not claimed.

## Reproduction

Use an existing disposable PostgreSQL admin DSN in `TEST_POSTGRES_DSN`; the
fixture creates and removes its own databases and roles. A missing DSN produces
a skip and does not count as acceptance. Do not use a production database.

```sh
go test -race -v -count=1 -run '^TestAPIBaseContractVectors$' ./internal/transferclient
go test -race -count=1 -timeout 5m ./internal/transferclient ./cmd/transferqueue
go test -p 1 -v -race -tags=integration -count=1 -timeout 90s \
  -run '^TestComputerDeliveryPostgresAuthorizationCASAndPreservation$/^api-base-prefix$' ./internal/db
golangci-lint run --build-tags=integration \
  --new-from-rev=3ec5d309a8c8e6dd8917fd4599d6d526d397037d \
  ./internal/transferclient ./internal/db
```
