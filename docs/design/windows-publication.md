# Native Windows publication candidate

This disabled binding implements `LocalPublication` inside the existing Herdr
component. It reuses the reviewed NTFS root/ACL validation, native relative
opens, file identity, no-write/no-delete sharing, LockFileEx and directory flush.
There is no service, path registry, credential, ACL repair or production setup.

`publications-v1` is a private, versioned child of the queue's already resolved
Herdr locator. It has its own lifecycle, a bound directory identity and the same
immutable byte-budget value as its owning queue. Up to 128 publication receipts
are retained. Published bytes count against its budget; queue Retire never
removes destination data or fences. There is no automatic eviction or retention
deletion in this first candidate. Reaching a bound fails explicitly. Destination
retention/consumption policy needs separate integration before production use.

The consumer opens a logical reference through `OpenPublished`; no source path,
arbitrary workspace destination or physical filename is accepted or returned.
Every action calls the injected, current host task/destination authorizer. A
logical operation ID, successful storage probe or local file is not authority.

## Protocol

The existing queue verifies a download and holds its private handle throughout
the publication callback. An unexported, invocation-limited source proof binds
that handle to the reference. Publish fsyncs those bytes and persists a prepared
receipt containing the NTFS volume/file identity before rename. It then moves
the same file into the private destination by a handle-relative, same-volume
rename with replacement disabled. No full-file copy or path fallback occurs.
Directory flush and a checksummed, fsynced terminal receipt precede acknowledgement.

The receipt format reuses the queue's bounded append-only journal framing and
torn-tail recovery. A single nonblocking native store lock serializes publication
and cancellation. Busy is explicit backpressure. Cancel fences a not-yet-moved
object; a verified destination with the prepared identity wins as published even
if the final receipt or HTTP response was lost. Missing both source and target,
replaced identity, links, corruption or an existing unrelated destination fail
closed; they never mean canceled or permission to overwrite.

Resolve/OpenPublished revalidate identity, private ownership and the full hash
using a bounded buffer and a cooperative context. They hold a no-write/no-delete
share handle while reading. Slow local verification can still exhaust a finite
invocation; it returns an error, not a fabricated terminal outcome. Readonly is
additional hardening; identity/hash validation does not rely on that attribute.
No physical power-loss guarantee is made for hardware that misreports flush.

## Windows API basis and acceptance

[FILE_RENAME_INFORMATION](https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/ntifs/ns-ntifs-_file_rename_information)
defines the relative directory handle and failure when a destination exists with
ReplaceIfExists false.
[NtSetInformationFile](https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/ntifs/nf-ntifs-ntsetinformationfile)
requires DELETE access for FileRenameInformation. Only the existing private
download `.part` handle receives that right; delete sharing remains disabled.

Acceptance requires actual Windows NTFS runs, including process termination on
both sides of rename, durable fence/publish races, reopens and adversarial paths.
Cross-compilation and Linux fixtures do not substitute for those runs. Production
PC identity, HTTPS, destination consumption/retention and real UI/human acceptance
remain separate prerequisites. Media Range behavior is unchanged.
