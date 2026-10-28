# Garnet Assistant Harmony source

The current source tree declares **1.0.25 / 1000025**. Its compatible backend
is `565c0b4b3386d87d057234efe6828479d35fcee0`; the shared composer UI is
`2422db2011231d5a4e538aa0fd7b573d133cb350`, with the original Harmony adaptations.
The [1.0.25 build record](../docs/testing/harmony-unified-1.0.25/README.md)
records the official API26 compile and unsigned artifact identity. It does not
establish signing, installation, phone/NAS execution or human acceptance.
The old 1.0.18 package is a separate preserved field baseline.

A later [source-only directory-read guard](../docs/testing/harmony-files-directory-guard/README.md) restores original-Bot/activity checks around reads and retries. The existing1.0.25 build record and package identity remain unchanged; that package does not include this follow-up.

Contact navigation, original history deep links and account-scoped Bot creation recovery are retained. The current build adds F04 ordinary Files multi-selection while retaining scoped inaccessible-attachment presentation; the old 1.0.24 package remains separately preserved.

The [source manifest](SOURCE-PROVENANCE.json) records **172 reviewed entries**,
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
The obsolete standalone observation pipeline has been removed; its current replacement is the guarded complete-archive producer.

The source-only manifest/syntax check is:

```sh
python3 mobile/check_source_subset.py
```

Its `fullClientBuild: false` describes that check; it neither undoes the recorded
1.0.19 compilation nor substitutes for a fresh runtime test. This documentation
cleanup does not rebuild or change the existing HAP. Its recorded 167-entry
build checkpoint remains separate from this 162-entry cleaned source inventory.

Keep the native shell, attachment/voice adapters, exact scoped authorization,
source hashes and [license notices](PROVENANCE-AND-LICENSES.md) intact. Generated
Web resources, SDK/CLT binaries, dependency caches, signing material and private
configuration do not belong in the publishable source set. The existing Windows
owner supplies approved private configuration and signing; configured output
must not return to the public repository. No automatic route, identity or token
migration is implied by the new version or by this source inventory.

The pending [model catalog and unavailable-label source alignment](../docs/testing/harmony-model-catalog-7af/README.md) is pinned to `7af02cb35f85582713213cb98c382d36df744ea3`. It is not included in the qualified 1.0.20 artifact and awaits the final unified combination.

Pending source-only [Bot creation recovery and account-scoped storage alignment](../docs/testing/harmony-bot-create-recovery-434d/README.md) consumes stable `434d82a46f48e4b2047d067282f0af434cc4f0bd`. The qualified 1.0.20 artifact is unchanged; the next package waits for the final unified Web/SDK combination.

The 1.0.21 generic unsigned handoff locks the final feffaf Web/SDK combination, including the recorded model and creation alignments. Earlier pending-source paragraphs describe their original checkpoints; 1.0.20 remains a separate preserved artifact.

The [219/schema179 compatibility review](../docs/testing/harmony-219-compatibility/README.md) records that the qualified 1.0.21 retains existing chat/media compatibility but omits the new `inputs`-bearing explicit native task resume. The source-only correction is not relabeled as the existing signed package.

The1.0.22 unsigned handoff delivers the documented original-inputs resume fix against219; earlier compatibility statements describe1.0.21, which is preserved unchanged. Device/signing acceptance remains with the original onsite writer.

The1.0.23 generic handoff includes the fixed shared457 cross-Bot ownership/navigation race correction. Earlier version notes remain historical; no existing signed package or history is relabeled.
