# Pre-send parallel acceptance recorder

**Consumer correction (2026-10-30):** The Python/Playwright recorder below is historical cloud evidence and must not be run in Ego2.1.0, whose supported workflow prohibits importing Playwright or launching another browser. Use the [Ego Page adapter and one-time contract probe](ego/README.md). Its cloud fixtures pass; actual Ego envelope/retention readback is still pending. Parent now reports Server/Web9270 healthy. No new paid field test was run for this adaptation.

This is a **local, one-shot acceptance observer**, not a product change, persistent service, scheduler, model client or browser UI. It addresses the lost A identity and lost B queue response in the parent-reported field run. It does not execute another paid test. No SQL, database credentials, recovery POST, new expert task or model configuration is present.

Contract baseline: Server `3f361e38a0a30fe52248c9361ead008c052ef2ea`. The newer9270 image was reported released but not applied when this work was requested; no assumption about its deployed status is made. The real `dedda02f-1510-417c-baf3-1ddd466d7c7e` sample completed with visible replies, but overlap remains unproven. Its field files were not read here: their contents are parent reports.

## What is recorded

`capture.py` attaches to an existing locally controlled Chromium page through the writer's supported CDP endpoint. It does not launch or close the writer's browser. Install/use the writer's existing Python3.11+ and Playwright package; the recorder does not require a new browser installation. It uses standard local file writes, not Library or `os.setxattr`.

It records the selected source contact's HTTP request bodies and **complete asynchronous response bodies** with a per-request sequence ID. It also records source Bot WebSocket frames, including frames without `invocation_id`. HTTP and WS credentials, headers, endpoint host/root, URL queries, known auth values and URL strings are not persisted. The actual origin/root remains in process memory for GETs. Conversation content remains sensitive: keep JSONL in the writer's restricted local evidence directory, review it before sharing, and do not upload raw user history as a public artifact. Redaction retains identity/structure but the file is not a byte-identical wire dump. No raw headers, HAR, CDP URL or private server address belongs in the handoff.

`arm A` / `arm B` bind the next actual source-contact UI send by transport identity, **not by matching text**. A WS send records its original `invocation_id`. A queue POST records its original request invocation plus the matching202 response's `item_id` and `enqueued_during_run_id`. The admitted queue invocation is the existing protocol `follow-up:{item_id}`. Interleaved responses cannot replace this binding.

GET observation uses the existing page login, copying an already observed authorization header in memory and using browser cookies. Start before A and reload the existing page manually so fresh API and WebSocket events are visible. Arming is refused until a fresh source socket and successful source-contact GET have been observed. Use only the selected existing fixed-contact page; no other task submissions, account switches or configuration changes during the one sample.

The observer automatically reads exact invocation identities and A's exact `turn_id` history. Tool outputs may also arrive in runtime frames with only `run_id`; these are retained and correlated after A's admitted run is known. Only delegation views belonging to this exact A are tracked; unrelated old history is not used to select a task. No latest-run or text search fallback exists.

When B is completed, the observer awaits its history GET, selects its exact turn, and only then requests the tracked expert state. An `overlap_candidate` contains the three ordered evidence sequence numbers. It is **not an automatic PASS**: the writer must confirm the B turn contains a final substantive natural reply (not progress/tool/empty output) and the final GET identifies the same actual expert run as running. An incomplete history window produces `history_window_missing`, not guessed evidence. If the expert is already terminal, no overlap candidate is emitted. It is possible for a short real task to complete between observations; that remains INCONCLUSIVE.

## Writer operation — start before A

1. Preserve the existing field baseline and selected Bot/contact IDs. Use the existing approved browser observation session; do not expose or open a new public debugging port. Select the intended page index among the existing CDP browser contexts/pages. The command below deliberately contains placeholders, not production addresses or IDs.
2. Start the recorder **before sending A**:

   ```text
   python capture.py --cdp "<existing local CDP endpoint>" --page-index <index> --api-root "<existing API root>" --bot "<source Bot ID>" --contact "<fixed contact ID>" --output "<new restricted local path>.jsonl"
   ```

   The output must be a new file; existing evidence is never overwritten. The supplied API root is exact and may include an existing path prefix; `/api` is not invented. Use `--interval` to adjust GET observation cadence and `--timeout` for response/probe timeout. These are observer controls, not a duration or success condition imposed on the task.
3. After `LISTENING`, manually reload the same fixed-contact page. Wait for its ordinary connection/history load. Enter `arm A`; only after `ARMED` send A **once** using the normal UI and the parent's already authorized bounded text-delegation procedure. If arming says not ready, do not send A. Keep this terminal running.
4. The JSONL now records the original A request, acceptance/runtime frames, exact A run/turn lookup and original delegation tool output. Observe the real specialist through the existing task/delegation view. When it is actually running, enter `arm B`, wait for `ARMED`, and send the one independent short B question using normal UI. The recorder does not send A/B, hold the worker, bypass the queue or control model execution.
5. Let the existing task and natural result return complete. Automatic GET samples wake on observed frames/queue responses and otherwise follow the configured observation cadence. `sample` requests an immediate GET cycle; it never sends a business request. Refresh the original contact once after the result is visible, keeping recording active. No resubmission/recovery/extra model test is part of this script.
6. Enter `stop`. The recorder awaits already observed requests and pending full response bodies up to the explicit timeout, records every missing/failed body as unknown, removes its listeners, flushes and closes its new file. It does not close the user's browser. An early disconnect/timeout cannot be treated as failed business execution; inspect the preserved evidence and original identities before any further action.
7. Hand back a redacted evidence subset and the outcome fields below. Do not publish auth data, real private addresses or unrelated history. If the result is INCONCLUSIVE, stop the sample; do not run repeated paid attempts to manufacture a window.

