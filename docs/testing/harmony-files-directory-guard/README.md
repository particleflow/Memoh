# Harmony original-Bot and active directory-read guard

Source-only follow-up after **d87b50a0d446a5d9983d6d4f5c0434b7400226e4**, the qualified1.0.25 build. The earlier F04 checkpoint deliberately consumed only its ordinary-picker delta and documented the separate parent guard as excluded. This follow-up checks that exclusion and restores the missing source behavior. It does not retroactively relabel or rebuild1.0.25.

## Confirmed gap and exact restoration

`pollTree`, `startTreePoll`, visibility listeners and the active/Bot watchers guard when a refresh starts. They do not guard an already-running `listDirectory` or its transient-error retry loop. `FileTreeKey.listDirectory` directly receives `safeListDirectory`, which delegates to that loop without another active/Bot check. Before this fix the loop reread mutable `props.botId`, so an original path could be retried under a different Bot; a completed read or rejection could also surface after the pane became inactive or changed owner. The existing native save adapter contains no equivalent read-loop protection.

The [exact function delta](exact-directory-guard.diff) is copied from **5b797fb970f4b67ab4d79d27a530f0ea540a8ccb**, the direct parent of565c0b4:

1. Capture the original Bot and decline inactive/empty-owner reads.
2. Check activity/owner before each attempt and use the captured Bot in the SDK call.
3. Return no obsolete entries after a successful read; suppress obsolete failures before retry/toast.
4. Retain the existing maximum-three transient retry loop and normal current-owner error handling.

No new generation mechanism, API, permission, SDK field, cancellation protocol or UI is introduced. This is the exact upstream guard, not a broader observer rewrite. Upload queue behavior and native saves are unchanged.

The guarded shared component now byte-equals **565c0b4b3386d87d057234efe6828479d35fcee0** before the existing native save adapter: SHA256 `f2339f46ef671af0d88ddb9567b9aca7ba6d4202c0112d266a1da6366536ae2e`. This equality is for this one component, not the full shared Web tree. Original5 upload tests remain exact upstream copies. The [generated comparison](generated-diff.json) against the actual1.0.25 context proves only this product component and one added nine-case test differ. SDK/native code, remaining44 reviewed adapter entries and both original test adaptations are unchanged.

## Actual directed checks

- [Old-code regression log](red.log): **6 failures / 3 passes**, covering inactive reads, stale successful entries, post-switch retry and stale error display. The two old retry cases reach the test timeout because they incorrectly keep retrying; their original log is retained.
- [Fixed compiled-component tests](green.log): **14/14**, comprising9 new directory cases plus all5 unchanged ordinary-picker cases. Tests call the real provided FileTree callback through a mounted FilesPane, with isolated SDK responses/fake timers. They cover inactivity, success/error races, switching Bots during retry, immutable source entries, preserved bounded retry and current permission errors. There are no real production requests.
- Complete generated Web [types](types.log) and affected [ESLint](lint.log) pass; no assertion or rule was weakened.
- [Source / exact45+2 adapter guard](source-guard.log) passes:172 entries/181 source-support files, public configuration remains empty. Historical19 unidentified source edits remain unverified.

This is directed cloud component verification, not a full Server/PG browser session or ArkWeb/phone acceptance. No new visual layout/control was added; the existing F04 browser/screenshot evidence is retained as prior evidence and is not presented as proof of this new asynchronous-read guard. No new phone screenshot or physical-device test is claimed.

## Handoff boundary

No version bump, SDK download, HAP build, artifact release, signing, deployment, field operation or private configuration change occurred. Original1.0.25 HAP remains SHA256 `abba93e0984949ce9bb9bafd42638a353b2e27b4f27fcaffdebc60f5dec73006` and does not contain this source follow-up. Existing1.0.25 signing/distribution belongs to the original onsite writer; phone acceptance remains unverified. This commit is the narrow source handoff requested for the next unified composition.
