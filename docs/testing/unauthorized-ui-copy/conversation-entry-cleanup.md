# Main-chat creation entry removal — 2026-10-29 supplement

User instruction: “bot就不应该有所谓的创建新会话的能力”. This supplements Web `9690095b4a6575af9f63915b05e713de38c55bf6` and Harmony `afa60196940d1e21bbf514d401c763a79f90f9d0`; the original 221-copy audit is unchanged.

Removed visible entry points:

- Desktop `dockview/header-add-actions.vue` and mobile `mobile-top-bar.vue`: no add menu while the main chat is active, and no new-chat item in secondary tool menus. Existing terminal/browser/display/split operations remain where already available. Harmony retains its original download action and native file-save wiring.
- `sidebar/panel-sessions.vue`, `recents.vue`, `folders-section.vue`: remove main-chat creation buttons, including per-folder creation. Existing history search, viewing, context operations and folder creation remain.
- `contact-choice-dialog.vue`: remove the new-contact option, retain existing candidate names/IDs/current marker/radio selection, actual error, pagination and confirm/cancel. No replacement conversation-selection UI was added. This composes with Astra `14355dd1f7049cca42d49e67391f5486368ff6ee`; preserve the earlier removal of DialogDescription too.
- `chat-pane.vue`: remove `/new` shortcut, new/reset/clear runtime command suggestions and the fork dialog trigger. `slash-command-result.ts` also rejects new/reset/clear quick-action rows; task goal-clear operations remain.
- `message-actions.vue`: remove inline/menu fork-creation controls and the resulting empty menu. Existing source-of-fork historical navigation remains.
- `keyboard-bindings.ts` and `workbench-commands.ts`: remove the new-session default binding, native menu/settings row and command registration. Saved old overrides cannot restore an absent binding. Legacy command ID type remains for compatibility, without handler or visible entry.

Boundary for Astra/unified owner: this patch changes UI entry points only. Existing internal `openDraftChat`/reset paths, typed `/new`, Agent changes, workspace-close/focus fallback, prefill resolver, and auto contact admission belong to the separate main-chat identity contract. Do not infer persistent identity is fixed from these removals. Browser review observed existing POST `/sessions/contact` admissions; no session-creation endpoint was called. No old sessions were deleted or merged. Scheduled/background new execution contexts and browser sessions remain intentionally.

Verification on current source:

- Web: 172 relevant unit/contract tests plus 8 workspace keyboard integration tests passed; Vue typecheck and changed-file ESLint passed.
- Harmony generated consumer: 170 relevant tests plus 8 keyboard integration tests passed; Vue typecheck passed. Source/provenance check passed with 176 source entries, existing 45 native adaptations and 2 test adaptations intact. 14 supplemental shared outputs mapped; aggregate cleanup overlay now has 113 entries.
- Copy-policy baseline reviewed after authorized removals: 2046 accepted surfaces, no locale changes or new explanatory copy. Original 221 audit/fault-injection suites were not rerun.
- Real running Go server + isolated existing PostgreSQL fixture, no response interception: five actual UI checks passed across desktop and mobile. No top add control or `/new` suggestion; existing historical record selectable; historical search and folder creation retained. Zero page errors; only original contact-admission POSTs occurred. No model calls/device actions.
- Three local screenshots were explicitly viewed: `no-create-desktop.png`, `no-create-mobile.png`, `no-create-history.png`. They are verification-only and excluded from the source deliverable.

No push, deployment, HAP build/signing/version change, physical ArkWeb acceptance or human QA is claimed. Integrate these component deltas with Astra's identity work; do not overwrite newer complete components with this older base.

## Final rejection gate, user instruction 09:52

User repeated: all buttons and small print without a requested or native functional basis must be removed, zero visible characters remaining. `retained-controls-origins.json` traces 597 control/selection records; `retained-content-origins.json` traces 1298 body/name/state/confirmation records; `retained-script-origins.json` traces 151 script translation sets. Each has the exact component, source excerpt/binding and originating Git commit/line. Earlier copy-audit decisions are cross-referenced when applicable. The original-source lineage identifies the existing functionality; it cannot override a later rejection. Native consumer has a separate 30-record source-origin manifest for its own visible controls/state, alongside the shared source mapping.

The root Web build and lint gate now explicitly reject removed task-newest, assistant stop/enable/resume, new-contact, main-chat-new and fork controls, including CSS-hidden or icon-only substitutes. Three additional regression tests verify the gate also permits original interruption/replay gestures, historical navigation and actual errors. Native source preparation checks the absence of rejected keys and fork UI across all 339 generated Vue templates. This is a visible-interface rule: unused legacy locale/command IDs and machine error codes are not user-visible residues and remain where compatibility requires them.

Known integration blocker from unified independent review: assistant read-aloud NotAllowedError recovery regression remains assigned to the read-aloud owner. Recovery must use existing long-press/right-click gestures; no recovery button is permitted. This source-only UI cleanup does not change or certify that engine behavior. Unified acceptance must remain blocked until that owner repairs and verifies it.
