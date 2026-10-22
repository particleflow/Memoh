# Native delivery validation package

This is a review/test package, not a production installation. It contains a
finite Herdr plugin executable, manifest, and isolated Windows test executable.
It installs no service, daemon, Memoh runtime or Agent modification.

## Run the binary-only acceptance fixture

Obtain the Actions artifact from the reviewed commit. Compare its source SHA
and hashes using the trusted Actions artifact/PR; a checksum file bundled beside
binaries alone is not an authenticity signature. Extract it to a disposable local
NTFS directory. Run `./verify-fixtures.ps1` with normal user permissions. Do not
change execution policy or elevate to get a passing result; report an existing
policy or permissions failure instead. The PC needs no Go compiler or source.

The tests create synthetic files in private temporary directories and loopback
TLS servers. They do not use NAS addresses, real credentials, plugin state,
production ACLs or persistent identities. Expected tests cover actual directory
flush, ownership/DACL checks, identity/sharing, process locks and process death,
resumable admission/cancellation, TLS upload/lost response/outbox reconciliation,
and Range/hash download. This does not prove physical power-loss behavior or
validate a real PC/NAS route. Report test names, exit code, OS/filesystem and
bounded failure messages; never send originals, full production logs or secrets.

## Herdr boundary

The manifest uses Herdr v0.9.5's existing argv action mechanism. No startup worker
or timer is registered. `verify-storage` invokes `transferqueue.exe -action probe`:
it validates/initializes its queue only under `HERDR_PLUGIN_STATE_DIR`, requiring
an existing private local NTFS parent. Its fixed `native-profile-v1` quota profile
is immutable in the locator. It never repairs ACLs, changes roots or falls back.

The executable also exposes finite local `inspect`, `cancel`, and `retire` actions
with `-delivery <logical-id>`. The production upload/download enqueue, advance
and authorized result registration binding is still missing: the library's
`InvokeHerdr` accepts those injected ports, and isolated native fixtures exercise
them. Neither operation_id, this manifest, nor a successful storage probe grants
server authority. The probe explicitly reports `transfer_binding: required`.

Do not install/link this package or run the storage probe against production as
part of fixture acceptance. Actual Herdr installation, an approved scoped Memoh
credential provider, authenticated HTTPS route, and atomic task/storage
Commit/Resolve binding must be reviewed separately. No broad Bot token, Agenvo
OAuth substitution, insecure TLS option or credential argv/config is provided.

The documented API fault model, limitations and official references are in
`docs/design/windows-native-binding.md` in the source repository. CI fixture
success is not real UI or human QA. The PR remains draft pending required checks.
