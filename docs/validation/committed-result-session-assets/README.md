# Committed computer results → source conversation attachments

Status: product adapter fix and isolated checks complete. Real application/UI,
Windows transfer, paid model and phone acceptance are pending. No production
requests, model calls, device actions or production data edits were performed.

## Fixed gap and contract

Base: `801fa1666fea5e2170c9c6cf346ed2ee279678e0`. Previously the computer tool
returned `delivery.reference` / `delivery.files` JSON without creating a message
asset link. A committed NAS file was therefore not automatically a source
conversation input for professional Bot delegation. Source remote workspace
selection does not imply that arbitrary PC bytes already exist on NAS.

The existing computer tool adapter now associates committed successful declared
producer results on normal `computer_task_result`, `computer_task_get` (including
delivery-page reads), and `computer_task_artifact` observations. It reloads the
exact operation's trusted producer/commit ledger; it never scans result JSON for
hashes. The existing projection checks operation, user, Bot, session, target,
run, attempt and generation. Both legacy JSON ledgers and current row ledgers
remain supported. No new transport, protocol, permission or Agent core change
is introduced.

Every selected reference must be an upload committed for that declaration. The
adapter opens the original under the owning source Bot, hashes the actual
stream and checks actual length; declared MIME remains ledger metadata. It
reauthorizes the same registration/fingerprint and operation revision after
reading. Missing/corrupt bytes, revoked/changed targets or changed ledgers fail
closed. Failed, canceled and unknown executions do not acquire links. In a
successful multi-file result, only individually committed files are associated;
pending/uncommitted files remain unlinked.

The host then checks the active account and source chat grant, exact source
session owner/Bot, original operation run/team and persisted resume user. It
requires a visible, non-superseded original turn and its fixed assistant message
(sequence 2), rejecting reset/missing/foreign anchors. It uses the existing
runtime-fenced `message.DBService.LinkAssets` transaction and the existing
`(team, message, content_hash)` upsert. Re-observations target the same original
message even from a later observer run; they do not send a message or copy a file
into the specialist Bot. Metadata retains the operation and original run IDs.
The existing session invalidation event refreshes the source transcript.

The expert continues to require an actual delegation declaration, the source
session asset association and both Bots' current authorization on every loader
read. The new association is not a permanent cross-Bot grant. Existing camera
uploads and delegated outputs retain their existing asset paths.

## Isolated evidence

[Before-fix adapter failure](before.txt): disable only the automatic tool hook,
then run the committed JSON/row ledger cases. They reach the trusted link hook
only in the explicit service check; repeated actual tool observations do not
link. Restoring the hook makes those observations link automatically.

The new tests use actual local Originals storage and real file streams:

- Source adapter: real original bytes with JSON and row commit ledgers; the
  actual result/get/artifact tool paths, repeated result queries, partial files,
  and denial of pending/ready/unknown/failed/canceled results, unrelated JSON
  hashes, foreign producer identities/users/Bots/sessions, changed target or
  provider, same-length corrupted bytes, revocation during open and revision
  changes during reads.
- Host association and expert loader: real PNG bytes, exact original source
  run/session/message mapping, one attachment anchor after duplicate reads,
  the existing UI projection exposing exactly one original-Bot attachment,
  a scoped professional Bot reading the identical original PNG without a
  destination-owned alias, and denied foreign identities/teams/run users,
  missing resume identity, reset/superseded/foreign anchors, wrong message role,
  absent/foreign fences, storage-link failure, revoked or unscoped expert access.

Ledger, message-store and permission ports in these new tests are synthetic;
this is not a PostgreSQL transaction or real native-model end-to-end acceptance.
The unchanged SQL upsert/fence is reused, not reimplemented. Existing delegated
image byte/authorization tests are included in targeted race verification.

Passed:

```sh
go test -race -p 3 ./internal/computertask ./cmd/internal/core -run '^(TestCommittedResultAssociation|TestComputerResultAssociation|TestDelegatedImageReferenceUsesScopedOriginal)' -count=1
go test -p 3 ./internal/computertask -count=1
golangci-lint run ./internal/computertask/... ./cmd/internal/core/...
bash scripts/check-agent-core-boundary.sh
git diff --check
```

Lint uses the CI-pinned v2.10.1 binary. No unchanged matrix or build was rerun.
Full byte revalidation adds storage IO to successful result observations; large
file observation latency has not been measured for this adapter.

## Combined acceptance still required

This change starts with an already committed NAS original. The existing file
owner handles formal PC upload/commit and NAS→PC input materialization; an
arbitrary PC path cannot be passed here as an attachment. No background poller
or new message/channel send is added. The normal trusted result observation
must occur before the source link exists. A missing historical run/visible
assistant anchor is rejected, not repaired by selecting another message.

For the authorized combined acceptance, the single field owner should collect
one operation's normal committed result, then verify its original source
message asset link and session refresh without an extra model `send`; delegate
that declared hash to the existing expert and verify the original byte hash at
its tool input/loader; finally verify the expert result attachment and download
in the original source session/phone. Repeat result observation must retain one
asset link and must not create another message or business operation. Preserve
failed/unknown historical records; do not replay an uncertain model invocation.

The affected real flow and screenshots required by AGENTS.md are not verified
in this source-only, no-model/no-production-write task. The draft is not a
claim of delivered Windows↔NAS↔expert↔phone capability or human QA.
