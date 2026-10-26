# Computer workspace storage: NAS configuration slice

Base: `4d23244c683aca4c436efec4db375921fdf60472`. Independent branch:
`feat/computer-workspace-storage`. The frozen candidate is not moved by this
work. No image, NAS configuration, credentials or installed client is changed.

## Existing mechanisms and scope

The Windows owner supplies `directory-pick` and fresh setup's
`--browse-source-root native` at windows-endpoint
`eb3e3c39abfc1e7e0b7024e62db30bee9871c3cf`. Local defaults remain Windows-owned.
This change adds the NAS settings slice, using the existing account Runtime
registry, reverse RPC Stat, PostgreSQL configuration CAS and shared directory
picker. It does not synthesize a Bot to use the Bot-scoped workdir API.

Configure the same **existing** NAS directory in the original private config:

```toml
[container.storage]
root = ""       # Server's existing mounted workspace-storage directory
share_root = "" # The same directory's existing LAN UNC path for Windows
```

Empty/unreadable/malformed configuration leaves NAS actions unavailable.
The code does not mount anything, discover a server address, authenticate SMB,
add a service or credential, copy/synchronize content, change network security,
or infer that an HTTP URL is a filesystem directory. The operator must confirm
the existing mount/share mapping and permissions; those private values are not
present in this source or evidence.

The default namespace is `<root>/<stable Runtime UUID>`; selection is relative
within that computer's namespace. Display names do not determine ownership or
rename physical data. Browsing does not create the default directory. An
explicit Create or Save can create it after checking the existing share from
the current owned Windows connection. Another computer's directory cannot be
selected through `..`, an absolute path or a symlink at the computer boundary.
This slice does not offer arbitrary NAS-root/Bot-directory browsing outside
that namespace; such access requires an established ownership/root mapping.

## Account API and shared consumer

All paths below are under `/users/me/runtimes/{id}` and require a revalidated
account JWT and the activated, non-revoked native Runtime's actual owner/team.
Chat-route tokens cannot use them. Responses, including errors, are no-store.

| Method/path | Input | Effect |
| --- | --- | --- |
| GET `/workspace-storage` | optional relative query `path` | Read directories and the saved selection; no writes |
| POST `/workspace-storage/directories` | `{"path":"project"}` | Create a NAS child; does not select it |
| PUT `/workspace-storage` | `{"path":"project"}` | Save only `provider_config.workspace_storage.path` by existing CAS |

The read returns `path`, `selected_path`, `windows_path`, and
`directories[{name,path}]`. Root is `.`. `windows_path` is the **saved selection's**
complete UNC path, independent of the browsed `path`; it is not a grant, mount,
executable configuration or task.
Existing Windows path validation is reused without modifying the validator.
Configuration conflicts and existing admission-pause triggers remain enforced.
CAS protects server read/write races; no client expected-revision is invented.

The shared route is Settings → Workspace storage → Computer. It lists only the
owned native computer names and directory-selection action, using existing
PageShell/SettingsRow/FormDialogShell/DirectoryPicker components. The picker
accepts a directory-reader callback so this account UI does not fake a Bot ID.
Create, selection and Save are separate existing filesystem/settings actions.
Cancel does not save a selection, remove a created directory or delete originals.
A failed/uncertain request is not automatically repeated. A known unavailable
share disables NAS writes; local Windows selection remains independent.

`workspace.storage_unavailable` uses the original Problem/catalog/localization
pipeline. Private filesystem and RPC diagnostics are not returned. No model or
device execution is started by opening or saving this settings page.

## Important consumption boundary

Saving NAS storage deliberately does **not** change Herdr `work_dir`, Runtime
`sourceRoot`, an old session, receipt, queue identity or operation fingerprint.
The existing validator accepts UNC roots but requires each requested directory
to stay inside the configured target root (`internal/agenvo/workdir.go`). A NAS
selection is therefore not proof that an existing local-root Pi task now runs
there. Fresh setup/profile configuration must consume the returned UNC reference
through its existing supported configuration path. Old active/unknown tasks
cannot be redirected to the new root. This cross-owner execution/default-root
composition remains to be completed and verified; there is no silent migration.

The availability check proves that the selected owned connection can Stat the
**configured existing LAN share**. It does not prove a physical network route,
the operator's mapping, real Windows credentials, or offline NAS I/O. W03/W04
are not removed: accepted local work may continue after control disconnection;
a task requiring an unavailable NAS share cannot be promised continued I/O.

## Verification and evidence

See `validation.json` and `evidence/`. Commands and actual exit codes are kept
separately from acceptance claims. Go tests cover owner denial before RPC/write,
read-only GET, selection persistence on a new service, preserved Herdr/admission
values, path validation, symlink exclusion, unavailable share and CAS conflict.
Real fresh PostgreSQL plus actual HTTP/JWT handlers verifies create/save/readback.
Its case is appended to the original PG step's regex and required-PASS set;
all existing PG gates remain unchanged.
The optional browser exercise uses the actual shared dev app and forwards only
this new account namespace to that isolated fixture. Its share endpoint is a
controlled existing-RPC fixture, not Windows/NTFS/SMB or field LAN evidence.

The actual browser flow browses, creates one child, saves once, reloads and reads
the same persisted selection, and cancels without another save. It uses the
picker's existing disclosure chevron to reveal nested folders. A disconnected
share fixture produces a real handler HTTP503; the page disables NAS writes and
the backend creates no directory for that failed request. Desktop and narrow
Chinese dark screenshots are actual application screenshots, inspected locally.

Initial failures are retained: type guards for optional SDK IDs, comment/test
lint diagnostics, unused core import cleanup, error-catalog registration, and
local disk exhaustion during test linking/type-cache writes. A browser script
initially omitted opening a parent folder; another hand-written error fixture
lacked the standard Problem status. These were test/setup failures, not proof
of a production defect. The final error exercise uses the actual HTTP handler.
Test binaries use `-ldflags='-s -w'` locally to reduce link-space requirements;
test bodies/assertions and CI rules are unchanged. Only known task-owned
reproducible build archives were reclaimed. No source, logs, DB or originals
were removed. Existing Web chunk warnings remain.

No migration is added; schema stays177. Rollback is the previous application
with the same DB and existing physical files, not migrate-down or volume deletion.
The old binary ignores the added configuration namespace; it is not an execution
root cutover. Full combined remote CI, real existing share/mapping, Windows
interactive picker, endpoint configuration and physical-client acceptance remain
separate requirements. Human QA remains unchecked.
