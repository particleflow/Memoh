# Historical scoped attachment preparation audit

This report records partial-recovery checkpoint `84ca0e5d764b9944fc0b6c9b8264562288477744`.
Its raw `6ddf61dc` Web base lacked the scoped routes from `dfc7cf5`; a temporary
standalone preparer applied five production modules and three upstream tests.
That preparer, its exclusive tests and obsolete lock object have now been removed.
Use [the active complete-archive producer](../mobile/BUILD-REPRODUCTION.md) and
[current observation entry](../mobile/SHARED-OBSERVATION.md) for this tree. The
record below preserves the exact guard/failure audit, not current build commands.

This is a new follow-up to recovery checkpoint
`6a2a3b1a8a799114a8e41a66fedc6207e89e4ba1`, not a reconstruction of missing private
revisions. The 72-path inventory was unresolved at that checkpoint; subsequent source restoration is recorded in the current source manifest. No native
shell, builder, transport service, server or Agent code is recreated here.
This report records checkpoint `84ca0e5`. The later explicit-LAN and native
download admission changes are documented in `harmony-endpoint-native-scope.md`.

## Required existing modules

| Module | Existing upstream behavior retained |
| --- | --- |
| `useMediaGallery.ts` | Validate the scoped path and matching hash; reject malformed metadata without a Bot/hash fallback. |
| `attachment-block.vue` | Carry the delegation path into preview identity and arguments. |
| `useFileManagerProvider.ts` | Type the credential-free `delegationUrl` argument. |
| `workspace-tabs.ts` | Preserve that argument in the asset tab's parameters. |
| `dockview/panel-asset.vue` | Re-resolve the scoped path with current credentials after tab restoration. |

The last three are required dependencies of the first two. Copying only the two
entry points would leave the preview contract incomplete. All five outputs are
byte-identical to `dfc7cf5104c382ecc622061ce43c3dcd733b7998`; they are not parallel
implementations. The four shared task modules retain their existing `be686` pin.

The preparation step verifies every baseline input and every upstream digest
before writing. Modified inputs, pre-existing new test paths, symbolic-link
inputs and repeated application are rejected. A rejected preparation leaves the
checkout's files unchanged; missing or unexpected source is never overwritten by
a guessed replacement.

## Reproducing the historical checkpoint

The original preparer and exclusive tests are available in the
[exact 84ca0e5 source tree](https://github.com/particleflow/Memoh/tree/84ca0e5d764b9944fc0b6c9b8264562288477744).
They are not preparation steps or executable test paths in the current tree.
That checkpoint required an unmodified `6ddf61dcd8199758e4110398645c556a3d6d7375`
Web base, UI `f69402a55dd87a59c10722fe11507ad8fd7048f5`, and exact `dfc7cf5`
objects. Its attachment-only path produced an integrity receipt without a build.

Its complete legacy SDK preparation correctly rejected a baseline digest
mismatch. No missing builder preprocessing was guessed or guard bypassed.
Current production uses the complete guarded archive/overlay context instead;
replaying that old SDK extraction against current inputs would be incorrect.

## Validation performed

- Real upstream Web base and pinned UI source, reusing the environment's existing
  dependencies without installation. Node 24.19.0, TypeScript 6.0.3, Vitest 4.1.10.
- Applicable existing baseline tests: 3 files / 108 passing tests.
- Applied result: 5 files / 121 passing tests, including delegated metadata
  validation, ordinary media behavior, preview scope and renewed credentials,
  and workspace layout regression tests. These are Vue/Vitest tests with their
  existing controlled API/DOM seams, not live server/device authorization tests.
- Source-preparation tests: 5 passed, covering exact output bytes, reapplication,
  local input drift, dependency digest failure, symlinks and existing test files.
- ESLint on the five production and three imported test files: exit 0.
- UI contract guard: exit 0, with three warnings in unchanged upstream files.
- Original public source checker: passed after updating only the two modified
  entries' hashes and preserving their previous hashes as provenance.
- Full `vue-tsc --noEmit -p tsconfig.app.json`: **failed before and after** with
  the same 145 diagnostics (comparing path, code and message, ignoring line
  shifts). No new diagnostics; no claim of a passing full Web typecheck.

See `testing/harmony-attachment-scope/results.json`. No complete client build,
native runtime, phone installation, UI screenshot or human QA was performed.

## Native boundaries at the audited checkpoint

The public client has one `apiOrigin`; `hapDownloadUrl` is an installer location,
not another attachment transport endpoint. `NativeResumableUpload.ets` sends
files to the configured service's `/bots/{bot}/media` and upload subroutes.
`harmony-resumable.ts` exchanges handles, metadata and authorization with the
native bridge while file bytes stay in native-owned files/HTTP bodies. The
recovered modules do not start a phone-local transport listener. The unavailable
composer prevented complete verification at that partial-recovery checkpoint;
this is not a statement that the current source lacks its composer.

The configured service must be the unified service endpoint; no additional
device-local or Linux-download service is introduced by this change. However,
the then-current `harmony-service-url.ts` rejected private IPv4, local
names and non-global IPv6. That checkpoint implemented neither LAN priority nor IPv6 fallback.
Changing routes also interacts with endpoint-bound authentication; that work
must retain a single authenticated service identity and be verified with the
native configuration code once recovered.

At checkpoint `84ca0e5`, `harmony-download.ts` and `NativeMediaDownload.ets` accept
only an owner Bot/hash download. They have not been made delegation-aware by this
Web dependency update. Do not route delegated attachments through that broader
native interface. Native scoped downloads, LAN/IPv6 selection and a real
phone-to-service-to-phone round trip remain unverified and unimplemented where
not demonstrated by the available source.

## Historical materialization failure

The original source materialization helper failed at hosted-app `tools/list`
with HTTP 401 before transfer. Metadata search still worked, so the failure did
not establish that the user's Library was disconnected. No token or alternate
credential route was requested. That failure remains a checkpoint audit fact;
it is not a current source-restoration blocker or an instruction to retry an
old helper. Later restoration and build results are recorded by the current
mobile entry and versioned package reports.
