# Read-only specialist Bot handoff observation

This Web increment is stacked on PR #45 at `096295d2e42dc79dfdb915a34da2bf9c2a00f7d3`.
It does not deploy that backend. At the start of this work, main `851e8da4`
and the reported `6ddf61dc` deployment candidate have identical `apps/web` trees.

The existing conversation Tasks dialog now has separate execution-task and
specialist-Bot groups. Delegations use their own generated SDK reader and opaque
cursor. The OpenAPI and SDK changes were generated from PR #45 handler annotations.
The observer only calls the two existing delegation GET endpoints. It never
submits, cancels, retries a delivery, or dispatches the specialist again.

Execution and return-delivery states remain separate: delivery `accepted` does
not prove the supervisor processed a reply. Only completed execution displays the
server's final visible summary as plain text. Running and unknown tasks do not
invent live logs, device status, or an authorization-failure cause. Current API
results contain target Bot IDs, not display names. No attachment download button
is offered in this increment; reported references are not converted into ordinary
Bot/media URLs. The existing scoped gallery behavior is preserved.

A server route-not-found error is explicitly unsupported, while a scoped
`session.not_found` is inaccessible. Neither becomes an empty list. Read reloads
are safe GETs; visible polling stops on 401/403/404. Closing, switching group,
Bot, conversation, selected workspace, identity, or hiding the page invalidates
and aborts pending reads. Stale replies cannot restore old content. The observer
has no mutation API and does not grant access: the server still authorizes reads.

## Verification

- Generated OpenAPI and SDK; SDK typecheck passed.
- 67 targeted Vitest checks passed across delegation reader/view, existing task
  reader/dialog, and Bot list/create/detail/network tests. The final completed-only
  reference display adjustment passed all 12 view tests again.
- Changed-file ESLint passed; Web build passed. UI contract passed with three
  existing warnings. `check-agent-core-boundary.sh` passed against official
  `bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046`.
- Whole Web typecheck still fails with the same 150 diagnostics before and after
  this increment on its PR #45 base, compared line by line. This was not a
  main851 baseline: the later isolated comparison found 145 on main851 and
  NAS834; see [full diagnostics and ancestry](TYPECHECK.md). No `any`, suppression, relaxed strictness,
  or disabled typecheck was added. The existing PR #45 scoped-gallery test has
  five of those diagnostics; this observer does not edit it.
- Node 24 test runs disable its experimental process-global WebStorage so jsdom
  supplies browser storage. This does not skip assertions or change production.

Actual Chromium exercised the running Vue application and the existing local
server on loopback. The old server's missing delegation route displayed the
unsupported state. Successful delegation responses were intercepted fixtures
matching the generated schema, not real specialist executions. Tests covered
list/detail, double click (one detail request), back during a delayed read,
execution vs delivery, pending content, plain text including HTML-like content,
403, disconnected network, empty vs unsupported, and Bot create-form/settings
entrances. The create form was not submitted. There were no business POSTs,
attachment fallback requests, or page exceptions. A separate 390px Chinese/dark
view had no horizontal overflow. A real SDK 401 response cleared the synthetic
identity, closed the dialog, redirected to login, and stopped polling; this is
separate from the component tests that only check localized error rendering. Development Vite required one
restart after SDK generation to discard its stale module export cache.

These screenshots were captured from that running application and inspected:

- [List](delegation-list.png)
- [Completed execution and separate return delivery](delegation-detail.png)
- [Running with no invented final reply](delegation-running.png)
- [Denied read](delegation-denied.png)
- [Network error](delegation-network.png)
- [390px Chinese/dark detail](delegation-dark-zh.png)

They contain synthetic data only. Browser fixtures are not production backend
acceptance and are not human QA.

## Follow-up race and cursor regression

Executable Vitest regressions add the previously untested A → B → A return:
Bot, conversation, identity and workspace selection each cover list and detail
reads, with old A and B responses deliberately ignoring abort. Both old success
and scoped denial responses are delivered after a fresh A result, which must
remain visible with its own cursor and no stale error. Polling must still work
after the stale denial. These are 16 new cases, not an inference from the
implementation's generation counter.

A mounted Tasks dialog test navigates to delegation page two, switches to the
original execution Tasks, advances that group's page, then returns to delegations.
It verifies both visible content and the exact SDK cursor sequence: each group
restarts at its own first page and never sends the other group's cursor. No detail
or mutation call is made in this list-only flow.

