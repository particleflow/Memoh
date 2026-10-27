# Harmony unified 8a242293 candidate

The existing phone source now locks complete shared Web/SDK source to
`8a2422935916f56b5bacfac2f991824d1cb9bd9f`. The next generic package is
`1.0.9 / 1000009`, with the unchanged `cloud.begonia.workassistant` identity.
The signed/distributed 1.0.8 and its original generic artifact remain preserved.

There are no Web page differences from the already reviewed fa77 baseline.
The generated SDK includes the fixed native Herdr API and optional producer
locator; no existing phone consumer requires a new configuration component.
All 43 phone adaptations and the test-only attachment adapter still apply.
The initial alignment changed only the complete-source lock and app version.
Its compiled-page regression then reproduced an inherited login-only service
button remaining over the chat task action: Vue Router's programmatic navigation
does not dispatch the `hashchange` event used by that button. The minimal repair
uses the existing router's `afterEach` and current route to update its visibility;
the configuration regression now checks login and logout visibility. This is a
confirmed UI defect, not a native configuration problem. No new component,
configuration schema or route is added. Login storage, Bot/Agent observation,
owning-Bot attachment saves and Windows-only private injection are unchanged.
Public configuration remains empty.

## Source and compatibility evidence

- [Archive verification](source-archive-verification.json): all 6,278 upstream
  Git blobs match the downloaded public fixed source; the existing UI archive is
  reused. The combined archive follows the unchanged preparation recipe.
- [Source/patch checks](source-check.txt): 126 source entries / 135 files, all 43
  existing adaptation hashes retained, public API/download configuration empty.
- [Alignment and preservation](alignment-check.json): existing login/config and
  attachment inputs and the original 1.0.8 HAP match their recorded hashes.
- [Targeted checks](targeted-tests.txt): 7 fixed-SDK compatibility checks and 12
  phone attachment checks passed with the final SDK. The synthetic fixture from
  the PR84 review was copied into the disposable generated context and removed
  afterward; no test-only API or configuration enters the product.

Build the new source through the existing sequence in
[BUILD-REPRODUCTION.md](../../../mobile/BUILD-REPRODUCTION.md), using the retained
official API26 toolchain and verified LibRaw inputs. Build/package evidence will
be recorded separately against the exact source commit. A successful build is
not signing, phone overwrite-install, live Windows/NAS integration or human QA.
Only the generic empty-config HAP may enter the public distribution directory;
Windows injects its existing private config before signing the private derivative.

## Qualified candidate and handoff

Final source is `c9c00580052d09d5dce273e958ec384bef2ebed3`.
[Historical 1.0.9 artifact identity and Git recovery](../../../artifacts/harmony/1.0.9-c9c005800/README.md).
The official API26 build and integrity checks passed with the retained SDK:
[summary](build-summary.json), [package scan](package-verification.json),
[actual HAP synthetic injection](real-hap-injection-test.json).
All 801 Web resources, 104 native source inputs, compiled ABC and both native
ELF architectures are verified. The module/app metadata and permissions match
1.0.8 exactly except the intended version increment.

The first rejected candidate remains recorded in [first-candidate-review.json](first-candidate-review.json).
The VM label discrepancy was caused by sandbox `EPERM` on the official
`spawnSync` version probe. Formal execution approval restored the official
`24.0.0.0` query and generated `ark24.0.0.0`, without editing SDK files or HAP
metadata. The first failed UI trace and package are retained; neither was
distributed as the final candidate. The old debug source map is still present:
its keys/source paths match 1.0.8, it embeds no source bodies, and all its bytes
are included in the URL/credential scan. No new readable URL was introduced.

The existing configuration fixture passed [16 checks](router-config-regression.json),
including router login/logout visibility, preserved session, unknown binding,
different destination and failed writes. Final compiled resources passed
[task/PC/attachment observation](specialist-browser-result.json) and
[automatic injected-config/login migration](compiled-login-regression.json)
in isolated Chromium 390x844, Chinese locale, with synthetic HTTP and OS bridge.
There were zero page errors and no task dispatch/continuation writes. Screenshots
were inspected: [before obstruction](service-button-before.png),
[PC/task controls after repair](native-runtime-selection-390.png),
[partial result checkpoints](collection-checkpoints-390.png),
[attachment preview](attachment-preview-390.png),
[retained login](compiled-login-retained.png).
These are compiled UI tests, not ArkWeb/phone or live NAS acceptance.

At the exact final source commit, [Generated Harmony Web](https://github.com/particleflow/Memoh/actions/runs/40203617560/job/121199797661)
and [contribution policy](https://github.com/particleflow/Memoh/actions/runs/40203617597/job/121199797509)
passed. Skipped governance is not counted as a pass.

## Retained and withdrawn references

The empty, never-published 1.0.7 draft release `423199483` is now reversibly
marked withdrawn/superseded; [read-back state](old-release-revocation.json)
confirms draft=true, zero assets and no publication timestamp. Its tag, commits
and evidence are untouched. Existing 1.0.7/1.0.8 repository artifacts, old private
Library references, original source ZIPs, private config, SDK and rejected local
candidates remain retained; none is deemed safe for permanent deletion by this
review. This inventory does not revalidate old private Library downloads.
