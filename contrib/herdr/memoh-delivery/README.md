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

The executable exposes finite `enqueue-upload`, `enqueue-download`, `advance`,
`advance-next`, `reconcile`, `inspect`, `cancel`, and `retire` actions. Explicit
job actions select `-delivery <logical-id>`. The manifest's `advance-delivery`
runs `advance-results`: at most 64 existing queue steps on one due job within
two minutes, then exits. It does not poll, schedule,
reset retry budgets, create a business attempt, or run a background service.
Herdr/Agenvo can invoke the installed command through its existing command path;
no connector or Agent core change is needed. Input acceptance alone is not a
completion receipt.

## One-time connection checklist (not executed by this package)

1. Approve the Memoh HTTPS origin and its valid hostname/certificate trust. The
   helper requires HTTPS and normal certificate verification. Existing HTTP is
   not encryption and cannot be configured as a shortcut.
2. Upgrade the fully integrated Server through migrations 171/172 and approve
   enabling `files.device_transfers_enabled`. The existing runtime registry
   includes the stable credential backend and Server verifier. The runtime
   owner enrolls the already paired connection/device/instance once through
   `/users/me/runtimes/:id/transfer-credential`; only its digest is stored on
   Server. Choose expiry/revoke/rotation policy and privately provision the
   one-time returned purpose-specific device credential. Do not use a PC account JWT, broad Bot
   token or the Agenvo control/OAuth credential. No per-file token or pairing is
   required. Missing verifier means unavailable, never account-auth fallback.
3. After installation approval, install this bundle through the existing Herdr
   plugin mechanism, without a service. Herdr supplies `HERDR_PLUGIN_CONFIG_DIR`
   and `HERDR_PLUGIN_STATE_DIR`. Both must already be private local NTFS folders
   belonging to the invoking user. The helper validates and refuses unsuitable
   locations; it does not repair ACLs, elevate or configure trust.
4. Privately create `transfer.json` in that config directory from the packaged
   `transfer.example.json`, and a separate private `device.credential` containing
   only the approved opaque credential (optional final newline). This package
   does not create either real file. Secret files never go into the repository,
   bundle, argv, model context or terminal output. Rotation replaces that same
   private credential through the approved administrative process. Requests read
   it afresh; server revocation is checked on every request and after scans.
5. For uploads only, configure `source_root` to the producer's approved private
   NTFS export directory. It must not overlap config or queue state. Producers
   close output writers and supply one logical filename (`source_ref`, letters,
   digits, underscore or hyphen), declared hash/size/MIME and existing task
   selectors. Downloads publish under the existing queue's retained native
   publication store; no arbitrary overwrite destination is exposed.

`transfer.json` is resolved once per invocation. Its digest is recorded in the
existing queue journal. Changing the endpoint or source root (even formatting)
requires explicit drain/migration; old jobs fail `configuration_changed`, never
silently switch directories or send their credential to a different endpoint.
The queue's `native-profile-v1` limits remain 8 jobs, 256 MiB reserved bytes,
8 MiB per journal and one worker. This finite profile is not a claimed maximum
protocol file size; changing quotas requires a reviewed locator migration.

## Producer and finite-command handoff

Enqueue reads **metadata only** from stdin, at most 16 KiB within the two-minute
command budget. The trusted local producer supplies `job` (version 1), including
`delivery_id`, `snapshot_id` (same ID), `bot_id`, `operation_id`,
`locator_revision: native-profile-v1`, `phase: queued`, `spec` and `device_task`.
The latter contains `user_id`, `session_id`, `target_id`, `run_id`, `attempt` and
`generation` from the existing operation handoff. Add `source_ref` for uploads.
Do not put a credential or file bytes in this JSON. These IDs are lookup
selectors, not authorization; the Server verifies them against its current
operation, Bot permissions and authenticated device.

Reinvoke the same enqueue/ID/metadata after admission timeout to resume sealing;
do not invent a new ID. Completed admission uses its immutable snapshot even if
the producer file or export directory has since been removed. New or incomplete
upload admission still requires the original authorized source. Once admitted,
invoke `advance` or the fixed manifest
`advance-delivery` action. Each call performs bounded work then exits; the existing
host owns when to invoke again. Backoff and unknown outcomes persist in the
queue. `cancel` records intent; subsequent advance reconciles publication/remote
commit and can report that delivery already won. `reconcile` is an explicit
recovery action, not a retry-budget reset. The original create-unknown protocol
fence remains; never guess a new receipt after uncertain creation.

## Bounded task producer

The optional task declaration `artifact: {source_ref, mime}`, or `artifacts` with
1–16 distinct entries (never both), is available for
`herdr_pi_task` / `pi-json-v1` at `work_dir: "."`. The registered task root must
match the existing private `source_root`; a mismatch fails explicitly. Write and
close the declared files before Pi exits. The envelope supplies non-secret
`MEMOH_DELIVERY_CONTEXT_FILE`, closes a metadata manifest, and invokes the fixed
`collect-result` action. This does not change Agent code or its prompt.

`enqueue-result` accepts only bounded closed metadata on stdin. The official
Herdr CLI does not forward arbitrary stdin: its fixed `collect-result` action
opens the manifest through the configured root and feeds that same decoder.
After durable admission the original snapshot/receipt are reused; repeated task
result queries can wake finite advancement through the dedicated control pane.
No host timer or daemon is added. NAS `delivery.reference` is available to the
parent task only after committed association, separately from Pi completion.

The bounded list retains one stable delivery/closed manifest per file, within
the existing eight-job /
256 MiB total snapshot limit. Finite host actions automatically release only
confirmed committed queue payloads and retain the original delivery/receipt.
Unknown, canceled and blocked jobs are not automatically cleared. The compact
receipt catalog stops new admissions at 1024 records; proofs are never silently
evicted. See `docs/delivery-queue-retention.md` for recovery and upgrade bounds.
Each finite hook still handles at most one manifest; later task queries/host
actions continue remaining work. Multi-file aggregate `partial` preserves each
committed reference; it does not mean all files delivered. Recovery from a crash
before a closed manifest exists is still pending/unknown and never reruns Pi. A pending delivery is not a
successful file transfer, and task cancellation does not retract delivery.

CLI stdout contains bounded phase/offset/association/reference data only. It is
not execution success and does not contain source locations, credentials, file
bytes or full logs. Producer wiring is implemented and cloud tested. Actual installed Herdr/PC
ownership, HTTPS trust and the real PC/NAS route still require acceptance.
Windows producer tests use a protocol fixture; Linux upload Server/PG and
Windows download publication/PG tests are separate combinations.

Do not install/link or configure this package against production as part of
fixture acceptance. The API fault model and limitations are documented in
`docs/design/windows-native-binding.md` and `docs/design/device-delivery-binding.md`.
CI success is not real UI or human QA. The PR remains draft pending those checks.
