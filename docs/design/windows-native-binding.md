# Native Windows binding and acceptance boundary

This follow-up consumes PR13's outbox/Merkle fixes, PR14's resumable admission,
and main `43d2509f6e4f70c565af0bc310a3b3e3068a0780`. The main merge is separate
from the native patch; PR9 permission and no-store code is unchanged.

## Native storage operations

- Open the one resolved Herdr state locator using Go `os.Root`. Validate its
  opened handle, local fixed drive, NTFS filesystem, final canonical path,
  current-user ownership and DACL. Null/empty DACLs and allowed ACEs outside the
  current user, SYSTEM and Administrators fail closed. Unknown ACE kinds fail.
  No existing ACL is changed. A private security descriptor is supplied only
  when creating new queue-owned objects, including isolated fixture roots.
- Use `NtCreateFile` relative to an open root with a single validated component
  and no reparse traversal. Go's Windows `os.Root` uses the same primitive.
  Logical delivery names hash to lowercase filenames, avoiding DOS devices,
  case collisions and ADS without a new path configuration or registry.
- Keep writable directory handles without delete sharing for each finite
  invocation. Check `GetFileInformationByHandleEx(FileIdInfo)` volume and 128-bit
  file IDs, regular file type, single link and no reparse points. The admitted
  seal still binds those identities and Merkle root.
- Reopen the producer's already-authorized source handle with `ReOpenFile`,
  withholding write/delete sharing. A producer with a live writer cannot admit.
  Snapshot/tree reads likewise withhold write/delete sharing. `LockFileEx`
  provides nonblocking cross-process job/worker/admission exclusion; it is not
  treated as protection against writable mapped views. Sharing restrictions
  provide the additional protection documented by CreateFile.
- Call `FlushFileBuffers` on a verified writable NTFS directory handle. Fail on
  access/unsupported/error returns; never silently succeed, flush a whole volume,
  or ask for admin rights. File data/metadata flushes precede progress and journal
  acknowledgements. Retirement uses handle-based disposition with the readonly
  flag override on queue-owned files, not a path-based ACL/attribute repair.
- Free-space checks use the verified handle's location and preserve the existing
  reservation margin. Progress, snapshots and downloads stay under the same
  private queue; NAS originals still belong to the existing storage provider.

## Official basis and limits

[MS-FSA 2.1.5.7](https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-fsa/0de7dc40-9627-437e-a4df-c4696cdc3d02)
specifies directory metadata flushing. Its
[implementation note 80](https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-fsa/4e3695bd-7574-4f24-a223-b4679c065b63)
limits this behavior to NTFS; other filesystems can return success without doing
that work. Therefore this adapter rejects non-NTFS and remote storage even if a
flush might return success. Win32
[FlushFileBuffers](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-flushfilebuffers)
requires writable access; a whole-volume flush requires admin privileges and is
not an alternative used here. The native fixture must execute both directory
open and flush; cross-compilation cannot establish they succeed on the target.

[CreateFileW](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-createfilew)
documents sharing, reparse flags and write-through limitations.
[LockFileEx](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-lockfileex)
notes mapped-view bypass and potentially delayed lock release after process
termination. The fixture retries release checks for a bounded one second.
[DACL rules](https://learn.microsoft.com/en-us/windows/win32/secauthz/null-dacls-and-empty-dacls)
explain why a null ACL is rejected, not mistaken for private storage.

The supported model is finite invocation/process crash recovery plus successful
OS-reported file/NTFS namespace flush ordering. This is not an absolute guarantee
against physical power loss, dishonest device caches, hardware corruption or
an administrator/current-user attacker. Merkle proofs and hashes detect changed
bytes; they do not create an authorization boundary against the owning account.
Real PC filesystem/filter-driver/policy behavior remains an acceptance item.
Unsupported policy or filesystem errors must be reported, not bypassed.

## Finite Herdr package

The manifest follows the pinned official
[Herdr plugin mechanism](https://github.com/herdrdev/herdr/blob/2ecf19a92fc3a7fff75b6ce3655ef04f4625ec01/docs/next/website/src/content/docs/plugins.mdx).
It exposes local storage validation and an explicitly isolated fixture command;
no daemon, startup worker, polling loop, listener or task-state service is added.
`transferqueue.exe` performs local probe/inspect/cancel/retire. Enqueue/advance and
outbox ports remain `InvokeHerdr`; native tests inject synthetic HTTPS auth and
atomic association fixtures. The executable does not invent a production token
provider or claim an installed registration service exists.

The binary bundle includes exact source SHA, per-file hashes, native test exe,
manifest and a PowerShell runner. Checksum files alone do not authenticate a
package. The runner neither builds source nor changes execution policy/ACLs or
installs the plugin. Native CI uses disposable Windows NTFS fixtures; it is not
real PC/NAS, Herdr UI, production credentials, or physical power-cycle testing.

Remaining integration conditions: reviewed plugin installation in the real
Herdr host; existing private writable NTFS state directory; a specifically
approved scoped Memoh credential provider; authenticated HTTPS reachable from
PC with normal certificate validation; task/storage Commit/Resolve binding that
reauthorizes and atomically fences cancel versus late association. Operation ID
is correlation, not authority. Production IPv6 HTTP remains unencrypted; neither
Agenvo OAuth nor a broad Bot token is substituted by this package.
