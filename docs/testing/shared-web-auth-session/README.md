# Preserve the new login when an older request returns 401

Product commit: `7ec0f50c17d7f7361b49e7d84a84a6065988da9b`.
Integration base: `cfec37361e46889c94f10e2b7c95f85a39e50b0d`.
This is a shared Web increment on the existing draft integration PR. It does not
publish an image, change production, or repeat the earlier task-reconnect work.

## Source and behavior

The two production files apply the formal Harmony handoff unchanged:
[upstream handoff at 787780](https://github.com/particleflow/Memoh/blob/787780fbe3c1a91262d99a5a3b8820d531406c20/docs/testing/harmony-source-audit/upstream-handoff.md).
The exact transferred patch is included here for review. No mobile builder,
private shell source, audit package, exported test seam, or duplicate Provider
refresh fix was copied.

`api-client.ts` captures the current token and in-memory session generation
before awaiting the SDK fetch. A 401 from another generation or token cannot
clear the new identity. `auth-session.ts` increments the generation through the
existing notification boundary; the real user store already calls it for a new
login, including a login that receives identical token bytes.

Current-session 401 handling, user-scoped cleanup, credential storage, URL
selection, authentication protocol and server permissions remain unchanged.
The generation is not a credential or a persisted authorization mechanism.
Agent runtime/contextview and all backend code are unchanged.

## Regression and build evidence

`evidence.json` records commands, exit codes, byte counts and SHA-256 hashes.
Published logs are byte-identical copies, including the initial failures. The
red-test invocation was not saved verbatim; its listed command is the equivalent
reproduction command, explicitly marked in the manifest.

| Check | Result |
| --- | --- |
| Four new tests on the unpatched base | 4 failed, 13 intentionally unselected; the new login token was cleared |
| Four affected test files | 69 passed, exit 0 |
| Complete Web typecheck | exit 0 |
| Three changed TypeScript files and published browser script, ESLint | exit 0 |
| Web production build | exit 0; existing large-chunk warning retained |
| UI contract | exit 0; 3 existing warnings and baseline counts retained |
| Official Agent runtime/contextview boundary | exit 0 against `bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046` |

The new regression matrix covers REST and SSE, each with an anonymous request
before login and a prior authenticated request followed by a new same-token
login. Tests use the real notification implementation and generation counter;
they do not replace the counter with a constant fixture. Existing tests retain
same-session unauthorized cleanup, changed-token protection, network failures,
SSE cleanup and interceptor idempotency.

## Actual browser verification and limits

The run reused the current-source Web at `http://127.0.0.1:18402`, the existing
isolated Server at `http://127.0.0.1:18400`, and its synthetic PostgreSQL fixture.
The existing Server is the prior test build, appropriate for this Web-only
change; this is not acceptance of a freshly deployed server binary.

The published browser script exercises the real login form, user store,
bootstrapped SDK, router and rendered application. It privately reads an
existing isolated test identity, intercepts the login response and three probe
responses, and uses the real authenticated Server for normal application GETs.
Login interception avoids creating or changing credentials. The optional
profile write is intercepted too. Unexpected writes are blocked and counted;
none occurred. There was one intercepted login and zero task submissions.

1. Start an anonymous SDK request; submit the login form; release its 401.
   The new login remains usable.
2. Hold an authenticated request; invoke the real user store's login boundary
   with the same token; release its 401. The new identity and fresh selection
   survive. Normal reload retains authenticated access.
3. Return a 401 belonging to the current session. The real cleanup removes the
   token and the router returns to the login page.

- [Retained authenticated application](login-retained.png).
- [Current-session 401 returned to login](current-session-cleared.png).

Both screenshots were inspected. They contain only the existing synthetic
fixture. There were no page JavaScript errors. The response tally also records
three background HTTP 503s and one other HTTP 401; this run does not claim all
fixture services were healthy or reinterpret those statuses as successful.

The first browser attempt timed out locating a localized submit button by its
English accessible name. The script switched to the existing form's submit
button, without changing application code, and the complete run then passed.
The initial timeout log is retained. This does not establish real OAuth,
production credential recovery, native Harmony/Windows acceptance, real model
execution or PC task completion. No production browser or data was accessed.

## Cross-end handoff

Shared Web now supplies `authSessionGeneration` and the fetch-context guard.
When Harmony next updates its upstream source, its overlay must recognize the
upstream implementation and avoid inserting a second counter/guard. That
builder integration belongs to the existing Harmony task and was not modified
here. No HTTP schema, API base, package identity or login storage contract
changed. Existing desktop consumers continue importing the same Web module.

The previous `cfec3736` CI results do not certify this increment. Exact new-head
remote checks remain separate; the expired `gh` API credential was not retried.
No workflow, permission, dependency, official Agent core, migration or deployed
image was changed. Human QA is not claimed.
