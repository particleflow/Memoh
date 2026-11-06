# Native compact-result read diagnostics

Base: `4d23244c683aca4c436efec4db375921fdf60472`.

This patch diagnoses an existing explicit `computer_task_result` call. It does
not recover a past call retroactively, dispatch another call, start Pi, change
unknown/completion truth, replay an artifact, or implement another task system.
The shared runtime/contextview core, public tool schemas and error envelopes,
registration/permissions, revision CAS, timeouts and retry behavior are unchanged.

## Scope and correlation

The existing host logger is injected through `Source.SetLogger` at normal DI
construction. An explicit result call gets one process-local trace in its current
context. The synchronous read path records fixed enums/counters; the source logs
one warning only if the call returns an error WITH its retained computer object.
No trace is persisted into the operation, model response or public API. Normal
status/recover calls and successful result calls do not emit this warning.

Message: `computer task result read failed`.
Correlation: `operation_id`, original `operation_run_id`, current tool `run_id`,
`tool_call_id`, plus the existing logger's request/trace correlation if present.
Do not assume the current tool run is the operation's original producer run.

## Safe fields

- `stage`: completion_metadata, completion_page or completion_parse.
- `read_stage` / `reason`: native lookup/dispatch/snapshot, invalid envelope or
  session, duplicate/oversized frame, invalid base64, short page, exhausted window,
  ended context, invalid reader, or existing reader reservation/save failure.
- `file_kind`: fixed result/feedback/receipt/stdout/stderr/unknown enum;
  `offset`, `count`, `actual_bytes`, `completed_pages`.
  actual_bytes=-1 means no decoded payload was obtained, not a zero-byte file.
- `elapsed_ms`, `read_elapsed_ms`, deadline_exceeded, canceled. The read path still
  uses its original shared20s context. Feedback/wakeup elapsed times and failure
  booleans explain budget consumption before compact-result reads.
- `save_kind`, `expected_revision`, `save_error_class` apply ONLY to existing
  recovery-reader reservation/ready saves reached through the page-read path.
  A no_matching_row category does not by itself prove a CAS conflict rather than
  a concurrent permission/deletion/RLS change. Other save branches are not logged
  or changed. No extra DB query, revision rewrite or save retry was added.

A2269-byte completion should use offset0/count2048 then offset2048/count221.
For example, completion_page / short_page with offset2048, count221,
actual_bytes1, completed_pages1 identifies the second-page boundary. A parse
failure with completed_pages2 is a different branch. These are diagnostic facts,
not verified Pi/business completion.

No command, provider URL/key, private path, nonce, terminal text, file/base64
payload, model output or raw native/SQL error is written. The trace only adds
metadata; it returns the IDENTICAL existing errors, including sentinel identity.

## Isolated verification

Nine scenarios compare logging off/on: two-page success, first/second-page native
failure, short second page, bad base64, parse rejection, an already-creating
reader, failed reader reservation save, and frame-wait deadline. Each fixture
starts one synthetic Pi dispatch before collection, then checks unchanged error,
state, phase and command counts; no original task or real model is invoked.
Logs must correlate the original call, contain exactly one failure record and
exclude private fields/markers. A separate test checks exact error identity.
The intentional direct error comparisons are locally documented in that test;
no lint rule/configuration is changed.

Related bounded-read, reader CAS/revocation/restart and compact-result regressions
are selected explicitly. Core DI is compiled without executing unrelated tests.
Full commands, test list and actual check outcomes are in adjacent validation
files. This is not a full-repository test pass or Windows/production acceptance.

## Field procedure after normal integration/deployment

Only the unique authorized field owner may issue a separately planned single
original-operation result call after the deployed source/logger is confirmed.
Retain its toolcall and safe response, then select this one warning by exact
operation/toolcall correlation. Do not read or publish complete logs/transcripts.
If success yields persisted Pi facts, verify the original record; artifact
commit/download remains an independent condition. If unknown remains, stop and
use the concrete stage/page/deadline/save category. No log is not proof of a
completed read: success, a normal revision-change yield, an outer error without
computer, or a missing logger can all produce no warning.

This source task performs no field call, deployment, credential access, queue
cleanup or history alteration. A previous file-delivery success cannot mask an
unconfirmed Pi result. No production incident root cause is asserted here.
