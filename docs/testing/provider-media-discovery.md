# Read-only provider media discovery

This change adds a catalog preview to the existing provider page. It does not
configure transcription or video execution, import models, change provider type,
or call a model. It leaves the existing Images and chat behavior unchanged.

## Native operation

1. Open Settings → Providers and select the existing enabled OpenAI-compatible
   provider. Save or discard unrelated form edits first.
2. Open **Media catalog**. The dialog names the **Magpie directory protocol**;
   opening it does not make a remote request.
3. Click **Read catalog**. Record the observation time, exact model IDs, kind,
   input/output modalities and their advertised source. Do not use Import,
   Refresh or Test connection for this read-only operation.

The authenticated application requests
`GET /providers/{id}/media-catalog?protocol=magpie`. The server reads the same
team-scoped saved provider and uses its static credential internally for one
`GET <saved base URL>/models` with `X-Magpie-Videomakers: 1`. The user does not
enter or copy a credential, endpoint or guessed model ID. This header is the
versioned Magpie catalog contract, not a host/name heuristic:
[Magpie v0.1.1285 remote discovery](https://github.com/yetone/magpie/blob/d783df46cbac9021e26a6bd71ca0b55544552eb2/internal/provider/remote_magpie.go).

The reply only includes provider ID, protocol, observation time and sanitized
catalog facts. Capability sources distinguish `kind`, `modalities`, both, or
neither. No model name is interpreted as transcription support. Transcription
remains `unverified`, even if a row advertises audio input and text output.
Successful discovery establishes catalog visibility only; it does not establish
model execution or attachment delivery.

## Boundaries

- No database writes, credential refresh, model import/delete, or media POST.
- Only enabled `openai-completions` / `openai-responses` records are accepted.
  OAuth provider types are rejected before credential resolution.
- Existing authentication and provider team scope apply. Anonymous/revoked
  sessions return 401; foreign providers return 404 before any upstream request.
- Only the saved destination is used. Redirects are refused and cookies are not
  shared. The existing probe timeout and catalog response-size limit apply.
- Misconfiguration, upstream 401/403, transport errors and malformed/oversized
  responses are failures, never an empty success. Upstream response bodies,
  credentials, URLs and transport diagnostics are not returned to the browser.
- Empty `data: []` is a distinct successful catalog observation. Missing/null
  data, duplicate/unsafe IDs or trailing JSON are rejected. Exact valid IDs are
  preserved. Unknown capability fields are not inferred or promoted.
- The dialog clears old facts on errors or provider/form changes. It does not
  fetch automatically on open, reload or reconnect.

## Verification

The isolated development Server, PostgreSQL and native Web were exercised with
synthetic catalog HTTP responses. The browser logged in normally with a new
local test identity. That identity's onboarding-complete metadata was prepared
using the existing profile API; no Bot or workspace was created for this test.

The final browser run covers success, upstream denial, a valid empty catalog,
and a narrow dark Chinese view after reload. It makes four catalog GETs and no
media POSTs. Full provider/model row snapshots match before and after (two
synthetic providers, one existing synthetic model). Native HTTP checks return
401 for anonymous access and 404 for a different team's provider. Unsaved form
edits disable discovery, opening/reloading does not query, and no page errors
occur. An earlier screenshot exposed a missing Close translation; the label and
its browser assertion were corrected and the screenshots refreshed.

Relevant commands:

```sh
go test -p=2 ./internal/providers/... ./internal/handlers/... ./internal/apperror/... ./internal/server/...
go vet -p=2 ./internal/providers ./internal/handlers
golangci-lint run ./internal/providers/... ./internal/handlers/...
NODE_OPTIONS=--no-experimental-webstorage pnpm exec vitest run \
  apps/web/src/pages/providers/components/provider-media-catalog.test.ts \
  apps/web/src/pages/providers/components/provider-form-images.test.ts \
  apps/web/src/pages/providers/components/provider-form-test-connection.test.ts \
  apps/web/src/pages/providers/components/provider-form.test.ts \
  apps/web/src/utils/api-error.test.ts \
  apps/web/src/composables/api/sse-error.test.ts
pnpm --filter @memohai/web typecheck
node scripts/check-ui-contract.mjs
node .husky/checks.mjs
```

Unit fixtures cover exact header/destination, no writes, authorization, redirects,
secret/address echoes, response limits, errors and stale UI requests. No real
provider credential was read, and no real Magpie catalog or model was called.
Human QA is unconfirmed. Production discovery, actual authorized ASR/video IDs,
model calls and attachment return remain unverified.

## Integration

Source annotations are included. Generated Swagger/SDK files are deliberately
held outside this commit to avoid overlapping the concurrent integration branch;
no generated file was manually edited. Generation and SDK type checking were
already verified against this source. Regenerate after integration with:

```sh
go generate ./internal/handlers
pnpm run generate-sdk
pnpm --filter @memohai/sdk typecheck
```

The page currently uses the existing authenticated SDK client through a small
typed local helper, following the existing page API pattern. It creates no
second client or credential path. Integrate only this discovery commit onto the
unified candidate; its branch base already contains the earlier delegation-run
observation change.
