# Garnet Assistant 1.0.16 unsigned release candidate

This is the next patch version of the approved
[`591c05fca72626cfc25e4263a56ebc013a02e12a` UI review](../harmony-minimal-task-ui-review/README.md).
The only product-source change after that review is the application version:
**1.0.16 / 1000016**. The task button opens the existing sidebar and its shared
name/state/original-conversation list, not the old task detail dialog. The
mobile hidden-panel mount change is retained. No image timing draft, new
directory feature, application identity, permission or private-config change is
included.

## Build and integrity

The 108 functional tests, typecheck, UI screenshots/DOM and CI of the approved
source are reused. No manual functional suite or old CI rerun was requested.
The existing package gate detected three final test-wiring input differences in
the earlier Web producer receipt. The normal Web generator refreshed that
receipt and resources, including its mandatory type gate; no metadata was
fabricated or validation bypassed. The existing package/materials generators
then bound the new version to its actual resources and 114 source inputs.

The retained official CLT26.0.0.851 / SDK26.0.0.105 ran the existing release
`assembleHap` once. Actual CompileArkTS, CRC, source/rawfile digests, both native
ELF architectures, license resources, unchanged identity/permissions and the
empty 8192-byte deployment slot passed. The package is **unsigned**, release mode
and debug=false. See [verification](verification.json) and
[scope/reuse evidence](reuse-evidence.json).

HAP: **35,620,288 bytes**.
SHA256: `8663e5d352af4326ea4354d066e81d63f1ee9a1624b262ef71ec3e05089a481d`.

The content check covers every HAP member for explicit credential patterns and
URL-authority credentials, plus blank deployment/signing fields. It does not
claim exhaustive detection of arbitrary encodings. No private service address,
configuration, credential, profile or signing key was provided to this build.

## Handoff and remaining acceptance

The normal Git artifact branch carries the original generic unsigned HAP,
unchanged configuration injector, source lock, verification and checksums in a
ZIP. The authorized Windows writer must use the existing private configuration
with productName `Garnet Assistant`, preserve the established endpoint/legacy
binding/HTTPS fields and sign using the existing identity/profile/device. The
injected or privately signed output must not return to GitHub.

The new production `GET /bots/{bot_id}/activity` remains a deployment dependency.
The 4d23244 image is published but its deployment is in `waiting_decision`;
production readback is not verified here. The package must not be described as
having passed full cross-Bot phone acceptance. The earlier local sidebar
median improvement (86.3 to 69.6ms) is not proof of resolved phone stutter.
Signing, personal distribution and actual phone acceptance remain separate.
