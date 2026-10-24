# Harmony task-observation unsigned build

Source `7ceca0e3eecbc83f0f8edd11be73e711e377d125` has now been built into a
new API26 unsigned HAP. This includes the shared downstream observation recovery
fix described in [the source checkpoint](harmony-observation-recovery.md).
The older 1.0.7 HAP is retained separately and is not relabeled as this build.

| Field | Verified value |
| --- | --- |
| File | `memoh-harmony-1.0.7-1000007-7ceca0e-api26-unsigned.hap` |
| Bytes | 36,901,197 |
| SHA256 | `76f9832e7e5879c2d581d86ead8c2655bd6a806e4d76363fa2277ea0b94869f7` |
| Source commit | `7ceca0e3eecbc83f0f8edd11be73e711e377d125` |
| Bundle | `cloud.begonia.workassistant` |
| Version | `1.0.7 / 1000007` |
| SDK / build | API26, 26.0.0.105, debug, unsigned |
| Native source snapshot | `9f36c18c2da3f9e511e0c93f1cb7ec6e576da27486f280e66947ed88d1b49e6c` |

The existing version code already exceeds the reported installed 1.0.6 /
1000006, so no version or application-source change was needed. Bundle,
permissions, vendor, module and ability metadata match the earlier 1.0.7 HAP
exactly. Login storage, the stable WebView origin and service configuration
code are unchanged. This is version preparation, not proof of a signed upgrade.

## Executed build and integrity checks

The current cloud workspace reused its installed pnpm, dependency store,
official CLT/SDK and verified LibRaw source. No SDK or dependency download was
needed. The existing Harmony Web builder ran with offline dependency resolution,
passed its frozen-lock and patch checks, full Vue typecheck and production build,
and produced 786 resources. Both downstream modules exactly match their pinned
hashes from candidate `0a324013f9fe1beec77ac948d0af8f210194ba52`; the context
archive matches the previously tested source context.

The existing packager and materials generator regenerated and verified the
resource manifest, license bundle and both source metadata copies. The official
API26 entry then returned compile and qualification exit codes zero. This was
an incremental official build: ArkTS/resources and HAP packaging ran, while
unchanged native library steps reused existing output. Both native ELF libraries
remain byte-identical and passed architecture checks. The build log's `SignHap`
task reports no signing configuration; it did not produce a signed application.

All 95 source inputs, every packaged rawfile, compiled ABC, unchanged SDK
metadata and HAP ZIP CRCs passed verification. Compared with the previous HAP,
the only changed existing entries are the Web index, source-build metadata,
compiled ABC and source map; 117 content-hashed Web assets were replaced.
Module and pack metadata are identical. Compiler warnings are retained rather
than suppressed.

The new HAP was saved as a separate private Library file. An official Library
readback into this same cloud environment passed exact size, SHA256, ZIP CRC and
Library identity-metadata checks. The original HAP, new HAP, readback copy,
previous generated inputs, raw logs, original source ZIPs and SDK are retained.
No binary, private Library identifier, service address or credential is
published to GitHub.

Evidence: [package and readback receipt](testing/harmony-observation-hap/verification.json),
[official build report](testing/harmony-observation-hap/build-report.json),
[compiler log](testing/harmony-observation-hap/api26-build.log.txt),
[Web build summary and original-log hashes](testing/harmony-observation-hap/web-build-summary.json),
and [compiled Web source hashes](testing/harmony-observation-hap/compiled-web-inputs.json).
Public logs normalize workspace prefixes, terminal formatting and trailing
whitespace; unmodified logs remain in private evidence. The existing focused
tests and browser observations are linked from the source checkpoint and were
not rerun as unrelated packaging checks.

## Remaining delivery and runtime boundaries

The previously reported Windows Library helper failure (`os.setxattr` is
unavailable) still blocks the signing-side receipt workflow. It was not retried
or bypassed here. Cloud Library readback does not prove Windows delivery or
browser download compatibility. No signing key, certificate or profile was
transferred, replaced or reset.

This package is unsigned. Certificate/profile continuity, signed installation
over 1.0.6, login retention on that physical upgrade, real server tasks,
permissions/TLS, phone-to-NAS transfers and human acceptance remain unverified.
No Windows/NAS development, production change or official Agent modification
was performed. Earlier source/browser verification is not native runtime QA.
