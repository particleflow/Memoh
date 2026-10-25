# Cross-end transfer alignment and shared acceptance data

This is a source-level alignment audit and an executable test-data handoff, not
an all-end acceptance result. It adds no product code, protocol, service, storage
root, credential, queue limit or installer. Field operations belong to the
existing field-verification owner; none were performed here.

## Pinned implementations

| Component | Inspected revision | Qualification |
| --- | --- | --- |
| NAS Server and shared Web | `8a2422935916f56b5bacfac2f991824d1cb9bd9f` | Actual route/config/caller source, not just generated SDK availability |
| Windows endpoint | `41ddb9be845eae09a1ac49a7bde3e8a108e510b0` in `particleflow/windows-endpoint` | Actual endpoint config, lifecycle consumer and packaging pin |
| Producer | `0894a6eb8ff69c8796e2da5450fa172d4cd5ef81`, 0.10.1 | Windows packaging pins this revision; transfer product behavior in 8a matches (feedback file has a whitespace-only delta) |
| Harmony | `1bd851eca27bfb86adad690d01907b56f4b3f82f` handoff; package source `c9c00580052d09d5dce273e958ec384bef2ebed3` | 1.0.9 / 1000009; `mobile/client/config/memoh_web.lock.json` and package build identity already pin shared 8a. Unsigned package/field acceptance remains separate. |
| Prior native integration evidence | `82efb75e931b5bf26c6121f34d2df5da46486ac4` | Linux Server/PG + pinned producer; separate from Windows host and phone execution |

## What is shared, and what is not yet aligned

