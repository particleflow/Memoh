# Private device response cache policy

Based on frozen integration `813d50ff97f22f613e0967fac2528b5c2fb5400e`.
The production change adds 22 lines in the two device handlers, with no changes
to authorization, device observation, status projection, schemas or Agent core.

## Scope and HTTP regression

- Echo Pre middleware sets `Cache-Control: no-store` before routing and JWT for
  `/bots/{bot}/workspace-targets` and `/users/me/runtimes`, including descendants
  and all methods. Unrelated routes and lookalike prefixes retain their policy.
- The matcher follows Echo's escaped request path. Encoded dynamic Bot segments
  remain in scope; encoded static names are distinct routes. Echo's accepted
  empty Bot segment is also covered when the handler rejects it with 400.
- A real TCP HTTP test constructs the production Server/JWT/session-validator/
  CORS/Problem chain with actual handlers and synthetic device stores. It covers
  successful lists, anonymous/malformed/expired/bad-signature/revoked JWTs,
  cross-user 403, missing Bot 404, namespace 404/405, and neighboring cache policy.
  Rejected requests disclose no device markers or private causes and do not
  reach device observation/storage. The authenticated target response keeps
  `authentication_unavailable` with `online: false`.
- The new test reproduced missing headers on the unchanged baseline before the
  production fix. Focused handler/server race tests, including existing task and
  OAuth no-store regressions, scoped golangci-lint and server build pass.

## Current application and baseline comparison

The rebuilt Go server (`http://127.0.0.1:18080`), Vite UI
(`http://127.0.0.1:18082`) and local PostgreSQL schema 170 were healthy. Actual
login and authenticated queries passed through both Go and the Vite proxy.
Only synthetic fixtures were used; no production PC, secret or configuration
was imported or published.

- Ten real application boundary checks and six device status checks passed.
  Account/Bot browser flows inspected seven actual device HTTP responses, all
  with no-store. Authentication unavailable, explicit offline and native-service
  unavailable remain separate. No browser errors; operation records unchanged.
  The temporary native HTTP fixture/configuration was restored after verification.
- A same-environment run of frozen 813d50ff versus the patched binary confirmed
  identical statuses, with only the cache header added. The full application's
  existing `/users/:id` fallback returns 500 for the synthetic unknown path
  `/users/me/runtimes/missing/extra`; this is not a new 404 regression. The patched
  500 is also no-store. Bot unknown paths return 404, and both anonymous list
  paths return 401. This increment does not change fallback routing/error codes.
- Browser screenshots below are freshly captured and visually inspected. They
  show UI semantics, not proof of authorization or cache behavior; those are
  established by the HTTP assertions. This is agent verification, not human QA.

[Account authentication unavailable](account-auth.png) ·
[Bot authentication unavailable](bot-auth.png) ·
[Explicit device offline](account-offline.png)

No frontend source or dependency changed. This patch does not include the
separate typecheck fix or claim that the baseline's 442 type errors are resolved.
No new candidate, deployment or main promotion is part of this checkpoint.
