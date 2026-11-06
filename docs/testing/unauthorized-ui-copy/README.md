# NAS Assistant: remove unrequested copy and controls

The requested source cleanup removes unsolicited explanations and the explicitly rejected controls. It does not fix the independently owned attachment upload failure, unnamed conversation, primary-contact history, text streaming, or speech streaming issues.

## Concrete findings and changes

| Item | Existing source and meaning | Source change |
| --- | --- | --- |
| Return to latest / 返回最新 | Introduced into the current task directory by `67e82daf6d995f1e7bd5f5f5ba97ad63d37806b3`. `reader.newest()` clears the opaque page cursor and reads page one. It is a pagination reset, not a task state or task destination. | Removed the button from `bot-task-directory.vue`, `session-task-results.vue`, and `bot-delegation-tasks.vue`. The existing reader, five-second refresh, close/reopen reset, next-page access, destination validation and task history remain. |
| Stop reading / 停止朗读 | `9c361d603` added composer controls and a context-menu stop item in `chat-pane.vue`. The composer also added enable/resume playback buttons. | Removed those control nodes. Right-click/long-press replay, message-click interruption, input interruption, speech state, bubble animation and the speech controller remain. No playback-engine or streaming change. |
| Attachment catch-all explanation | The earliest traceable Git introduction is `8184928c3` (reviewed 1.0.11 source subset). `harmony-resumable.ts` used a catch-all mentioning checksum, authorization, network and a recovery entry regardless of the specific native failure. | Removed the speculative and reassuring tail. The existing failure remains visible as `附件未完成`; the original native code is retained as `Error.cause`. The 401/403 handling, checksums, native receipts, journals and transfer behavior remain. This message is not evidence of the actual upload cause. |
| Other supplemental copy | Page subtitles, row descriptions, empty-state instructions, slash-menu explanations, field help, duplicated recovery guidance and native transfer/recording/setup reassurance. | Deleted the corresponding template nodes or optional component inputs. Mixed status-plus-advice messages keep their factual state. No CSS hiding and no replacement explanation paragraphs. Required `DeviceCodePanel` hint inputs are empty strings, containing zero visible characters. |

No authorization for the rejected additions was found in the supplied user requirements. The provided instruction was to show names, true states and correct destinations in management lists, and to use the established playback interactions.

## Audit and cross-client consumption

- `copy-inventory.json` records 628 candidate copy sites, their component, original expression and Chinese text, and a keep/remove reason. 221 were removed or narrowed in the initial candidate census; later direct-text and field-help checks are included in the reviewed source surface inventory.
- `reviewed-surfaces.json` records 1,907 final template text/control surfaces plus 152 script i18n surfaces (2,059 total), with all three locale digests. Existing user/catalog body, control names, genuine state/error, consequential action body, and nonvisual accessibility descriptions are distinct from added explanations. Development-only component examples are excluded from production policy checks.
- The policy guard is `scripts/check-ui-copy-policy.mjs`, wired into the normal JavaScript lint command. It checks new text/control occurrences, script translation references and changes to any locale content. Deliberately reintroducing a small paragraph or stop-reading control was rejected; source was then restored and accepted. See `policy-proof.json`.
- Web base: `ac7eba92fee9c2824f058b1394a6c55362b787e8`. Harmony base: `013e91f7dd244061a941b7bb63994fb5270e8356`. The Harmony overlay applies the corresponding 101 changed Web outputs after existing native adapters, with original/output SHA256 and literal-count checks. It does not replace older whole-page snapshots over native code.
- Direct Harmony review includes `SharedChat.ets`, service setup, upload, uncertain-write, download, recording and voice-player display strings. The attachment name, real size and resumable capability are retained as original data/state.
- The previously stale Harmony baseline contained two pre-cancellation input/output hashes. Original Git `013e91f7` builder output was compared with the current pre-cleanup native patch output and proved identical. Only the two documented `chat-pane.vue` and `home/index.vue` hashes were reconciled; the source-check guard and 45+2 adapter count were retained.

## Verification

- Actual local Go server built from the selected Web base, real isolated PostgreSQL, and the current Vite application. Web: `http://127.0.0.1:18183`; API: `http://127.0.0.1:18180`. No response interception, paid model call, real device command or production write.
- Ten actual browser checks: desktop/light and 390px/dark task-directory rendering, missing return-latest control, genuine browser-offline failure visibility, recovery after reconnection, appearance controls without help text, and profile title-model controls without their added explanation. `browser-proof.json` records responses and interactions.
- Web: 71 targeted tests; full typecheck; full ESLint with zero errors; UI contract; copy policy; whitespace check.
- Harmony composed source: source/provenance check (176 source entries, seven approved artwork inputs, 45 original non-test adapters and two test adapters); full typecheck; 75 targeted tests. Generated output hashes and the preserved interruption/replay entry points are checked in `harmony-proof.json`.
- Actual screenshots were captured and inspected in the selected workspace: `chat-desktop.png`, `tasks-{light,dark}.png`, `tasks-offline-{light,dark}.png`, `appearance-{light,dark}.png`, and `profile-{light,dark}.png`. They are local verification material, excluded from the source handoff archive.

This is a source handoff. No push, PR, production deployment, HAP build, version increment, signing, installation, physical ArkWeb acceptance, or human QA is claimed. Active manual playback was covered by existing controller/anchor tests and source-entry checks; this task did not make a new paid synthesis call or claim a physical playback test.
