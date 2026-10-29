# Exact HAP1.0.28 entry/contact/title source handoff

For the existing Bot owner only: inspect first-binding/cold-start continuity and compare the separately owned cc81049 fix. No Bot repair is implemented here. Physical installation of28 is **unknown**.

The built product source is **12faf4c06db01d8fffc29aad3a247b4a85e884e7**, effective shared **5a42d013e801cb6229ee029113e3a63782bc3e13**. Later013e91 cancellation source is deliberately excluded. The current source branch may be newer; do not use its mobile overlay inputs to infer what28 contains.

## Read the exact bytes

[Small existing-source ZIP](garnet-assistant-1.0.28-entry-contact-source.zip):278973 bytes, SHA256 `564de7cf9619bf216090b3702e882bf63e64e7528bcb1145fd56298cec549327`.52 members:51 selected source/license files plus the complete source-map. [Every path, Git blob, SHA256, size and source reference](source-map.json). [ZIP verification](package.json).

`fixed-source/` files are taken using git show at12faf4c0; their blob IDs exist at that exact source reference. `generated-source/` contains the retained actual Web producer context for28, compared byte-for-byte with the saved checkout, not the repository's older top-level apps/web tree. For generated files that exactly equal5a42, source-map also supplies a directly readable Git source reference. Other generated-file Git blobs are **computed content identities**, not a claim that their raw apps/web path exists at12faf4c0; read their actual bytes from this ZIP. The subset is for source review, not an independent build checkout. Original LICENSE is included; SDK, tool binaries, signing materials, actual storage values and private deployment config are excluded.

## Actual entry and selection map

Paths below are relative to `generated-source/apps/web/` unless explicitly marked native.

| Consumer | Exact file(s) | Existing28 behavior to compare |
| --- | --- | --- |
| Native cold start | fixed-source/mobile/client/mobile/harmony/app/entry/src/main/ets/entryability/ClientAbilityStage.ets; entryability/EntryAbility.ets; pages/SharedChat.ets; common/SharedWebPolicy.ets | Registers persistent secure begonia-client scheme, loads SharedChat with WEB_START=begonia-client://app/index.html; original JavaScript/storage policy remains. |
| Native configuration command | same native common/NativeClient.ets and common/MemohClientConfig.ets | Existing configure/configuration commands and generic defaults. These are public empty source inputs, not the Windows-injected private package or actual phone settings. |
| Web entry | index.html; src/lib/harmony-entry.ts | HTML enters harmony-entry, installs Back, reads packaged deployment config, applies existing local service migration, configures native bridge, then imports main and router. It does not contain a new Bot-binding implementation. |
| Authentication/config | src/lib/harmony-service-config.ts; src/lib/harmony-service-url.ts; src/lib/auth-session.ts; src/lib/api-client.ts; src/router.ts; src/store/user.ts | Existing local config/TLS/legacy binding and token-based auth; reset cleanup is conditional on resetPending. Hash router and existing onboarding/auth guard remain. No actual token or address was read. |
| App shell and Home | src/main.ts; src/App.vue; src/components/main-container/index.vue; src/pages/home/index.vue | Home stays mounted behind settings, route-gates synchronization/title. Without route Bot name, it uses initializeWithRecovery when no currentBotId, otherwise selectBot(currentBotId). Explicit session/task URLs follow their separate history branch. |
| Cold-start Bot/session seed | src/store/chat-selection.ts; src/utils/tab-scoped-storage.ts | chat-bot-id uses tab-scoped storage with a localStorage cold-start seed; chat-session-id has no cold seed. Explicit selection/draft intent are separate. This does not prove a first-time phone already has a Bot seed. |
| Bot fallback | src/store/chat/bots.ts; src/store/chat-list.ts | Existing usable Bot id is retained; otherwise list fallback prefers a ready Bot. This is observed source behavior, not a new approved first-binding policy or proof of the field outcome. |
| Fixed contact/open/reopen | src/store/chat/bootstrap.ts; src/store/chat/session-list-init-recovery.ts; src/store/chat/session-list.ts; src/store/chat/session-actions.ts | Actual existing resolveContact/openContact, selection request and user generation fences. Primary openContact resolves server-owned contact directly; legacy inspect/bind paths remain distinct. Follow these real functions when comparing cc81049, not an assumed local session key. |
| Contact API wrapper | src/composables/api/useChat.ts; useChat.chat-api.ts; useChat.types.ts | Existing POST sessions/contact, GET sessions/contact and POST sessions/contact/bind wrappers, plus Bot/session ownership checks. No new endpoints are introduced by this source handoff. |
| Bot display name/selection | src/components/sidebar/bot-switcher.vue; src/components/sidebar/index.vue; src/composables/api/useBot.ts |28 switcher currentLabel uses currentBot.display_name or currentBot.id; select switches store and routes via bot.name or id. Actual values are not supplied here. |
| Document/tab titles | src/pages/home/index.vue; src/store/workspace-tabs.ts; src/pages/home/components/chat-workspace.vue; components/dockview/panel-chat.vue | Home document title uses activeSession.title, then routeConversationLabel, with Garnet Assistant app title and route/tentative-session gates. Dockview/restored tabs consume their stored session title. These must not be conflated with the Bot switcher's name source. |
| Phone/task navigation adaptation | src/lib/harmony-back.ts; src/pages/home/components/chat-pane.vue; src/pages/home/composables/useTaskRunAnchor.ts; src/utils/task-history-link.ts | Exact28 native Back, task admission and shared Home speech gate. No later15-second cancellation delta from013e91 is in this source ZIP. |

The native prefix shown in the first row is shared by the remaining native relative paths in that row. `source-map.json` contains their full unambiguous archive paths; this table is a navigation aid, not substitute source.

## Existing artifact/source binding checked read-only

Original HAP remains37660218 bytes, SHA256 `67164ea48d41c766f58fe38f6f7b84dc5e37c87b05fdfe28c0578ef879bb79db`. Its embedded source-build.json binds the saved build.json SHA256 `f4a628b7782148d4388c66f3ce3d6638ba764d056c6b211779c62687e463609a`. All78 recorded producer inputs match fixed12faf4c0 Git bytes. Selected generated files match the retained official-context.tar and saved28 checkout. The original28 verification already bound produced resource hashes to HAP raw resources; no frontend/native build was rerun. TypeScript source is not claimed to be embedded verbatim inside the minified HAP.

This documentation/source archive neither changes28 nor rebinds it to the newer source head. No version bump, tests, model call, signing, Windows/NAS operation or public HAP upload occurs. Original generic unsigned28 and later source checkpoints remain unchanged. First-binding/title/history repair and actual device acceptance stay with the existing Bot owner and field writer.
