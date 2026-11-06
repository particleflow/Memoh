# Historical mutation entry cleanup (P2)

Scope: the final UI restriction now also removes historical rename/delete/bulk-delete controls that c4 fixed-contact endpoints intentionally reject403. No server/store permission was relaxed; no history was removed, hidden or merged. Legal file operations remain unchanged.

Review input: Library `libfile_598d489937848191912dd0bed9de5159`, version11, ZIP SHA256 `bf1d92ea193512d579fc38966f53397a0b8381188ff254f731f42f98b78b3279`. Reconstructed its complete review patch on b360 and verified tree `1ef52303e55e9711ffcf508bc1177f71174a3818`. All four affected sidebar components match cleanup205 byte-for-byte in that tree, so the same component delta applies to the admitted fixed-contact/replay composition.

Removed: historical row dropdown and all rename/delete/select entries; corresponding context entries; rename/delete event wiring from Recents and Folders; mutation dialogs; bulk-selection header/action/dialog/provider and keyboard selection branch; now-unused session-only selection module/test. There are no replacement hints, icons or controls. The deleted files are source code, not user historical records. Shared store/API methods remain compatibility/backend boundaries; no Vue entry still calls these historical mutations.

Retained with explicit basis:

| Retained item | Basis and original function |
| --- | --- |
| Row title and click/Enter/Space | Explicit instruction to preserve historical reading; `session-item.vue` emits the original `select` event for the same session. |
| Right-click Open | The existing `openNewTab` action opens the same historical record for reading, without creating a conversation. Active-record disable behavior is preserved. |
| Streaming indicator, Agent/schedule identity, real title/route tooltip | Actual state and identity names required to distinguish original historical records; no new explanatory text. |
| History search, pagination, folder grouping/configuration | Original historical reading/navigation and legal folder-management features; no mutation of conversation history. |
| File rename/delete/upload | Existing legal file-management operations are outside this defect; `files-pane.vue` and `folder-item.vue` are unchanged against the admitted review tree and cleanup205. |
| Actual errors | Global and historical-read error surfaces are unchanged. Removing mutation entry points does not swallow or convert a real failure. |

The current origin catalog is narrowed from2046 to2031 records:587 controls/selections,1295 body/name/state/confirmation records,149 script translation sets. `history-readonly-origin-delta.json` records the15 removed older source-origin entries and the overriding decision. No full original221 audit was rerun.

Incremental validation:

- Three new compiled-component tests on the exact unified composition plus this delta: historical click/keyboard reading, Open-only context actions, actual streaming state. Three corresponding generated-native-consumer tests pass. Vue typechecks pass on both compositions.
- One new targeted regression-gate test rejects historical rename/delete/bulk selection while permitting legal file rename/delete. Existing old test waves were not rerun.
- Source copy/control guard passes on both cleanup branch and reconstructed unified composition:2031 reviewed surfaces, no additions. Search of all application Vue files finds no session-dialog/selection imports, historical mutation calls or rejected history keys.
- Native preparation verifies all four obsolete source files are absent from the actual context archive; source/provenance validation retains176 reviewed inputs,45 original phone adaptations and2 test adaptations. Rejected controls absent across337 generated Vue templates. Native overlay has117 exact entries, including four explicit hash-checked source deletions; the new component test is an added shared test source.
- Four actual desktop/mobile-viewport checks on the complete combined Web source, compiled matching Go server and independently cloned PostgreSQL fixture, without response interception: no history action button/rename/delete/bulk choice, Open-only context menu, historical record still opens. Zero page errors, zero API5xx, zero historical mutation requests. Two existing idempotent contact admission POSTs are recorded separately; no model/device calls. Screenshots were explicitly viewed and remain local verification files, excluded from deliverables.
- The fixture clone was prepared without disconnecting or mutating the original test database. Only its admitted180 schema migration was applied, required by the full reviewed composition. No production database or device was touched.

Current status supersedes the earlier task checkpoint: c4 fixed contact and fab04 independent replay recovery are admitted by unified review on the specified composition and are not remaining original-source gaps. Real phone/ArkWeb acceptance remains unverified. No push, deployment, HAP build/version/signing or phone installation occurred.
