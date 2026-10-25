# Harmony 1.0.7 unsigned HAP from 775b494d / shared 68be060

This is a new build of the updated phone source, not the retained f248 package.

- Phone source: `775b494db1b888f63140afba5030a9177eb2400f`.
- Shared Web/SDK: `68be06092a1ea85d961115aac404082982bdecf4`.
- File: `memoh-harmony-1.0.7-1000007-775b494d-api26-unsigned.hap`.
- Size: **37,019,091 bytes**.
- SHA-256: `aa509e7d561ee7a9e07076b0d735b8f71b66dfa4ccf3d868230dbf91d3050d6c`.
- Bundle: `cloud.begonia.workassistant`; version **1.0.7 / 1000007**, above the
  reported installed 1.0.6 / 1000006. Module/pack identity and all six permissions
  match the previous 1.0.7 package. No credentials or login storage were reset.

## Build and contents

The original source-generation, Web build, resource packaging, LibRaw,
materials and official native entry were reused. Existing CLT and API26
26.0.0.105 were retained; no SDK download, dependency-version change, alternate
builder or official Agent change occurred. Web frozen dependency checks,
patched inputs, typecheck, production build, native compilation and native
qualification all exited zero. All 244 LibRaw source files were reverified.
The 86 existing ArkTS warnings have the same locations as the f248 build.
Signing is deliberately absent; the official tool reports no signingConfig.
[Build summary](build-summary.json), [native log tail](native-build-tail.txt).

The HAP contains 797 exact Web resource hashes and a 104-input native snapshot.
Its source fingerprint is
`3dbecab7bfabc81767f1119718c2ae9b5b37f19b18fe3dc36a885f668ea98904`.
Packaged source metadata matches the actual inputs, including the pinned source
lock. ZIP CRC, duplicate names, ArkTS ABC, both native ELF architectures and
module/pack identities were checked. [Package verification](hap-verification.json).

Reviewed empty endpoint defaults and unchanged runtime configuration keep
infrastructure addresses out of the package. No signing files, embedded private
configuration, high-confidence secret patterns or new URL hosts were found.
Third-party notices and original licenses remain included. Web source maps are
absent. The retained official ArkTS debug map has 19 relative-source entries,
no embedded source bodies and no absolute workspace paths; it was not silently
stripped. [Content review](content-review.json).

## Executed UI checks

[Source alignment](../harmony-unified-68be060/README.md) includes 140 passing
unit/contract tests, targeted lint, complete typecheck and the real phone Vue
application at `http://127.0.0.1:4181` with synthetic isolated inputs.
After native compilation, the Web files were extracted directly from this HAP
and served from an ephemeral loopback listener. The 390x844 Chromium flow passed
again: distinct root run and child task, separate pending delivery, authorized
expert history navigation, reload retaining login, hash-verified expert-owned
attachments, image preview/back and the existing native save command sequence.
There were zero observation/continuation writes and zero page errors.
[Compiled-resource result](compiled-ui-result.json).

Inspected actual compiled-UI screenshots:
[root RunID/state](expert-root-run-390.png), [child task](child-run-distinct-390.png),
[attachment preview](attachment-preview-390.png), [expert history](expert-history-390.png).
HTTP, runtime snapshots and the OS bridge were controlled fixtures; no real
model run or production task was started. These are automated UI checks, not
human QA, ArkWeb/device runtime or native file-picker acceptance.

## Retention and remaining delivery

The new named original and older f248/f640 packages remain separate. The new
file was saved privately to Library and read back through the supported cloud
flow. Readback size, SHA-256, ZIP CRC and Library identity/metadata match the
original. [Cloud retention verification](cloud-retention-check.json).
Transfer receipts and Library identifiers remain in the private cloud evidence;
no temporary URLs, credentials, SDK archive or HAP binary are committed here.

This evidence commit changes documentation only; the binary remains bound to
source `775b494db1b888f63140afba5030a9177eb2400f`. No public binary upload, release,
tag, Windows/WSL transfer, signing, device install or production deployment was
performed. Public artifact permission and the supported signing handoff remain
pending at the coordinating task. Phone upgrades, real NAS file transfer and
human acceptance are still unverified. The historical 19 later source changes
remain unmapped; the unavailable original 5a7 source identity is not claimed.
