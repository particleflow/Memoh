# Harmony consumer of the shared NAS workspace directory settings

Base: image-timing source `b0849bc832cbe82f6c5a5ce4d155857fb9d7ae35`.
Fixed shared source: `56b38d39fadda858963b36e597dcabd3a0fb0a1d`,
tree `596be2d3a5b2471c473af90fdfd6b1c1a91f08bc`.
This is source-only preparation for a single subsequent unified package.
The distributed 1.0.16 and its configuration are unchanged; no HAP, signature,
installation, release, deployment or model task is produced here.

## Exact reuse

The current phone context's directory picker, settings routes and settings
sidebar were byte-identical to the fixed commit's parent before this change.
The existing source producer now copies those three fixed shared files and the
new page/test verbatim, guarded by their actual baseline and resulting SHA256.
It adds only the shared workspaceStorage/error locale group and the three
verbatim generated SDK methods with sixteen corresponding generated types.
Other SDK declarations remain on the existing phone base. No new page copy,
phone protocol, permission model or NAS service is invented.

Normal source preparation verifies these hashes and retains the existing 43
phone adapters plus two package/dependency patches. The image timing patch,
login/HTTP-to-HTTPS binding migration, empty public deployment slot, original
bundle/permissions, four upload entry paths and scoped original-byte validation
are preserved. Native code and the SDK toolchain are not changed in this slice.

Reviewed source copies are named `shared-workspace-*` in the existing mobile
source input area, with the exact destination/source/hash mapping in
`config/memoh_web.lock.json:sharedWorkspaceStorage`. These are build source pins,
not additional application or endpoint configuration. Generated SDK code comes
from the fixed upstream declarations, not hand-written API guesses.

## Account contract

All three routes use the current account API base and existing JWT:
`/users/me/runtimes/{id}`. The backend revalidates activated, non-revoked native
Runtime ownership/team; a chat-route token is not an account credential.
No synthetic Bot ID is used. Responses/errors are no-store.

| Action | Method/path suffix | Fields and effect |
| --- | --- | --- |
| Browse/read selection | GET `/workspace-storage?path=...` | Optional relative `path`; returns `path`, `selected_path`, `windows_path`, `directories[{name,path}]`; does not write |
| Create child | POST `/workspace-storage/directories` | JSON `{path}`; 204; creates a child, does not save selection |
| Save selection | PUT `/workspace-storage` | JSON `{path}`; 204; existing CAS saves only provider_config.workspace_storage.path |

Root is `.` within `<existing storage root>/<stable Runtime UUID>`.
`windows_path` is the **saved** selection's complete UNC path, independent of
which folder is being browsed. It is not a permission grant, a task or an
execution-root change. The existing private LAN share/mount configuration and
connection Stat are prerequisites; the client does not discover addresses,
authenticate SMB, enable Internet share access, loosen TLS or change routing.
An offline computer is disabled. An unavailable share stops NAS writes.

Relevant existing Problem codes are:

- `request.field_invalid` (400): invalid Runtime/path input.
- `http.unauthorized` (401): ordinary current account authentication failure.
- `http.forbidden` (403): chat token or ownership/scope denial.
- `user_runtime.not_found` / `http.not_found` (404): unavailable Runtime/route;
  not an empty successful directory listing.
- `workspace.storage_unavailable` (503): disabled/unreadable share or unavailable
  owned connection; localized through the existing error pipeline.
- `http.conflict` (409): existing configuration CAS conflict.
- Existing admission checks retain `computer.admission_paused`,
  `computer.admission_conflict`, `computer.admission_scope_unverified` (409).

No client revision/schema is added. Errors and unknown outcomes are not
silently repeated. The shared page does not display private diagnostics. The
mobile page is the same Settings → Workspace storage → Computer route with the
same DirectoryPicker/FormDialogShell. Cancel does not Save or remove a directory
created by a separate explicit Create. Existing phone Back dismisses that form
before returning from the settings detail to the settings list.

## Validation and limits

27 tests pass: five unchanged shared directory-page cases, ten image-timing
cases and twelve existing scoped attachment cases. There is no separate
`directory-picker.test.ts` in the pinned source; the originally named optional
filter matched no file and is not counted. The real picker is instead exercised
in the compiled browser flow. Generated Web types/build pass; ESLint on the five
shared changed UI/route files passes with no output. Source manifest checks
153 reviewed sources / 162 source-and-support files. The inherited nineteen
historical source changes remain unverified; this does not reconstruct them.

Actual compiled phone Web at local loopback, 390×844, with a synthetic exact
account contract verifies:

- Discoverable settings entry, correct native-PC identity, legacy provider
  isolation and offline computer disabled without a directory read.
- Browse only; explicit Save once; refresh reads the saved selection.
- Create one child under the selected directory; Cancel retains that child and
  the previously saved default rather than changing/removing either silently.
- Existing phone Back cancels an open form without Save, then returns the
  detail page to the settings list without exiting or losing login.
- 503 disables creation/Save; 403/404 disable Save; private fixture diagnostic
  text stays hidden; 409 does not change selection or automatically repeat PUT.
- No horizontal overflow, page errors, model calls or unrelated business writes;
  the same login is retained across reload/navigation.

[Entry](storage-entry-390.png), [directory form](storage-dialog-390.png),
[unavailable share](storage-error-390.png), [browser flow](browser-storage-flow.json)
and [verification](verification.json) are isolated browser evidence, not ArkWeb,
real NAS/PostgreSQL/RPC/SMB, LAN throughput or physical-phone acceptance.
The upstream owner retains backend/production checks; no backend file is changed.

Initial local fixture failures are preserved separately: Request.method was
mistakenly called as a function, then the fixture initially listed a parent as
its own child. Both fixture mistakes were corrected, the complete flow repeated
and fresh screenshots inspected. An existing source-private-config ignore glob
also matches `locales.json`; the reviewed build input is named `i18n.json`.
The normal context was regenerated after that filename correction and all 1382
Web/SDK/UI source files matched the types/tests/build context byte-for-byte.
No metadata or compiler assertion was bypassed.

**Still incomplete:** saving this directory does not automatically update Herdr
work_dir, native Runtime sourceRoot, old sessions/tasks/queues/receipts or any
operation fingerprint. Arbitrary NAS directories outside the computer's mapped
namespace are not supported. Execution/default-root composition, the real share
mapping and owner permission, physical LAN/IPv6 behavior and real phone remain
separate unverified work. This patch must not be described as fully satisfying
those capabilities. Combine the fixed shared/native/files changes before one
new package; do not rebuild the old 1.0.16 or claim this source is deployed.
