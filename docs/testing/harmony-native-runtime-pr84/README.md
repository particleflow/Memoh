# Fixed PR84 SDK compatibility review

Reviewed phone source `4e3c4d279f7f3c7770843d0714682f3297253fd7`
(shared Web locked to `fa77a073fe6380d297474a6053a1445201bcab83`) against
native server/SDK candidate `2030c2d522695f52a227f12421800fe075760813`.
PR84 is a published source candidate, not evidence of deployed Windows behavior.
This review introduces no product or build-input changes.

## Result and minimum scope

The fixed candidate changes no `apps/web` files relative to the phone's shared
baseline. Its four generated SDK files add the native configuration method and
optional native identity/profile fields. Of 2,292 existing type definitions,
none are removed; only `AgenvoTarget` and `UserruntimeRuntime` gain optional
fields. Existing task, admission and transfer request/response types are unchanged.
Exact file SHA-256 values and scope checks are in [contract-check.json](contract-check.json).

| Capability | Existing interface and consumer | Necessary phone change |
| --- | --- | --- |
| Herdr/Pi paths, session and model | PR84 `PUT /users/me/runtimes/{id}/native-herdr`, generated `AgenvoNativeHerdrProfile` | None. Use the owner deployment API; no second phone configuration schema or form. The new SDK method has no existing page caller. |
| Computer listing and Bot access | `use-computer-access.ts`: list with `getUsersMeRuntimes`, grant by Runtime ID, revoke by mount target ID | None. Optional `native_herdr` is a server binding, not a replacement target ID. |
| PC selection and availability | `workspace-target.ts`, `chat-pane.vue`: `kind=remote`, `provider=memoh`, existing connection status | None. Windows is not the `kind=native` NAS workspace; offline PCs remain unavailable for dispatch. |
| Admission and transfer configuration | Existing SDK admission and transfer-credential methods, no phone page callers | None. PR84 supplies the native server branch; no phone scheduler or credential enrollment is introduced. |
| Task content, observation and deliveries | Existing scoped task readers and owning-Bot attachment bridge | None. DTOs/components are unchanged; process, business, unknown and cancel-intent states remain separate. |
| Address, login and package identity | Persistent configuration and Windows pre-signing injection slot | None. Seven preservation hashes and all 43 phone adaptations match; public configuration stays empty. |

PR84 source confirms native admission skips the Agenvo probe, transfer SQL admits
activated/non-revoked configured Memoh runtimes, and configuring Herdr returns
the Runtime DTO without reissuing its key. Generated transfer ID comments still
say “Agenvo”; the actual path/type is unchanged. This wording does not require
a second phone endpoint or a generated SDK hand edit.

Source: [fixed contract](https://github.com/particleflow/Memoh/blob/2030c2d522695f52a227f12421800fe075760813/docs/native-runtime-herdr.md),
[admission](https://github.com/particleflow/Memoh/blob/2030c2d522695f52a227f12421800fe075760813/internal/agenvo/admission.go),
[Runtime projection/configuration](https://github.com/particleflow/Memoh/blob/2030c2d522695f52a227f12421800fe075760813/internal/userruntime/service.go),
[transfer SQL](https://github.com/particleflow/Memoh/blob/2030c2d522695f52a227f12421800fe075760813/internal/db/postgres/sqlc/runtime_transfer_credentials.sql.go).

## Isolated validation

In the disposable generated phone context, replace only the four SDK files in
the JSON with their exact `git show 2030c2d:<path>` bytes. Copy
[the fixture](native-herdr-sdk.compatibility.fixture.ts) to
`apps/web/src/components/computer/native-herdr-sdk.compatibility.test.ts`, reuse
the existing dependencies, and run:

```sh
pnpm --filter @memohai/web typecheck
pnpm test --run --testTimeout=15000 apps/web/src/components/computer/native-herdr-sdk.compatibility.test.ts apps/web/src/pages/home/components/session-task-results.test.ts
pnpm exec eslint apps/web/src/components/computer/native-herdr-sdk.compatibility.test.ts
```

- Full generated Web application types passed with `tsconfig.app.json`:
  [raw output](full-typecheck.txt).
- Seven SDK checks and twelve phone attachment checks passed:
  [raw output](compatibility-tests.txt). Tests use actual SDK serialization,
  distinct Runtime/mount/Bot IDs, `/api`, exact string revision beyond the safe
  integer range, 403/409 without retry/fallback, and transfer CAS. HTTP is entirely
  synthetic; no real enrollment or configuration mutation occurred.
- Fixture ESLint passed, zero diagnostics. The four SDK files were then restored
  to locked fa77 bytes and the temporary test removed. No dependency, CI workflow,
  phone source, lock, signing input or HAP changed.

The current phone API surface remains compatible without a split SDK overlay.
After the unified revision is fixed, update the existing complete-source lock
once so its generated SDK arrives with the shared revision, then build the next
generic package. This review does not claim PR84 is already in a released HAP.
Real Windows execution, phone upgrade and NAS delivery acceptance remain with
the existing integration/onsite tasks; synthetic checks do not replace them.
