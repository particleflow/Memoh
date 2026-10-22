# Native delivery follow-up evidence

Frozen parent: `517a73cebfc9968aee431616d3f4a8ae51596a86`.
Branch: `feat/windows-native-delivery-host`.
[Design and official API sources](../../design/windows-native-delivery-followup.md).

## Association review fixes

[Before](association-before.txt) reproduces the deadline/cancellation and lost
commit/cancel regressions on the frozen code. [After](association-after.txt)
passes with race, including real context deadlines/cancellation, durable
in-flight marker before callback, process exit inside commit, unknown recovery,
absent lookup followed by delayed commit/cancel fencing, revoked reconciliation,
bounded retry pause/resume and download reconciliation without republishing.
[Actual merged handler integration](association-handlers.txt) still passes.
Client lint 2.10.1 and formatting pass. All registrar behavior is an explicit
fixture; no production task binding or credentials are configured.

The host interface now supplies `AssociationBinding.Commit` and `.Resolve`.
Both are required before a commit. The latter must atomically fence a delivery
key if canceling, or observe a committed reference. `OutcomeAbsent` alone never
confirms cancellation. Automatic retry exhaustion pauses an unknown outcome;
`reconcile` explicitly starts another bounded budget without clearing intent.
Cancellation cannot withdraw an already committed result or a NAS original.

## Frozen performance baseline

[512 MiB repeated verification](baseline-512m.txt): 64 iterations, 505.198 ms
per verification, approximately 32.33 seconds of timed verification. This is
32 GiB read for a 512 MiB file with 64 upload chunks. Full-file buffer allocation
does not grow with file size, but I/O does grow quadratically with chunk count.
The cloud filesystem cache makes this a CPU/cached-I/O baseline, not NTFS or
physical disk throughput. The optimized implementation follows the design below.

## Bounded snapshot verification fix

Admission now computes the expected full-file SHA once while writing the private
snapshot and a domain-separated fixed-block Merkle tree. The journal binds its
root and stable file identities. Reopened handles check identity, size, private
permissions, links and the stored root; each upload reads and verifies only the
blocks it will send plus logarithmic proof metadata. The server's final full
SHA check is unchanged. Snapshot verification deadlines retain the upload's
resumable state rather than permanently blocking it.

Queue schema is now 2, in the same existing locator namespace. Schema 1 returns
`ErrVersion` and does not initialize a different directory or migrate silently.
There is no production deployment of the frozen prototype; an offline migration
tool is not included. The proof tree consumes quota and is retired with its
snapshot. No extra root, identity credential or general filesystem API exists.

[Race tests](integrity-tests.txt) pass 31 top-level tests and 53 subtests, covering
exact data-read counts across protected handle reopen, unaligned/odd-sized trees,
data/proof/root tamper, stale identity, schema refusal, snapshot cancellation and
previous protocol/queue/outbox regressions. [Real merged handlers](integrity-handlers.txt)
still pass. Native Windows remains explicitly disabled in this review-fix
checkpoint; its API binding and packaging will be a separate increment after
these P1 fixes. Client lint 2.10.1, vet, formatting and Agent boundary pass;
Windows amd64 test binary cross-compiles, but has not been executed.

[Repeated measurements](integrity-benchmark.txt), using the same cloud CPU and
cached local filesystem, with three single-iteration runs per size:

| Work | 512 MiB time | Scope |
| --- | --- | --- |
| Frozen full verify before 64 chunks | 32.33 s | 32 GiB data verification reads; excludes admission/network |
| Sealed block verification for all 64 chunks | 0.564–0.645 s | Reopens files and reuses caller chunk buffer; excludes HTTP, chunk allocation and production ACL/identity checks |
| One-time seal admission | 1.013–1.281 s | Writes snapshot/tree, computes full SHA and block hashes, syncs both files |

The instrumented 64 MiB test reads exactly 64 MiB of snapshot data and 12,288
proof bytes across eight reopened chunk handles, plus one 32-byte root check
per open. The equivalent 512 MiB geometry requires one file of data and 147,456
proof bytes plus root checks, rather than 32 GiB of full-file verification.
Admission scratch is about 1 MiB. Aligned reads verify in the caller's bounded
chunk buffer; an unaligned range can need at most one extra 1 MiB block buffer.
Total allocations per entire file are not peak RSS. These are integrity-work
measurements, not a Windows/NAS, NIC saturation or end-to-end speed claim.

No Windows execution, production ACL/security changes, identity provisioning,
plugin installation, Memoh runtime installation, real PC/NAS connection or
deployment has occurred. Dev-stack/UI/screenshot requirements remain incomplete.
