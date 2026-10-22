# Cloud image wiring checkpoint — not a release candidate

This increment follows `e146c540e4154b4755411b5489afc58a639677c9` on the
same branch. It adds no service, credential, migration or Agent-core patch.
The foundation README describes the earlier, unwired checkpoint; the changes
below supersede its integration status, not its historical evidence.

## Storage admission correction

A billable provider request now requires an atomic, retained reservation in the
existing UploadStore first. The reservation counts against receipt, active-slot,
owner and byte quotas. Its maximum response size must fit MaxUploadBytes. Once
admitted, finalizing the same reservation can shrink it without competing again
with browser uploads for a new slot. This is finite capacity accounting, not a
guarantee against filesystem failure after dispatch. A crash or disk failure
before response bytes are durably staged still leaves an explicitly unknown,
potentially unrecoverable result; it never causes a provider replay.

The three pre-admission cases (maximum bytes, 16 active slots and 128 receipts)
were reproduced against detached e146 and tested against this correction. The
updated cases require zero provider requests. A concurrent-upload case fills
the remaining slots during Invoke and still publishes the reserved image.
PNG/JPEG validation now performs a complete decode after bounded header checks;
a truncated file with a valid PNG header is rejected.

## Actual service and tool wiring

- The existing `generate_image` tool accepts generate/edit/result operations.
  The old workspace write and inline/base64 result fallback were removed.
- Bot.ImageModelID and its existing provider record remain the configuration
  source. The new `image-edit` compatibility declares the OpenAI Images API
  edit contract for an OpenAI-compatible client. Vision/image-output alone do
  not enable editing. The model-management capability list exposes this flag.
  Unsupported edits do not fall back to generation. Existing generation
  adapters remain available through the same durable publication service.
- Current account, Bot chat permission, conversation ownership, ancestor chain,
  team and message-asset association are checked against PostgreSQL. This
  checkpoint admits owned chat roots and their subagent descendants; it does
  not claim support for every shared/channel-session policy.
- The application host carries the persisted root run identity. The DB root
  turn plus normalized request determines the business key; a new child or tool
  call does not generate another key for the same request. Unsettled work blocks
  changed keys in the same root scope. This is not semantic deduplication of
  arbitrary reworded, already-completed user requests, and it is not a token or
  an extra human approval process.
- The spawn tool's outer adapter collects IDs only from real service results,
  re-reads them with fresh parent authorization, returns structured artifact_refs
  and uses the existing live attachment event. No Agent loop, provider request
  composition or context-retention behavior was changed.
- Journal/upload assembly uses the same configured originals root and checks
  the opened upload directory identity at startup. There is no directory move.

## Verified

- Actual PostgreSQL + registered tool + existing outer SpawnProvider, with a
  synthetic child runtime and local HTTP edit provider: one original, exact
  multipart bytes, one provider call, ready media hash/full readback, structured
  child result and same-parent attachment event.
- A replacement child under the same root turn returns the original ready task.
  Unknown across replacement children and changed request keys cannot dispatch.
- An unrelated conversation, missing edit capability, and account revocation
  cannot dispatch or return a parent artifact. All test files are synthetic.
- Image/media race suites passed. Targeted tool/schema/subagent/application race
  regressions passed; service build and selected-package golangci-lint passed.
- A full tool-package run had seven existing image-download test failures caused
  by the environment's proxy address being rejected by SSRF validation. Proxy
  settings were not changed. A new failure in the old automatic-429-retry test
  was corrected to assert the new single-attempt contract, and that test passed.
- `internal/agent/runtime` and `internal/contextview` remain byte-for-byte equal
  to official baseline `bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046`.

## Remaining acceptance

The running current-source server is healthy, but real browser end-to-end image
verification is incomplete. Two synthetic browser requests stopped before any
model/image call: first the old development bridge socket refused connections;
then the host bridge could not create `/data` on the read-only host filesystem.
An isolated bridge container was attempted using only fixture mounts; its old
binary could not start in the cached Alpine image. No business request was
replayed after an uncertain image result. These are development-environment
failures, not production or paid-provider outcomes.

The verified child path is foreground. Background status currently has its
existing text result shape; structured image references in that retrieval path
and durable attachment-delivery retry still require follow-up. No claim of a
complete background image workflow or general channel delivery is made.
No real provider, production image, production credential or NAS deployment was
used. This checkpoint must not be released before browser acceptance, review
and the remaining delivery boundaries are resolved.

## Follow-up

The queue, background-result and browser gaps above are historical at b256.
See [the subsequent increment](background-and-browser.md) for fixes, current
verification and remaining limitations.
