# Harmony 1.0.14: remove unsolicited recovery UI and reuse activity navigation

The native recovery button, explanatory paragraph and unused binding are removed
exactly as approved in `f76375468262237232f9342f922fdaa0e06d08ee`. Existing native
transfer recovery, journal/history and the four intake routes remain unchanged.

The shared activity navigation from `d2bade8b30e4b6df778bc2c0b402f3b1fe592eeb`
needs its earlier sidebar entry and read-only reader, absent from the phone's
9bb6fc3 source. The builder reuses exactly its activity component/test/reader,
sidebar visibility/entry and ordinary authorized delegation session navigation.
The duplicate composer task menu is removed. Web durable-transfer changes and
file-tree error changes are excluded. No API, service, permission or framework is
added. The existing 43 phone adaptations are preserved; only the expected
composer input/output hashes change because its task menu was removed.

## Verification

- Complete generated phone Web typecheck and production Web build passed.
- 83 activity/task-attachment tests and 12 system-back tests passed. The first
  command selected three existing test files; back was copied into that build
  context and then run separately. ESLint for affected shared modules: zero.
- Source inventory: 135 reviewed sources, 144 tracked source/support files.
  The 19 unavailable historical changes remain explicitly unverified.
- Actual compiled page at a local loopback HTTP server, Chromium 390x844,
  synthetic authorized HTTP/native seams: no recovery button/hint or duplicate
  task button; activity shows name and execution state; clicking opens the
  original child session and closes the mobile drawer. One login, identity
  retained, no page errors or task/transfer mutations.
- Initial browser fixture attempts incorrectly tried to edit an input awaiting
  an unsimulated runtime snapshot and expected a different Chinese status label.
  Those raw failures are retained privately; neither required a product change.
  This focused page check does not claim a live runtime result or completed
  phone rendering/upgrade acceptance.
- Existing official CLT26.0.0.851 / SDK26.0.0.105: release `assembleHap` passed
  once, including real CompileArkTS. CRC, every rawfile digest, source binding,
  both native ELF architectures, empty configuration slot and unchanged
  application identity/permissions passed. See [verification](verification.json)
  and [content scan scope](content-scan.json).

Public HAP and ZIP are generic and unsigned. Windows must inject the authorized
private configuration and sign there. Injected/signed private packages must not
be returned to GitHub. Existing 1.0.13 is retained for recovery.

## Actual compiled-page screenshots

- [Composer without unsolicited recovery controls](composer-390.png)
- [Activity name and execution state in the mobile drawer](activity-390.png)
- [Original child conversation after clicking](activity-opened-390.png)

The conversation skeleton is expected in this isolated fixture, which does not
simulate its runtime synchronization. The screenshots demonstrate the changed
controls and navigation, not successful task execution or real-phone acceptance.
