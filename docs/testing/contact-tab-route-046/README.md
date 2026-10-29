# Contact tab URL synchronization after 046

Switching from a task to the pinned main-contact tab changed the visible session but left the task's `session` and typed run query in the address bar. Refresh then reopened the old task. The same-Bot change never triggered the existing `currentBotId` URL watcher.

Product commit: `2c0e277c2cb49b5a4a99b89a5e568ef9f413ab8a`, directly based on deployed `046023913592b00951d7f28fe54842080c5cdc51`. Cherry-pick this single product/test commit; the following documentation commit is optional. No backend/schema/Agent-core changes.

`apps/web/src/pages/home/index.vue:363` now mirrors an activated chat tab into the injected router with replace. Contact tabs clear only session/task/run query fields; unrelated query fields survive. History tabs restore the original session and typed task anchor. Existing route resolution rechecks server identity and authorization; panel data is not authorization. Layout restoration, pending/failed entrances, loading, non-chat tabs and superseded asynchronous Bot-name lookups cannot overwrite the current route. No contact record is rebound, reset, merged or deleted.

## Differential verification

- Before product edit: 2 new failures / 19 passes, no skips. Returning to contact retained the task URL; activating a task tab did not restore its anchor.
- After edit: 24 navigation tests passed, no skips. Seven added cases cover contact/task URL synchronization, unrelated query preservation, pending/denied entrances, denied task-tab navigation, non-chat tabs, and late lookup completion after a newer route/tab.
- Scoped ESLint, Web vue-tsc, and git diff --check passed. No application build and no old directory/PG suites rerun.
- Actual Chromium at `http://127.0.0.1:28110/bot/local-dialog`, real Vite application from 046 plus this patch, existing isolated PostgreSQL records, reused local 37e backend. `internal/sessiontask` and `internal/handlers/bot_task_directory.go` are byte-identical between 37e and 046. This is not a newly built exact-046 backend or field acceptance.
- Red browser: contact C visible while old task URL remained; refresh reopened the old task. Green browser: contact tab clears task query; refresh and copied contact URL retain contact; switching back to the task restores its original run; task refresh visibly anchors the original turn. Zero page errors. Screenshots were inspected after the loading mask settled.
- Narrow 390×844 / zh / dark: copied main-contact URL opens existing contact, composer remains visible. No page errors; session/run/message row counts and sorted-ID digests unchanged (24/76/335). These are ID/count checks, not hashes of every field.
- Browser write requests were only the existing idempotent contact-resolution endpoint. No message send, new task, schedule mutation or model call. Browser fixtures reuse synthetic history from prior tests; displayed background-result text is not new parallel/model execution evidence.

## Rendered evidence

- [Main contact selected](green-contact.png): original ongoing conversation and composer; exact clean URL recorded in green-browser.json.
- [Task selected after refresh](green-task.png): original TASK_PROJECTION_V6 turn and retained tool history, with newer turn also preserved below.
- [Narrow zh/dark contact](green-narrow-zh-dark.png): existing contact and writable composer at mobile viewport width.

`browser.py` and `narrow.py` require the existing private local fixture harness/auth; credentials are not distributed. Raw server logs are excluded.

## Field handoff

After the normal combination/release process, the unique field writer can reuse the same existing C/task sample: task → original contact tab → inspect clean address → refresh/copy link stays C → task tab restores the original task/session/run. No new task or paid call is required. This patch has not been deployed, tested on the field device, or verified in a packaged HAP. Shared source is changed; native packaging/integration remains with its owner.

Parent-reported directory acceptance on 046 remains valid for its samples (E=12/X=0/U=0, limit2 six pages equals limit100, old/new run navigation, cross-history unknown, cross-Bot delegation, unchanged 17 business-table digests). Not rerun here. Still untested in the field: incoming delegation; failed/schedule samples; missing execution association; admission/execution coexistence deduplication; UI paging beyond20; restricted account; empty page carrying a cursor. New paid delegation/parallel-message approval remains unanswered; none was attempted.

- [ ] Human QA passed
