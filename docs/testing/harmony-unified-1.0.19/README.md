# Garnet Assistant 1.0.19 bug-fix handoff

Unified compatible source: `69ef2fac2ceeaf68458f0f68bc30d0e8a31872ee`. Its Web/SDK bytes equal final shared UI `2422db2011231d5a4e538aa0fd7b573d133cb350` (PR85). Harmony-specific source is `d213ee7842fc76527de712cd5b2d825a5697a613` (PR86), a direct descendant of 1.0.18 source `78c89c278494f750ad45708837487b00e3a34192`. The existing archive plus hash-checked phone overlays remain; no wholesale server tree import.

The one existing action button queues a draft and stops only when empty, retaining the final repeat-gesture guard. No obsolete two-button iteration or independent contact-structure draft is included. Palette/system-following corrections and native ArkWeb Auto dark mode with forced conversion disabled are retained. The installed official SDK exposes both `darkMode` and `forceDarkAccess` in `ets/component/web.d.ts`; compilation is recorded separately.

Existing PR86 evidence is reused: 55 prior directed tests, 94 attachment/image/task baseline tests, final 19 submission/queue tests, generated phone types/lint and actual narrow-screen single-control/palette/setup browser evidence. See ../garnet-harmony-composer-themes/README.md and its single-button/ files. These are not claimed as rerun device tests. Four inherited full generated UI-contract voice-adapter failures remain documented there, unchanged; the full guard is not called green.

This round verifies the exact 45 product plus 2 test generated adaptations, source hashes and empty configuration, then performs the mandatory formal Web production/type pipeline and one new SDK release HAP build. Source checker passes 167 reviewed entries / 176 source/support files. Historical 19 later source changes remain unverified.

No permission, identity, login/HTTPS migration, attachment journal, TLS rule, timing default, dependency or signing change. No contact UI, server/core change, execution-root switch, physical routing test or paid recovery is added. Public artifacts contain empty configuration only; existing Windows owner injects approved private settings then signs, and private configured outputs never return to GitHub. Existing 1.0.18 stays intact. No signing, installation or production/phone acceptance is claimed here.

## Official build

Existing official SDK 26.0.0.105 release `assembleHap` passed in 17.421 seconds (33 tasks: 24 executed, 9 up-to-date). HAP: 35655466 bytes; SHA256 `2b935a85edc4c44e59554c6bf5041a72343a0c82a49dc36f88da6f9109ad2aeb`. All 817 members passed CRC plus project resource/source identity checks. Source identity: `460c10abb3b5a74d38ed099b3cd8eb8a98775814ee487709a778034fb28bab30`. All 138 formal inputs and 802 Web resources are bound. Empty 8192-byte deployment slot, original identity/permissions, unsigned mode verified. Explicit credential/private-authority/signing-material scan found no matches; arbitrary encoded data is not claimed exhaustively checked.

One early package preflight ran before the producer completed and correctly rejected stale build input. It wrote no accepted package; the original failure log is preserved, and packaging ran only after the current producer succeeded. This is not an SDK compile failure or a relaxed guard.
