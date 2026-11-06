# Owner configuration and read-only media catalog integration

This checkpoint preserves published base
`8d583a14f355630a5f4d71976c8265ec1575846c` and adds two independent increments:

- Owner endpoint location: `f89100fe1b64e4a6c057776f6745fbf7f141b1d1`.
  [Contract and runtime/PG evidence](../computer-admission/endpoint-location.md).
- Read-only media catalog source: `3379ae82dc848f8a83df9305afe32ac33e5c264c`,
  cherry-picked with source attribution as
  `75e0dfbc159c93f2d0fe75bcd891a64cde2198d5`.
  [User flow, limits and source-author verification](../provider-media-discovery.md).

There were no source conflicts. The normal `go generate ./internal/handlers` and
`pnpm run generate-sdk` commands regenerate the union; no generated file was
edited manually. All 368 owner checkpoint operations remain, with only
`GET /providers/{id}/media-catalog` added (369 total). The admission/owner routes,
existing SDK operations and migration 0175 remain intact. This increment has no
new schema migration, CI rule change, deployment or production configuration.
Docker #51 / run 40168028583 continues to refer only to the earlier fixed 8d head.

## Review boundary

Discovery uses the current authenticated team's saved, enabled provider and its
existing static OpenAI-compatible credential path. The explicit Magpie protocol
selects a fixed catalog header; it does not infer capability from the model name
or provider hostname. It performs one bounded GET, with redirects disabled and
no shared cookie jar. OAuth client types are rejected before resolution. No
configuration/model import, credential refresh, inference POST or writes are
part of discovery. Responses expose whitelisted catalog facts; errors exclude
upstream bodies, addresses and credentials. Unknown ASR support remains
`unverified`, including audio-input/text-output metadata.

Configured private/self-hosted destinations are deliberately supported; this is
not a claim of universal SSRF prevention or arbitrary encoded-secret detection.
The fixed-header interpretation was inherited from the source author's official
Magpie reference. No real Magpie server was contacted during integration.

The owner locator is independent of the execution Target fingerprint and task
ledger. It configures explicit paths; it neither executes them nor establishes a
new control identity. Current Windows native commands cannot authenticate a
Memoh owner supplied in local JSON, and a Herdr pane has no handle for the
running host's stdin. Automatic stop remains unavailable and
`task_handoff_unverified` stays fail-closed. Observation and result collection do
not depend on adding a stop interface. No stop permit, receiver, credential or
acknowledgement format was invented here.

## Combined verification

These results were executed on the combined source in the existing isolated
cloud environment. They are distinct from the source author's earlier tests.
No production, paid model, Windows host or real provider identity was used.

| Check | Actual result |
| --- | --- |
| `go test -race -count=1 -v ./internal/providers ./internal/handlers -run 'TestMediaCatalog\|TestProviderMediaCatalogBoundary'` | Exit 0; [full output](evidence/go-media.log). Covers read-only requests, failure and empty-list distinction, redirects, sanitization and authorization. |
| Six targeted Web test files listed in the source document | Exit 0, 157 tests; [output](evidence/web.log). |
| `golangci-lint run ./internal/providers/... ./internal/handlers/...` (2.10.1) | Exit 0; [output](evidence/go-lint.log). |
| `pnpm --filter @memohai/sdk typecheck` | Exit 0; [output](evidence/sdk-type.log). |
| `pnpm --filter @memohai/web typecheck` | Exit 0; [output](evidence/web-type.log). |
| Affected Web ESLint | Exit 0, 15 fixture warnings retained, zero errors; [output](evidence/web-lint.log). |
| `node scripts/check-ui-contract.mjs` | Exit 0, three existing warnings and baseline counts retained; [output](evidence/ui-contract.log). |
| `pnpm --filter @memohai/web build` | Exit 0, 5.23s; existing large-chunk warning retained; [output](evidence/web-build.log). |
| `go build -o <isolated-server> ./cmd/agent` | Exit 0; [output](evidence/server-build-corrected.log). First operator command used nonexistent `./cmd/memoh`, exited 1 before compilation; [failure retained](evidence/server-build.log). No product fix was needed. |
| `bash scripts/check-agent-core-boundary.sh` | Exit 0, official runtime/contextview baseline unchanged; [output](evidence/core.log). |
| Actual combined Server/PG/native Web | Passed; [runtime output](evidence/runtime.log), [assertions](evidence/browser-proof.json), [wide screenshot](evidence/catalog-success.png), [narrow dark Chinese screenshot](evidence/catalog-narrow.png). Both screenshots inspected. |
| Real staged `node .husky/checks.mjs` | Exit 0, 9.91s; generated Web lint (four ignored-file warnings), affected Go lint and **complete handlers package** (4.323s), spec has no tests; [output](evidence/aggregate.log). |

