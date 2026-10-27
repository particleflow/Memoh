# Harmony build inputs and reproduction

The source tree declares **1.0.19 / 1000019**. The
[1.0.19 qualification record](../docs/testing/harmony-unified-1.0.19/README.md)
and [artifact identity](../docs/testing/harmony-unified-1.0.19/unsigned-verification.json)
record the existing unsigned build. This guide describes its current input
contract; it does not request a rebuild of an already qualified artifact.
Signing, installation and physical-device/production acceptance remain separate.
The existing package belongs to its recorded 167-entry source checkpoint. Removing
unused inputs and their lock metadata changes the future formal source identity;
no old receipt is rewritten to bind it to this 162-entry cleanup tree.

## Inputs

- `client/config/memoh_web.lock.json` pins the complete Memoh base archive at
  `9bb6fc3d84387012b4e2f5daffcc95d81ece5db4`, with UI revision
  `f69402a55dd87a59c10722fe11507ad8fd7048f5`. The deterministic combined archive
  SHA256 is `4f1d3403a3738b0d6080a200749b19ddabc404e6089704b630cdd10bd8f02757`.
- The base archive is composed with hash-checked workspace, activity,
  conversation, accepted-input recovery and composer/theme overlays. The final
  compatible backend source is `69ef2fac2ceeaf68458f0f68bc30d0e8a31872ee`; that
  value is not a replacement for the base archive pin. Repinning requires
  reviewing the generated baseline and removing any already-consumed overlays.
- `SOURCE-PROVENANCE.json` describes 162 reviewed source entries and records
  origins/current hashes. `scripts/check-harmony-source.py` expects exactly
  45 non-test and 2 original phone test adaptations. Read the current lock and
  guard rather than copying a patch count from an older checkpoint.
- Python 3.12, pnpm 10.27.0 and the pinned Web dependency graph are the recorded
  build inputs. The earlier verified Web run used Node 24.19.0; native
  compilation uses the official CLT's bundled Linux Node.
- Official `commandline-tools-linux-x64-26.0.0.851.zip`: 2,348,063,309 bytes,
  SHA256 `ab604bd92721d5cbcafd154e6461d46b9f1b105e7b89eab04a9d046681198082`,
  containing SDK API26 / 26.0.0.105. Acquire through the supported Huawei channel;
  vendor binaries and temporary signed download links are not source files.
- LibRaw 0.22.2 source SHA256
  `de86b035655accff8d4010f1a221fdf50d353cb7b1422ba26f14a0db92612cfa`.
  `client/mobile/harmony/raw-codec.lock.json` pins its URL and 244 source hashes.

## Commands

From `mobile/client`, with the pinned pnpm executable and already authorized
build inputs available:

```sh
python3 mobile/prepare_memoh_source.py
python3 mobile/build_memoh_web.py --platform harmony --builder native --pnpm /absolute/path/to/pnpm
python3 mobile/package_memoh_web.py
python3 mobile/harmony/tools/prepare_raw_codec.py
python3 mobile/harmony/tools/materials.py
python3 -B mobile/harmony/tools/build_api26.py --toolchain-root /absolute/path/to/toolchains
```

The official SDK belongs at
`/absolute/path/to/toolchains/commandline-tools/command-line-tools`.
The guarded `build_memoh_web.py` context is the current Web producer. The obsolete
standalone observation preparer and its exclusive host/adapter regressions have
been removed; no second preparation step is required.

The Web builder verifies frozen dependencies, typechecks and bundles the
composed source. Packaging verifies resource hashes and generates the shared
URL validator while preserving runtime configuration. `materials.py` prepares
the native notices and source identity from verified inputs. Do not patch SDK
or package metadata to conceal a failed toolchain probe or input check.

The native build checks SDK identity, both native architectures, ArkTS/ELF and
packaged resources against source/dependency receipts. Its run report is under
`client/docs/evidence/development-runs/`, with the latest local report at
`client/mobile/harmony/.build/build-report.json`. These are generated local
outputs, not promised repository downloads. Reuse matching verified inputs;
do not repeat the completed Web matrix solely to repeat a native stage.

## Historical evidence boundaries

- [Initial API26 qualification](../docs/harmony-native-api26.md) records the
  recovered compiler/materials boundary; its old source pin and counts are not
  current build instructions.
- [1.0.8 local-configuration slot qualification](../docs/testing/harmony-windows-injection-108/README.md)
  records the migration/injection boundary used by the existing Windows handoff.
- [1.0.9 shared-source qualification](../docs/testing/harmony-unified-8a24229/README.md)
  records that exact archive's package identity. Its former latest-candidate
  designation has been superseded by the 1.0.19 record above.

These records establish what those particular checks covered; they do not
certify older HAPs as safe rollback targets for a newer installation. The 1.0.7
binary has one concrete reader: `docs/testing/harmony-service-migration/build-input-test.py`
checks that the real old package without an injection slot is rejected. That
optional regression branch is its current identified use, not a field rollback
claim. The unused 1.0.8/1.0.9 binary copies have been removed from the current
tree; their exact byte/hash/blob identities and normal Git recovery commands
remain in the [1.0.8 record](../artifacts/harmony/1.0.8-ee00ff76a/README.md) and
[1.0.9 record](../artifacts/harmony/1.0.9-c9c005800/README.md). Those records audit
past qualification and do not offer the old payloads as current installation
or rollback targets. Current 1.0.18/1.0.19 delivery is unchanged. The 19
unidentified later historical edits remain unverified, independently of the
current build's successful qualification.