## Public contract and exact evidence chain

- Main message is a WS `type=message` with original `invocation_id` and `session_id`. Preserve admission events; later runtime events can name only `run_id`.
- Queue POST: `/bots/{bot}/sessions/{contact}/follow-up-queue`, body `{invocation_id,text}`. The202 response contains `{item_id,status,position,text,enqueued_during_run_id}` (fields as returned). Keep both whole sanitized bodies and their shared `request_id` in this recording.
- Exact lookup: `GET /bots/{bot}/sessions/{contact}/invocations/{encoded invocation}` returns `found`, `invocation_id`, `session_id`, and when found `run_id`, `turn_id`, `turn_position`, `state`. B's queued invocation is `follow-up:{item_id}`. `found=false` is not permission to resend.
- History: `GET /bots/{bot}/messages?session_id={contact}&limit=100` returns `{items:[UITurn...]}`. Match `turn_id` from exact lookup. Do not use the first/latest turn or text equality. The script does not add a new pagination API: if the exact turn is outside this bounded window, it flags the gap for supported existing pagination/readback.
- Delegation: `GET /bots/{bot}/sessions/{contact}/delegations/{delegation_id}` returns the real `target_bot_id`, `target_session_id`, `target_run_id`, `target_run_state`, `state`, `delivery_state`, `summary` and existing view timestamps. Source session and target fields must match the original A tool output and subsequent same-delegation observations.
- `overlap_candidate` ordering is: B invocation completed response → B exact persisted turn response → request/response for the original expert still running. All are awaited in one observer's sequence; no client/server clock comparison is needed. `updated_at` is not promoted to a dedicated completion/commit timestamp.

Relevant source: `internal/handlers/session_queue.go:324-345,613`; `internal/agent/application/session_queue.go:99-107`; `internal/agent/application/queue_continuation.go:338`; `internal/handlers/session_invocation.go:26-67`; `internal/handlers/message.go:71-73,185-200`; `internal/botdelegation/service.go:35-52`; `internal/handlers/local_channel.go:855-881,990-1002`; `internal/agent/event/event.go:70-90`.

## Remaining public-contract gap — no DB fallback

Capturing the response gives a complete supported path from queue item to actual durable run/turn, so this acceptance collector needs **no new product API** for the normal case. The public APIs do not expose the internal claim token or exact dequeue timestamp; this script does not claim to measure them. `enqueued_during_run_id` plus exact run observations identifies which A B queued behind.

If the original queue response is genuinely lost, the client has only the request invocation. Current `GET /follow-up-queue` does not expose that original invocation, and `session_queue.go:107` generates the item with `uuid.NewString()`. The script cannot derive its item ID from the request. It records `queue_unresolved`, `http_failed_unknown`, `http_body_unknown` or `http_response_unknown_on_stop`, with the original request. It does not match equal text, look at the latest run, repeat the POST or read a private database.

If product-level recovery of **that lost-response boundary** is later requested, the smallest contract work belongs to this Bot/queue owner: an authorized exact original-request-invocation lookup returning the retained item/admitted invocation/run mapping, with truthful unknown when not retained. Do not invent deterministic IDs client-side or claim this read-only collector adds that contract. No such product change is included here.

The earlier event-specific SQL authorization is not extended. Public message projection does not expose `task_result_receipt`; delegation view does not expose internal `report_state`. Receipt uniqueness across attempts/sessions remains a separate specifically authorized evidence step. The old event's repeated POST409 was already field-verified and is not retried here.

## Evidence outcome fields

Record Server/Web revisions, source Bot/contact, A original request invocation and run/turn, raw A tool-call/result event sequence, delegation/target session/run, B original invocation, queue request/response sequences, item ID, `enqueued_during_run_id`, admitted invocation and run/turn, B final-text review, ordered overlap sequences, original expert terminal/result, source-contact result reply turn, refresh comparison, unknown/error records, actual usage and each PASS/FAIL/INCONCLUSIVE separately. `overlap_candidate` alone never checks the final text or whole goal.

## Verification performed here

`test_capture.py` runs a local HTTP/WebSocket fixture and real headless Chromium. It makes zero paid/model/production calls. `fixture-result.json` records the successful run. It verifies pre-A WS capture, tool frame without invocation, unrelated-history exclusion, a split delayed202 body with an earlier unrelated409, exact queue/run/turn mapping, causal read ordering, no candidate after worker terminal,403 blocking, transport unknown, draining a partial response on stop, credential/address redaction and a lost queue response without invented identity.

Initial fixture run exposed use of an unsupported Playwright `remove_all_listeners` method; fixed to remove each registered listener. A subsequent overly strict fixture assumption of exactly three server POST arrivals failed: Chromium retried a disconnected fixture transport. Final evidence preserves three explicit fixture POST calls versus four server receipts; the recorder itself initiated zero POSTs. This distinction is not a claim that production duplicated execution. Both failures are retained here rather than erased as an all-green first run.

Only this new collector's fixtures and Python syntax/whitespace checks ran; no unchanged product suite or build was repeated. The actual field overlap goal remains unaccepted until a valid real sample provides its evidence.
