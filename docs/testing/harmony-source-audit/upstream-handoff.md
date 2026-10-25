# Shared Web delayed-401 handoff

Verified phone fix: `e26d56b8b544231bb9e9d7990656307274b41337` on
`recovery/harmony-source-20261023`, pushed and verified by `git ls-remote`.

`upstream-5e55-auth-session.patch` applies directly to
`5e55c45c85659f04b339b4df42aeff72966542a8` and changes only:

- `apps/web/src/lib/api-client.ts`: capture the token and session generation
  before awaiting fetch; ignore a late 401 from a different session.
- `apps/web/src/lib/auth-session.ts`: increment the in-memory generation on
  the existing login/logout/token-clear/unauthorized notification. The real
  user store already calls this path when starting a new login session.

No persisted authentication fields, credentials, URL selection, permissions,
store reset rules or HTTP protocol change. A current-session 401 still clears
login; network errors preserve it. The generation covers a second login with
identical token bytes, which a token-only comparison misses.

Reproduce: start an anonymous API request; complete login; release the first
request's 401. Before the fix the new login is erased. Also hold a request,
start another session with the same token, then release its 401. Existing
`harmony_auth_race.cjs` failed both cases before and passes after. The real
phone-source browser verification and 146 relevant passing tests are recorded
in this directory's README and raw logs. Browser responses were synthetic;
this is not a production or hardware acceptance claim.

The patch was applied to the exact upstream files in a local isolated scratch
folder and compared with the verified phone source. They are identical apart
from the phone's existing exported `createApiFetch` native file-I/O/test seam and the local
counter name. This patch does not include that export or phone adapters.
It is a transfer of the verified fix, not a second implementation. Shared Web
integration should retain its own regression coverage and adapt any existing
mock of `auth-session` to supply the real generation semantics or an explicitly
controlled generation. The phone test fixture's constant-generation stub does
not test the race; separate production-source and browser checks do.

```sh
git apply --unidiff-zero --check upstream-5e55-auth-session.patch
git apply --unidiff-zero upstream-5e55-auth-session.patch
```

The separate `api-session-race.patch` is the original zero-context difference
against the already-adapted phone API module. Use the upstream patch above for
plain shared Web; it includes the previously phone-only generation helper.
