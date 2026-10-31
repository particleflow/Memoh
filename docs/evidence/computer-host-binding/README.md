# Computer tool host binding

Reference integration: `3d5f145c452dda5e17861d401de049b7ddded2e6`.
The changed application, federation and computer-source files are identical
there and at prerequisite `2782ee0607967f6eff0f7df48cd72d195181551b`.

## Proven boundaries

- An ordinary native Web send with complete host identity passes the existing
  binding gate. That synthetic case does **not** reproduce the reported field
  message failure or prove its cause.
- An ask-user or approval continuation enters on the admitted run lifecycle
  context, before the original stream's workspace preparation. Re-enter the
  existing host preparation using the original resolved decision identity,
  session, run and selected target. Approval execution uses the same preparation
  before its executing checkpoint. Missing identity, revoked permission/target
  and a conflicting session remain rejected before tools or the model.
- Explicit accepted-answer recovery must supply its admitted RunID during
  workspace preparation. If preparation fails, persist a failed continuation;
  do not let an empty terminal status imply completed.
- Federation tool assembly has the owning host context. A callback with only
  transport cancellation must retain that context in the existing RunContext
  contract. Keep callback cancellation and owning-run cancellation effective;
  neither model parameters nor session routing fields create authority.

The callback-without-values case reproduces the same binding sentinel as the
field report, but the field callback's values were not captured. Do not present
that isolated reproduction as a confirmed production root cause.

## Error interpretation

Both targets and submission check runtime ownership and trusted host binding
before target dispatch. A binding error proves neither device disconnection nor
permission revocation. The existing generic target-unavailable error also does
not establish an offline device; only a verified connectivity/preflight result
can do that. No platform-protection or new-session conclusion follows from these
errors. Public error text/codes and authorization behavior are not replaced.

Binding failure diagnostics contain tool/run/call correlation and boolean host
presence/identity-match facts only. They omit command text, user/target values,
paths, credentials and terminal contents. A canceled callback remains canceled,
not a fabricated binding or connectivity failure. Future diagnostics can locate
the exact missing/mismatched host edge without replaying an accepted task.

## Validation and acceptance

New isolated tests exercise native Web/decision tool assembly, a real synthetic
PostgreSQL recovery admission and controlled HTTP model, federation callback
context loss/cancellation, and safe diagnostics. They retain the existing unknown
operation and public-header denial regressions. The callback boundary test stops
at a nil task service after binding; it cannot dispatch a business operation.
Raw red/green commands and initial fixture failures are retained privately.

Directed race selections passed 47 test/subtest events, including real synthetic
PostgreSQL recovery. The affected-package aggregate passed lint and application
and computer-task tests, but failed seven existing image-download tests in the
tool package at the configured proxy/SSRF boundary. Those failures are retained;
this patch does not change the image transport, proxy configuration or guards,
and does not claim a fully green aggregate or establish the failures' origin.

No Agent runtime/contextview changes, schema/API changes, new service, model
policy, production operation, deployment or historical-operation rewrite.
Field root-cause matching and actual application/device acceptance remain with
the unique field owner. No real model, image or Windows task was submitted.
