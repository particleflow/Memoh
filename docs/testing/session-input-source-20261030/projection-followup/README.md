# Live assistant source projection correction

Follow-up to the independent P2 review of `65649801a58946993d2824f3963255750661eb2a`. The initial candidate was not admitted: its history checks and direct `normalizeTurns` test did not prove that a live assistant retained its source. This correction is on the same `fix/session-input-origin` branch, with exact parent `65649801a58946993d2824f3963255750661eb2a`. It awaits unified review; no publication or field acceptance is claimed.

## Defect and minimal change

The reviewed `runtime-projection.ts` SHA256 was independently confirmed as `e6d5337a66851d23b84a0b4fb418702d3f86860a5310b7a0b9e26c43c839273e`. The request user turn retained `input_source`, but `runtimeAssistantTurn` and both segment construction paths omitted it. History's Go UI conversion supplied the field later, so the source appeared only on the history path.

- `apps/web/src/store/chat/runtime-projection.ts` now binds each assistant segment to the source of its **exact user turn ID**. It does not reuse the first request source for another turn. Provisional/missing/foreign source stays unknown, scoped to the receiving Bot/session. Known/unknown source validation uses the existing source fields and server semantics, not message text or the signed-in account. Snapshot and upsert annotations are copied so a later caller mutation cannot change an earlier frame's identity.
- `apps/web/src/store/chat/runtime-client.ts` passes the established Bot/session scope when its real animation-frame delta batch rebuilds the projection. Fixing only `reduceRuntimeProjection` would leave this production path unchecked.
- `runtime-input-source.test.ts` covers the actual snapshot/delta → projection → transcript controller/normalizer path and the real runtime client's batched path. It also covers six source kinds, known/unknown steer boundaries, another Bot/session/task, source clearing, frame immutability and edit replacement input.
- Two existing helpers in `transcript.test.ts` now declare their actual user/assistant return types instead of the entire `UITurn` union. This resolves the source test's overly broad spread type; runtime behavior is unchanged.

No Go, Agent runtime/contextview, SDK/API schema, SQL, identity authorization, model/provider, management control, or resume-format implementation was changed.

## Red/green evidence

The same final regression file was run against the exact two original production modules from `65649801`, then against the correction. The original modules were temporarily materialized in the exclusively owned source seam and restored in a `finally` block before subsequent work; no history or failed evidence was overwritten.

| Check | Result |
| --- | --- |
| Final 14 regression cases against exact original modules | 13 source cases failed, one scope guard passed, exit 1; missing assistant `input_source` — [red.log](red.log) |
| Corrected source regression plus existing projection/client/steer/turn-position/transcript seams | 6 files, 99 tests passed, exit 0 — [green.log](green.log) |
| ESLint on all four changed/new TS files | Exit 0, no diagnostics — [eslint.log](eslint.log) |
| Actual application Vue/TypeScript project | `vue-tsc --noEmit -p apps/web/tsconfig.app.json`, exit 0 — [checks.txt](checks.txt) |
| UI copy/contract and official Agent boundary | Passed; three inherited UI contract warnings, no new copy/control — [checks.txt](checks.txt) |

Repeatable seam test:

```sh
node_modules/.bin/vitest run \
  apps/web/src/store/chat/runtime-input-source.test.ts \
  apps/web/src/store/chat/runtime-projection.test.ts \
  apps/web/src/store/chat/runtime-client.test.ts \
  apps/web/src/store/chat/steer-turn-identity.test.ts \
  apps/web/src/store/chat/turn-position.test.ts \
  apps/web/src/store/chat/transcript.test.ts
apps/web/node_modules/.bin/vue-tsc --noEmit -p apps/web/tsconfig.app.json
```

The earlier empty root `tsconfig.json` invocation did not traverse the app project references and must not be used as typecheck evidence. This follow-up uses the actual app configuration, including the TS tests. Its initial diagnostics identified the helper return-type issue and one new test narrowing issue; both were corrected before the final passing app check. The original 105 Web tests and Go/race/PostgreSQL/model-capture evidence remain retained. Unchanged backend suites and builds were not rerun; they are reused, not represented as new executions.

## Actual application verification

The existing isolated Server binary was reused because all Go source is unchanged; current Vite source ran at `http://127.0.0.1:28083`, backed by Server `http://127.0.0.1:28080` and the retained synthetic PostgreSQL fixture database. Chromium opened the actual application with its normal REST, runtime client, projection, transcript and message component.

Only the browser's WebSocket server frames were controlled for this source seam. No model/action command was sent, and the test did not bypass the known missing workspace Hook bridge. These are controlled runtime transport fixtures, not evidence that a production scheduled model turn completed.

The same original-module run was also reproduced in the real application: the request's label existed and the live assistant's label was absent. See [before-snapshot.png](before-snapshot.png), [ui-before-evidence.json](ui-before-evidence.json). The final source was restored before the complete green UI check.

Two different Bot/session pairs (schedule and Bot delegation) each passed:

1. Snapshot assistant label/tooltip equals its exact input source.
2. A text delta retains that source through the animation-frame batch.
3. A known user steer has its own actor; subsequent assistant output follows that turn.
4. A later steer with spoofing text but no source displays unknown on both input and assistant. Its tooltip contains only the real receiving scope, without the first task ID/actor.

The real DOM/tooltip records are [ui-live-evidence.json](ui-live-evidence.json). No page errors occurred. The final screenshots are [schedule snapshot](snapshot-0.png), [delegation snapshot](snapshot-1.png), [schedule segments](segments-0.png), [delegation segments](segments-1.png), and [390×844 dark keyboard tooltip](segments-narrow-dark.png). The narrow view had no document overflow. This supplements the prior history UI evidence without claiming a history-navigation fix or fabricating persisted runtime replies.

## Compatibility and remaining boundaries

- Wire fields are unchanged: this correction consumes the existing `request_user_turn`/`user_turns.input_source`. A source-less old server/frame remains unknown. No source is inferred from a Bot name, body header, current login or latest task. A caller without an independently established receiving scope cannot treat the annotation itself as scope proof; its source remains unavailable. The real snapshot and batched client paths both supply the established scope.
- The parent source fix's **resume v4 remains incompatible with the old 9270 resume consumer**. This follow-up neither changes that version nor removes provenance to make an older reader accept it. Integration/recovery must preserve a v4-capable forward reader and the original queue/receipt/history fields. No schema downgrade, dump replacement or old-reader rollback over v4 work is justified by this Web correction.
- No deployment, paid model call, Harmony device acceptance, or human QA was performed. HAP35 does not include this correction; the original Harmony owner must align it later. PC57 is untouched.
- The compatibility register at exact `3a2e5d97e7f26fdcd47195f0f89bbb5c852b01a5` was read first. Existing C05/process and contact-bind 403 restrictions were not retried or probed. Prior navigation/refresh and workspace bridge blockers remain as recorded in the parent evidence.

Human QA: not verified. Unified independent re-review is pending.
