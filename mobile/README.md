# Garnet Assistant Harmony source

The current source tree declares **1.0.19 / 1000019**. Its compatible backend
is `69ef2fac2ceeaf68458f0f68bc30d0e8a31872ee`; the shared composer UI is
`2422db2011231d5a4e538aa0fd7b573d133cb350`, with the original Harmony adaptations.
The [1.0.19 build record](../docs/testing/harmony-unified-1.0.19/README.md)
records the official API26 compile and unsigned artifact identity. It does not
establish signing, installation, phone/NAS execution or human acceptance.
The old 1.0.18 package is a separate preserved field baseline.

The [source manifest](SOURCE-PROVENANCE.json) records **167 reviewed entries**,
zero excluded source paths and 19 unidentified historical later edits. Those 19
are an unresolved provenance boundary, not missing current build inputs and not
permission to reconstruct the unavailable original Git snapshot. The original
restoration hashes remain necessary to audit the recovered source's origin.

The complete shared archive pin and every overlay live in
`client/config/memoh_web.lock.json`. The base archive is `9bb6fc3d`, not the final
compatible backend commit; the guarded builder composes the reviewed overlays.
Use [BUILD-REPRODUCTION.md](BUILD-REPRODUCTION.md) for the active sequence,
[REGRESSIONS.md](REGRESSIONS.md) for version-specific validation, and
[SHARED-OBSERVATION.md](SHARED-OBSERVATION.md) for the active observation entry.
Do not invoke the legacy standalone observation preparer as a current build step.

The source-only manifest/syntax check is:

```sh
python3 mobile/check_source_subset.py
```

Its `fullClientBuild: false` describes that check; it neither undoes the recorded
1.0.19 compilation nor substitutes for a fresh runtime test. This documentation
cleanup does not rebuild or change the existing HAP.

Keep the native shell, attachment/voice adapters, exact scoped authorization,
source hashes and [license notices](PROVENANCE-AND-LICENSES.md) intact. Generated
Web resources, SDK/CLT binaries, dependency caches, signing material and private
configuration do not belong in the publishable source set. The existing Windows
owner supplies approved private configuration and signing; configured output
must not return to the public repository. No automatic route, identity or token
migration is implied by the new version or by this source inventory.
