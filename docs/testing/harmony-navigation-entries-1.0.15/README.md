# Garnet Assistant 1.0.15: restore discoverable navigation

In published 1.0.14 (`ea9746a92c6d9ea773ff131ac877b94f277a0b17`), the builder's
shared composer patch removed the existing `SessionTasks` trigger/import. The
replacement sidebar activity component renders only active rows and hides the
whole section when none exist. Completed tasks, an empty list or unsynchronized
observations therefore left no permanent current-session monitor entry.

Bot management was not deleted: it remained nested in the top Bot dropdown.
The immediately visible sidebar offered only the selected Bot's settings.

## Minimal correction

- Remove only the builder's composer-trigger deletion. Reuse `SessionTasks`
  unchanged, including its existing permissions, scoped reads and UI. No new
  detail, log, API, permission or monitoring mechanism is added.
- Add one existing `SidebarNavButton` to the existing session sidebar, reusing
  `sidebar.manageBots`, the `bots` route and `closeMobileNav()`. Keep Bot settings.
- Leave the shared activity component/reader byte-for-byte pinned to d2bade8;
  the unified owner can integrate its full projection without a competing copy.
- Preserve the deleted attachment recovery button/hint, native transfer paths,
  login/HTTPS migration, Garnet Assistant name, empty public configuration and
  application identity. Advance only the patch version to 1.0.15/1000015.

## Actual validation

The local compiled page was exercised in Chromium at 390x844 with synthetic
authorized HTTP/native seams. Before: current-session monitor count 0 and direct
management count 0 with no active tasks. After: each count 1, both visible and
clickable. The existing task dialog opens; direct management reaches
`#/settings/bots`, shows the Bot list and closes the drawer. Bot settings remains
visible. One login, unchanged identity, no page errors or task/transfer writes.
These are isolated page checks, not ArkWeb, actual phone or live task acceptance.

Full generated Web typecheck/build and 95 activity/attachment/back tests passed.
Source hashes and 43 phone adaptations passed. Panel ESLint: zero errors, one
attribute-layout warning, retained in the raw evidence. The official retained
SDK26.0.0.105 release `assembleHap` ran once for this new version; compilation,
CRC, every rawfile/source binding, native ELF checks, identity/permissions and
the empty deployment slot passed. See [verification](verification.json).

## Screenshots

- [1.0.14 conversation with no monitor](baseline-chat.png)
- [1.0.14 sidebar with no direct management](baseline-sidebar.png)
- [Restored permanent conversation monitor](monitor-entry-390.png)
- [Existing monitor opened with no active tasks](monitor-open-390.png)
- [Direct Bot management, alongside Bot settings](management-entry-390.png)
- [Existing Bot list after navigation](bot-list-390.png)

The fixture intentionally has no runtime snapshot. The task dialog reports that
state; this evidence does not claim synchronized task content or real-device
transfer performance. Public handoff is unsigned and blank; the existing Windows
writer must inject private configuration, sign and distribute through the normal
personal channel. Keep 1.0.14 for rollback. Image timing investigation is separate
and is not included in this fix.
