# Harmony client reviewed source subset

This Draft imports the reviewed source additions from frozen client
`1.0.11-dev.deliverybe686` (versionCode 1000011), source checkpoint
`a01a3ec207326987d13b857f5b11c97f3511c6d4`. It contains 42 per-file reviewed
entries and the existing standalone configuration regression. It is a partial
source import, not a complete independently buildable client or an application
release. The frozen full source and compiled package were not modified for this
import. No installation package belongs in this directory.

The Web baseline remains Memoh `6ddf61dcd8199758e4110398645c556a3d6d7375` and UI
`f69402a55dd87a59c10722fe11507ad8fd7048f5`. Only the explicit shared observation
and result-delivery capability is pinned to
`be6862497a58a01ef8c6f4168f02d5624cd44061`; this is not adoption of all upstream
Web, desktop, SDK or Agent changes. Public repository import base is
`851e8da4284c1f01e7e95f9547b16e7250bf553b`.

`SOURCE-PROVENANCE.json` lists exact digests, origins, preparation changes and the
72 excluded inherited paths. Native shell, Web overlay builder, original helpers
and assets remain held for attribution review. Missing sources are explicit
prerequisites, not generated replacements. Existing notices and unresolved
AGPLv3/ISC upstream metadata are documented in `PROVENANCE-AND-LICENSES.md`.

No SDK, cache, dependencies, generated Web, HAP, signing material, private service
address, real account data or private handoff records are included. API origin
and installation download origin are separate runtime settings. Service changes
invalidate the previous authentication scope. An unbound legacy token is never
sent to a newly entered service: older installations without a persisted endpoint
need one explicit configuration/login migration. A bound unchanged endpoint keeps
login while the server session remains valid; refresh renewal is not invented.

## Subset checks

With Python 3, Node 22 and TypeScript available, run from repository root:

```sh
python3 mobile/check_source_subset.py
node mobile/client/tests/unit/service-config.test.cjs
```

The latter resolves TypeScript normally or through `HARMONY_TEST_TYPESCRIPT`.
These offline checks do not require or start a server. Full browser/native
reproduction has missing and external inputs listed in `BUILD-REPRODUCTION.md`.
The source import remains a Draft: full deployed-runtime verification, published
screenshots, signed installation, real Harmony device upgrade and human QA remain
unverified. Existing synthetic browser/API26 compile evidence is described in
`REGRESSIONS.md`; it is not proof of those missing steps.
