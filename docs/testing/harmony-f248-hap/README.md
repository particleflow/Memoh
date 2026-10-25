# Verified f2488597 unsigned Harmony HAP

Built source: `f2488597aba37ef8c3557cc3f156b78d870c82c9` on the existing
`recovery/harmony-source-20261023` branch. Shared source remains exactly
`5e55c45c85659f04b339b4df42aeff72966542a8`. This is a new build of the updated
source, not a relabeled f6403de package. Evidence-only commits after this build
do not change the package's source identity.

- File: `memoh-harmony-1.0.7-1000007-f2488597-api26-unsigned.hap`
- Size: `37013036` bytes
- SHA-256: `b7eee63f9751ae5d7772299fd22e410ef129c39571269eb5f7bbf9a43d842f0d`
- Bundle: `cloud.begonia.workassistant`
- Version: `1.0.7`, code `1000007`, above the reported installed 1.0.6 / 1000006
- Native source fingerprint:
  `738c6fbb4fcd85c177ac93b124d7d8906410406188a6a2e1b7a0681532e6f49a`

## What this package includes

Relative to the previously compiled f6403de package, it incorporates the
reviewed component notices, shared native task content/continuation and
identity observation, native Provider Images/reference controls and saved
Provider state, truthful image-tool attachment references, and the phone's
late-401 session guard. The shared Provider mount refresh is consumed directly
from 5e55, with its duplicate phone patch removed. Existing original/delegated
attachment routing, owner Bot scope, app identity, permissions and persistent
login storage are retained.

It does not claim to contain any shared integration after 5e55. The e26 login
fix is included through the existing phone adapter even while its separate
shared-Web integration proceeds. The unidentified historical 19 later edits
remain unverified; this package does not recreate the inaccessible 5a7 tree.

## Build and package evidence

The retained official Linux CLT and API26 SDK `26.0.0.105` were reused, together
with the existing pnpm cache and verified LibRaw 0.22.2 sources (244 files).
No SDK download, new environment, Windows development or NAS change occurred.
The ordinary project sequence ran once: Web build/typecheck, resource packager,
existing dependency verification, materials generation, then official hvigor.

All five Web checks and both native compile/qualification exits are zero.
The package validator matched all 797 Web resources, the 104-file native
source snapshot, compiled ArkTS ABC, both arm64-v8a and x86_64 ELF libraries,
LibRaw notices, and unchanged toolchain metadata. ZIP CRC and duplicate-entry
checks passed. Both `module.json` and `pack.info` identify the version above;
module/app/permission metadata is identical to the previous 1.0.7 package.

The build reports 86 ArkTS warning locations, identical to the prior f6403de
build, including existing deprecated API and exception-handling warnings.
The explicit no-signing-config warning is expected for this unsigned output;
this is not a warning-free build claim.

See [build summary](build-summary.json), [package verification](hap-verification.json)
and [official build result](native-build-tail.txt). Exact raw build logs and
prior receipts remain in the existing cloud workspace; their hashes are
recorded in the summary.

## Content and artifact retention

The public build configuration contains no service address; runtime API
selection remains unchanged. No Vite environment override or dotenv file was
present. Resource screening found no high-confidence secret, signing material,
private configuration or newly unreviewed host. No SDK/dependency tooling is
bundled. The 13 Web component notices are retained. Web source maps are absent.
The existing official debug build's `ets/sourceMaps.map` is retained: its 19
relative-source entries contain no embedded source text or absolute workspace
path. It was inspected rather than stripped from the official output.
[Review record](content-review.json).

The newly built HAP was saved as a distinct private Library item. Official
materialization back into the same Linux cloud environment reproduced the
exact bytes, size, SHA-256 and ZIP CRC; local Library identity metadata was
also verified. [Retention checks](cloud-retention-check.json). Private Library
IDs and transfer data remain outside the public repository. The old named
f6403de HAP, original source ZIPs, private configuration and SDK remain intact.
No public binary upload, release or tag was created.

## Compiled UI check and remaining delivery boundary

The existing compiled Bot-navigation harness ran against resources extracted
from the **Library-readback HAP**, served on an ephemeral loopback HTTP port.
Real Chromium at 390x844 exercised Bot settings, create/cancel, duplicate submit
with exactly one synthetic POST, 403 refusal, 403/404/500 detail handling and
return to chat. Login remained present; no page errors occurred. API responses
were intercepted synthetic fixtures and no live Bot was created.
[Result](compiled-bot-navigation.json), [actual settings page](bot-settings-390.png),
[actual refusal page](bot-create-403-390.png). Both screenshots were inspected.

This remains an **unsigned** package. Windows receiving/signing, signed upgrade
and login persistence on the device, native permissions/picker, live Bot/Agent
execution, and phone-to-NAS-to-phone transfers are not accepted by these checks.
The known Windows Library metadata failure was not retried or bypassed. No
signing secret or certificate was exported or reset. The next delivery step is
the existing Windows signing task's supported receiving path, then signing
with the existing identity/profile and on-device acceptance. Cloud retention
success does not establish that Windows receiving has been repaired.
