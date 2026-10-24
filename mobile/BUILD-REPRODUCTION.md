# Reproduction inputs and verified build sequence

The sequence below was exercised against source checkpoint
`ef3678a86584bf197719d2cd1bcde881fec43d6d`. Official native compilation and all HAP
integrity checks passed; the resulting artifact is unsigned. See
[the build report and output hashes](../docs/harmony-native-api26.md).

## Inputs

- Python 3.12 and pnpm 10.27.0. The verified Web run used Node 24.19.0;
  native compilation used the official CLT's bundled Linux Node.
- `client/config/memoh_web.lock.json` pins complete public Memoh source at
  `40f0a69fd4b7e34dfcf36ec8a44a9805403e7be8` and UI at
  `f69402a55dd87a59c10722fe11507ad8fd7048f5`, including component archive hashes.
  The deterministic combined archive SHA256 is
  `75fe83655dfed0aecb43cb64d3ea9edec8fd206c46a5f0165ef35719678c62f2`.
- Official `commandline-tools-linux-x64-26.0.0.851.zip`, 2,348,063,309 bytes,
  SHA256 `ab604bd92721d5cbcafd154e6461d46b9f1b105e7b89eab04a9d046681198082`,
  containing SDK API26 / 26.0.0.105. Acquire through the official supported
  Huawei channel; temporary signed links and vendor binaries are not in Git.
- LibRaw 0.22.2 source, SHA256
  `de86b035655accff8d4010f1a221fdf50d353cb7b1422ba26f14a0db92612cfa`.
  `raw-codec.lock.json` pins its official URL and all 244 source-file hashes.

## Commands

From `mobile/client`, with the pinned pnpm executable available:

```sh
python3 mobile/prepare_memoh_source.py
python3 mobile/build_memoh_web.py --platform harmony --builder native --pnpm /absolute/path/to/pnpm
python3 mobile/package_memoh_web.py
python3 mobile/harmony/tools/prepare_raw_codec.py
python3 mobile/harmony/tools/materials.py
python3 -B mobile/harmony/tools/build_api26.py --toolchain-root /absolute/path/to/toolchains
```

The extracted official SDK must be at
`/absolute/path/to/toolchains/commandline-tools/command-line-tools`.
The Web builder validates frozen dependency inputs. The packager verifies
resource hashes and generates its Web manifest and shared URL validator while
preserving runtime service configuration. No private service address is needed
for a distributable build. `materials.py` generates the native license bundle
and both source metadata copies from verified inputs, then verifies the entire
snapshot before compilation.

The native entry checks the SDK identity, builds both native architectures,
checks compiled ArkTS ABC and ELF headers, compares every packaged rawfile,
and rejects source/dependency/toolchain drift. Its report is frozen under
`client/docs/evidence/development-runs/`; the latest report is also available
under `client/mobile/harmony/.build/build-report.json`. Generated files and
compiler caches are excluded from source publication. Reuse matching verified
inputs when continuing a build; do not repeat completed Web tests just to rerun
a native stage.

## Remaining qualification

The current build proves that these pinned inputs compile and pass packaging
integrity checks. It does not establish byte-identical HAP output across future
runs. A signed installable build requires the appropriate signing material and
provisioning profile. Phone runtime, permissions/TLS, NAS authentication,
phone-to-NAS-to-phone transfers and human acceptance have not been exercised.
The historical 19 later cloud changes remain unidentified; no unavailable 5a7
source identity or old A11 acceptance is claimed.
