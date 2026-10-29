# Harmony v13 shared-source alignment

The recovered original executor first preserved v12 as `03361d21c8542632f1d4bedbde8dcd3d950e8d86` on the existing `review/harmony-image-timing-20261026` branch. A recovery bundle verified against the already-pushed prerequisite `5f674c8a3e72681bfc6bfd34574b53cd21410446` has SHA256 `870d82065326dbfee99c4efe4932cc554449945ef13705ffe1ad35af874504cc`. Initial executor disconnection prevented materialization from starting; it did not erase the fourteen pending source files. No environment reset or replacement was used.

## Exact input and composition

Library `libfile_598d489937848191912dd0bed9de5159`, version13, was materialized by the official consumer-local helper after recovery. The 3,145,672-byte ZIP matches SHA256 `d9724fc0035937823dc0e55666bf88100a3db2e59394c519252cdf999942e19b`; all CRCs and 296 current-manifest entries passed. The imported bundle and ordinary Git readback both identify `69452d9d5a0338be0be704c2107bb914a75b5ab2`, tree `c829b59168abf8615a86346298b47999b0ecda39`, parent `959efb3aab3f7165b1dd0e9e7f00dac27526688e`.

Only 22 new shared paths are composed: eight modifications, ten additions and four removals. The 143-path consumer map has 139 present paths and four explicit deletions. The generated phone retains existing native adaptations; 122 present mapped files are exact upstream bytes, 17 retain phone or test adaptations. Two unchanged shared journal dependencies and two cached dependency-metadata entries from v12 remain. All 47 existing adapter records remain; only chat-pane and message-item hashes change in this increment. The final guarded generation matches all 6,995 tested context files.

The history components keep existing title, streaming state, click/Enter/Space reading and Open navigation. Rename/delete/bulk-selection UI and its four obsolete modules are removed; no historical rows, sessions or drafts are deleted. Read-aloud consumes actual rendered text, preserves current Home/workspace/task-location gates and original message replay, and uses the same response for unsupported-MSE WAV fallback. WAV fallback waits for EOF; this is not a 500ms audible-onset claim.

## New verification and exact repair

The initial full generated Web typecheck exposed six diagnostics in two newly admitted tests: an absent external review JSON, a review-only import alias, two incorrectly inferred EventTarget callbacks, an unused fixture parameter, and an assistant-message union spread. `consumer-test-types.patch` fixes only those tests: it imports the actual paired composable, extracts the original assertions' expressions from the actual composed chat template, explicitly retains EventTarget identity, and narrows the assistant fixture. No assertion is removed, no TypeScript option is relaxed, and product playback code is unchanged. Patch SHA256: `aff4b8744a838e74d6ad3203b6b8c358ba41ad521273848b2740683e6a723784`.

The final complete generated Web typecheck and ESLint for both repaired tests pass. Sixteen focused cases pass across the two repaired streaming/replay files, the three history-row cases and two combined phone WAV/visibility/gesture seams. Initial type failures and the initial package-manager command error remain beside the successful logs. These tests run the actual generated phone sources with controlled DOM/audio responses; they do not run a real phone or bill a model.

V12's metadata outbox, actual generated-SDK/old-server and workspace recovery results are reused, as are the source author's nine WAV cases. The four generated SDK files are unchanged from v12: 379 original operations plus three first-error operations. All native ETS and the three exact API26-compatible hashes are unchanged from the already successful official SDK26.0.0.105 receipt. No new native compilation is necessary for this Web-only delta and none is claimed. The source guard passes with 179 text inputs, seven approved PNG inputs and 195 files; the nineteen historical later changes remain explicitly unverified.

## Delivery boundary

No HAP, version bump, signing, deployment, new SDK/dependency download, live model invocation or private-configuration publication occurs. HAP1.0.28 retains its original source and artifact identity. Schema180 is not assumed deployed. Actual ArkWeb codec/autoplay behavior, audible timing, public-network photo URI behavior and physical-phone acceptance remain unverified. A final new HAP must be built only after the exact Server/Web/Harmony version is frozen and separately authorized; this source checkpoint is not that package.
