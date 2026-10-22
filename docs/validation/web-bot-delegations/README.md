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
  this increment, compared line by line. No `any`, suppression, relaxed strictness,
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

## Integration acceptance still required

After PR #45's separate attachment fixes are integrated, regenerate the SDK if
its annotated contract changes, then test an authorized supervisor-to-specialist
handoff against the real service: pending → terminal summary, independent return
receipt, current-grant revocation, pagination, and identity/session changes.
Confirm that observing never dispatches another task. Attachment delivery must
pass its own backend validation before enabling any observer download action.
No NAS changes, paid model request, production credential, or Agent core change
is part of this increment.
