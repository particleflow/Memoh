# Harmony 1.0.13: two reported bug fixes

This release candidate combines only the existing system-back fix
`4302b326c637585d3fccf510b4fa66180bb9aee1` with the independently reviewed
microphone fix `e0cf31b43926725ac3d08b17149fa9a63d26755c`.

## Source boundary

The 30,227-byte source handoff ZIP has SHA256
`6dad7404decee8a4f0b51098815071560c1197fe902ec80350daa04c39a147c4`.
Its incremental `mic-on-back-4302.patch` SHA256 is
`9d4626c082061272decf7dccecc277b386aa4bbe89720b5d48c2ff7f24d2abab`.
A separate Git index reconstructed the exact application tree
`fe46adf5409f20816ecb38f0d2c778cb6658d3bb` before application/version update.
The larger producer review tree `43de7cf10e5415cfff0cbce529572bbee02367e9`
also contains author evidence and is not claimed as this application tree.

The five-file increment changes the builder's existing composer callback,
`HarmonyVoiceInput.vue`, `HarmonyVoiceFan.vue`, source hashes and precisely one
expected composer output hash. The existing 43-adaptation assertion and other
42 outputs are retained. The existing app version advances to 1.0.13/1000013
with its source hash updated. Shared source remains 9bb6fc3; network consumers,
login/HTTP→HTTPS migration, attachment intake/recovery, bundle and permissions
are unchanged. No pending sidebar/activity, shared durable-transfer, delegation
download or other unified patch is included.

Microphone landing now follows the matching recording's actual target after
asynchronous send/attachment preparation, tracks live target bounds, and clears
animation observers/frames on completion. Transcription uses the current editor
bounds. System back retains the already-reviewed layered return behavior.

## Build and verification

The existing official SDK26.0.0.105 and CLT26.0.0.851 were reused. The normal Web
build, package and materials generators passed. The native invocation uses the
project's existing release configuration through the official CLI:

```sh
hvigorw assembleHap --mode module -p product=default -p module=entry@default -p buildMode=release --no-daemon
```

The old qualification wrapper hardcodes debug; it and the toolchain were not
modified. A recorded private driver runs this standard release command and reuses
`verify_snapshot`, RAW dependency, rawfile and HAP resource verification from the
existing project helpers. Actual CompileArkTS and assembleHap succeeded once.
The complete command, raw compiler log, unchanged SDK hashes and input snapshot
are retained privately. See [verification.json](verification.json).

The final unsigned release HAP is **35,658,253 bytes**, SHA256
`1ad4660c31e7fb780d3abd4690dfc79176e0ac504b30d72fae39e75e6654c487`.
Source identity:
`f8c58b756fcd344b0c8de6df3ef097894d5e08e895a4a54a1695a050350ccef2`
(106 inputs). ZIP CRC, compiled ArkTS ABC, both native-library ELF architectures,
licenses and every packaged rawfile digest passed. Packaged metadata confirms
1.0.13/1000013, release mode and debug=false. Module identity/permissions/platform
metadata match rollback1.0.12 after excluding only version and requested build mode.
The public configuration slot remains empty, stored at 8192 bytes. No private
service address, credential or signing material is injected in this environment.

The producer's exact combined source check/types, 12 Back unit cases, 9 native
seam scenarios, and mic owner's 15 isolated scenarios are reused with their
scope unchanged. The current full Web build performs its normal typecheck.
A new smoke test exercised the actual compiled combined page with synthetic
API/native seams: boot with the combined voice callback, drawer back, and
settings return preserving the draft and login. One login, zero page errors,
zero missing-required-prop warnings, no task/model/management/transfer writes.
[Results](smoke.json); reviewed [chat](combined-chat.png) and
[returned draft](combined-returned.png). These are current browser interactions,
not new recording/animation or phone acceptance evidence.

## Private onsite handoff

The generic unsigned HAP, unchanged `client_config.py`, checksums and exact source
lock are handed off privately. The onsite writer must verify these bytes, reuse
its already-approved local deployment configuration/legacy binding, inject locally
with the existing expected-SHA guard, then sign/verify with the existing identity
and device profile. No private configuration or signed output returns to GitHub.
This task does not sign, distribute, deploy, alter a device binding or use Windows.
Original1.0.12 rollback and all previous evidence remain preserved.

Real left/right gestures, microphone landing on the installed phone, foreground
restoration and actual device TLS/transfer acceptance remain outstanding. Human
QA is reported truthfully and is not used as a prerequisite for this source commit.
