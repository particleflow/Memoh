# Unified reader recovery and existing Images controls

Local product checkpoint: `37d429d021bb1ffee672eb36455613e2e1c44ae8`.
Base: `9e6c47076bc507e0555002b73d3567297d914939`. This is a subsequent
integration on the existing branch, not a replacement of the published
candidate identity or a deployment.

## Exact sources

| Preserved source commit | Included change |
| --- | --- |
| `ec50146992b9f177a20d99f7a91cd7015daad307` | Operation-owned batch recovery reader, original `ed4a` regression and evidence |
| `18545f2763fa9a9a3fd1ffab1d867c9cd4ee880e` | Refresh Provider query before persisting its sanitized display cache |
| `6ab5971fe85e70149e3b10f03330954bc1cbbab7` | Image tool result states and persisted content references, without invented download URLs |
| `f0f89cd17adaf323c28d2102a2361c745cbb80ed` | Existing `images_api.edits` Provider form control; default false |

All four were merged with their ancestry intact. No manual conflict resolution
or additional product changes were required. The three locale files merged
automatically; a per-object duplicate-key check passed for en/zh/ja. The
official runtime/contextview boundary check passed. No migration, credential,
backend Images protocol, SDK contract, workflow, or Agent core changed here.

## Executed integration checks

`evidence.json` records each command, actual exit code, original and published
hashes, and any local-path normalization. Readable logs include failures.

- Focused Provider/cache/tool-card/observation UI selection: 4 files, 77 tests
  passed. One supplied test path did not exist; Vitest executed the four
  matching files, not five. Shared task content and result UI: 2 files,
  11 tests passed after using the environment's existing Node compatibility
  setting. These are 88 targeted tests, not a full Web suite.
- Recovery regression: 25 PASS records including subtests, zero SKIP. The
  four original collection stages, one-create CAS, unknown creation, changed
  identity, revoked access, hashes and page reread cases passed using the
  synthetic official-method transport. No real Herdr process was restarted.
- Existing isolated PostgreSQL: account/Bot authorization and reader JSON/CAS
  tests both passed without SKIP. Reader test uses a rolled-back transaction.
- Complete Web and SDK typechecks passed. ESLint on the six changed Vue/TS
  files passed with 9 existing fixture warnings, zero errors. UI contract
  passed with 3 existing warnings. Web production build passed with its
  existing large-chunk warning. No full Go lint or full Go suite was rerun
  locally; exact final-head CI remains required.

The first shared-task test invocation omitted
`NODE_OPTIONS=--no-experimental-webstorage`. Node 25's experimental storage
object caused eight `localStorage.setItem is not a function` failures in test
setup. Repeating only those two files with the environment's established
option passed; no product, test assertion or CI rule was changed.

## Actual browser check

Reused the current-source Vite application and the existing isolated Server +
PostgreSQL fixture (loopback Web `18402`, API `18400`). The Provider HTTP
handlers are unchanged by this integration. The running Server was the prior
native-content fixture binary, not a new recovery-reader binary; recovery
code was exercised through its Go service tests and real PG above. This
browser check does **not** claim a complete final-binary end-to-end recovery.

Using the existing synthetic login, created one synthetic Provider pointing
to `provider.example.invalid`. In the actual Provider form, enabled the
reference-image switch, saved with HTTP 200, observed the new sanitized
display cache, and hard-reloaded. The switch and image policies persisted;
the password input remained blank, and the stored address/key did not change.
An anonymous PUT returned 401 and the configuration remained unchanged. No
model, connection-test, import-models or follow-up request was dispatched.

- [Saved Provider form](provider-reference-saved.png): actual persisted switch
  and protocol; synthetic provider, no key shown.
- [Narrow form](provider-reference-narrow.png): same page at 390 px; no
  document horizontal overflow.
- [Existing task detail](combined-task-view.png): current combined Web opens
  the task returned by the existing task API. Connection unavailable,
  unknown identity and unverified completion remain visible. No content or
  follow-up request was issued. Automatic observation GET is unchanged.

The initial browser script completed all Provider checks, then timed out
looking for the old native fixture's ID. That fixture had previously been
restored. The subsequent task-only script selected the current persisted
batch task from the actual tasks API and passed. No task was resubmitted,
no fixture was made to look successful, and the first failure log is retained.
This is agent-driven browser verification, not human QA, a signed phone
application, real provider generation, or real Windows/Herdr acceptance.
The image tool card's states were tested in the component suite; this run did
not execute another image generation to create a new browser transcript.

## Release and remaining boundaries

Only the final integrated source head should run the required remote checks.
The cache source had no observed PR CI at handoff; its source tests are not
represented as remote success. This environment's previously rejected `gh`
API is not retried or replaced by another credential. The parent coordinator
will inspect exact-head CI through its existing authorized connection and,
only after success, use the existing candidate publication flow.

Recovery preserves the limits in [its source evidence](../batch-recovery-reader/README.md):
no atomic expected-generation operation, no automatic claim/cleanup of
unknown resources, no Pi replay, no replacement of a changed ready reader,
and old writers must be stopped before enabling optional reader records.
Verified Pi completion remains separate from file delivery. The Provider
switch declares existing capability; it does not prove upstream reference
requests work. Legacy image policies remain preserved or explicitly rejected.
No production configuration, image tag, main branch, NAS/PC process, or data
was changed by this integration.
