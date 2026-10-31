# Native Runtime Bot delegation regression

Status: draft implementation; live specialist/Runtime acceptance is incomplete.
No production or user device was accessed. No live model was called, and no Bot,
Runtime, grant, or MCP connection was created.

## Fixed source evidence

Reviewed `dd7409e7157b440a5a62337b61bc4d6fac9cae76` and the exact release
candidate `69ef2fac2ceeaf68458f0f68bc30d0e8a31872ee`. There is no diff between
these commits in `cmd/internal/core/bot_delegation.go`,
`internal/botdelegation/authority.go`, or `internal/agenvo/native_profile.go`.

At candidate lines 185 and 204, `delegationHost.Plan` filters both mounts to
`agenvo.Provider` and decodes only `DecodeTarget`. At authority lines 287–295,
`registration` rejects every other provider and requires an owned HTTP MCP
connection. Thus the native `memoh` Runtime registration cannot enter the shared
device plan or authorize a selected native remote mount. This is separate from
the NAS workspace selection named `native`.

## Change and compatibility

Registration uses the existing `DecodeRuntimeTarget(provider, runtimeID, config)`.
Native registrations additionally reuse `OwnedNativeHerdrRuntime`, which checks
owner, team, activation and revocation in the database. Returned identity and
activation are checked, and the mount and current registry target fingerprints
must match. Both Bot mount identities, owner equality, shared Runtime ID and
pinned configuration remain mandatory. Each tool retains the current user,
source ancestry, both Bot grants and separate read/write/exec permission checks.
Text and original-media reads do not acquire or require execution permission.

New device plans include only native `memoh` runtimes. Source PC selection maps
to the target Bot's own mount of the same Runtime; source NAS selection includes
only the resource intersection. Invalid selected PC mappings fail closed.
Historical Agenvo registration decoding/authorization remains readable under
existing access checks. Production legacy execution remains blocked by the
unchanged native-only computer service; there is no OAuth or execution fallback.
Ledger, queue, receipt, result routing and history schemas are unchanged. The
ordinary same-Bot subagent implementation and official Agent core are unchanged.

## Differential and automated checks

The native positive regression was first run against the unmodified candidate
and failed at binding with `delegation is unavailable in this conversation`.
After implementation, restoring the candidate's exact registration function also
failed the same regression: [red evidence](baseline-registration-red.txt).
Restoring the old planner provider gate in the extracted planner separately
failed both NAS and selected-PC cases: [red evidence](baseline-plan-red.txt).
Both temporary changes were restored before the final checks.

Passed:

```sh
go test -p 3 ./cmd/internal/core ./internal/botdelegation ./internal/agenvo -count=1
go test -race -p 3 ./cmd/internal/core ./internal/botdelegation -count=1
go vet -p 3 ./cmd/internal/core ./internal/botdelegation
golangci-lint run --new-from-merge-base=69ef2fac2ceeaf68458f0f68bc30d0e8a31872ee ./cmd/internal/core ./internal/botdelegation
go build -buildvcs=false -p 3 -o /tmp/garnet-native-bot-delegation-server ./cmd/agent
```

The focused lint uses the CI-pinned v2.10.1 binary, verified against its published
archive checksum. Its first run identified five unused test fixture fields,
parameters or receivers; these were corrected before the final lint and race run.

The initial build failed to obtain VCS status in this worktree; disabling VCS
stamping allowed the same source to build. No implementation changed for that
recovery. The last changes after the build add tests/documentation only.

New tests cover native planning and scope reconstruction; missing or inactive
registration; runtime/mount revocation; wrong owner, Bot, mount, Runtime ID,
provider or target; changed configuration snapshots and replaced profiles; both
Bot permissions; source-user and target-session forgery; historical reads with
new legacy plans denied; separate read/exec grants; and return to the original
source session. Native accepted/running and unknown replays each execute only
one synthetic specialist turn. Native fixtures fail any legacy MCP lookup.
Existing tests additionally cover original attachments, queue/result routes,
unknown receipts, delivery-only retry and native-only legacy transport rejection.
These are synthetic host/transport fixtures, not live Herdr/Pi execution.

## Actual local application check and missing acceptance

Used the built server from this worktree at `http://127.0.0.1:18080` and current
Vite source at `http://127.0.0.1:18082`, with the existing local PostgreSQL and
pgvector containers. A separate `garnet_native_delegation_test` database migrated
cleanly to version 177 (`dirty=false`); the pre-existing application database
was not migrated. The server's HEAD `/health` returned 200. Chromium submitted
the real login form, reached authenticated onboarding and reported no page
errors. The inspected [screenshot](local-app-login.png) demonstrates only this
startup/login smoke check, not the affected delegation flow.

The isolated database has zero Bots, runtimes and delegations. Creating a real
specialist or accessing a real Runtime/model is outside this task's scope, so
the real UI delegation and Runtime → Herdr → Pi execution/result flow were not
exercised. This does not satisfy the affected-flow acceptance requirement in
`AGENTS.md`. Existing authorized source/specialist Bots and native Runtime are
needed for later permitted environment acceptance, including original session,
attachment and mobile return checks. Human QA and production acceptance remain
unconfirmed; source tests and CI are not delivery evidence.
