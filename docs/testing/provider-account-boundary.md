# Provider management account boundary

Provider management now requires an account JWT that passes the server's
existing current-session validation. A signed `chat_route` token receives
`403 http.forbidden` before provider/model reads, writes or upstream requests,
even when its user ID matches an active account. The global chat-token behavior
and the Agent core are unchanged.

The guard is attached to the existing ProvidersHandler group and to the four
separately registered OAuth management routes. OAuth callbacks retain their
existing code/state validation and are not put behind the account guard.

## Route inventory

| Method | Route |
| --- | --- |
| GET, POST | `/providers` |
| POST | `/providers/from-template` |
| GET, PUT, DELETE | `/providers/:id` |
| GET | `/providers/:id/models` |
| GET | `/providers/:id/media-catalog` (inherited route, source unchanged) |
| GET | `/providers/name/:name` |
| GET | `/providers/count` |
| POST | `/providers/:id/test` |
| POST | `/providers/:id/import-models` |
| GET | `/providers/:id/oauth/authorize` |
| POST | `/providers/:id/oauth/poll` |
| GET | `/providers/:id/oauth/status` |
| DELETE | `/providers/:id/oauth/token` |

The 16 method/path combinations are enumerated from the actual registered
routes in the regression test. `/auth/callback` and
`/providers/oauth/callback` still reach the existing state validation.

## Synthetic reproduction and regression

On the unchanged base, the new regression demonstrates that a chat-route token
can update a synthetic provider's destination without providing a new key, then
probe that destination with the retained synthetic credential. The observed
baseline is PUT 200, test 200, one store write, one request to a second local
HTTP fixture and zero account-session validations. No real credential or
external provider is involved. This is a credential-use boundary issue, not a
claim that the standard masked GET response returns the full key.

With the guard, the same two requests return 403 with zero store writes and
zero upstream requests. The route matrix covers chat tokens, chat tokens naming
an active account, anonymous, revoked and expired account tokens, through both
Authorization headers and query tokens. Unexpected store calls panic in the
fixture rather than silently succeeding; configuration snapshots remain equal.
Existing error/probe fixtures now send a signed, session-validated synthetic
account token so their original error assertions still exercise the real guard.

Normal account list/detail/name/models/count/create/update/delete/import/test
paths remain covered. Callback checks still reject missing state rather than
requiring an account token. Global auth tests and a chat-route transport fixture
confirm that chat tokens remain valid for their intended non-management use;
this does not claim a real Bot/model chat was executed.

## Runtime verification

A fresh isolated PostgreSQL database and a server built from the modified source
were exercised through the real Web. All 16 management requests with a
synthetic chat-route JWT return 403; complete provider/model row snapshots match
before and after, with zero upstream requests. Anonymous and revoked accounts
return 401, and a foreign-team provider returns 404. Both OAuth callbacks still
report missing state (400) without an account token.

A normal synthetic account logs in through the native page, saves a provider
name change, reloads it, tests the connection, and restores the name. The test
makes one GET to the local synthetic `/v1/models?limit=1` endpoint. The saved
credential and destination remain unchanged, model snapshots match, and no
browser page errors occur. This does not perform a model call or OAuth exchange.

## Integration and limits

The product patch is the small group-registration hunk in `providers.go`, four
OAuth route-registration additions in `provider_oauth.go`, the shared handler
middleware in `provider_account.go`, and 401/403 API annotations. No media-catalog
implementation file, provider credential merge, model import algorithm or
Agent core is changed. Preserve the concurrent media-catalog change when
integrating the registration hunk.

Swagger/SDK were regenerated and type checked, with generated output held as a
separate patch for integration. Regenerate after combining branches:

```sh
go generate ./internal/handlers
pnpm run generate-sdk
pnpm --filter @memohai/sdk typecheck
```

Required checks for this change:

```sh
go test -p=2 ./internal/auth/... ./internal/providers/... ./internal/handlers/... ./internal/apperror/... ./internal/server/...
go vet -p=2 ./internal/auth ./internal/providers ./internal/handlers
NODE_OPTIONS=--no-experimental-webstorage pnpm exec vitest run apps/web/src/utils/api-error.test.ts apps/web/src/composables/api/sse-error.test.ts
node .husky/checks.mjs
```

All reproduction and runtime work uses an isolated cloud test database and local
synthetic HTTP fixtures. No production provider settings, keys, OAuth grants,
NAS or Windows environment are accessed. A source fix is not production
mitigation until the unified candidate is released and deployed through the
existing workflow. Human QA and production verification are not claimed.
