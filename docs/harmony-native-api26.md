# Official API26 native build checkpoint

The recovered Harmony client at `ef3678a86584bf197719d2cd1bcde881fec43d6d`,
using unified Web candidate `40f0a69fd4b7e34dfcf36ec8a44a9805403e7be8`, now
compiles with the official Linux SDK. The existing `build_api26.py` exited zero
and completed its post-build integrity qualification. No application-source
change or relaxed validation was needed for this compiler run.

## Verified inputs and build

The approved private handoff was consumed with the official Library helper.
The supplied official download succeeded with normal TLS validation, without
account cookies or a new authentication flow. The CLT archive is
`commandline-tools-linux-x64-26.0.0.851.zip`, 2,348,063,309 bytes, SHA256
`ab604bd92721d5cbcafd154e6461d46b9f1b105e7b89eab04a9d046681198082`.
The signed link and handoff contents are not included in this repository.

The inspected archive contains 114,666 entries and 7,244,704,449 expanded bytes.
Paths and symlink targets stay within its toolchain root; extraction completed
with executable Linux entries preserved. SDK metadata confirms API26 /
26.0.0.105. The original ZIP and extracted toolchain remain in the private
workspace, outside the repository. No SDK/CLT binaries are committed.

From `mobile/client`, the existing entry was run with the extracted toolchain:

```sh
python3 -B mobile/harmony/tools/build_api26.py --toolchain-root <toolchain-root>
```

`<toolchain-root>/commandline-tools/command-line-tools` is the extracted CLT.
The run reused the verified 786-resource Web package and locked LibRaw 0.22.2
source. It did not repeat Web builds or previous feature tests. ArkTS compilation,
C++ compilation for arm64-v8a and x86_64, packaging, exact HAP rawfile hashes,
compiled ABC presence, native ELF architectures, source snapshot integrity and
unchanged toolchain metadata all passed.

Compiler warnings remain in the log, including declared-permission/API warnings,
a resource-name conflict, Clang argument/macro warnings and an ohpm user-directory
initialization warning. They were not suppressed. The build reports no signing
configuration. See the [qualification report](testing/harmony-native-api26/build-report.json),
[compiler log](testing/harmony-native-api26/api26-build.log.txt) and
[input/output receipt](testing/harmony-native-api26/verification.json). Public
logs normalize only task paths, terminal sequences and trailing whitespace;
original logs and their hashes remain in private evidence.

## New unsigned artifact

- Name: `memoh-harmony-40f0a69-ef3678a-api26-unsigned.hap`
- Size: 36,900,790 bytes
- SHA256: `d5b8410e241d2c9e5eaab8ed3945f34474c3745a26ce96e6bf0edd85516032c2`
- Application: `cloud.begonia.workassistant`, version 1.0.2 / 1000002
- SDK: 26.0.0.105; debug build; unsigned

This is the newly compiled artifact, not the old A11 package. Its ZIP CRCs were
also checked. A private Library copy and a separate workspace deliverable copy
are retained; the binary is not published to GitHub. The only added ignore rule
covers official compiler `.cxx` intermediates. Source manifests and archived
backups remain preserved; the historical 19 later cloud edits are still
unverified and no identity with the unavailable 5a7 tree is claimed.

A signed installable release and physical-device qualification are still
outstanding. No signing certificate/profile was supplied or invented, and no
phone, NAS authentication, LAN/IPv6 selection or phone-to-NAS-to-phone transfer
was exercised. The previous browser preview and controlled source tests remain
separate evidence. Native compilation is now verified; native runtime and human
acceptance are not.
