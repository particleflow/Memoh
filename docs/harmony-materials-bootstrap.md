# First-time native metadata generation

Current checkpoint: [official API26 native compilation and HAP integrity checks passed](harmony-native-api26.md). The new artifact is unsigned; device/runtime acceptance remains unexecuted. Earlier blocked-stage records below are historical.

The recovered `materials.py` checked native rawfiles before it generated
`source-build.json`, and did not generate `raw-codec-license.txt`. A checkout
with the verified Web package and LibRaw source could not create its first
source snapshot. This correction only changes that bootstrap sequence and
verification of its three outputs. Application code and Agent core are unchanged.

The existing generator now:

1. Verifies the current Web package and all locked LibRaw source hashes.
2. Assembles `raw-codec-license.txt` from the verified `COPYRIGHT`, `LICENSE.CDDL`
   and `LICENSE.LGPL` bytes, retaining the source URL/version/archive identity.
3. Enumerates and hashes actual source inputs, including the generated license,
   then writes the two `source-build.json` copies.
4. Verifies the complete source snapshot, both metadata copies, packaged Web
   resources and exact native rawfile set before exporting evidence or succeeding.

Normal build verification still performs every check. Both metadata copies must
match the deterministic encoding of the actual snapshot. License bytes must
match a fresh assembly from the locked dependency; their own current hash is
not accepted as proof. Generated outputs have exact ignore entries and are not
handwritten or added as substitute source inputs.

After the existing Web package and LibRaw preparation, run from `mobile/client`:

```sh
python3 mobile/harmony/tools/materials.py
python3 mobile/harmony/tools/build_api26.py
```

The second command still requires the official SDK. The first does not. No Web
build or previously completed feature tests were repeated for this correction.

## Executed regression and preflight

`python3 tests/mobile/test_harmony_materials_bootstrap.py` passed nine integration
checks using disposable copies of the actual source, verified Web package and
locked LibRaw source. They start without all three generated files. No build
receipt, source manifest, compiler result or SDK response is fabricated.

Checks cover clean generation with actual file/base64/hash correspondence,
exact license bytes, deterministic regeneration after deleting outputs, recovery
from partial prior generation, both metadata copies, license and Web-resource
tampering, unowned rawfiles, and rebinding a changed native source file.
See the [test output](testing/harmony-materials-bootstrap/regression.log.txt) and
[verification receipt](testing/harmony-materials-bootstrap/verification.json).

The actual workspace generator subsequently succeeded with 95 source inputs.
Both 12,170-byte metadata copies have SHA256
`2d26578a0dc39898eb12167cd2b9cb88cc751944415a4f7952277f5bec62ee11`.
The 42,681-byte license bundle has SHA256
`18cca3c964b4df76e43b73b9cfa67ddec074581cd2b075203ee6414c4b43934f`.
The source identity is
`655585a7a296ce7b273d5315bd40cb9118ea6d59abefc547f3709963226b5754`.
These describe this new checkpoint, not the inaccessible historical 5a7 tree.
The actual generated files and frozen source evidence remain in the workspace
with a separate local backup.

The unmodified `build_api26.py` then passed its complete source preflight and
stopped in `toolchain_preflight`, before any compiler process, on the absent
`validation/harmony/.build/commandline-tools/command-line-tools/bin/hvigorw`.
`officialApi26Compile` remains false and the compiler exit code remains null.
The missing-input order defect is resolved; SDK acquisition remains blocked as
recorded in [the official toolchain requirements](harmony-libraw-input.md#remaining-official-toolchain-input).
No SDK network retry, certificate bypass, HAP generation, phone/NAS transfer or
human acceptance occurred.
