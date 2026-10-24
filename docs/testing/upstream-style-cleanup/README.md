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
in this change. The upstream chat/SDK path can accept image parts; the separate
`generate_image` tool's text-only schema does not establish that a reference image
needs a custom edit extension. PR68/76 necessity remains pending actual
target-protocol acceptance. No Images behavior or NAS artifact delivery changes.

## Evidence-script lint correction

At head `78541c7954bbc63914426c2cb022d41168002788`, ESLint run
[40088000489](https://github.com/particleflow/Memoh/actions/runs/40088000489)
failed its Run ESLint step (job 120854757237); its Test job passed. The log download
returned Forbidden, so it was not retried or fetched through another route.
The exact remote diagnostic text is unavailable.

Running ESLint locally on the added browser script reproduced four
`@typescript-eslint/no-unused-vars` errors: `parentPath`, `countChildren`, `title`,
and `missing` at the original lines 27, 30, 32 and 34. These declarations were
left over when the longer reconnect observer was narrowed to a detail read.
The correction removes only those four unused declarations, preserving every
interaction and assertion. No product, workflow or lint rule changed.

The [local failure](eslint-fix/local-before.log) is preserved. Full `pnpm lint`
then [exited 0](eslint-fix/full-lint.log), with zero errors and 20 existing Vue
test-fixture warnings. The corrected script was rerun against the same current
server and isolated PG data: [browser output](eslint-fix/browser.log),
[GET-only proof](eslint-fix/browser-proof.json), and
[inspected screenshot](eslint-fix/browser-detail.png). No Go tests or builds were
repeated because this correction has no product diff. Historical test outputs
above remain unchanged; the manifest reflects the current script and both sets
of evidence. Remote success is assessed separately on the new pushed head.
Only surplus EOF blank lines were removed from the three newly published lint-fix
logs; [provenance](eslint-fix/log-provenance.json) records original and published
hashes and sizes. The originals remain in the private cloud evidence directory.
