# Selected NAS directory: execution wiring increment

Base: frozen `9020c93f7f99bd98f7f8b4e15db1ea685ef27ea2`. This increment
does not move its published image, change private configuration, deploy a server,
or modify Windows/Herdr/Pi/producer binaries. The existing three storage HTTP
operations and generated SDK are unchanged; schema remains177.

## Implemented host wiring

- A new computer submission with an omitted directory or `.` resolves the saved
  owned NAS directory. A stored session workdir takes precedence; an explicit
  non-default task directory is not replaced. An absent NAS selection preserves
  the original local default, including when NAS storage is unconfigured.
- The normal DI factory resolves the mount's real owner/Runtime and reuses the
  existing owned selection and current connection. One Stat checks the selected
  directory before a default-based claim. There is no repeated per-block probe.
  Current root validation is unchanged: an existing local target cannot silently
  become an authorized NAS execution root.
- File-workspace resolution projects the same configured directory as its
  DefaultWorkDir. This does not mutate the official Runtime's WorkspaceBase.
  Existing explicit workdir bindings suppress the account default in host
  preparation and tool contexts; this flag confers no permission or path grant.
  Projection performs no added network probe. Actual file access retains the
  existing Runtime OS permission and bridge errors.
- The resolved default is retained as `default_work_dir` in the original
  operation's existing Details JSON. Explicit resume of that original default
  request uses this value instead of a subsequently changed account selection.
  Legacy records without it keep their original `.` interpretation. Original
  request/target fingerprints, revision CAS, receipts and Pi identity remain
  authoritative. No old operation, stored workdir, profile or queue is migrated.
- New native claims also retain the existing typed Target as `target_snapshot`
  in the same Details JSON: original root, Herdr references and producer
  program/config/state directory pair. Its hash must match the operation's
  TargetFingerprint. The producer's existing plan still carries RootHash from
  that original target root. Neither this snapshot nor a local receipt grants
  authority to a changed registration: current grant/fingerprint checks remain
  mandatory. A global profile/locator change still fails closed for the old
  operation; this patch does not invent a cross-root takeover mechanism.

The code changes only application/adapter wiring. The official Agent runtime and
contextview match the repository's fixed official baseline.

## Contract verification

`workspace_execution_contract_test.go` joins public Runtime activation and
controlled existing gRPC Stat, the actual storage selection service, trusted
`computer_task_submit` Source, original claim/save service and native adapter.
It checks the literal cwd arriving at `workspace.create` equals the selected
UNC, and the effective file default is identical. It stops with an uncertain
synthetic creation response: **zero Pi input**, not successful Pi execution.
The official Runtime handshake is unchanged.

Cases include an omitted directory and model `.`, root escape rejection before
dispatch, explicit local input and bound local session during unrelated NAS
failure (zero NAS probes), and unavailable NAS default (zero dispatch).
Separate regressions cover absent defaults/owner denial, public routing inputs
not establishing workdir precedence, and a later default change not altering an
original resume fingerprint or issuing another creation/Pi input.

Successful and failed command outputs are retained under `execution-contract/`.
Whole host/Core compilation and incremental lint encountered local ENOSPC;
these failures are not test passes. Only two confirmed stale task-owned compiled
archives were reclaimed; source, evidence, DB and original files were retained.
There is no new UI surface or HAP build. Full final combination CI and physical
acceptance remain required; this increment is not a release approval.

## Two distinct field prerequisites

1. **Configuration/mapping:** the field owner reports `root` and `share_root`
   absent and no saved workspace_storage selection. The current storage GET503
   is therefore expected. `[container.storage].root` must identify the intended
   workspace-storage directory inside an existing Server mount;
   `share_root` must be an existing Windows-readable LAN UNC mapping to that
   same physical directory. An RW media-originals mount, NAS share existence or
   listening445 alone does not establish this mapping or its Windows access.
   Do not guess a UNC, use media originals as writable task storage by assumption,
   create a share/mount/credential, or rewrite existing roots. The unique field
   writer must verify the actual existing mapping and selected Windows identity.
   Missing mapping blocks NAS browse/create/default execution, not local work.
