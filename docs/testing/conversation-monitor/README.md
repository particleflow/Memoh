# Conversation-scoped monitor checkpoint

Base: `9d91623e1f3325066adddfa8a11db16cef0d46a7`.

The conversation footer now opens scoped Task and Bot lists rather than the global sidebar. Rows show only names, original status and navigation. Existing session ancestry and delegation source/target session IDs establish membership; there is no new runtime, queue, permission, API or migration. Unknown stays unknown. The global activity sidebar is unchanged.

## Shared consumer handoff

Reuse these shared files, without replacing the trigger with the old mobile global-sidebar opener:

- `apps/web/src/components/sidebar/session-activity-trigger.vue`
- `apps/web/src/pages/home/composables/useConversationRelations.ts`
- `apps/web/src/pages/home/components/conversation-relation-row.vue`
- The two footer trigger instances in `chat-pane.vue` (`kind="bots"` selects related Bots).

The trigger accepts optional `botId`, `sessionId`, `visible` and `kind`; omitted scope uses the existing injected chat-view target. Existing authorized session/tasks/delegations GET routes supply the data. Navigation revalidates the actual session identity and uses the original Bot route and workspace chat tab. No device observation, workdir lookup, model request or mutation is needed for the list.

Harmony's builder currently copies `mobile/client/mobile/memoh/shared-session-activity-trigger.vue` over this shared path. That old overlay calls `openMobileNav()` and must stop overwriting the fixed shared implementation. Preserve the mobile shell, login, media bridge and other adaptations; do not import its entire tree into Web. No HAP was built by this checkpoint.

Synthetic production-build screenshots: [desktop](desktop.png), [390px mobile](mobile.png). These contain only fabricated fixture records; they are not field screenshots.

## One failure surface

The same identified `runtime_run_failed` already present in its assistant reply suppresses the composer duplicate. Frontend-only run/turn metadata follows the existing send/retry/edit result. Other errors, standalone failures and failures belonging to a different run/turn remain visible. There is no matching on translated text. The underlying reply/error history is unchanged.

The remaining inline hint is NOT wired to whole-turn retry. An accepted answer followed by a failed continuation cannot be safely recovered by repeating the answer, `retry_message` or the lost-run recovery path. The backend owner is implementing the original decision/run recovery contract; clickable continuation remains unfinished until that contract is available.

## Provenance of the duplicate surfaces

These are official-baseline ancestors, not additions made by this checkpoint:

- `349a954c04fdd7b3b2624d19f36fda21934c6e8d`, qqqqqf, 2026-07-22 18:55:39 +08:00: adds `composer-panel-error.vue`.
- `29e542ea90d215d1f2275cb845bedf40e713e188`, qqqqqf, 2026-10-05 15:47:46 +08:00: current dismissible composer card and CalloutBanner styling for the inline error.
- `dead2cdfccc51408e3194105a921299c5557e90a`, 东云, 2026-08-29 04:16:58 +08:00: current inline error-rendering branch; earlier inline history also exists, so this is not claimed as its first-ever introduction.
- `2078440995365987780e643181dc68e0f73db95a`, 晨苒, 2026-07-28 22:24:46 +08:00: the separate rejected-control `showError`/toast path. This is distinct from the composer's dismissible banner.

The screenshot's two layouts match the inline reply and composer error components. Their independent paths lacked a shared run/turn deduplication check. No captured client trace proves which composer assignment produced that particular field screenshot or the first commit that ever caused both paths to fire. No subjective authorization/intent attribution is made.

## Validation and limits

- 194 tests across chat store, runtime integration, composer failure matching, send-context handling, scoped reader and actual shared dialog component passed. After the final one-close-button correction and store identity assertion, the four affected files passed 168 tests again; the unchanged other two account for 26 tests.
- Whole Web TypeScript, affected ESLint (zero diagnostics), production Vite build and UI contract passed. UI contract reports three pre-existing warnings outside this change; the build retains its normal chunk-size warning.
- Chromium ran the production build with synthetic, read-only API responses. Actual Task/Bot entry clicks, three-level nesting, unrelated Bot exclusion, child and expert original-session navigation, 390px layout, and zero API writes passed. Desktop and narrow screenshots were inspected. No real server/PG, provider, device, model, production or HAP acceptance is claimed by this browser fixture.
- Early fixture shape errors were corrected in the test harness, not product code. An external Google Fonts certificate request failed under the environment's existing trust settings; trust/TLS was not relaxed, and normal font fallback rendered the page. The final browser run has no JavaScript exception; an unused starter-prompts request aborted during navigation.
- Human/field QA is not complete. No paid call, accepted-answer replay, image generation, production deployment or new package signing was performed.

The first staged Web check stopped because this environment's pnpm 11 tried an unsolicited dependency install into an unavailable home path. No product/dependency configuration changed. Reusing the already verified installed dependencies with `pnpm_config_verify_deps_before_run=false node .husky/checks.mjs` ran the same lint-staged/ESLint check successfully (6.17 seconds); this disables auto-install, not lint or a repository gate. Initial added test-fixture typing errors were corrected to the actual assistant-turn schema without changing product behavior.

The adapter source `daba5a37a4804cd6c7659a1bdc290a24ddbe03d2` is combined separately with origin recorded by cherry-pick. It publishes an existing deferred retry/edit replacement before answer continuation. It does not provide recovery for a historical failed run. Combination checks use directed application tests and the existing core boundary; source PostgreSQL/race evidence remains in `docs/evidence/deferred-decision-replacement/README.md` and is not claimed as rerun here.
