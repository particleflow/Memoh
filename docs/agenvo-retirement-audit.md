# Agenvo project retirement audit

This is a source handoff based on PC candidate `50d69c7b77be0dda884e2708db6167a1d561bba0`, on independent branch `fix/retire-agenvo-project-wide`. It does not change the frozen candidate or production `391adba82a2347123f02f29fdbd289cde84d26cb` and is not deployment or device acceptance evidence.

The project delivery checklist was successfully read on 2026-10-26; its private reference is omitted from this public source handoff. The default oversized read returned `page_read_selection_required`; a subsequent scoped read succeeded. A01 requires removal of current entry points and scheduling paths while retaining historical records. It still lists per-schedule live verification as incomplete. No Page content was edited.

## Scope and actual call paths

| Class | Entry / files | Actual dependency and source disposition |
| --- | --- | --- |
| A: removed | `apps/web/src/pages/runtimes/index.vue` → `components/computer/connect-agenvo-dialog.vue` | The old action loaded Bot MCP connections, discovered Herdr devices and posted `provider=agenvo`. Remove the button, dialog import/state/component and registration callback. The native Connect Computer action remains. |
| A: removed | `internal/handlers/user_runtime.go`, deleted `user_runtime_endpoint.go` | Remove `/users/me/runtimes/agenvo/discover`, `/{id}/agenvo-agent` and GET/PUT `/{id}/agenvo-endpoint`. Remove their userruntime wrappers. Keep native Herdr and admission routes. The HTTP regression asserts authenticated 404 with no configuration/native effect. |
| A: rejected | `internal/userruntime/{service,types}.go` | Reject legacy provider or legacy `agenvo` payload before issuing credentials or touching a connection. Retain the old request field only to reject old clients; exclude it from the public schema. Provider schema advertises only `memoh`. |
| A: removed from selection and grant creation | `db/postgres/queries/user_runtimes.sql` and generated sqlc | Account Runtime list, Bot mount list and account grant list now filter unconditionally to `provider=memoh`, regardless of old MCP activation or malformed connection IDs. New mount creation also requires `memoh`. These are query predicates, not row deletion, migration, default reassignment or revocation. |
| A: transport disconnected | `cmd/internal/core/agenvo.go` → `agenvo.Service` → `nativeherdr.Executor` | Production constructs the shared adapter with a nil legacy SourceCaller. The old `OwnedHTTPMCPConnectionBot → OAuth/federation gateway → search/execute` closure and its dependencies are removed. Generic MCP OAuth setup remains owned by the existing general providers. Native Exec injection remains unchanged. |
| A: old selection fails closed | `internal/agenvo/service.go`, `internal/workspace/remote.go` | A service without a legacy caller rejects a stored Agenvo registration before transport. Mount listing suppresses legacy records defensively; direct mount lookup rejects them; resolving an old primary returns a retirement error rather than silently selecting another machine. The legacy workspace probe/resolve branch is removed. Task discovery/submit/follow-up/recovery use this shared resolver, so an old task cannot dispatch via its saved target. |
| A: old transfer rejected | `runtime_transfer_credentials.sql`, `internal/userruntime/transfer.go` | Only native Memoh Herdr registrations can enroll/verify transfer credentials. Legacy credentials remain stored and are not rotated or deleted. Target decoding also rejects a legacy provider if a storage implementation returns one. |
| A: misleading active copy removed | Web locales, composer, `workspace-target.ts`, `internal/computertask/source.go`, `internal/agent/tool/container.go`, `contrib/bot-skills`, current documentation | Remove connection/install fields and old capability badges. Native tool descriptions refer to Memoh Runtime/Herdr. Old online snapshots cannot authorize a clickable action. Preserve receipt warnings under `computerExec`, including unknown outcomes and retained workspaces. Change shared errors from Agenvo-specific labels to computer-target labels. |
| B: retained native dependency | `internal/agenvo` task types, fingerprints, admission, batch/receipt/result code; `internal/nativeherdr`; workspace/task/session/delivery adapters; generated `Agenvo*` shared types | Native provider is `memoh`; core installs `nativeherdr.Executor.Invoke`. Native tasks use the same grant checks, operation ledger, path validation, Herdr/Pi command mapping and result verification. The old package/type name does not imply an Agenvo connection. Do not delete the directory or rename durable JSON/fingerprints. |
| C: retained history/compatibility | Historical migrations, `provider=agenvo` database rows and private compatibility queries, wire decoders, old protocol fixtures, `docs/validation`, `docs/evidence`, archived design | Retain old task/queue/history/credential data, including unknown operations. Low-level legacy transport helpers still remain and require a separate native-fixture conversion/removal; they are executable residue, not a historical-data requirement; no production constructor installs their caller and no enrollment/configuration route exposes them. Existing reserved-MCP connection checks remain so old stored endpoints/credentials do not reopen raw federation. |