All 39 tests in the two affected suites pass (17 new cases). This increment only
changes tests and this record; it does not enable attachment downloads or change
runtime/UI behavior. These are mocked SDK + Vue/jsdom regressions, not new
production, browser, or PostgreSQL acceptance. The screenshots above still show
the unchanged application implementation.

## Integration acceptance still required

After PR #45's separate attachment fixes are integrated, regenerate the SDK if
its annotated contract changes, then test an authorized supervisor-to-specialist
handoff against the real service: pending → terminal summary, independent return
receipt, current-grant revocation, pagination, and identity/session changes.
Confirm that observing never dispatches another task. Attachment delivery must
pass its own backend validation before enabling any observer download action.
No NAS changes, paid model request, production credential, or Agent core change
is part of this increment.


## Shared body contract for Web and native-dialog hosts

`apps/web/src/pages/home/components/bot-delegation-tasks.vue` is now a body-only
component. It does not inject Reka DialogRoot, own a modal, fetch data, grant
access, cancel work, or offer downloads. The Web shell in `session-tasks.vue`
keeps its existing Dialog/Panel/Header and owns its `DialogDescription`.
A Harmony native HTML dialog can embed the same body without that context.

| Prop | Type / meaning |
| --- | --- |
| `items` | `BotdelegationView[]`, current list page |
| `detail` | optional `BotdelegationView`, current detail |
| `selected` | optional delegation ID, detail selection |
| `cursor`, `nextCursor` | optional opaque cursor strings for this reader only |
| `loading` | boolean, a read is in progress |
| `error` | optional `unknown`, localized through existing API error helpers |

Events: `openTask(task: BotdelegationView)`, `page(cursor?: string)`, and
`refresh()`. Wire these to the same `useBotDelegations(scope, visible)` methods;
pass its returned refs as props. `scope` is `Ref<TaskScope | null>` with `bot`,
`session`, `selection` (workspace selection), and `identity` from the authenticated
host. `visible` is `Ref<boolean>` and must reflect the active tab/dialog as well
as page visibility. Set it false on close/cancel or host hiding; clear scope on
auth loss. Do not share cursors with `useSessionTasks`. The reader handles
abort/generation checks, identity changes and visible polling; a host must not
add a second polling loop or interpret Reload as business retry.

The host owns accessible title/description and focus lifecycle. Web uses its
existing DialogTitle/DialogDescription. A native dialog supplies a visible
heading/paragraph and matching `aria-labelledby` / `aria-describedby`, calls
`showModal()` / `close()`, and updates visibility on native `close`/`cancel`.
Use `chat.delegations.title` and `chat.delegations.description` for that native
heading/description. No extra ticket, permission, device or credential prop is
introduced. Backend authorization remains mandatory.

Two mounted Vue tests exercise the real body and same reader in Web/Reka and
native HTML dialog containers, including list → detail, description references
and no links/images. A separate Web-shell test verifies Escape restores the
Tasks trigger. The two changed view suites pass 24 tests; all four task reader/view suites
pass 74 tests. The final shared-body worktree still has the identical 150
compiler diagnostics of PR54 667, with no added or removed diagnostic.

Fresh Chromium verification on the running local Web app exercised both hosts
with synthetic readonly API responses: four delegation GETs, zero mutations,
zero page exceptions, visible final summary as plain text, and no downloads.
Web preserved its focus trap and Escape restoration. In the native-dialog
fixture, Tab may reach browser chrome (normal HTML dialog behavior), but never
an outside page control; Shift-Tab returns inside and Escape restores the opener.
The native screenshot verifies the shared body, not a shipped Harmony screen or
HAP. The temporary fixture module was removed after verification.

- [Existing Web shell with shared body](shared-body-web.png)
- [Native HTML dialog fixture with the same body](shared-body-native-dialog.png)

The body still requires Vue/i18n and the shared UI library; only the DialogRoot
dependency was removed. Harmony integration and real delegation backend
acceptance remain separate work. Attachment downloads stay disabled.

Final changed-file ESLint and Web build passed. The UI contract passed with its
three existing warnings, and the runtime/contextview official-baseline check
passed. The build retains its existing large-chunk warning. A successful build
or reporting CI does not turn the failing whole-Web typecheck into a pass.
