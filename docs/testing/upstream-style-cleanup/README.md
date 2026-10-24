# Restore three upstream expressions

Parent candidate: `9ee1040db545b33ff1dc6b023e512e2bbaa4ad4f`.
Reference upstream: `bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046`.

This reversible cleanup changes exactly three handwritten product files, +5/-8:

- `internal/agent/tool/background.go`: return `backgroundStatusMap(session,
  task.Snapshot())` directly. Neither intermediate variable had another caller.
- `internal/providers/service.go`: construct the SDK provider before deriving
  the model-list timeout, matching upstream. No request runs at construction;
  `ListModels` still receives the timeout context and existing credentials.
- `cmd/internal/core/providers.go`: construct the background and spawn providers
  inline in the provider list. Neither alias had a setter or another consumer;
  ordinary host injection still iterates over the same provider instances.

The first two files now match upstream byte-for-byte. The core file retains all
other adapters, including media persistence, memory references, PC scope and Bot
delegation. `internal/agent/runtime`, `internal/contextview` and `packages/runtime`
remain identical to the reference upstream. Images generation/edit routing,
`SessionHasMediaAsset`, migrations and authorization semantics are unchanged.

## Checks performed

```sh
go test -json -race -count=1 -timeout 120s -p 2 \
  ./internal/providers ./internal/agent/tool ./cmd/internal/core \
  -run 'Test(BackgroundProvider|GetBackgroundStatus|WaitUntil|FetchRemoteModelsViaSDK|ImageCapability|ProvideTool|ToolProviders|Inject)'
go build -o /tmp/upstream-style-cleanup/server ./cmd/agent
git diff --check
```

The [test output](affected-tests.jsonl) contains 25 test PASS events (including
subtests), 3 package PASS events, zero FAIL and zero SKIP; exit 0. The actual
internal spawn test uses production host/runtime services with synthetic storage
ports and an HTTP model fixture. It does not call a real provider or PC.
The server build exited 0. A first command mistakenly named the nonexistent
`./cmd/server`; its [exit-1 output](build.log) is retained, followed by the correct
`./cmd/agent` build result. No product change addressed that command error.

## Current runtime and browser

The existing cloud-only fixture database `integration_web_types` had 8 completed
and 1 failed run, with no active run, before restarting its development server.
Only that server process was restarted, using the newly built binary and the
same private synthetic configuration/identity. The initial launch used the wrong
environment variable `MEMOH_CONFIG_PATH` and failed with `jwt secret is required`.
Using the documented `CONFIG_PATH` loaded the existing configuration successfully;
no credential was generated or changed. HEAD `/health` returned 200 (GET returns
405 on this route). The original database, files and records were retained.

The existing Vite UI at loopback port 18102 was exercised against the changed
server on loopback port 18100. The [browser script](browser-detail.mjs) navigated
an existing synthetic supervisor session -> specialist handoff -> child task
detail. The current detail returned 200 and showed the stored synthetic final
text. [Proof](browser-proof.json): 34 API requests, all GET, zero page errors.
The [screenshot](browser-detail.png) was opened and inspected. This was a real
browser/server/PG read path; no new delegation, business task or model invocation
was submitted. The affected background tool and model-list behavior were exercised
by the focused runtime tests, not inferred from this screenshot.

Evidence files were checked against the private fixture's secret values before
copying. Private configuration, tokens and server logs are excluded. Published
output files are unchanged; [manifest](manifest.json) records their byte counts
and SHA-256 values. Screenshot identifiers and visible text belong only to the
existing synthetic fixture. The browser script needs the original private fixture
input files and this environment's Playwright installation; it is not standalone.

## Remaining boundary

This is source cleanup, not production acceptance or approval to deploy. No main
merge, image build, tag promotion or NAS/Windows operation was performed. Existing
production holds remain. There is no further demonstrated removable Images code
in this change: whether generation interception is redundant still requires the
upstream path's acceptance; minimal edit support and NAS artifact delivery remain.
