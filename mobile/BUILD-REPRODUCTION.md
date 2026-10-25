# Reproduction inputs and verified build sequence

The current source-only pin is deployed shared commit
`150219908c316d75272b77c65148436fac3b7074`, including parent-detail reconnect
recovery and the upstream login response-ownership fix. Equivalent phone login
patches are removed; native adapters are retained.
[Alignment checks](../docs/testing/harmony-unified-1502199/README.md).
This update has not been rebuilt into a HAP; the verified f248 package below
remains bound to 5e55 and is preserved unchanged.

The current verified unsigned HAP is built from
`f2488597aba37ef8c3557cc3f156b78d870c82c9`, pinned to shared Web/SDK commit
`5e55c45c85659f04b339b4df42aeff72966542a8`. The existing source sequence and
retained official API26 SDK compiled and qualified the updated client with exit
codes zero. It contains 797 Web resources and the 104-file native source
snapshot. [Current package identity, checks and delivery boundary](../docs/testing/harmony-f248-hap/README.md).

This package includes the reviewed notices, shared native task and Images
controls, saved Provider state, original/scoped attachment adapters and phone
late-401 login guard. The Provider cache and mount-refresh fixes are now upstream,
so both duplicate phone patches are removed. No later shared candidate is claimed.
[Full production-source comparison](../docs/testing/harmony-source-audit/production-5e55.md).

The prior unified source/HAP checkpoint is
`f6403deaded6e76c0788aa66cffcaac5fef0badd`, pinned to shared source
`91af53f43566cb827d1fc58d18102e8ad29383bc`. Its completed unsigned build and
resource checks are in [the task-delivery evidence](../docs/testing/harmony-task-delivery/README.md).
The subsequent notice supplement is included in the new f2488597 package under
`client-web/licenses/`. It does not change the preserved older HAP. See
[notice sources and remaining UI licensing evidence](../docs/testing/harmony-release-notices/README.md).

The earlier [observation-fix HAP](../docs/harmony-observation-hap.md) was built
from `7ceca0e3eecbc83f0f8edd11be73e711e377d125` using the same sequence, existing
SDK and dependency cache. Web resources were rebuilt to include the source fix;
the official native build and all integrity checks passed. Its version remains
`1.0.7 / 1000007`, above the reported installed 1.0.6. No signing was performed.

The sequence below was exercised against source checkpoint
`ef3678a86584bf197719d2cd1bcde881fec43d6d`. Official native compilation and all HAP
integrity checks passed; the resulting artifact is unsigned. See
[the build report and output hashes](../docs/harmony-native-api26.md).

The same native sequence was repeated after the version-only update at
`8f8dc99761a467d5dafa9dd6e46ab1438d406ab5`, producing `1.0.7 / 1000007` with
compile and integrity exit codes zero. Existing verified Web/dependency inputs
were reused and native intermediates were preserved separately before a fresh
compile. That [earlier unsigned output](../docs/harmony-upgrade-107.md) is retained; the earlier 1.0.2 HAP is not an upgrade candidate for
the reported installed 1.0.6.

## Inputs

- Python 3.12 and pnpm 10.27.0. The verified Web run used Node 24.19.0;
  native compilation used the official CLT's bundled Linux Node.
- `client/config/memoh_web.lock.json` pins complete public Memoh source at
  `150219908c316d75272b77c65148436fac3b7074` and UI at
  `f69402a55dd87a59c10722fe11507ad8fd7048f5`, including component archive hashes.
  The deterministic combined archive SHA256 is
  `fa932c7c9acf637e54149aacda9b5aeb1af13d717dd55ae58e0b328f7fff1275`.
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
