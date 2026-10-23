# Scoped attachment source preparation

The recovered client's pinned Web base (`6ddf61dc`) lacks the delegation-scoped
attachment routes already present in candidate `dfc7cf5`. The existing
`mobile/client/mobile/prepare_shared_observation.py` now consumes five exact
upstream production files and three upstream test files. Their source and base
SHA256 values are pinned under `sharedObservation.delegatedAttachments` in
`mobile/client/config/memoh_web.lock.json`.

This is a new follow-up to recovery checkpoint
`6a2a3b1a8a799114a8e41a66fedc6207e89e4ba1`, not a reconstruction of missing private
revisions. The 72-path missing-input inventory remains unresolved. No native
shell, builder, transport service, server or Agent code is recreated here.

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

## Narrow reproduction

Use a clean checkout of `6ddf61dcd8199758e4110398645c556a3d6d7375`, its pinned UI
`f69402a55dd87a59c10722fe11507ad8fd7048f5`, and compatible dependencies. The
repository supplying Git objects must contain those commits and `dfc7cf5`.

```sh
python3 mobile/client/mobile/prepare_shared_observation.py \
  --attachments-only --checkout /path/to/clean-web-base \
  --repository /path/to/Memoh
python3 tests/harmony/test_attachment_inputs.py -v
```

`--attachments-only` prepares source and emits an integrity receipt. It does not
compile Web/HAP, install dependencies, inject a replacement builder, or apply the
shared task/SDK adapter. The ordinary complete preparation path remains guarded.
On the raw base it rejects `packages/sdk/src/types.gen.ts`: the old lock expects
`3b62ca7e3f1dc7976583dbee92b693506e38a5c0cc4b0723bfc7b9ae7900a784`, while the
actual base is `df24fb67371413c7464f60d15337ac116e3d0eed1023a9ebeaeae433efa00eda`.
The unavailable builder's preprocessing was not guessed or bypassed. Its order
relative to native overlays must be verified when recovered; an overlay that
already changes one of the pinned input files will be rejected deliberately.

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

## Unified endpoint and remaining native boundaries

The public client has one `apiOrigin`; `hapDownloadUrl` is an installer location,
not another attachment transport endpoint. `NativeResumableUpload.ets` sends
files to the configured service's `/bots/{bot}/media` and upload subroutes.
`harmony-resumable.ts` exchanges handles, metadata and authorization with the
native bridge while file bytes stay in native-owned files/HTTP bodies. The
recovered modules do not start a phone-local transport listener. The unavailable
composer integration still prevents complete verification of the final message
attachment-reference contract.

The configured service must be the unified service endpoint; no additional
device-local or Linux-download service is introduced by this change. However,
the existing `harmony-service-url.ts` explicitly rejects private IPv4, local
names and non-global IPv6. It implements neither LAN priority nor IPv6 fallback.
Changing routes also interacts with endpoint-bound authentication; that work
must retain a single authenticated service identity and be verified with the
native configuration code once recovered.

The public `harmony-download.ts` and `NativeMediaDownload.ets` currently accept
only an owner Bot/hash download. They have not been made delegation-aware by this
Web dependency update. Do not route delegated attachments through that broader
native interface. Native scoped downloads, LAN/IPv6 selection and a real
phone-to-service-to-phone round trip remain unverified and unimplemented where
not demonstrated by the available source.

## Library blocker

The current official Library skill and its materialization reference provide no
authentication refresh, reconnect or runtime-auth diagnostic operation. The
failed action was the consuming environment's official helper accessing hosted
apps `tools/list`, which returned HTTP 401 before any transfer. Metadata search
works, so this is not evidence that the user's Library connection is disconnected.
The runtime authentication for that helper-to-hosted-apps request must be
restored through a supported environment mechanism. No token was requested or
printed, and no alternate route or blind download retry was attempted.
