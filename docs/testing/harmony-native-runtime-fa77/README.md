# Harmony shared Runtime UI alignment

This source-only checkpoint prepares the existing phone UI against unified
commit `fa77a073fe6380d297474a6053a1445201bcab83` on the independent branch
`feat/harmony-native-runtime-ui-20261025`. Source preparation verified 6,198 Git
blobs and the fixed UI revision `f69402a55dd87a59c10722fe11507ad8fd7048f5`;
all 43 existing phone adaptations applied. See the
[source lock](../../../mobile/client/config/memoh_web.lock.json) and
[existing builder](../../../mobile/client/mobile/build_memoh_web.py).

No new HAP was built. The signed/distributed 1.0.8 delivery remains based on
source `ee00ff76a07ac27e08f4b4f58fd1354f0ecbeef3`, with its generic unsigned
artifact preserved at distribution commit
`cdedf9239f0c93c41a0e679b570778ac999eadbe`. Public packages must retain empty
configuration; [Windows-local injection before signing](../harmony-service-migration/README.md)
remains unchanged. Configured private derivatives must never return to GitHub.

## Runtime identity and existing consumers

In the inspected contract, a native Windows Runtime is a remote computer with
`provider: "memoh"`, `kind: "remote"`, and the existing runtime/target IDs.
`kind: "native"` and `target_id: "native"` identify the NAS workspace. They must
not be reused for Windows simply because its implementation is called native.
The [runtime service](https://github.com/particleflow/Memoh/blob/fa77a073fe6380d297474a6053a1445201bcab83/internal/userruntime/service.go)
accepts the existing Memoh provider and projects connection state/capabilities.

| Surface | Existing consumption and boundary |
| --- | --- |
| Computer registry and Bot access | [use-computer-access.ts](https://github.com/particleflow/Memoh/blob/fa77a073fe6380d297474a6053a1445201bcab83/apps/web/src/components/computer/use-computer-access.ts) reads account runtimes and uses the existing Bot-scoped mount/unmount endpoints without an Agenvo-only filter. |
| PC selection | [chat-pane.vue](https://github.com/particleflow/Memoh/blob/fa77a073fe6380d297474a6053a1445201bcab83/apps/web/src/pages/home/components/chat-pane.vue) queries targets per Bot and retains selection per chat view. Missing/revoked targets do not silently become another PC. Folder-bound sessions let the server derive the target instead of sending a conflicting target override. |
| Tasks and logs | [useSessionTasks.ts](https://github.com/particleflow/Memoh/blob/fa77a073fe6380d297474a6053a1445201bcab83/apps/web/src/pages/home/composables/useSessionTasks.ts), `useComputerTaskObservation.ts`, and `useNativeTask.ts` use the existing authenticated Bot/session/task routes. Identity changes retire requests and clear content; no provider-specific phone protocol is needed. |
| Results and transfer state | [useComputerTaskDeliveries.ts](https://github.com/particleflow/Memoh/blob/fa77a073fe6380d297474a6053a1445201bcab83/apps/web/src/pages/home/composables/useComputerTaskDeliveries.ts) validates `version`, `operation_id`, `files`, `next_after`, and `has_more`. Only `committed` references become attachments; the last page does not establish task completion. |
| Phone attachments | [harmony-files.ts](../../../mobile/client/mobile/memoh/harmony-files.ts) retains the owning Bot and scoped delegation URL where present, with the existing original-byte validation and native save bridge. The phone does not connect to a new Windows transfer endpoint. |

Do not remove the current Agenvo availability/capability presentation conditions
without a server contract change. At this fixed revision, Memoh runtimes report
their connection's online flag and handshake capabilities; Agenvo supplies its
additional availability states. A live connection alone does not authorize
Herdr task execution, follow-up, or transfer capabilities.

## Confirmed shared changes since the phone's 68be baseline

These are existing upstream changes to reuse, not a new mobile implementation:

- `computer-task-checkpoints.vue` separates receipt state, history continuity,
  collection offsets, and current original availability. Unknown business outcome
  remains unknown.
- `session-task-results.vue` displays owning, direct-parent, and aggregation-root
  session references without rewriting ownership.
- `useComputerTaskObservation.ts` requires explicit observation before device
  reads; an HTTP 200 `authentication_unavailable` result also stops automatic
  polling.
- `useNativeTask.ts` and `native-task-content.vue` expose authentication pauses
  and preserve the existing follow-up gate. An unknown request outcome retains
  its original request UUID rather than submitting a replacement.

These modules are present at the fixed unified commit. Their presence does not
establish that the new Windows native-to-Herdr adapter has been integrated.

## Dependencies on the server and Windows owners

1. Preserve the existing account runtime identity, Bot grants, remote target ID,
   and authorization checks. Do not introduce a phone-specific permission model.
2. Fix the native-to-Herdr contract before changing its consumer. Existing task
   readers expect `task_type: "computer_operation"`, a matching `operation_id`,
   the current task status projection, and the `native` observation object. Any
   change requires an exact server/SDK revision; no alternate status contract is
   assumed here.
3. Continue to gate content and input through `supported` and
   `followup_available`. Existing modes are `current_agent` and `bound_session`;
   unknown outcomes must not trigger resubmission.
4. Reuse the existing delivery ledger and committed references. Native host
   output must not become attachment bytes embedded in the conversation or a
   second phone transfer service.
5. The Windows host owner must fix its installation/enrollment entry. The
   [current shared command generator](https://github.com/particleflow/Memoh/blob/fa77a073fe6380d297474a6053a1445201bcab83/apps/web/src/pages/runtimes/command.ts)
   emits the existing `@memohai/runtime@latest` enrollment and service commands;
   this audit does not invent another installer, heartbeat, or synchronization
   configuration path.

## Verification status

The adapted phone suite reported **89 passed / 4 failed out of 93** in
[targeted-tests.txt](targeted-tests.txt). The four failed assertions demand Web
anchor downloads or images using an unscoped synthetic Bot name; the existing
phone adapter instead validates scoped references and uses native save / verified
Blob previews. These remain recorded failures, not retroactively passing tests.
The same unmodified upstream attachment suite passed **8/8** against the original
shared components ([log](upstream-attachment-tests-final.txt)). The phone's actual
attachment bridge was separately exercised by the browser test below. An initial
isolated upstream run could not resolve its UI submodule alias; after attaching
the existing verified UI sources it ran successfully, without product changes.

- [18 delivery-reader checks](delivery-pages-tests.txt) passed, including partial
  page failure and malformed/unauthorized responses. The 89 passing checks above
  include scope changes, late responses, task relations, explicit observation,
  authentication stops and unknown follow-up handling.
- [Typecheck](typecheck.txt), [targeted ESLint](eslint.txt), and the source inventory
  passed. The full [UI contract guard](ui-contract.txt) still reports four inherited
  findings in `HarmonyVoiceInput.vue` / `HarmonyVoicePlayer.vue` (spinner/alpha
  styling). Its output is byte-identical to the [previous 1.0.8 guard](ui-contract-previous-108.txt).
  No unrelated voice rewrite, baseline edit or suppression was made.
- [Actual phone-adapted Vue UI](specialist-browser-result.json) at
  `http://127.0.0.1:4181`, Chromium 390x844, Chinese locale, synthetic HTTP and OS
  bridge: select the online Memoh Runtime, disable the offline PC, isolate another
  Bot's targets, traverse specialist/child tasks, show exact receipt revision and
  partial collection offset, pause polling on authentication loss, explicitly
  resume the same task, open/save scoped original attachments, navigate authorized
  history and retain login after reload. Zero task dispatch/follow-up writes,
  zero page errors. This fixture exercises the existing API contract; it does not
  prove the new Windows transport implements it.
- Screenshots inspected by the agent: [native PC selection](native-runtime-selection-390.png),
  [partial collection](collection-checkpoints-390.png), [other Bot isolation](other-bot-isolated-390.png),
  [attachment preview](attachment-preview-390.png), [dark narrow history](expert-history-390.png).
- [Preservation hashes](preservation-check.json) confirm unchanged login, injection,
  native attachment adapters, empty public config and application identity/version.
  No SDK download, HAP compilation or new binary upload occurred.

Run the existing source-context preparation for the pinned lock, then the focused
Vitest files listed in the logs. `HARMONY_TEST_BASE=http://127.0.0.1:4181 python
 docs/testing/harmony-native-runtime-fa77/browser.py` exercises the disposable,
intercepted browser fixture; it never uses a real user profile or live API.

No fixed new native-to-Herdr integration contract, actual Windows execution,
phone overwrite-install, live NAS round trip, or human acceptance is claimed.
Build the next generic HAP only after the interface and validated source are
fixed; do not rebuild or relabel the already verified 1.0.8 package.
