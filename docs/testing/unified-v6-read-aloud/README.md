# Unified historical task navigation and speech boundaries

This candidate extends `98081cf290e026e9d384d92cc733e0c202674e8f`, whose parent
is the restored exact `aa379f639ad288112377d6b0421548d098fe6e0e` Bot v5 tree.
The independent validated release `2edfd88dafec2291df6eee9343a7a2bd494da24a`
remains in the full parent history and in a local checkpoint reference.

## Admitted source increments

- Task history: `8d48dfe7c7e84849c7986608f89a114b7bf85d75`, parent
  `453a1178c25213772dbead9f428f1dca83e2a027`, tree
  `7bf086765c25bc8e3194d3ed78c9a82ff76a450f`.
- Historical speech boundary: `b43875b5c541afef744909335004c0cb2f146c3a`, parent
  `81597deb05bac53edfa2dea3f75dbcd066382c1f`, tree
  `bcd380a47928fb8b8d9061300025406dfd79080b`.
  The latter parent is the author's local equivalent of the already admitted
  contact gate `bbbb448a0c6fd6f35f90d28316f4d5a51c5e9dc6`; it is not reapplied.
- Speech evidence: `b14c7f3e79ce95c78c1f6fab07be73efd41b6d48`,
  `docs/testing/chat-read-aloud/history-run-review/README.md`.

The v6 Library ZIP SHA256 is
`52c5b32b5cea66b36ab0b49ece69f1bf9f5f883e47a1018313f4fd56b7a6ac55`.
All 151 content hashes passed, with only the manifest itself unlisted among
152 members. Its patch reconstructed the exact source tree above.

## Combination review and executed difference check

The exact original task, Bot, session, run, turn and typed invocation must match
after existing account, Bot grants and session ancestry authorization. A missing
association retains the task without a fabricated latest-run destination;
storage errors remain errors, and opaque computer run IDs are not blindly cast.
History and unknown states remain intact. The read-only query adds no migration.

One overlapping Go composite literal was a formatting conflict only. The merge
retains the unified formatting and the new exact-run reader. The new PostgreSQL
test received import grouping and explicit ignored deferred cleanup results,
matching the existing test conventions. No product authority was relaxed.

The independent red case used the actual combined pane's speech expression with
the real speech and task-anchor composables, but simulated synthesis, media and
authorization. Same-session anchor selection left status `pending` and hid the
transcript while speech still reported `playing`; the expected `idle` assertion
failed. After b43875, that same one-case boundary passes: audio stops and its
source is cleared without another synthesis request. Its fixture supplies the
new expression dependencies and follows the actual corrected initialization
order. This is a composable integration check, not a new native-browser test.

The entire combined `chat-pane.vue` matches the reviewed b43875 source bytes.
The locator initializes before the speech hook; the gate checks both contact
navigation and `!taskAnchor || locator.status === 'located'`. Existing synchronous
disable handling aborts synthesis and consumes interrupted replies, so resolving
another run does not restart the old queue. Unanchored ordinary chat remains
eligible. No controller, player, styling or new UI was introduced here.

Reuse the author's unchanged 139 Web and nine Go/real-PG checks, four focused
speech cases, changed-file lint/UI guard and isolated Chromium evidence. They
were not rerun or relabeled as final-head CI. The independent initial external
test-path resolution failure executed zero tests and is not counted as a red.

## Consumer and recovery contract

Schema stays 179; configuration, Windows producer, held 1b filename behavior,
unmerged 3ca recovery and official Agent runtime algorithms are unchanged.
The generated SDK delta is the optional destination `turn_id` field. Existing
task APIs carry the original task/occurrence IDs and resolve the native turn.

Harmony must consume the precise shared-source hash list supplied with this
candidate: directory links, task-history utility, pane/locator and per-panel
parameters, contact navigation, locales, matching SDK type, both speech gates
and the corrected setup order. Do not replace its whole tree or discard its
native Back/artwork/attachment adaptations. Signed 1.0.27 lacks these fixes.
A new run-link client needs the matching backend `destination.turn_id`; do not
describe an old 2ed backend as supporting this new interface. Prepare one later
matched package after source alignment instead of rebuilding the old package.

Preserve the current database, history, queue and result receipts. New durable
commands and publication events need compatible consumers; a blind older-binary
retreat does not prove recovery liveness. Do not down-migrate, restore an old
dump over new data or delete a queue. The original 2ed image remains a separate
release checkpoint, not an automatically safe active-task rollback.

This source combination and its normal source CI do not approve a PR, merge,
publish an image, change NAS, sign a HAP or perform paid inference. Exact field
acceptance, ArkWeb media/activation, external-channel acknowledgements, the full
cancellation/revision matrix and human QA remain unverified. Prior source and
fixture failures remain recorded.