2. **Producer source capability:** even a correct mapping and coherent root
   configuration do not make producer0.10.1 read a UNC. Its source path currently
   reuses private queue-root/file checks that reject UNC and remote drives.
   This is a project-owned transfer adapter limitation, not an official Agent
   core or OAuth/TLS issue. It requires the transfer owner's separate increment.

### Mapping the requested NAS hierarchy

Existing source distinguishes these roots; it does not define a global NAS
filesystem browse root from which the others can be inferred:

| Existing key/source | Owner and meaning |
| --- | --- |
| `files.originals_root` | Immutable attachment originals; not an execution root or a general NAS browser grant |
| `container.data_root` | Runtime infrastructure/metadata and fallback media; not the user's NAS root |
| `container.storage.root` | Server-visible physical workspace-storage directory under the intended existing NAS root |
| `container.storage.share_root` | The existing Windows UNC mapping of that exact workspace-storage directory |
| Runtime `WorkspaceBase` / Herdr work_dir | Existing Windows host/target execution references, not a NAS share discovered by this service |

Let `N` denote the intended, already approved NAS file root (not necessarily the
parent of originals). The requested layout is `N / 工作区存储 / computer`. Configure
root as the existing Server mount's path to that workspace-storage directory,
and share_root as its verified existing UNC. The computer namespace below it is
the stable Runtime UUID; the UI displays the registry's computer name. Selection
is relative inside that namespace. Do not repurpose the originals' garnet-assistant
directory itself, infer `N` from that name, or pick unrelated mapped X:/Y: drives.
The field report proves neither Windows access to the project share nor that
this share is the intended `N`. Get-SmbConnection error5 was not bypassed.

The exact two path values and existing mount/share correspondence remain for
the sole field writer to establish. No new NAS-root configuration service,
SMB share, credential, drive mapping or original-data rebinding is introduced.

## Exact producer handoff; no transfer files changed here

- `host_config.go`: `openSources -> privateHostRoot`, and
  `OpenSource -> openPrivateFile`, currently apply private queue rules to sources.
- `producer_feedback.go` also opens the source attempt directory with
  privateHostRoot and reads/writes context/feedback using private file guards.
- `durable_windows.go`: handlePath/checkRootHandle require a fixed NTFS drive;
  checkPrivateHandle checks local private ownership/ACLs. Queue/config/state,
  journals and publication durability must keep these protections unchanged.
- Source-specific access must retain configured-root containment, pinned handles,
  reparse/link defenses, stable identity, exact size/hash and OS access checks.
  Do not solve it by relaxing all queue checks or introducing execution copies.
- Existing calls use producer.program_path/config_dir/state_dir; no task-level
  `--source-root` exists. transfer.json bytes determine HostBinding; do not
  overwrite an old file to redirect an old queue. RootHash and the original
  target fingerprint/producer directory pair remain immutable references.
- Current artifact declarations require `work_dir == "."` and flat source refs.
  A selected non-root cwd is not proof its artifacts are collected: retain that
  boundary until the original transfer owner aligns the existing producer
  contract. This increment does not change it or claim shared-source delivery.

The existing Windows configuration can express UNC roots, but actual UNC source
access, canonical path spelling (including drive/UNC aliases), effective Windows
identity, old queue compatibility and same-operation field recovery remain
unverified. No automatic profile/Host sourceRoot cutover is performed here.

Application rollback uses the existing schema177 database and original files.
Previous binaries ignore the added Details field; they may reject a new
default-based explicit resume by fingerprint rather than restore its cwd. Use
the new source for such a continuation; never change IDs/replay to bypass that
rejection. No migrate-down, history deletion or queue-root rewrite is warranted.
