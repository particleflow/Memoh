# Native Windows delivery follow-up design

Base under review: `517a73cebfc9968aee431616d3f4a8ae51596a86` (unchanged).
Work branch: `feat/windows-native-delivery-host`. This design precedes fixes and
native implementation. No production ACL, credentials, network settings or
Agent code are changed. No daemon or separate task-state service is proposed.

## Review regressions first

The frozen code has three confirmed problems: every upload action rereads the
whole snapshot; context cancellation/deadline errors after a committed
association become permanently blocked; cancellation after an ambiguous commit
suppresses reconciliation. Regression tests are added first and must fail on
the frozen code. Results belong in the follow-up validation evidence.

### Association protocol inside the existing host binding

Persist an outbox phase before invoking the host: `in_flight`, then `unknown`
when the outcome is ambiguous, or `committed` after acknowledgement. This is
file-association state only, not an execution task lifecycle. A reopened
`in_flight` record is treated as unknown. Context cancellation/deadline errors
are ambiguous just like lost transport responses. A finite retry budget may
pause automatic attempts, but it must preserve the unknown outcome and permit
an explicit bounded reconciliation action.

The injected host binding must distinguish `commit` and `resolve`. Both use
the same stable delivery-id/content/task binding and reauthorize. Resolve returns
committed, absent/pending, or canceled. An absent query does not prove a delayed
original request cannot still commit, so it must not reset to "never attempted."
Idempotent commit replay remains possible after a non-canceling absent result.

Cancel persists intent. Before any commit attempt it can suppress local
association. After an ambiguous attempt it must resolve against the same
existing task store. A canceling resolve must atomically either observe the
committed association or fence that delivery key against delayed commits and
return canceled. Plain "not found" is insufficient. If the service already
committed, report delivered with cancel intent recorded; never claim it was
withdrawn. The implementation uses an explicit fixture for this atomic binding;
the production adapter remains a coordination point with ComputerOperationStore
and its revision CAS, not a new service.

Tests: committed-then-deadline/canceled, process crash after in-flight checkpoint,
lost commit reply then cancel, cancel tombstone versus delayed commit, absent
query versus delayed commit, revoked resolve, retry budget pause/resume, and
download reconciliation that does not republish bytes.

### Bounded snapshot integrity

Admission streams the source once into a private snapshot, computes its full
expected SHA-256, and creates a fixed-block Merkle tree in the same owned queue
namespace. The admitted journal binds geometry, root digest and opened-file
identities. No new secret or credential is required. Block data and hash nodes
use domain separation and bind position/length; geometry derives from size.

Each transfer reads only its touched fixed-size blocks, checks their Merkle
proofs against the admitted root, then copies the requested bytes. A changed
block/proof/identity fails before upload. The server still checks the final
full-file hash. Identity, containment, private ownership and exclusive handles
remain necessary; size/mtime alone are not a seal. Metadata and scratch memory
are bounded, and proofs require logarithmic metadata reads. A journal version
change must reject old state explicitly; no silent fallback/migration.

Performance tests compare frozen repeated-full-verify against admission plus
sequential verified block reads at 1/64/512 MiB with 8 MiB upload chunks. Count
actual source/proof bytes, allocations and time. Frozen 512 MiB with 64 chunks
requires 32 GiB of full-file verification reads. Target post-admission data reads
are approximately one file, with bounded boundary-block amplification and
O(chunks × log(block count)) proof metadata. Repeat with reopen per action,
unaligned offsets, cancellation, and corrupted data/proof nodes. Warm-cache
cloud tests are not NTFS, physical disk or NIC measurements.

## Native API facts to preserve

- [CreateFileW](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-createfilew)
  sharing modes can reject existing/future writers and deletion/rename; an
  existing writable mapping also conflicts when write sharing is withheld.
  Snapshot reads must use this mechanism rather than treating an advisory
  byte-range lock as complete immutability.
- [LockFileEx](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-lockfileex)
  releases locks after process termination, potentially with delay; mapped
  views bypass byte-range locks. Use nonblocking locks for cooperative queue
  ownership and handle-sharing restrictions for file access.
- [FlushFileBuffers](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-flushfilebuffers)
  flushes an opened writable file. Flushing an entire volume requires admin
  privileges; this adapter must not request them. Windows
  [file caching documentation](https://learn.microsoft.com/en-us/windows/win32/fileio/file-caching)
  requires file flush or write-through for file metadata persistence. It does
  not establish a POSIX directory-fsync equivalent for arbitrary namespace edits.
- [MoveFileExW](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-movefileexw)
  documents write-through behavior, specifically including copy/delete moves.
  Do not infer an unrestricted atomic-rename-plus-directory-durability theorem.
- [DACL semantics](https://learn.microsoft.com/en-us/windows/win32/secauthz/null-dacls-and-empty-dacls)
  distinguish absent/null ACLs from deny-all empty ACLs. Validate the current
  process user's ownership and allowed ACEs through handles. Do not repair
  production permissions automatically. New isolated test objects may receive
  a private creation descriptor; existing production ACLs remain untouched.

The Windows implementation must explicitly distinguish process-crash recovery,
acknowledged file-flush ordering, namespace recovery and hardware power-loss
limits. It must use real supported API operations, not a success-returning stub
called "directory sync." Existing Herdr plugin state/config locations stay the
single locator source. Native identity/reparse/volume checks, case-insensitive
names and reserved-name handling are required before enabling the binding.

The package will provide a finite executable and reviewable Herdr manifest plus
an isolated Windows test bundle/runbook. No credentials in argv/model messages,
no runtime installation, no production plugin installation and no new listener.
Actual Windows execution will be performed by the parent's existing local task;
cloud build results must not be described as native test execution.
