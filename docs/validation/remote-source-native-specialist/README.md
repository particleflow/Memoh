# Remote supervisor → native specialist admission

Status: offline fix and targeted checks complete; real UI/model acceptance is
pending. No production requests, paid models, device effects or data edits were
performed for this fix. All committed IDs and hashes in fixtures are synthetic.

## Reproduction and fix

Base: `7af02cb35f85582713213cb98c382d36df744ea3`. The production `98ebb47`
planner had the same behavior. A source conversation selecting a remote Runtime
required the specialist to mount that same Runtime before any durable claim.
Consequently, a native specialist with no remote bindings was rejected even
when source user/session/attachment checks and the specialist owner's chat
permission all passed. The built-in tool mapped this admission denial to
`session.not_found`. [Before-fix callback failure](before.txt).

The new regression uses the native Generate loop with a local deterministic
chat endpoint, the actual built-in list/delegate providers, actual owner
permission resolution (default deny, no grants), and actual delegation planner.
It reproduces the remote source + native specialist case before the fix and
reaches the claim port after it. The ledger port returns an existing claim to
prevent asynchronous specialist execution: this proves admission, not a real
persistent row, child turn or attachment round trip.

New plans retain source selection and identity unchanged. For a remote source,
the expert's existing default selection is read through GetPrimaryMount. A
missing primary means native, matching the existing selection policy. The plan
then records native target execution and no device pairs. A selected remote
primary still requires a shared, live, matching Runtime pair. Errors reading
the target selection do not become native defaults.

The reconstructed specialist scope honors its persisted native execution choice
while continuing to authorize the original remote source. It does not replace
the source ComputerScope, permit an unrecorded remote mount, or waive either
Bot's current permissions. Historical remote-target records keep their existing
scope. Ledger/queue/receipt schemas and existing unknown/retry behavior are
unchanged. No mount or permission is created.

## Validation

Passed the new callback regression before/after and targeted race checks for:

- actual native callback identity, source attachment links and owned admission;
- remote source with native/no-binding specialist, preserving source provenance;
- default-deny owner permissions without explicit grants;
- persisted native target scope, no PC adoption or cross-Bot scope reuse;
- both Bot execution grants and source Runtime revocation;
- primary lookup errors, invalid/mismatched remote targets and legacy scope;
- original return ownership, unknown deduplication and delivery-only retry.

Commands used:

```sh
go test -p 3 ./cmd/internal/core -run '^(TestDelegationNativeCallbackRequiresSourceLinkedOriginal|TestNativeDelegationPlan)' -count=1
go test -race -p 3 ./cmd/internal/core ./internal/botdelegation -run '^(TestDelegationNativeCallbackRequiresSourceLinkedOriginal|TestNativeDelegationPlan|TestNativeSpecialistPlan|TestRemoteSourceKeepsProvenance|TestPersistedPCScope|TestNativeHandoff|TestHandoffComputer|TestUnknownAndDelivery|TestRetryDelivery|TestTextAndSingle)' -count=1
golangci-lint run --new-from-rev=7af02cb35f85582713213cb98c382d36df744ea3 ./cmd/internal/core ./internal/botdelegation
```

Lint used CI-pinned v2.10.1. No unrelated tests or builds were repeated.
No new real UI delegation or screenshot is claimed: the user prohibited another
charged/production probe in this round. The affected-flow UI verification in
AGENTS.md remains incomplete. A later authorized combined acceptance must show
an actual specialist session/run, native target, source-owned original bytes,
returned attachment, and arrival in the original source session. Historical
failures and unknown records must not be replayed or rewritten to obtain that
acceptance. CI/source tests alone are not delivery evidence.
