# Contact navigation revision

Incremental source base: `e6574f4d73269b64f2fd57528a16648306a55693` (PR #89's
transaction/lifecycle revision). This is independent draft structure work, not a
change to the unified `98ebb47` bug-fix candidate, a deployment, or a bulk migration.
The existing PR #89 remains the review boundary; navigation is a separate commit.

## User flow and identity

```text
Bot contact
  -> existing account/Bot binding -> same conversation
  -> no binding, one verified ordinary old chat -> continue its original ID
  -> no binding, multiple verified old chats -> one explicit choice (or new)
  -> no binding, no verified old chat -> native atomic creation
History & debug -> original history/folders/New Session/settings
/bot/<name-or-id>?session=<uuid> -> that original session, never main binding
```

A login and the existing authenticated connection are sufficient for normal entry.
There is no additional session-permission grant, identity confirmation, task
management or new-session permission step. The multiple-candidate dialog selects
which history to continue; it is not an authorization prompt and never appears
again after a binding exists. Titles and most-recent update times do not select
history. Candidate display uses stable ID pagination. A current explicitly opened
candidate can be included even outside page one, after server eligibility checks.

Server authorization still rejects actual cross-account/team access and revoked
access. Those checks do not ask a connected user to authorize their own chat again.
The separate interrupted-auth/context bug fix belongs to the unified `98ebb47`
owner; this draft does not claim that release or field deployment.

Contacts are the primary existing sidebar view. History/debug is a secondary view
of the existing folders/recents/settings/new-session controls. Task and Bot relation
entries remain in the existing conversation. Returning to a Bot opens/focuses a
pinned conversation panel; existing drafts/history/error views are not repointed
or reset. An ordinary contact ignores the configured external-runtime default;
explicit Agent entry and draft controls retain their existing configuration.

No message history is copied/merged, no previous operation/queue/run/result session
is retargeted, and unknown work is neither retried nor relabeled. A known delegated
legacy target is excluded by the persisted delegation relation, not a title rule.
Direct human chats with specialist Bots remain eligible. No new dashboard, runtime
service, memory mechanism, Agent core change or Harmony implementation is included.

## Server contract and compatibility

- `GET /bots/:bot_id/sessions/contact`: read-only `{session?, candidates,
  next_cursor?}`. Optional `after` cursor and `selected_session_id` are UUIDs;
  the latter is only a display preference. It does not bind the supplied session.
- Existing bodyless `POST /bots/:bot_id/sessions/contact`: native atomic
  resolution/creation. Server derives account/team/Bot route key from login.
- `POST /bots/:bot_id/sessions/contact/bind`, body `{session_id}` only: atomic
  first binding. Even administrators must own the candidate. It must be a live,
  user-visible ordinary model chat, without a parent, route or delegation target.
  Native historical Web chats may have NULL/empty channel type; `local` is also
  accepted. External channel sessions and external runtimes are not candidates.
- Binding follows Bot -> selected Session -> Route locks. A pre-existing different
  binding wins; conflicts are 409. An existing empty route pointer is unknown,
  not permission to repair/adopt. Insert-conflict is distinguished from creation.
  An idempotent retry of the same bound ID reads its current legal descriptor.
- Adoption changes only `session.route_id` and the new route's active pointer.
  Original session timestamps, content and work identities are unchanged.
  Existing-session adoption publishes no creation event and runs no start hook.
  Unknown COMMIT is not retried internally; GET reconciles the persisted key.
- Session read permissions are checked again on the actual resolved descriptor;
  a contact legally changed to discuss/external runtime retains its original ID.
- APIs retain old endpoint compatibility. A new Web client recognizes only the
  stable route-absent 404/405 codes as an old server and retains its old navigation;
  `session.not_found`, permission/storage failures and unknown outcomes do not
  authorize a different conversation. Old server fallback is not the new product.
- No schema migration or automatic backfill is added. Before rollout, both new
  server and client must be reviewed together. To roll back, return to the previous
  client/server and retain durable bindings/history; do not delete routes/data.

The known-successful creation callback guarantee and hook limitations documented
in [the lifecycle revision](../lifecycle/README.md) are unchanged. First native
creation may run the existing hook; adoption and resolution do not replay it.

## Verification

Development environment: actual Server on `http://127.0.0.1:18180`, current Web
source at `http://127.0.0.1:18182`, PostgreSQL 18.6 on the normal development
endpoint. App databases are dedicated `contact_lifecycle_app` /
`contact_lifecycle_vector`, with synthetic Bots/users and no configured model or
workspace. Browser requests were not intercepted. No model message was sent.
The `browser-proof.json` fixture supplies the exact demo Bot slug and session IDs.

