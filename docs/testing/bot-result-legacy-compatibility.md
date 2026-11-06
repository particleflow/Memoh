# Unknown result delivery opt-in boundary

Independent review found that the result-delivery slice admitted `unknown`
notifications on legacy paths even when the persisted record had no
`result_delivery_version: 1`. That unintentionally extended the new Web/native
behavior to old records and unsupported ingress/runtime types.

The follow-up restores the old boundary at execution completion and delivery
retry. Unknown execution is retained, but notification requires the persisted
opt-in. The host also rejects unknown records on its legacy enqueue and queued
admission paths; opted-in unknown reports continue through the receipt-aware
path. Completed/failed legacy delivery and all record access checks are unchanged.

Executed evidence in `/workspace/bot-dialog-handoff`:

- `legacy-unknown-red.log`: the new legacy regression failed before the fix with
  one unexpected delivery and delivery state `accepted`.
- `legacy-unknown-green.log`: six focused top-level tests passed, including
  legacy no-delivery/no-retry, opted-in unknown notification, deadline handling,
  no specialist redispatch, original-route checks, and queued legacy admission.
  Existing native tests now explicitly expect notification for their new
  opted-in records; the new regression separately covers unopted legacy records.
- `legacy-unknown-runtime.json`: actual PostgreSQL row with the opt-in field
  omitted; current Server build returns authorized detail `unknown`, delivery
  retry returns HTTP 409, restart adds no message or run, and the complete legacy
  row remains unchanged. No model provider was running.
- `legacy-unknown-directory.png`: actual Chromium at
  `http://127.0.0.1:28082/bot/local-dialog` shows the retained record as
  `Delegated task / Execution status unknown`. This synthetic record has no target
  session, so its existing directory link is disabled. No new contact bubble.
- Build, gofmt and `git diff --check` passed. No broad crash-window suite rerun,
  deployment, HAP signing, production mutation or paid model call.

The browser harness initially assumed the task body was its directory name,
then assumed a shorter status label. It was corrected to the actual authorized
catalog ordering and existing localized labels. The same synthetic record was
reused throughout; no duplicate task was created.
