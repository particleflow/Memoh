# Verified LibRaw build input

Current checkpoint: [official API26 native compilation and HAP integrity checks passed](harmony-native-api26.md). The new artifact is unsigned; device/runtime acceptance remains unexecuted. Earlier blocked-stage records below are historical.

The generated-input order issue recorded below is subsequently resolved by
[the first-time metadata correction](harmony-materials-bootstrap.md). The LibRaw
verification and SDK acquisition requirements remain valid.

The existing `prepare_raw_codec.py` restored the dependency at source checkpoint
`59126ace1e408baff5fb790aa41732c6414f2617`. It fetched only the locked
[official LibRaw 0.22.2 source](https://www.libraw.org/data/LibRaw-0.22.2.tar.gz)
using normal certificate validation. No source, business logic, RAW feature,
Agent core, lock file or build recipe was changed.

The archive is 1,682,962 bytes, with SHA256
`de86b035655accff8d4010f1a221fdf50d353cb7b1422ba26f14a0db92612cfa`.
All 244 paths and file hashes exactly match `raw-codec.lock.json`, including
`LICENSE.CDDL` and `LICENSE.LGPL`. Unpacked files occupy 7,763,312 bytes.
The source is in `mobile/client/mobile/harmony/.build/raw-codec/LibRaw-0.22.2`;
the original archive and another verified local backup are preserved. No global
install or vendored dependency upload was performed. The existing lock and
preparation command reproduce this input.

## Minimal preflight results

- The restored build entry's `verify_raw_dependency()` passed.
- `materials.inputs()` enumerated 94 current native build inputs successfully.
- Native rawfile validation stopped at missing
  `mobile/harmony/app/entry/src/main/resources/rawfile/source-build.json`.
  `raw-codec-license.txt` in the same directory and
  `mobile/harmony/materials/source-build.json` are also absent. These are
  generated packaging inputs, not missing LibRaw archive files. The restored
  `materials.py` currently verifies rawfiles before writing its first source
  metadata; this fresh-checkout bootstrap remains to be repaired. No historical
  manifest or generated license assembly was fabricated during this dependency
  retrieval and read-only preflight.
- Local toolchain preflight stopped before installation or compilation with
  `Official Linux CLT/SDK not installed: missing SDK package metadata`.

Paths in preflight records are relative to `mobile/client`. See the
[verification receipt](testing/harmony-libraw-input/verification.json) and
[preparation output](testing/harmony-libraw-input/materialize.log.txt).
The successful Web build and tests from `59126ac` were not repeated.
No compiler, HAP generation, phone or NAS acceptance ran.

## Remaining official toolchain input

The required historical artifact is `commandline-tools-linux-x64-26.0.0.851.zip`,
2,348,063,309 bytes, SHA256
`ab604bd92721d5cbcafd154e6461d46b9f1b105e7b89eab04a9d046681198082`,
containing SDK API26 / 26.0.0.105. Its historical toolchain directory
is not a readable backup in this environment; no Library copy is known.

The supported acquisition entry is
[Huawei developer downloads](https://developer.huawei.com/consumer/cn/download/).
The previous `net::ERR_CERT_AUTHORITY_INVALID` remains unresolved. No retry,
alternate browser/tool, guessed URL, token-bearing download link or certificate
bypass was attempted. This environment needs that exact official Linux package
from a valid supported download, followed by size/hash and SDK metadata checks;
restored client source does not need to be transferred again.