- [PostgreSQL navigation race](postgres-navigation-race.jsonl): 8 scenarios plus
  top-level PASS (9 events), zero FAIL/SKIP. Covers read-only inspection, native
  NULL-channel history, immutable history timestamps, no hook replay, cross-user
  rejection even for an administrator, two-device competing choices, unknown
  commit readback, corrupt empty pointers, ordinary-role cross-team RLS, old
  delegated-target exclusion versus a direct specialist chat, unchanged unknown
  delegation row, bounded pagination and an explicit selection outside page one.
- [Lifecycle regression](postgres-lifecycle-regression.jsonl): 14 PASS, zero
  FAIL/SKIP. Repeated because adoption extends the accepted native channel identity
  to NULL/empty historical Web chats; the existing lifecycle cases still pass.
- [HTTP race](http-race.jsonl): 26 PASS, zero FAIL/SKIP. Current login scope,
  unauthorized/chat-token denial, strict body/cursor boundary, descriptor read
  permission and conflict status are covered.
- Web: 266 tests PASS in the directed bootstrap, chat store and workspace suites;
  typecheck, changed-source ESLint, SDK generation/typecheck and Web build pass.
  The build reports its existing large-chunk warning.
- [Integration delta lint](integration-delta-lint.txt) passes against `e6574f4d`;
  [official Agent boundary](agent-boundary.txt) passes. The **22 existing full
  integration-tagged lint diagnostics in 10 unchanged files remain**. Their files
  are byte-identical to `e6574f4d`; the existing [baseline inventory](../lifecycle/lint-summary.json)
  is preserved. This is not a claim that full integration lint is clean.
- [UI contract](ui-contract.txt) passes with three existing warnings in unchanged
  UI/sidebar/MCP files, plus its recorded baseline counts; none is suppressed.
- `chat-pane.vue`, composer controls, settings themes and the UI submodule match
  final PR85 `2422db2011231d5a4e538aa0fd7b573d133cb350`; those paths also match
  `98ebb47ba8de52e88f4e27f130766f96d36ffd58`. The single button and palette were
  not overwritten. No source in official Agent runtime/contextview was changed.

Actual browser evidence: [main flow](browser-proof.json), [final supplement](browser-supplement.json),
[PG operation readback](database-readback.json). It covers first multiple choice,
confirmed old ID, reload, independent browser contexts, explicit history URL,
same-Bot return, secondary history, unsent draft preservation, no-history native
creation, a real second member account's denied history, unique old history with
no prompt, and 390px Chinese/dark cancellation without binding. Screenshots below
are from the actual app. A failed first run exposed the cold-history transport
initialization race, which was corrected; a later screenshot exposed a denied
Bot lookup stuck in loading, which was corrected and recaptured. Initial PG
pagination failed because the fixture omitted user visibility (native DB default
is internal); the fixture was fixed, not the eligibility rule. Harness-only strict
locator collisions and a post-binding assertion were corrected. Failed attempts are not counted as passes.

| Screenshot | Observed state |
| --- | --- |
| [First history choice](01-first-choice.png) | Two server-verified candidates, none selected |
| [Bound old conversation](02-bound-contact.png) | Original legacy conversation retained |
| [Explicit history](03-explicit-history.png) | Original other session; no contact POST |
| [Secondary history](04-secondary-history.png) | Existing folders/recents/debug controls remain |
| [Narrow Chinese history](05-history-narrow-zh.png) | 390px original history view |
| [Preserved unsent draft](06-preserved-draft.png) | Returning from contact retained original draft text |
| [No old history](07-no-history-contact.png) | Native contact, no selection/permission dialog |
| [Second account denied](08-second-account-denied.png) | Completed genuine access denial, no chat leakage |
| [Only trusted old chat](09-unique-history-no-prompt.png) | Same original ID with no prompt |
| [Narrow Chinese dark choice](10-choice-dark-narrow-zh.png) | No automatic choice; Cancel makes no binding |

The reproducible dev harnesses are [fixture.py](fixture.py),
[browser-review.py](browser-review.py) and [browser-supplement.py](browser-supplement.py).
They require the named isolated databases and documented development admin login.
They create marked synthetic fixture rows only; do not point them at production.
No test container/volume/permission repair or global cleanup is performed.

## Harmony handoff and acceptance remaining

The original Harmony owner should use the same account login and the above GET /
resolve / bind contract. Keep the persisted contact ID separate from a temporarily
opened history ID. Normal Bot entry has no additional grant dialog; offer a history
choice only when genuinely ambiguous. Preserve existing composer/queue/stop
semantics and all old session/result references. On uncertain binding response,
inspect first, never guess/create another chat. No Harmony source or device test
was performed here.

This draft is not product acceptance. Parent review, authenticated field testing,
real specialist delegation/results, and the separate native-provider adaptation
remain outside this navigation change. No automatic migration, production merge,
release, deployment, user-device access or Human QA confirmation is claimed.
