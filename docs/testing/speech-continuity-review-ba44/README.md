# Independent speech continuity admission review — ba44c062

Date: 2026-10-30. Current unified source: `1fa17fd72aca0f8ee4ab46f1578a369fb039016b`.
Candidate source sequence: `e57d218b34ccdd8bf1e55b790de62f7d73de6eb5` →
`ba44c06261fb3be30557160840bc25e339cab650`, based on cefdf0. The latter parent
is evidence commit `043da08717c12a59f16814830318dda9759c9509`; final product tree
`b38fb15c22262d77dcc9ad964f6b25df442895c1`. Evidence head is
`d90f0536e52ad782f0436abccb26994ad4d1bb3a`, especially
[CURRENT.md](https://github.com/particleflow/Memoh/blob/d90f0536e52ad782f0436abccb26994ad4d1bb3a/docs/testing/chat-read-aloud/continuity-20261030/CURRENT.md).
The combined product patch SHA256 matches the author's delivery manifest:
`f823c48bdf90e9d48047a01b1fd167394ff46a47c6ccac2cb6b7ec0da990916a`.

**Not admitted: one independently reproduced P1 remains.** No product source was
changed, pushed, built, released or deployed by this review. No provider call or
field browser was used. The current navigation Web release remains independent.

## P1 — missing reconciled runtimeRunId repeats the late final tail and invalidates its cache

Affected candidate file: `apps/web/src/pages/home/composables/chat-read-aloud.ts`.
In `LiveSpeechReplies.update`, line115 reads the spoken cursor using the
previously observed revision when a settled row omits runtimeRunId, but line127
always writes it using the current revision. For the same rendered row:

- Streaming identity: row `same-render-row`, turn `original-turn`, run
  `original-run`; effective observed revision `same-render-row:original-run`.
- Speak the visible prefix `Original sentence.` once while streaming.
- History reconciliation preserves the render row/turn but omits runtimeRunId.
  This is an existing real transcript contract, also exercised by the author's
  `useChatReadAloud.cache.test.ts` settled-history reconciliation test.
- The final renderer exposes `Original sentence. Delayed tail.`. The first
  update correctly emits ` Delayed tail.`, but writes the new cursor under
  `same-render-row` instead of the observed revision it will read next.
- A second collection with unchanged settled text emits ` Delayed tail.` again.
  MutationObserver, post-render collection or another settled snapshot can
  legitimately collect the same visible row again; this is not a new reply.

The duplicate is eligible for another queue entry. No actual production fee or
provider request is claimed. The separate actual-cache counterexample captures
the emitted parts into EOF-completed same-identity audio streams: retained run
identity yields `ready`; omitted run identity yields `changed` because the
duplicate source span no longer equals the final visible text. Thus automatic
coverage and completed cached replay are both affected. This is not declared
the cause of the user's actual phone complaint.

## Exact narrow reproduction

Copy [counterexamples.test.ts.txt](counterexamples.test.ts.txt) to
`apps/web/src/pages/home/composables/speech-continuity-independent.review.test.ts`
in a clean checkout of the exact candidate. It contains no credentials or network
client. Use the already installed Node24/Vitest toolchain; do not download a new
tool or modify system paths. The original task used:

```sh
NODE_OPTIONS=--no-experimental-webstorage /opt/codex/runtimes/codex-primary-runtime/dependencies/node/bin/node node_modules/vitest/vitest.mjs run apps/web/src/pages/home/composables/speech-continuity-independent.review.test.ts --reporter=verbose
```

The initial final-DOM pair produced **1 pass / 1 fail, exit1**:
expected no new entries, received `{key: original-turn, messageId: same-render-row,
text: ' Delayed tail.'}`. See [first red](final-reconciliation-red.log.txt).
After adding the cache cross-check, only that new group was run:

```sh
NODE_OPTIONS=--no-experimental-webstorage /opt/codex/runtimes/codex-primary-runtime/dependencies/node/bin/node node_modules/vitest/vitest.mjs run apps/web/src/pages/home/composables/speech-continuity-independent.review.test.ts -t 'independent cache coverage' --reporter=verbose
```

Result: **1 pass / 1 fail, exit1**; retained-ID ready versus omitted-ID changed.
The two previous tests were deliberately not selected, not waived or counted
as qualification. See [cache red](cache-coverage-red.log.txt). No original32/20
test suite or broad build was rerun. The first pnpm invocation failed before
Vitest because automatic package-manager setup tried an unavailable user path;
it is recorded separately and is not a product/test failure (register C14).

Original owner must make the read/write cursor refer to the same confirmed
row/run revision, preserve new-run/interruption/scope fences, and return a
minimal correction. Recheck these two paired counterexamples, plus the nearest
unknown/new-run and sibling-row guard; retain existing author evidence. Do not
solve this by deleting the repeated-collection assertion, ignoring the missing
run contract, synthesizing a new run or restoring a regeneration fallback.

## Other reviewed boundaries and integration disposition

- The new config GET uses the existing active-account JWT validation and the
  same Bot chat authorizer as POST; chat_route tokens are rejected before
  account/Bot reads. There is no anonymous exception or new grant. Existing
  POST and legacy synthesize authorization are retained. Reused author's22
  real middleware cases; not rerun or claimed field acceptance.
- Config identity is an opaque SHA256 over resolved model/provider/config,
  not a returned config or URL. Provider config contributes to that digest;
  actual secrets are not returned. GET is private/no-store and makes no TTS
  request. CORS only adds exposure of this header to the existing policy.
- Cache is page-memory, keyed by current principal/Bot/scope/model/turn/row/run
  and source text, with response identity/MIME. EOF completeness is required;
  a stopped/partial/missing/mismatched cache does not silently generate. Auth
  change/unmount clears bytes; ordinary stop retains completed bytes. Replay
  config response is fenced against later scope/token/text/input changes.
- Streaming MP3 uses one native decoder/clock with sequential append and one
  response ahead. Stop advances generation, aborts reader/request and clears
  queued text. Other MIME types keep the same-response native Blob path. These
  are source findings and reuse of the author's current native/browser tests,
  not independent real-provider/ArkWeb/acoustic confirmation.
- Only text roots are eligible; status, reasoning, tool output and explicit
  audio attachments remain excluded. Actual rendered Unicode/Markdown text and
  row identity are used. The confirmed cursor defect above blocks admission.
- No DB/schema/config migration. New GET, its SDK mapping and response header
  require a coordinated Server/Web consumer version. This is not a Web-only
  candidate. HAP36 lacks it and requires the author's shared overlay/new helper/
  locale/new SDK GET mapping; do not replace native-adapted panes wholesale.
- A nonmutating merge-tree preflight against current1fa has no textual conflict
  (tree `a21f58acdcadf9e90f3c19ac90488d74625d32a2`). This is not a committed or
  tested product combination. Candidate product remains unmerged.

Same-session incremental-text synthesis is still a separate original-owner
implementation. Client caching/MP3 buffering does not prove natural intonation,
full spoken-word coverage or500ms speaker output. Real provider/MIME, ArkWeb
lifecycle/gesture, human listening and final full-app combination remain open.
No new fee-generating probe is authorized by this review.
