# Reproduction inputs and verified build sequence

The current source-only alignment pins shared Web/SDK commit
`51b19cbeb76e9803eb8d4b614cfabb03ef18cc73`. It reuses the shared image-tool
reference display and native `images_api.edits` control, retaining the phone's
attachment, task and authentication adaptations. The Provider cache fix is now
byte-identical upstream, so its duplicate phone patch has been removed. A
browser-confirmed follow-up refreshes the existing providers query on mount so
a sanitized display snapshot cannot block ordinary saves. See
[the alignment checks](../docs/testing/harmony-unified-images/README.md).
This source has not been compiled into a new HAP; the previously compiled
package below keeps its original source/hash.

The latest unified source/HAP checkpoint is
`f6403deaded6e76c0788aa66cffcaac5fef0badd`, pinned to shared source
`91af53f43566cb827d1fc58d18102e8ad29383bc`. Its completed unsigned build and
resource checks are in [the task-delivery evidence](../docs/testing/harmony-task-delivery/README.md).
The subsequent notice-only preparation adds verbatim component notices to the
existing `client-web/licenses/` resources on the next build. It does not alter
the already built HAP or claim a new package has been compiled. See
[notice sources and remaining UI licensing evidence](../docs/testing/harmony-release-notices/README.md).

The latest [observation-fix HAP](../docs/harmony-observation-hap.md) was built
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
compile. Use [the current unsigned output](../docs/harmony-upgrade-107.md)
for signing preparation; the earlier 1.0.2 HAP is not an upgrade candidate for
the reported installed 1.0.6.

## Inputs

- Python 3.12 and pnpm 10.27.0. The verified Web run used Node 24.19.0;
  native compilation used the official CLT's bundled Linux Node.
- `client/config/memoh_web.lock.json` pins complete public Memoh source at
  `51b19cbeb76e9803eb8d4b614cfabb03ef18cc73` and UI at
  `f69402a55dd87a59c10722fe11507ad8fd7048f5`, including component archive hashes.
  The deterministic combined archive SHA256 is
  `860c9535bcb0058bc659bd97dc35ac31116a49cbd2b67b639f027e681bc3ef64`.
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
