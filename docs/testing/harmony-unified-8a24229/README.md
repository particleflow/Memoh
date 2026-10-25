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