**One NAS file module is already present.** In 8a,
[`provideMediaStorage`/`provideMediaService`](https://github.com/particleflow/Memoh/blob/8a2422935916f56b5bacfac2f991824d1cb9bd9f/cmd/internal/core/providers.go#L1247)
pass the same resolved `files.originals_root` to durable original storage and
the upload store. `.uploads` is derived inside that root. Client private config,
journals and source files have distinct ownership; they must not be relocated
into the NAS original directory. A nonempty originals root is required for this
deployment contract; the historical empty-root fallback is not an equivalent setup.

[`MessageHandler`](https://github.com/particleflow/Memoh/blob/8a2422935916f56b5bacfac2f991824d1cb9bd9f/internal/handlers/message.go#L133)
exposes media limits, multipart, resumable Create/Get/PATCH/Complete/Cancel and
GET/HEAD by hash. The
[`computer-transfers` wrapper](https://github.com/particleflow/Memoh/blob/8a2422935916f56b5bacfac2f991824d1cb9bd9f/internal/handlers/computer_transfers.go#L106)
calls these same handlers and adds operation/delivery-bound Commit/Resolve.
Phone/Web account authorization and the PC's independent mft device credential
are intentionally different. A Runtime mrk credential is not a file token.
Sharing a NAS identity does not mean sharing one credential or inventing a
computer operation for every ordinary chat attachment.

`files.max_upload_bytes` is the existing per-file server policy (zero selects
200 MiB), returned by `/media/limits`. Its deployed value was not read in this
audit. The prior 1 GiB download result does not prove a default server admits a
1 GiB upload; this must be checked independently of client active-window limits
or total queued metadata. No limit or production configuration was changed here.

The initial progress note incorrectly described Harmony's 8a candidate as still
unavailable. Reading its lock and `artifacts/harmony/1.0.9-c9c005800/build-identity.json`
corrects that statement: it is already based on 8a. The caller gaps below are
inside that pinned source, not evidence of an outdated shared-Web revision.

| Actual consumer | Confirmed behavior | Outstanding difference and owner |
| --- | --- | --- |
| Windows 41ddb / producer 0.10.1 | Existing environment config/state pair, unchanged raw `transfer.json` binding; same task/attempt/delivery/receipt across wakes. Download `feedback_status:not_applicable` is distinct from outbound `recorded`; `unavailable` still fails. | Native Runtime + installed Windows host + NAS + both directions remains a joint field acceptance step. Prior Linux native and Windows NTFS fixtures cannot be added together and called that result. Windows owner owns host selection, old-event gating and single instance. |
| Shared Web 8a chat upload | [`useComposerUpload`](https://github.com/particleflow/Memoh/blob/8a2422935916f56b5bacfac2f991824d1cb9bd9f/apps/web/src/pages/home/composables/useComposerUpload.ts) reads limits and posts multipart to Media; verifies returned hash shape and size. Its File/attachment cache is an in-memory WeakMap. | No persistent receipt/offset resume in this actual caller. SDK resumable methods existing does not wire them into the UI. Shared Web owner must adopt the existing file protocol for interruption/restart requirements. No shared UI was edited here. |
| Harmony 1bd chat upload | Build overlay calls [`harmony-files.ts`](https://github.com/particleflow/Memoh/blob/1bd851eca27bfb86adad690d01907b56f4b3f82f/mobile/client/mobile/memoh/harmony-files.ts#L204): `file.arrayBuffer()` hashing, existing-file GET/hash, multipart upload, full read-back verification; uncertainty tracking is in memory. | This is not streaming/persistent resumable upload and adds whole-file reads/memory. Harmony owner owns the caller correction using the existing NAS routes. |
| Harmony retained resumable uploader | `NativeResumableUpload.ets` contains private journals, offset/hash checking and a complete app-owned source copy. | **Not wired into this candidate.** [`NativeClient.ets`](https://github.com/particleflow/Memoh/blob/1bd851eca27bfb86adad690d01907b56f4b3f82f/mobile/client/mobile/harmony/app/entry/src/main/ets/common/NativeClient.ets) registers download/voice commands, not `upload.*`; `build_memoh_web.py` does not mount `HarmonyAttachments`. Its 16-total/4-owner selection ceiling is a latent implementation limit, not an executed failure of current phone uploads. Do not enable it unchanged and call it large-backlog support. |
| Harmony download | [`NativeMediaDownload.ets`](https://github.com/particleflow/Memoh/blob/1bd851eca27bfb86adad690d01907b56f4b3f82f/mobile/client/mobile/harmony/app/entry/src/main/ets/common/NativeMediaDownload.ets) is registered. Range/ETag/size/full hash, bounded foreground retry and picker save are present. | State/offset are in memory; an existing `.part` is not reopened as a durable job, and an unsuccessful fetch removes its temporary file. Process-restart resume is missing. Harmony owner; no Ark runtime/phone execution was done by this audit. |
| Shared Web and Harmony result reading | Web's [`resolveUrl`](https://github.com/particleflow/Memoh/blob/8a2422935916f56b5bacfac2f991824d1cb9bd9f/apps/web/src/pages/home/composables/useMediaGallery.ts#L51) preserves a validated `delegation_url`; Harmony passes that scope through its native download. Shared task UI only turns **committed** deliveries into attachment refs. | Browser download management is not proof of an application-owned durable journal. A phone package must retain these exact scope rules after its 8a update. Snow-scene recovery is owned by the media line; a reference alone is not evidence that that recovery executed. |

Source findings above are not newly executed mobile/UI failures. No producer or
NAS-file-module defect requiring a product change was confirmed in this audit.

## Identity and routing handoff

The Windows
[`transferLocations` and `transferEnvironment`](https://github.com/particleflow/windows-endpoint/blob/41ddb9be845eae09a1ac49a7bde3e8a108e510b0/endpoint.mjs#L337)
select the existing paired `HERDR_PLUGIN_CONFIG_DIR` and
`HERDR_PLUGIN_STATE_DIR`. Native setup uses its explicit pair; a delayed old
task must retain its old pair. No global active-profile pointer or rewritten old
queue is part of this contract. A reconnect only wakes the existing finite queue;
it does not submit Pi again. Locator, source root, operation, Bot, user, session,
target, run, attempt and generation remain bound. Native connection/device IDs
are the registered runtime UUID, and instance is the configured Herdr instance;
legacy Agenvo identities cannot be silently relabelled.

The exact HTTPS API base (including `/api` where deployed) remains the authority
and TLS hostname. Optional `transfer-network.json` lives in the **same** private
config directory. Its explicitly supplied `lan_addresses` are tried in order;
TCP connection failure falls back to the original authority using IPv6. After a
TCP connection succeeds, TLS/authorization failures must not try another address.
This policy is implemented by the existing
[`host_network.go`](https://github.com/particleflow/Memoh/blob/8a2422935916f56b5bacfac2f991824d1cb9bd9f/internal/transferclient/host_network.go).
It changes routing, not HostBinding, authentication or task references.

Neither the current Harmony native RCP caller nor browser fetch consumes that PC
policy file. They use the configured origin and platform DNS/routing. A global
IPv6 address can already be on-link LAN; address family does not prove LAN/WAN.
Explicit LAN-first behavior on all endpoints is therefore **not demonstrated**.
Endpoint owners must first verify the actual same-origin route on each client;
do not invent another NAS identity, assume IPv4=LAN, or claim a fixture route is
the deployed network. No DNS, trust, routing or proxy configuration was changed.

## Shared runnable data (no network side effects)

Run from this evidence checkout with Python 3.8+; choose a **new** directory under
an existing parent. The tool refuses to overwrite an existing dataset. It has no
HTTP, authentication, runtime, service or OS trust-store actions.

```sh
python3 scripts/acceptance/transfer_data.py create /tmp/memoh-cross-end-data
python3 scripts/acceptance/transfer_data.py verify /tmp/memoh-cross-end-data
```

This creates 36 real, non-sparse files totaling **19,997,326 bytes**:
`chunked.bin` is 17,825,915 bytes, 32 independent small files form `batch/`,
`same-content.bin` equals `batch/01.bin`, and the two `collision-*/result.bin`
files have equal names/lengths but different contents. `manifest.json` records
every expected full SHA-256 and size. This is a small cross-end corpus, **not**
a new scale benchmark, product limit, Pi declaration or claim of 10 TiB transfer.
It is not a product dependency to install on the phone/PC.

The checked-in [manifest](evidence/cross-end-alignment/manifest.json) is the
portable expected-data reference. Regenerating this fixed dataset must produce
manifest SHA-256
`c5f33ac4e64aefd8369751af6496f5345327827749e89c03aec7e51c2a48fa21`.
The [original tool-check output](evidence/cross-end-alignment/data-check.log)
has SHA-256 `f409274acc01d474c9cd6685ca38739b8569b2841d5412a3fabd2064313b0c78`.
All 36 files verified; duplicate-content verification passed; corrupted bytes,
same-name different content and an existing destination were rejected. A final
check confirmed every original unchanged. Actual allocated source-file blocks
totaled 20,144,128 bytes on the cloud filesystem. The script uses 64 KiB buffers;
this run did not measure peak RSS, wire throughput or transfer requests.

Give the field owner these exact files and the manifest through the approved
existing channel. Each downloaded result can be checked, even if its picker
changes the display filename:

```sh
python3 scripts/acceptance/transfer_data.py check-file /tmp/memoh-cross-end-data \
  chunked.bin /path/to/downloaded-copy.bin
```

On Windows the built-in `Get-FileHash -Algorithm SHA256` and `(Get-Item).Length`
can compare a downloaded file with the same manifest; Python installation is
not required. Checking an original before and after each scenario must return
the same size/hash. Do not delete unknown, blocked, canceled or original files
to make a test pass. The generator/verifier never performs cleanup.

## One field acceptance set and expected results

Use existing client screens/task submission and the installed finite producer;
do not pre-enqueue a result to hide a broken completion hook. Do not copy files
with HostFS and call that producer delivery. Capture only logical IDs, byte
counts, offsets, result states and hashes; no credentials, raw source/log bodies
or authenticated URLs in shared reports. These are tester observations, not a
new per-file approval or workflow imposed on users.

| Case | Action using the shared data | Required observation |
| --- | --- | --- |
| A: ordinary upload/read | Select `chunked.bin` on phone and Web for an authorized Bot; read the resulting reference from the other client. | Same original hash/size; exact configured API prefix; file bytes use Media, chat contains only the reference. Phone/Web resumable expectations currently remain open as listed above. |
| B: native task closure | Submit one normal native task using its original input ref; let the original completion wrapper produce/close a result. | Actual `collect-result` runs with the original locator pair; durable NAS Commit and original task/delivery ref appear. Business success alone, a HostFS file or a collector command being accepted is not delivery. |
| C: upload interruption | During a multi-chunk transfer, lose a PATCH response or kill only the isolated transfer process after a confirmed chunk; reopen the same configured queue. | Query original receipt; accepted server offset does not retreat. Already acknowledged chunks are not uploaded again. Full hash, one logical delivery and original operation/attempt remain. Record uncertain tail separately from confirmed-byte retransmission. |
| D: inbound interruption | Download the same NAS ref to PC, interrupt the isolated transfer process and restart; repeat on phone when its persistence is implemented. | Range resumes from durable offset with matching ETag/size; verified final file only; same receipt/task/ref after reopen. Phone currently lacks the persistent restart part: report this as open, not pass. |
| E: completion response lost | Withhold a successful Commit response in the existing isolated fixture, then restart the child. | Original Resolve/ledger supplies proof; no new Pi execution, receipt or result; unknown without proof stays unknown. A file with the same hash alone is not task delivery proof. |
| F: small batch and conflict | Transfer the 32 ordinary small files, interrupt one, resume; separately try equal content and same filename/different content. | Independent successful files remain readable; no missing/overwritten original. Content reuse is allowed under authorization; task identity/conflict checks remain distinct. Do not feed 32 files into an undeclared Pi manifest or equate UI page counts with byte transfer limits. |
| G: routes and denials | On the existing isolated TCP/TLS fixture, test configured first route, connection refusal, wrong hostname and permission rejection; field owner observes real route changes separately. | Same origin/task/receipt; only connection failure allows the configured IPv6 fallback. No fallback after certificate/auth failure. Record destination/family without exposing private addresses in public artifacts. |
| H: restart/reconnect | Restart the selected host/Runtime connection after committed delivery, then read the old task and queue. | Same task/attempt/ref and unchanged create/commit counts; no Pi rerun. Old principal's config/state remains unchanged and is not automatically advanced. |

Existing local read/advance interfaces (Windows syntax, existing variables only):

```powershell
$env:HERDR_PLUGIN_CONFIG_DIR = $taskConfigDir
$env:HERDR_PLUGIN_STATE_DIR = $taskStateDir
& $producer -action lifecycle-summary
& $producer -action inspect -delivery $deliveryId
# Only when the existing lifecycle says due; it owns locks and backoff:
& $producer -action advance-results
```

`due/idle/waiting/blocked/unknown`, `next_due_at` and `error_class` remain the
producer's existing lifecycle contract. `idle` is not a delivered result;
`registered` is not admitted bytes; `ready` is not a committed task attachment.
`not_applicable` only exempts a validated inbound download from the outbound
producer feedback projection. Never convert every `unavailable` into success.

## Reuse evidence rather than rerunning scale tests

- [Native generated completion + real Server/PG](native-transfer-producer-combination.md): actual fresh result collection, one Pi/Create/Commit/Resolve, lost Commit reply and three fresh wakes, old identity/pair isolation. Linux with explicit shim; not Windows Runtime/NTFS acceptance.
- [Windows real 1 GiB default-CLI recovery](transfer-native-large-byte-recovery.md): actual NTFS/Server/PG, fixture-only RootCAs, full hash and confirmed-byte retransmission assertions. Not formal EXE system trust, field LAN or physical power loss.
- [Three-file process recovery](transfer-batch-process-recovery.md): three real 17 MiB files, partial batch failure, original receipts/refs, reopened server stores. Not a PG/server power cut.
- [Configured LAN evidence](transfer-lan-preference.md): actual TCP/TLS loopback and original-identity checks. NAS protocol storage there is a fixture, not the real Server/PG; do not merge the scopes into a fabricated test result.
- Prior Linux 1 GiB/512-file and 10,000 sparse-metadata evidence remain separate: only the former transferred large real bytes. None proves a phone backlog, all-end automatic draining, or 10 TiB actual transfer.

No old tests were rerun in this audit. New execution is limited to deterministic
data generation/hash verification, corruption rejection and overwrite refusal.
The final signed mobile installation, real UI/phone/Windows-host runs, field routing and
physical durability remain unverified. This handoff does not install Memoh
runtime, take browser ownership, claim screenshots/human QA, or authorize any
new production security action.

## Mapping to the existing unified acceptance checklist

The parent owns updates to the [unified checklist](https://chatgpt.com/space/page_881a70a14c5081919d8fd93132fc6968).
This evidence does **not** check off any item or create another global checklist.

| Existing item | This handoff contributes | Still required |
| --- | --- | --- |
| F01 / W05 | Exact central NAS root/locator/API-base source paths and existing explicit directory-pair contract | Actual selected configuration and historical identity at the installed endpoints |
| F02 | Existing PC ordered LAN/TCP-to-IPv6 policy and preserved isolated TCP/TLS evidence; no claim that Web/phone implement it | Real client route and loss/recovery observations; platform callers' routing difference |
| F03 / F04 | Preserved real 1 GiB Windows evidence, separate batch/metadata boundaries; runnable common 36-file corpus with hashes | Actual field byte transfer and independent list-page verification; corpus generation alone proves neither |
| F05 / F08 | Exact active upload/download paths; missing phone/Web persistent resume identified; scoped refs retained | Correct caller wiring by the respective owners, then the same cases on each installed end |
| F06 / F07 / W04 / N03 | Prior 82ef generated-completion/unknown-Commit recovery and unchanged old queue/source evidence, reused without rerun | Actual Windows Runtime/Herdr/producer/NTFS/NAS combination and field restart/route validation |

Scope stays on the original path: test data plus exact source/evidence handoff.
No additional upload declaration, UI pagination framework, directory-sync product,
source migration, security change or second queue was introduced. No execution
was denied or pending in this audit; the listed gaps are unimplemented client
behavior or unperformed field verification, not a claim that all work is blocked.