Current connection configuration is `/users/me/runtimes/{id}/native-herdr` on an already activated owned Memoh Runtime. Its Pi profile remains an `agent` subobject. No installation, OAuth flow, credential rotation, new runtime service, protocol or approval mechanism is introduced.

## Repository coverage

The inventory scans tracked files case-insensitively for `agenvo`, including code, API/schema, tests, docs, migration and build/configuration files; it also checks the `packages/ui` submodule. The generated before/after path inventories accompany this audit. No Agenvo package/build dependency was found in Go modules, pnpm manifests/lockfile, Docker or config templates.

Desktop reuses the Web management routes and dialogs. This checkout contains no Harmony/ArkTS `.ets` source tree or separate Harmony package; therefore this audit covers the shared Web management surface, not an independently packaged Harmony client. No GitHub directory-read fallback, alternate content URL, production browser, NAS or device access was used.

## Preservation and verification limits

No production database, queue, credential file, scheduled task, deployment ref, official Agent runtime or contextview source is changed. The small `internal/agent/tool/container.go` edit changes host-owned tool copy only. The parallel AgentBot/attachment UI work is not included: no chat-pane, task row or attachment restoration file is edited. Locale changes are confined to replacing the Agenvo block with receipt copy; combine those hunks with the other UI branch if it also edits locales.

The source tests and checks are recorded in `docs/evidence/agenvo-project-retirement/checks.md`. PostgreSQL tests use a new local synthetic container and disposable test databases. They do not prove live PC, NAS, schedule or Harmony behavior. Real UI/device acceptance and final deployment remain with the designated unified release/onsite writer; this branch does not claim all Agenvo retirement acceptance has passed.

## Follow-up: remove unreachable owner configuration writers

The follow-up based on `e2cb0767eb12a54abd670d754c72ab7b4b7798c8` removes
`Service.ConfigureAgent`, `Service.ReadEndpointConfiguration`,
`Service.ConfigureEndpoint`, their private store interface, locator/configuration
DTOs, CAS error mapping and the positive tests dedicated to those removed
interfaces. Repository call-site inspection found no production caller after
the HTTP routes were removed. These implementations did not decode a receipt or
serve native execution, so retaining them as historical fixtures was redundant.
The shared `replaceRegistrationField` helper moves to the native profile file
because `ConfigureNativeHerdr` still uses it to preserve unrelated JSON fields.
Historical `DecodeTarget`, target fingerprints and original receipt readers are
unchanged. Admission regression tests retain only the historical-row read fake
they actually need, without the retired mutation fake.

The duplicate Agenvo setup design is removed from the delivery tree; Git retains
its original revision. Its single live source-document link now points to the
native contract. Current cutover and file-credential documentation now describe
unconditional native-only selection/rejection, direct producer collection and
original-history preservation. They no longer advertise live Agenvo inventory,
active old targets or a retained old plugin-dispatch path.

This is **not zero-residue completion**. Remaining executable residue is precise:
`internal/agenvo/service.go` still contains `Discover`, `Register`, `Probe` and a
`SourceCaller`; `internal/agenvo/client.go` still has the MCP `callTool`/relay
branch; private store/SQL legacy mutation helpers remain. Production wiring
supplies no legacy caller, but obsolete executable implementations must be
removed after migrating useful native regression fixtures. Historical migrations,
private row decoders and stored receipt identity are a different category and
must remain. Do not rename the shared package or rewrite durable fields to make
a string scan empty.

The Windows endpoint PR5 at `1537fe3` separately still ships schema 1/2 legacy
launch/qualification, old npm dependency pin, packaged example and active CI
fixtures. Its independent follow-up inventories exact files and supplies an
offline reference-only retirement planner. It does not modify installed PC/NAS
files or the general cleanup owner's UI verification files.

Verification of this follow-up is recorded separately from the old all-green
checkpoint. No current dev UI, screenshot, Windows runtime or onsite acceptance
is asserted by this text. The original task result bodies remain unavailable
in the isolated environment's tools; the repository evidence is not a substitute
for reading those thread results.