The browser uses the actual Settings → Providers → existing synthetic provider
→ Media catalog → Read catalog flow. Opening/reloading makes no upstream call;
unsaved form edits disable the action. Success preserves exact catalog facts;
upstream denial clears previous facts rather than displaying stale success;
`data: []` remains a successful empty observation. A subsequent narrow localized
read recovers successfully. Four catalog GETs, zero media POSTs, zero page errors.
Anonymous HTTP access returns 401. A separately seeded synthetic team's provider
returns 404 under the original user's actual Server/PG authorization, with zero
upstream calls. Provider and model **full-row** snapshots match before and after
discovery. All original operation rows and the owner location GET response also
match. Only the two newly created fixture providers and their temporary team
were removed afterward; the temporary catalog peer was stopped.

The first browser run stopped at an ambiguous test locator because the dialog
has both a text Close button and an accessible Close icon. Its
[failure output](evidence/runtime-first-failure.log) and
[partial assertions](evidence/browser-first-failure.json) are retained. Narrowing
the fixture selector to the text button resolved it; no UI/product change was
made to hide the failure.

An additional `go test -p=2 -timeout=180s` run over providers, handlers, apperror
and server returned exit 1: providers/apperror/server passed, while handlers'
`TestMediaHTTPUploadConfiguredAbove200MiB` and the `over_default_200MiB` case of
`TestMediaHTTPStreamingLimitAndAdmission` returned 500. The [full output](evidence/go-packages.log)
is preserved. At that point only about 263 MiB was free on `/tmp`; the successful
201 MiB upload needs both temporary spool and stored-original space. After
reclaiming exactly 528,891,206 bytes of this task's regenerable Go cache data older
than two hours (769 MiB free), both failing tests and their sibling limit cases
passed unchanged in 1.959s, exit 0; [targeted output](evidence/large-upload-recheck.log).
This supports a local capacity cause; the original masked HTTP error did not
include an ENOSPC syscall trace. No upload code, test expectation, storage rule or
lint setting was altered. The complete four-package command was not rerun;
the subsequent required staged aggregate reran the full handlers package
successfully, while the other three packages retain their initial passing result.

The first staged aggregate also rejected an unused variable in the newly copied
browser replay script; [failure retained](evidence/aggregate-first-failure.log).
Only that unused read was removed. The published scripts under `scripts/` record
the actual isolated assertions (with that cleanup and repository formatting);
they expect the existing private fixture/token/database helper paths and do not
provision credentials or a new environment. They are not a portable test platform.

The [hash manifest](evidence/checksums.json) describes byte-preserved selected
outputs. Tokens, private configuration/registration files, Server logs and raw
database snapshots are excluded. Screenshots contain only synthetic names and a
temporary loopback fixture address. Existing isolated PG, login, Server and Web
were reused. Two stopped task-owned regenerable Server binaries were removed
after confirming no process used them; source, history, DB and evidence remained.

Owner CAS/unknown/Server-restart and real PG tests were already completed for
f891 and were not rerun solely for this independent catalog merge. Remote CI is
not established by these local checks. Human QA, real Magpie catalog IDs and
capabilities, inference/attachment return, actual Windows program ownership and
automatic host stop remain unverified. This checkpoint is source/configuration
readiness, not production deployment or full cross-device acceptance.
