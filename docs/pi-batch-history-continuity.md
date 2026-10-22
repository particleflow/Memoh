# Pi batch history continuity

This host guard applies to new `pi-json-v1` computer operations. It does not
change Pi, Herdr, Agenvo, or Memoh's Agent runtime. There is no new schema,
executor, storage service, or automatic recovery/replay mechanism.

## Upstream contract

The contract inspected is Pi 1.2.0 at
[`06a466935536f8c9bcacb2d5efc0fc021be1bdff`](https://github.com/earendil-works/pi/tree/06a466935536f8c9bcacb2d5efc0fc021be1bdff).
[`main.ts`](https://github.com/earendil-works/pi/blob/06a466935536f8c9bcacb2d5efc0fc021be1bdff/packages/coding-agent/src/main.ts)
accepts `--session <path>` and forbids combining it with `--session-id`.
`--session-id` creates a new session when discovery finds none.
[`session-manager.ts`](https://github.com/earendil-works/pi/blob/06a466935536f8c9bcacb2d5efc0fc021be1bdff/packages/coding-agent/src/core/session-manager.ts)
uses append-only v3 JSONL trees, skips invalid JSON lines during loading, and
catches discovery read errors. Even an explicit missing/empty path can initialize
new history. An ID echo, exit receipt, model reply, or wrapper nonce cannot
establish that prior history survived.

## Host checks

1. Claim the existing durable unsettled operation slot. Duplicate requests retain
   their original operation; unknown requests are never replayed.
2. Read the latest prior operation for the same team, bot, Memoh conversation,
   user, target, and stable Pi session ID. No prior operation permits first-use
   checks; a database error does not. Never skip a newer unproven operation to
   find an older usable checkpoint. Registration changes fail closed.
3. Persist the expectation in the existing operation metadata before remote
   mutation. A legacy or incomplete record without valid file evidence blocks
   automatic continuation. A proven not-started receipt preserves its original
   expectation, including missing-history expectations.
4. Under the existing exact session-directory `mkdir` lock, check the local
   file. First use requires an empty session directory. Continuation requires
   the anchored basename, byte prefix length and SHA-256; appended bytes must
   also parse as UTF-8 JSONL with a matching v3 header, unique entry IDs and
   valid parent links. Empty, missing, modified, malformed, unreadable,
   ambiguous, or unsupported histories prevent Pi startup. A missing resume
   directory is not recreated. No old file or operation is removed or rewritten.
5. First use invokes the original `--session-id`; continuation invokes the
   original `--session` with the verified file path. The wrapper snapshots the
   entire pre-invocation file and verifies its prefix again after Pi exits.
   A supported resulting file containing a user or assistant entry can yield a
   new checkpoint. Pi flushes on either role, so an unfinished first user turn
   is valid persisted history; no assistant response or completion is implied.
   The receipt records `before`, `after`, and optional evidence.

The file locator is relative to `resolveBatchLayout`'s existing session directory.
No absolute host paths or transcript bodies enter operation metadata. Files
larger than 64 MiB, unknown formats, extra session-directory files, symlinks, and
ambiguous histories are conservatively unconfirmed. This is a conservative
structural check plus byte continuity, not a complete duplicate of Pi's schema or
context projection. Legitimate future formats need deliberate contract review.

Pi's normal no-retained-entries compaction uses its own ID as
`firstKeptEntryId`; retained-entry compaction must refer to an ancestor on that
branch. Branch summaries require string `summary` and `fromId` fields. The
official `fromId: "root"` sentinel and a new root branch are supported. An
unavailable source reference (including an imported fork) is unconfirmed, not
repaired. The last entry selects the reloaded branch; conversation entries only
on an abandoned branch do not confirm a setup-only or empty-summary new root.
Valid setup-only/empty active branches are unconfirmed, not corrupt. These
checks follow the pinned `session-manager.ts` and its official
[`session-context-edit.test.ts`](https://github.com/earendil-works/pi/blob/06a466935536f8c9bcacb2d5efc0fc021be1bdff/packages/coding-agent/test/session-context-edit.test.ts#L129)
no-retained-entries compaction regression.

`verified` means preserved file bytes and the supported branch structure at the
check time. It does **not** mean all old messages remain in the current model
context. Branch switching, compaction and context edits can legitimately omit
or replace model-visible content while every old byte remains in the file. The
host does not duplicate or certify Pi's full model-context projection.

The current file limit is **64 MiB**. Exceeding it blocks continuation as a
resource-limited `unconfirmed` result, not history corruption. There is no
automatic truncation, rotation or reset and no promise of unlimited long-term
session management; deliberate operator review is needed at that boundary.

## Reading results

Execution outcome and continuity are distinct. `process_stopped` / `completed`
prove the invocation exited; `pi_result` reports Pi protocol outcome, not business
acceptance. `continuity.last_invocation` records checks at that invocation only.
`continuity.status` remains `unconfirmed` on later observations because cached
receipts do not establish current filesystem availability.

`computer_task_recover` retains its read-only native metadata contract: it does
not type commands into panes or inspect live history files. It explicitly reports
current continuity as unconfirmed, including `legacy_evidence_missing` for old
operations. The next authorized new task checks the file before starting. There
is no automatic import/adoption of unanchored legacy histories.

The unified task registry continues to project the same operation and locator;
its Pi summary states that current history continuity is unverified. No separate
session-management protocol or new-task/follow-up switch is introduced. Current
scope still uses the raw `work_dir` spelling: two spellings may identify different
logical sessions even if the filesystem resolves them to the same directory.

## Limits and validation

The DB slot and local directory lock cover the system's serial execution path.
Neither can fence an external TUI or another process with the same filesystem
permissions. There is a check/open race in upstream Pi; arbitrary external edits
cannot be made absolutely atomic by this host wrapper. Post-run checks detect
loss/change but cannot undo business side effects that already occurred. Stale
locks are never stolen, and uncertain dispatch/persistence outcomes stay unknown.
Loss or rollback of the operation database itself is outside this guarantee.

Synthetic tests execute the actual generated Bash/Node guard with a synthetic Pi
process, including creation/resume, prefix damage, unreadable and missing files,
missing directories, malformed entries, post-run loss, old records, restart,
duplicates, legal self-referencing compaction, branch summaries/switches,
user-only first turns, size limits, latest-record selection and scoped PostgreSQL reads. They do not
prove actual Windows/Herdr/provider behavior or acceptance of a user's objective.
No real PC/NAS or model provider is contacted. Official upstream source was
inspected; a real official Pi/Windows runtime was not installed for this task.
The existing checkout has no official Pi coding-agent/core runtime dependencies,
so source-derived fixtures are not presented as an executed official Pi test.

Likely overlap with concurrent native Agent tracing work is limited to
`internal/computertask/service.go`, `observation.go`, `source.go`, the computer
operation store/query files, and the Pi summary in `internal/sessiontask/service.go`.
The implementation does not copy or replace native Agent per-turn tracing.

## Cloud validation record

- `go test -race` passed for `internal/agenvo`, `internal/computertask`,
  `internal/db/postgres/store`, and `internal/sessiontask`.
- Isolated PostgreSQL integration passed for prior-history scope/latest-record
  selection and the existing concurrent durable-operation/CAS isolation test.
  `scripts/test-pi-batch-history-postgres.sh` now requires the exact history test
  to emit PASS, rejecting missing selection, SKIP and skipped/failed subtests.
  Go CI appends this gate without removing any existing checks. Earlier green
  CI runs did not select this new history test and do not prove it ran in CI.
- golangci-lint 2.10.1 passed on changed packages with the fixed baseline filter.
- Agent runtime/contextview boundary check passed.
- Cloud server build, authenticated backend/Vite queries, and actual Chromium
  synthetic task list/detail interactions passed. See
  [UI evidence](evidence/pi-batch-continuity/README.md).
- Full `go test -race ./...` was run and **failed** in four untouched packages:
  seven image URL tests hit the environment proxy's restricted address guard;
  two container resolv.conf fallback tests observed mode 0600 instead of 0644;
  the bridgesvc cancellation test observed its synthetic child still alive.
  These failures were retained, not repaired or bypassed in this change.
  A subsequent identical three-test comparison on 834 and the bea6 checkpoint
  reproduced the two permission failures and the cancellation failure in both.
  The seven media tests were not repeated; their baseline attribution remains
  unconfirmed. Unchanged source files alone are not baseline test evidence.

CI, synthetic receipts and UI screenshots do not constitute human QA, official
Windows runtime compatibility testing, or business acceptance.
