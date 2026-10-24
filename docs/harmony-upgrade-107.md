# Harmony 1.0.7 upgrade preparation

The first recovered native build inherited `1.0.2 / 1000002`, below the
previous installable `1.0.6 / 1000006` reported by the parent task. Source
commit `8f8dc99761a467d5dafa9dd6e46ab1438d406ab5` changes only the two native
application version fields to `1.0.7 / 1000007`, retaining the existing
version-number pattern. The provenance entry records the new file hash.

## Actual unsigned package

| Field | Verified value |
| --- | --- |
| File | `memoh-harmony-1.0.7-1000007-40f0a69-api26-unsigned.hap` |
| Bytes | 36,900,790 |
| SHA256 | `4912fd74241a0e8109935a4e26a6b93be92fb8e600fc2179f5e7cd3650fa9af9` |
| Bundle | `cloud.begonia.workassistant` |
| Version | `1.0.7 / 1000007` in both `module.json` and `pack.info` |
| Vendor / module / ability | `Begonia` / `entry` / `EntryAbility` |
| Device type | `phone` |
| SDK / build | API26, 26.0.0.105, debug, unsigned |
| Compiled App ID | Absent in this unsigned artifact; signing identity is not verified |

The HAP still requests `INTERNET`, `GET_NETWORK_INFO`, `MICROPHONE`,
`DETECT_GESTURE`, `FILE_ACCESS_PERSIST` and `READ_IMAGEVIDEO`, all under
`ohos.permission`. Permission reasons and usage scenes are unchanged, including
`READ_IMAGEVIDEO` for `EntryAbility` with `when: always`. No permission was
removed or broadened to accommodate signing.

## Comparison with reported signing requirements

The parent task reports the previous installable application as
`cloud.begonia.workassistant`, `1.0.6 / 1000006`, App ID
`6917618151422994484`; its original profile includes `READ_IMAGEVIDEO`,
authorizes one device, and the certificate is valid until 2027-10-03. These
are supplied non-secret facts, not a new inspection of that profile in this
cloud workspace. No profile, private key, device identifier or credential was
transferred or changed here.

The new version code is greater and the bundle name matches. The requested
`READ_IMAGEVIDEO` permission remains declared. The original profile, App ID,
certificate continuity and original authorized device still need validation
when signing. The complete old permission set and installed platform details
were not supplied, so full signed-package compatibility is not established.

## Build and persistence evidence

The official `build_api26.py` entry completed with compile and qualification
exit codes zero. Prior native build outputs and caches were moved to private
evidence before the build, forcing ArkTS and both native architectures to
compile again. The verified Web resources, LibRaw source and official toolchain
were reused. The prior HAP, compiler logs and intermediates remain preserved.
No Web rebuild or unrelated feature test was repeated.

All 95 source inputs passed integrity checks; only `AppScope/app.json5`
differs from the previous successful recovery build. All HAP ZIP CRCs passed.
Exactly three archive entries changed: `module.json`, `pack.info` and the
generated `resources/rawfile/source-build.json`. Compiled ArkTS, both native
libraries and all 786 packaged Web resources are byte-identical to the
previous recovery HAP. See the [comparison receipt](testing/harmony-upgrade-107/verification.json),
[build report](testing/harmony-upgrade-107/build-report.json) and
[compiler log](testing/harmony-upgrade-107/api26-build.log.txt).

The stable WebView origin `begonia-client://app`, start document, storage keys
and login persistence implementation are unchanged by this version update.
Existing service-configuration behavior remains: an explicit endpoint change
invalidates authentication, and an unbound legacy token is not automatically
migrated to a newly entered endpoint. No account or credential was recreated.

The old signed 1.0.6 binary is not available here for a byte-level comparison.
Actual signed overwrite installation, retention of that installation's login,
device permissions/TLS and phone-to-NAS-to-phone transfers remain untested.
No runtime UI/screenshots or human acceptance are claimed for this version
update. Windows Library materialization remains a separate reported helper
compatibility blocker; this task does not patch or bypass that helper.
