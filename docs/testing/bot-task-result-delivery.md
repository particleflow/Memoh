# Durable background result delivery

This local slice extends the native queue identity change at `4768679adf01047bff66c148ded37f215c9994be`. It does not complete the remaining primary-chat activity visibility or precise historical-run navigation work.

## Contract

New native background admissions and Bot delegations opt into result delivery only for an owned Web conversation using the native model runtime. A persisted Web route ID is valid: the original route, recipient, account, Bot grants, session ancestry and workspace selection are revalidated. Other channels and external runtimes retain their previous behavior. Historical records without the opt-in flag are not automatically backfilled.

Execution status, a saved result event, and a published summary are separate facts. Result events live in existing native admission JSON or delegation result JSON. They preserve the original task and execution IDs. Each event has its own stable invocation and bounded summary-attempt counter. Unknown outcomes are reportable; their recovery never calls the execution or delegation entry point. A pre-admission crash retains an unknown original request instead of manufacturing another task.

Only a nonempty, completed, nontruncated final assistant step can carry the host-owned publication receipt. The receipt is stored in the same fenced PostgreSQL transaction as the visible reply. Progress, tool steps, interrupted text, empty output, queue acceptance and run termination do not establish publication. Recovery observes the run and reply in one database snapshot. A committed reply wins even if the summary run subsequently becomes lost. Missing output can retry only result summarization; summary turns receive no tools and do not consume steered user input. The existing follow-up queue continues to handle user messages separately.

Delivery JSON uses a separate compare-and-swap. Execution saves preserve it. Startup, terminal callbacks and the existing runtime repair callback wake delivery; there is no new service, scheduler, table or migration. Automatic summary attempts stop at three, or immediately after an aborted summary; the durable report state remains `attention`, never `reported`. Publication means a visible persisted Web reply, not an external-channel transport receipt or proof that a human read it.

## Validation

All execution used the isolated schema-179 PostgreSQL database and local Server/Web/Docker development stack at `http://127.0.0.1:28082/bot/local-dialog`. Models were loopback scripts; there were no paid model calls or production changes.

- Focused application/host tests cover internal input visibility, queue identity, final-step eligibility, legacy origin compatibility, route changes and unknown deadline handling. Real PostgreSQL tests additionally cover C1/C2/C3, eight concurrent acknowledgement recoveries, authorization revocation, preserving delivery JSON across a stale execution save, native unknown claims and cross-Bot requests without target admission.
- C1: kill the actual server after the original result is saved and while the first summary request is blocked. After the existing 90-second backend-loss grace, the same task/execution is reported by summary attempt 2. Original execution count remains one.
- C2: the scripted provider returns truncated progress, then empty output, then a final summary. Only attempt 3 gains a publication receipt. Original execution count remains one.
- C3: lock the original receipt row after summary admission; allow the final visible reply to commit; kill the server before its delivery acknowledgement can update. Restart reuses that exact message, with one summary call and one original execution.
- Unknown: kill the actual server during a native child execution. The existing runtime marks its original run lost; the original user receives one natural unknown report. No replacement execution occurs. The directory retains `Execution lost; result unknown`.
- Pre-admission cross-Bot fixture: seed one accepted synthetic delegation without a target session/run and start the actual server. The original record becomes unknown and is reported once; target executions remain zero.
- Final-source repair wake: clear only the acknowledgement cache of a synthetic, already-published result. With no new user or terminal event, the existing runtime repair callback restores the exact message receipt in 9.05 seconds and makes zero additional model calls.
- A new primary-chat response arrived in approximately 0.40 seconds while a background child was held at a barrier. Summary model requests had zero tools. Browser inspection found no machine-result user bubbles, and retained the visible truncated-progress warning, final summaries and unknown status. Ordinary native tool activity remains visible and is outside this slice.

Detailed evidence and current-source screenshots accompany the round-4 handoff. C1/C2/C3 and the native unknown process tests preceded only the final cross-Bot pre-admission recovery addition and reuse of the runtime repair wake; focused PostgreSQL checks and actual startup/UI/repair verification cover those final additions. No unchanged broad test suite was rerun. Human QA and production/device acceptance remain outstanding.

## Limits

This does not prove external-channel delivery, external-runtime summary recovery, model semantic quality, all cancellation-chain races, or the deferred multi-result revision matrix. Report state `attention` requires subsequent operator handling; the slice does not add an admin recovery UI. Existing history is retained. The directory still opens original execution sessions rather than precise run anchors, and primary-chat tool activity hiding remains separate work.
