# Remove the specialized image workflow

Base: `43e77123dc06e07bb56153cf616fe62c69ec8ffc`. This is a source cleanup,
not a data cleanup, runtime migration, new Agent framework or deployment.

## Removed and retained

| Removed | Retained |
| --- | --- |
| Dedicated image execution/publication service and authority | Ordinary subagent admission, model selection, inherited skills/tools and session scope |
| Image-only journal creation and startup injection | Existing NAS originals and upload records; no removal or migration runs |
| `generate_image` edit/result modes and original-input task binding | Upstream prompt/size generation and Bot `ImageModelID` |
| Image-only spawn/background result IDs and callbacks | Ordinary child text/history and explicit parent `send(content_hash)` |
| Image-specific task instructions, fixtures, current documentation and added model-discovery API | Generic file/hash/ready, ordinary resumable uploads/quota and queued submitter context tests |

The original generation tool publishes through `media.Service.Ingest` to the
existing file store. It returns a ready content hash after persistence succeeds;
it does not restore the old workspace path or inline-base64 fallback. A storage
failure reports that generation finished without publishing a reference. It
does not retry generation and does not claim a durable recoverable result.
The deleted image-specific billing/idempotency ledger is not replaced by a hidden
state machine. The original tool's provider protocol selection is unchanged.

The dedicated `image-edit` capability/selector added for this workflow is removed
from new configuration choices. Existing stored configuration is not rewritten.
The added provider model-discovery endpoint, its generated SDK/schema and adapter
image booleans are withdrawn too; official provider management and the original
provider-model list/import behavior remain. The unpublished Magpie directory
experiment is not part of this branch or any release.

## Withdrawn argument rejection

The `generate_image` execution closure validates the raw argument object before
calling the shared typed decoder. Only exact `prompt` and `size` keys are accepted;
unknown keys fail as tool errors before provider resolution or invocation. The
schema also declares `additionalProperties: false`, but runtime enforcement does
not depend on model/schema compliance. The global decoder and Agent core are unchanged.

The full `toolexec.ExecuteTools` regression reproduced the original problem:
withdrawn `mode=edit/result`, `input_refs` and `task_id` requests silently generated
images against the synthetic HTTP provider. After the guard, all six invalid cases
make zero provider requests; valid prompt-only and prompt/size each make one request
and publish a ready NAS hash. The test exercises registered tools, configured
settings/model/provider fixtures and real media persistence, not a replacement
callback or just schema inspection.

A rebuilt running server plus real Web also processes four legacy calls in one
fresh synthetic conversation: four explicit tool errors, two chat requests,
zero image API requests, no page errors. [Rejection evidence](strict-result.png).
The screenshot records the pre-fix Web caption "Generated 4 media files" despite
four failures. That caption is not success evidence; actual tool errors and zero
image-provider requests establish rejection. A separate UI commit corrects the
caption to invocation/outcome counts; see [current UI evidence](../tool-summary-outcomes/README.md).
The guard additionally rejects null-valued legacy fields and a top-level null,
with zero provider requests. These four additions extend the original six-case
regression without changing the accepted prompt/size contract.

## Ordinary subagents and attachments

`spawn_agent` is wired through `native.NewSpawnAdapter` to the existing in-process
native Agent. It selects an enabled chat model from the current session provider,
with the existing tool/skill inheritance and denylist. It does not launch an
external Pi, Codex or Claude Code process or automatically provision an image-capable
Agent. No Agent runtime or context-composition code is changed.

A child may return a NAS content hash in its normal text result; the parent may
explicitly send that reference through the existing attachment tool. This is not
a generic automatic artifact-promotion interface. The removed automatic result
callback was specialized to image tasks; no equivalent generic interface is
claimed. Access still goes through existing Bot-scoped media and authenticated
file APIs; no removed image-input route continues to consume unverified hashes.

## Cross-Bot delegation remains a separate acceptance requirement

The negative media lookup uses an unrelated Bot with no delegation or artifact
binding. It does **not** require rejecting legitimate cross-Bot cooperation.
The browser test is same-Bot: `spawn_agent` copies `parentSession.BotID`, and
`send` addresses a channel/conversation, not a configured specialist Bot.
No supervisor-to-specialist delegation entry point was verified by this cleanup.

Follow-up acceptance must include an already-authorized user/team delegating from
a supervisor Bot to a specialist Bot, passing NAS input refs, and returning result
refs to the original supervisor task/conversation. Also check revocation and
unrelated-access rejection. Reuse existing owner/team permissions and a trusted
task relationship, without per-file tickets, repeated human approval or another
file-transfer service. This positive cross-Bot flow is not yet implemented or
passed here; the negative test cannot substitute for it.

## Data safety

No startup cleanup, filesystem deletion or ledger replay is added. The removed
image service was the only caller of result reservation, retention and upload-root
journal assembly helpers; those helpers, the `reserved` state and TTL exemption
are removed too. Ordinary resumable uploads, quota, locking and hash publication
are unchanged from the pre-image foundation (`2472cbba`).

Deployment precondition: recheck that `.image-tasks` is empty and `.uploads`
contains no `retain_result`/`reserved` records or unpublished image payloads.
If this changes, stop deployment and preserve those files for a separate disposition;
do not run Sweep to make the check pass. Ordinary upload receipts, locks and originals
must remain. An empty legacy directory can be removed separately after verification.
Old persisted turns are history and are not replayed under the restored tool schema.

## Verification

- Official Agent runtime/contextview boundary check passes against `bf0318c5`.
- Targeted race tests cover ordinary spawn/send references, an unrelated Bot lookup without artifact binding,
  image publication through shared media, no inline payload on storage failure,
  schema goldens and retained queued-submitter context.
- Media/background/provider race suites pass after resolving local temporary-disk
  pressure: the only media failure was the 512 MiB copy test; that exact test
  passed on rerun after removing regenerable Go build-cache entries.
- SDK generation and typecheck pass. The Web model-list suite passes 11 tests;
  changed TypeScript ESLint and UI contract checks pass (three existing UI warnings;
  locale JSON is outside ESLint's configured file set).
- Scoped golangci-lint 2.10.1 and the final server build pass.
- Real Web at `http://127.0.0.1:18082` and the rebuilt cloud-fixture server were
  exercised after final source changes (`HEAD /health` 200). A fresh conversation
  uploads a synthetic text file, invokes one ordinary subagent, receives its hash
  in child text, then sends that hash from the parent. Four deterministic chat
  requests perform spawn / child answer / send / final answer; zero image endpoint
  requests occur. The downloaded attachment exactly matches the uploaded bytes.
- The removed discovery route returns 404; original provider model listing remains
  200. No browser page errors occurred. Synthetic settings are restored afterward.
- An earlier browser attempt reached an old fixture server and failed the removed-
  endpoint assertion; it is not counted as verification of this change. The final
  run above uses the rebuilt server. No uncertain or production task was replayed.

[Before submission](before-submit.png) shows the fresh synthetic request.
[Completed flow](result.png) shows the ordinary Agent activity and parent attachment.
Screenshots are from the running application; they are not human QA.

Tests use synthetic files and deterministic providers. No production configuration,
files, credentials or paid API were used. Generic subagent text plus explicit parent
send is distinct from automatic structured artifact promotion or arbitrary external
Agent support.
