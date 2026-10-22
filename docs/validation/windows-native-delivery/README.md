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
physical disk throughput. The optimized implementation and comparison follow
the design before any claim of completion.

No Windows execution, production ACL/security changes, identity provisioning,
plugin installation, Memoh runtime installation, real PC/NAS connection or
deployment has occurred. Dev-stack/UI/screenshot requirements remain incomplete.
