# Authorized task source and private queue boundaries

This project adapter separates an existing executable task workspace from its
private durable transfer queue. It changes no Agent core, task declaration,
producer/feedback format, transport protocol or HTTP route. The host continues
to supply the same private config/state directories. The exact `transfer.json`
bytes still determine HostBinding; the original execution root still determines
RootHash. Existing jobs are never rebound or migrated by this change.

## Existing producer contract

| Object/action | Scope and ownership |
| --- | --- |
| Task root | The already authorized execution directory, matching `target.work_dir` and `transfer.source_root`. It can remain writable by the task. No whole-workspace read-only requirement is added. |
| Artifact read | One declared flat logical source reference, via a read-only handle beneath the pinned root. Single ordinary file only; no hardlink, reparse point, symlink, ADS or path traversal. Original file ID/mtime, complete hash and block proofs remain checked by the existing queue. |
| Closed manifest read | Existing `memoh_delivery_<delivery-id>` at that same root. Strict bounded decoding, exact filename, original full task, RootHash and matching declaration in the existing attempt context are required. |
| Manifest consumption | Only after the original durable registration/admission. Reopen and compare the exact validated bytes before deletion. Windows deletes through that manifest handle without clearing read-only attributes. No artifact, unrelated metadata or original source is deleted. A lost consume acknowledgment replays the original registration; it cannot create another receipt. |
| Context read | Existing `.memoh-computer/attempts/<original-attempt>/producer-context.json`. Open existing components through the pinned root, reject aliases, keep the validated context handle while projecting feedback. No directory creation/fallback. |
| Feedback write | Only `delivery-feedback.json` in that same original attempt. Validate task, declared delivery IDs, RootHash and exact job HostBinding. Reject changed context, aliases and multiply linked files before truncation. Existing advisory format, queue lock, file sync and directory flush remain in use. No successful reference or secret is put in this advisory. |
| Config/state/queue/publication | Original private local NTFS owner/DACL, filesystem, no-reparse, handle/sharing, locking, flush and identity protections are unchanged. A source directory with shared permissions is still rejected as queue storage. |

No second artifact copy, new credential, connection setup, mapped drive, listener
or service is introduced. Source operations use only the invoking OS identity's
current permissions. Unsupported or denied operations fail explicitly. Partial
feedback remains advisory and can be rewritten by a later existing finite action;
the authoritative queue is preserved. Queue registration survives death before
manifest removal. New metadata-first jobs still need the original source to be
available and unchanged; legacy private snapshots retain their earlier semantics.

## Native filesystem support

Windows sources accept the existing local fixed NTFS task location or a canonical
native SMB UNC location. The opened directory's final path must match the exact
configured root (case-insensitive Windows comparison). A remote handle must
report SMB and must not report an offline-cache substitution. Mapped drives,
device namespaces, junctions and nonmatching canonical locations fail closed.
The adapter neither repairs access nor adds authentication. Source handles deny
write/delete sharing during reads; metadata deletion uses the verified handle.
SMB directory flush failure remains an explicit source/feedback failure, never
an ignored durability success.

Linux/macOS use descriptor-relative no-follow opens as an isolated equivalent
for OS-authorized shared source permissions. Local owner-only queue guards are
unchanged. Unix manifest unlink checks the live name against the opened inode;
it does not establish Windows sharing/delete-by-handle behavior or SMB durability.

## Endpoint integration and verification boundary

There is no calling interface or schema change. Existing `HostConfig.OpenSource`,
`CollectProducer`, `PublishProducerFeedback`, CLI actions and status values are
retained. The Windows endpoint must continue to enforce the same immutable
`target.work_dir == transfer.source_root` configuration pair. Preparing a new task
pair must preserve existing queue/config history; this patch does not overwrite
old `transfer.json` or HostBinding. Workspace-to-task CWD wiring belongs to the
unified owner and is not modified here.

`TestTaskSourceProcessRecovery` uses actual 131109 bytes, independent child
processes and an isolated TLS protocol fixture. It kills the first process after
durable original-ID registration, loses a PATCH acknowledgment and an association
acknowledgment, replays the original manifest, and checks a single Create, exact
bytes/hash, original receipt/task/binding, preserved source/config and no payload
snapshot. Scope tests reject foreign/missing contexts, wrong roots, undeclared
files, replaced manifests, foreign feedback bindings, hardlinks and traversal.
Unix tests independently revoke source permissions before a new process and
check blocking before any network call, plus shared-source/private-queue separation.

`TestWindowsNativeTaskSourceBoundary`, `TestWindowsNativeTaskSourceProcesses`,
`TestWindowsNativeTaskSourceScope` and `TestWindowsNativeTaskSourceRevocation`
are provided for the existing Windows
fixture runner. Cross-compilation is not native execution. Actual Windows NTFS
execution, SMB UNC access/revocation/reconnect, directory flush and field endpoint
recovery remain the sole onsite writer's acceptance work. No NAS deployment or
field acceptance is implied by cloud results. Web remains the existing NAS media
and result consumer. HarmonyOS remains its existing client contract; neither
client gains access to a PC source path or its private durable queue.
