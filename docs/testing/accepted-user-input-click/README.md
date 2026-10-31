# Accepted-answer hint integration

Base: `40765b9e0979d561c49e58d2f384799e6c589e27` (six CI workflows succeeded).
Backend source `2782ee0607967f6eff0f7df48cd72d195181551b` is preserved by cherry-pick `389698bb`; prerequisite daba5a37 was already included and is not repeated. No migration or new runtime/queue/service: schema177 and existing ownership/admission remain.

## Existing hint, original identity

Only the existing reply error surface becomes clickable, using the shared CalloutBanner's existing `clickable` behavior. No additional button, banner or toast is added. Once the identified error is visible at its reply, the old composer copy is consumed, so later history/recovery reconciliation cannot resurrect that top banner; independent errors remain unchanged. The input must have one normalized `userInput.status=submitted` decision in that same failed assistant reply. Pending/canceled inputs do not acquire this action. UI status is not authorization: the server rechecks the account, session creator, Bot permission, original checkpoint, fence, tail and absence of effects.

The original run comes from that message's runtimeRunId, or a synchronized runtime observation whose turn_id exactly matches that message and whose state is errored. Another/latest run is never substituted. Missing original identity remains non-actionable rather than guessed. The clicked identity stays immutable while its request is pending.

```http
POST /bots/{bot_id}/sessions/{session_id}/decisions/{decision_id}/recover
{"original_run_id":"<original failed run UUID>"}
```

Generated SDK: `postBotsByBotIdSessionsBySessionIdDecisionsByDecisionIdRecover`.
202 is admission only. Verify returned original_run_id/decision_id and retain recovery_run_id/invocation_id. The same hint then shows the actual recovery run state from:

```http
GET /bots/{bot_id}/sessions/{session_id}/invocations/{invocation_id}
```

GET must match returned run/session/invocation identities. Missing or foreign identity stops observation as unknown. Transient disconnection rechecks the same GET; there is no automatic POST on mount, reconnect, polling, hiding/reopening or deployment. A second click after admission also reads GET, not another POST. An unknown POST may be explicitly repeated with the exact original IDs; the server's durable invocation/CAS provides idempotence. 400/401/403/404/409 stop this request in the same hint, never resubmit an answer or fall back to whole-turn retry. Failed/lost recovery runs remain facts and do not start another run. The UI never rewrites the original message, accepted answer or error history.

## Shared consumer files

Consume the final unified snapshot, preserving Harmony's existing shell/media/login adaptations:

- `apps/web/src/pages/home/composables/useAcceptedUserInputRecovery.ts`
- `apps/web/src/pages/home/components/message-item.vue`
- `apps/web/src/pages/home/components/chat-pane.vue`: exact original-run identity and existing visibility/read-only scope passed to MessageItem.
- One `chat.tools.userInputRecoveryState` interpolation in en/zh/ja.
- The generated SDK source/index/types/colada and new endpoint's Swagger from backend source2782ee06. Do not hand-edit generated files or ship the new client ahead of the matching server route.

The prior conversation-scoped Task/Bot lists are retained. No directory, transfer, model selection or Agent core changes are part of this UI patch.

## Validation

- 26 directed tests across the recovery composable, real MessageItem/CalloutBanner click rendering, existing edit actions and narrow failure deduplication passed. Covers unanswered vs submitted (including stale raw transport fields), double click, exact original IDs, 202 admission, same-run read/reconnect, 401/403/404/409, unknown POST/explicit same-ID repeat, foreign identity, cross-Bot/session or same-token re-login late responses, terminal failure/history preservation and hiding/returning without a new POST.
- Whole Web types, production Vite build, affected ESLint (zero errors; three unchanged optional-prop stub warnings in message-item.test.ts) and UI contract passed. Normal build chunk warning and three pre-existing UI contract warnings remain. Normal `go generate ./internal/handlers` followed by `pnpm run generate-sdk` reproduced the source Swagger/SDK with no additional diff; official Agent runtime/contextview boundary remained unchanged.
- `TestAcceptedUserInputRecoveryHTTPUsesAccountAndOwnedSession` passed in the combined handler package (11 owner/auth/scope/body/error subcases). Source2782ee06's PostgreSQL/race/unknown-commit evidence is retained at `docs/evidence/accepted-user-input-recovery/README.md`; that full source matrix was not repeated here.
- Chromium ran the actual production build at390px with synthetic HTTP and WebSocket responses. One existing hint was clicked/double-clicked: one positive POST, then three same-invocation GETs showed accepted/running/completed, not completion inferred from202. A separate409 scenario issued one explicit POST and kept a single non-retrying inline hint. No user_input_response, retry_message or model message was sent over WS. Two total POSTs belong to these two separate scenarios. Zero JS exceptions; no real provider/device/production request. Initial harness selection of a hidden mobile sidebar item timed out; selecting the conversation at desktop width before narrowing corrected the harness, without product changes.

Synthetic screenshots: [before](before.png), [admitted](admitted.png), [409](denied.png). They contain fabricated fixture data only.

Human/real-device QA, the original failed request's actual recovery and business outcome remain unverified. No paid request, production write, HAP build/signature, formal image publication or deployment was performed by this integration.

The final full-repository Go test exposed a request-field literal contract missed by the directed handler package checks. The subsequent Go-only correction and original failure/red-green evidence are recorded in [field-guard](field-guard/README.md). Web/Harmony and API/SDK product files remain byte-identical to f4ef1b5d; no client rebuild is needed for that correction.
