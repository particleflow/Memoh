# Historical Harmony 1.0.9 unsigned specimen

Source: `c9c00580052d09d5dce273e958ec384bef2ebed3`.
Shared Web/SDK: `8a2422935916f56b5bacfac2f991824d1cb9bd9f`.
Bundle: `cloud.begonia.workassistant`; version: **1.0.9 / 1000009**.
File: `memoh-harmony-1.0.9-1000009-c9c005800-api26-unsigned.hap`.
Size: **37,053,849 bytes**. SHA-256:
`ebe3b6168ec013d1e78a2a523a155c07e2122000c98e99316ff88997ca948ae4`.

This unsigned specimen is superseded as a delivery candidate; it is not a
current installation or rollback instruction. Use the
[1.0.19 package/source handoff](../../../docs/testing/harmony-unified-1.0.19/README.md)
for the current qualification. Matching app identity does not prove safe downgrade.

[build-identity.json](build-identity.json) and the
[exact qualification report](../../../docs/testing/harmony-unified-8a24229/README.md)
record the shared-source alignment and injection-slot check on this artifact.
The injector changed only its reserved resource and preserved ZIP offsets/CRCs
in a synthetic test. Those records do not establish current Windows signing,
phone upgrade or live NAS acceptance.

No active builder or required current regression consumer of this binary has
been established by the cleanup scan. The original release owner must decide
whether reproducing that exact old qualification still requires the binary;
otherwise remove its current-tree copy and repair its metadata/references
coherently, preserving Git history. This is a pending retirement item, not a
blanket justification to keep obsolete build products.

The former delivery recipe is retained in this file's Git history. No private
configuration, configured/signed derivative, binding data or signing material
is introduced or published by this audit.
