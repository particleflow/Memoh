# Independent input-source admission review

Reviewed on 2026-10-30 UTC. Candidate
[`65649801a58946993d2824f3963255750661eb2a`](https://github.com/particleflow/Memoh/commit/65649801a58946993d2824f3963255750661eb2a),
parent `9270da0252128a296d9a641235cdfd32ab44cfda`, tree
`66b2c7a91c189782716e577c7ac282f577091d7e`. The original branch was fetched and
its full commit, parent and tree verified. This review does not merge that
product into the unified branch or qualify it for publication.

## Admission decision: return the live projection gap to the original owner

**P2: live assistant turns lose the recorded input source.** The real
`runtime_snapshot -> reduceRuntimeProjection -> runtimeAssistantTurn` path
preserves `request_user_turn.input_source` on the user turn but constructs the
assistant turn without it. `transcript-history.ts` can only copy a field that
exists; `message-item.vue` consequently renders no source label for the live
assistant. Once persisted history is read, `newPendingAssistantTurn` does supply
the source, so the same reply changes its provenance presentation at settlement.

The author's added transcript test supplies a pre-annotated assistant directly
to `normalizeTurns`. It does not traverse the runtime projection and therefore
cannot detect this gap. The independent [counterexample](runtime-source-repro.mjs)
does traverse the actual candidate function, transpiling the existing TypeScript
in memory with an already installed compiler. It starts no application, model,
database or network request. Its [recorded output](runtime-source-result.json)
has the expected schedule source on the user, `null` on the assistant, exit 1.
This is a controlled source-level reproduction, not browser/device acceptance.

Relevant candidate code:

- [runtime-projection.ts](https://github.com/particleflow/Memoh/blob/65649801a58946993d2824f3963255750661eb2a/apps/web/src/store/chat/runtime-projection.ts),
  `runtimeAssistantTurn` and its segment call sites.
- [transcript.test.ts](https://github.com/particleflow/Memoh/blob/65649801a58946993d2824f3963255750661eb2a/apps/web/src/store/chat/transcript.test.ts),
  added source-preservation test.
- [message-item.vue](https://github.com/particleflow/Memoh/blob/65649801a58946993d2824f3963255750661eb2a/apps/web/src/pages/home/components/message-item.vue),
  `sourceLabel`; and `internal/agent/view/uimessage_convert.go`,
  `newPendingAssistantTurn`/`persistedInputSource` on the settled path.

Repeat only this newly uncovered boundary against the original owner's fix:

```sh
node docs/testing/session-input-source-review-656/runtime-source-repro.mjs \
  <candidate-checkout> <already-installed-typescript-module>
```

The owner must preserve the source for the corresponding live turn/segment,
including delta and persisted reconciliation. Do not copy the first actor to
an unrelated later steer, another Bot/session or an unattributed segment. Keep
unknown as unknown. Fix implementation and add the boundary regression in that
workstream; the unified reviewer has not edited its runtime or UI product files.

The [source identity record](source-identity.json) contains SHA256 hashes of the
four exact candidate files used for this review. The fixture timestamps are
synthetic data, not execution timestamps or field receipts.

## Reviewed boundaries and reused evidence

The [author's evidence and limitations](https://github.com/particleflow/Memoh/blob/65649801a58946993d2824f3963255750661eb2a/docs/testing/session-input-source-20261030/README.md)
were read together with the production diff and selected original logs. Existing
105 Web tests, captured model requests, race/real-PG recovery checks, lint and
source UI screenshots are reused as author evidence, not relabeled as new runs.
No full suite, product build, CI dispatch or paid provider call was repeated.

- **Identity versus authorization:** `ChatRequest.InputSource` is in-process,
  `json:"-"`. The source is not added to `StartTurnCommand` as a client-selectable
  grant. Delegation/results are reconstructed after the existing ledger checks;
  native tasks restore their existing receipt/session scope and permissions.
  Source metadata contains routing identifiers, not credentials or grant data.
  The diff does not replace `req.UserID`, JWT claims or existing workspace checks
  with the displayed source. Non-user persistence clears speaker IDs rather than
  treating the authorization owner as the speaker. No permission widening was
  identified in these reviewed paths; this is not universal security certification.
- **Model and stored history:** source sections use host-authored metadata;
  history annotations remain in user-role content. Body strings do not write
  that metadata or change message role. `Scoped` rejects foreign receiving
  Bot/session and malformed/unknown kinds instead of borrowing an actor. Old
  rows without provenance remain unknown; no historical row/hash is rewritten.
  The unchanged XML formatter does not serialize its ChannelIdentityID member
  as a sender attribute, so retaining that authorization field alone was not
  reported as a model-envelope impersonation defect.
- **Schema and clients:** no SQL migration, config, credential or endpoint-route
  diff. API/SDK `UITurn.input_source` is additive; corresponding source rendering
  changes Web resources. Harmony needs its owner's matching shared/SDK mapping
  after the live projection fix; existing HAP35 is not claimed to contain it.
  No Windows/producer change is needed for the annotation, and PC57 stays separate.
- **Forward recovery:** automated run checkpoints can use resume version 4;
  9270's `resumeContext.resumable` does not understand that version. Stop old
  checkpoint consumers/writers before replacement and preserve current intents,
  source metadata, results and receipts. SQL schema equality (181) is not a
  rollback proof. Discuss/result-specific version handling also remains; do not
  strip the source or rewrite versions to make an old binary resume. Use a
  compatible forward correction unless an older consumer is separately proven
  compatible with the retained records. No migration down or old-dump restore.

## Independent limitations remain separate

The author rehydrated captured inputs as synthetic history for UI verification;
that does not prove a complete app-to-Hook-to-model production turn. The absent
workspace bridge stopped the original direct flow at `hook.user_message_failed`.
The exact local contact-binding POST returned 403 and was stopped, with no
identity substitution, SQL binding or alternate route. The `/proc/27592/cwd`
denial and old Vite liveness/cleanup remain unresolved. Later distinct test
process shutdowns do not establish that denied process's cleanup.

The author's history-switch/refresh observation and production 9270's initial
body-load defect are not proven to have the same cause. The navigation owner
retains that separate investigation. Existing 9270 deployment/history/unknowns
are unchanged by this review. No source candidate, author test or review result
is a new field acceptance receipt.

The accompanying C05/C12 register update records the author's distinct metadata
denial and verified task-cache recovery; it neither retries those operations
nor imports the unqualified product. See the
[maintained compatibility register](../compatibility-incident-register.md).
