# Reviewed Bot v3 integration on c304

The initial local source integration was prepared without a fresh CI/test/build
matrix, paid model request or production action. Its later authorized source
push triggered normal CI, recorded below. This is not a published or deployed
version and does not establish whole-product acceptance. Human QA remains
unconfirmed.

## Lint-only follow-up to the initial integration

The original `6a1fdcc1d61427c5cb02e0184a2b48a92645dac2` Go workflow
`40772835444` passed Test, pgvector, Agent boundary, Redis, Valkey and PostgreSQL
fencing jobs, but Lint job `123114289444`, step4, failed. Its
[22 original diagnostics](go-lint-6a1-failure.txt) are retained: four cleanup
return-value checks, eight import-group findings, two conditional-chain findings,
five formatting findings, two constant-error constructors and one HTTP method
constant. They are source findings, not an infrastructure failure.

The follow-up only addresses those diagnostics in ten source/test files. It
preserves the claim transaction, post-lock authorization, terminal-state choices,
unknown/no-replay behavior and test assertions. CI configuration is unchanged;
3ca result recovery is not included. The union snapshot below describes the
pre-lint integration, not byte identity after this formatting/style correction.

This workspace resolves `go` directly to `/usr/bin/go`, with no shell alias; that
nonstandard executable cannot run `go version` or `go env`. No matching installed
toolchain was found in the checked normal locations. CI uses setup-go with
`go.mod` (Go1.25.7); the older mise pin is not changed by this fix. No toolchain
was downloaded. The existing standalone formatter applied the correction, but
its gci/goimports phases reported the unavailable `go env` and are not claimed
as a local full-lint pass. The separate gofumpt-only diff check and
`git diff --check` passed; full lint/type validation remains a remote CI gate.

## Exact provenance

The first parent line retains the existing unified branch and its fixes. The
reviewed source head is retained as a second parent of the integration commit,
so its original ancestry (including the evidence-only follow-up) remains
reachable. The five selected changes were first applied in the required order
with `cherry-pick -x`; there was no unreviewed branch-tip merge.

| Slice | Reviewed original | Local first-parent application |
| --- | --- | --- |
| Internal result origin | `f5de31d5aba2b50f64df1813919eee15764ddfee` | `4a6d3f44d7b3d6238ca155958585945f309e7bf0` |
| Fixed-contact entrance | `9f3dc3a288dd472aa039b8098b9f0d6808a2a46a` | `9899c5f966df109f46c185136ee5d3dbd3d9a623` |
| Original Bot task directory | `e5a1aa75cf79267b659b63b24bc0ee60cd46b7e7` | `67e82daf6d995f1e7bd5f5f5ba97ad63d37806b3` |
| Permission-read error propagation | `271f2b97451e572e6fe89eaccd0b3c9443a71794` | `336a66238d45109cd5e9b7a0a35090c336e4835f` |
| Durable native queue identity | `4768679adf01047bff66c148ded37f215c9994be` | `76f04189e3dec3781933c15d5fe221fb998a8713` |

Integration base: `c304609ab15b15fac5eb07cc92b7611d15841644`, tree
`9fd86f1f274ae81f9dab21be12e7c3d84c16145e`. Reviewed source head tree:
`c3b03a164e7741dd21e7cec0c5314a0555c92e59`.

Only `docs/bot-dialog-task-layer.md` conflicted when recording source ancestry.
Its reviewed final text restores the real runtime recovery and initial fixture
failure disclosures from `27d2352f91d4ed83d63c3c9b13142cc01eae7b06`.
The status link in that historical document points here to avoid presenting its
older pre-queue limitations as the current implementation state.

## Combination-only checks

- [Source union check](source-union-check.json): all 65 non-document source files
  equal the reviewed source head; all 16 base-only files equal c304; path sets do
  not overlap. SQL-generated Go, Swagger and generated SDK are included in that
  byte comparison, not edited or regenerated independently.
- English, Chinese and Japanese JSON parse and contain the queued-state label.
  No migration or official native runtime algorithm file changed.
- The existing UI contract guard passed with three existing warnings and no
  baseline changes. Its first invocation failed because this checkout's UI
  submodule directory is empty. The recovered invocation used an isolated guard
  directory, the actual combined Web files and a clean already-present UI tree
  at the identical pinned commit `f69402a55dd87a59c10722fe11507ad8fd7048f5`.
  No dependencies were downloaded and no guard assertion was disabled.
- `git diff --check` and conflict-marker checks apply to the final combination.

These are integration guards, not a fresh final-head runtime/UI acceptance run.
No combined-tree screenshot or remote CI success is claimed.

## Reused evidence and its limits

Author handoff: Library `libfile_aeb7be4b95f08191bef4fa9938346a46`, version 3,
ZIP SHA256 `bdd463cec7d3e8d062a08e717db059c8df8ebbffd1443a5af763e01bf99a994e`.
The consumer verified the ZIP and all 61 manifest members before review.

The unchanged 285 frontend checks were not repeated. The earlier slice evidence
covers result-origin live/history consistency, duplicate delivery, fixed contact
navigation, original task discovery and PostgreSQL history preservation. V3 adds
permission-read red/green tests and focused tool/background tests. The actual
isolated PostgreSQL native-queue test passed with eight concurrent claims/one
winner, duplicate completion callbacks, fresh-host recovery, unknown/no replay,
and revocation during lock wait. A different older directory PG test was skipped
in the v3 focused log because its DSN was absent; its earlier real-PG evidence is
separate and is not relabeled as a new v3 pass.

The actual process-crash experiment recovered the same task/record/session with
one child model invocation. It preceded the final post-lock permission recheck;
that final defensive check has separate PostgreSQL and actual UI evidence.
The author's real Server/Web evidence used a scripted loopback model, not paid
inference or autonomous real-model task planning. Its screenshot paths are
producer-local and are not claimed as consumer-local screenshots.

## Version and consumer matrix

| Consumer | State at this checkpoint |
| --- | --- |
| NAS Server/Web | Parent reports deployed `a0a821a4cdaa32121a945d04e80c76bba4b6f2b4`, schema179. This local integration has not been published or deployed. |
| Existing c304 fixes | Turn-end cancellation and explicit declared result filenames remain byte-identical. |
| Windows producer | Explicit filename support still needs the matching actual producer executable; the old 57/d034 labels or unchanged 0.10.1 string do not prove adoption. No Windows files or queues were migrated here. |
| Harmony shared source | Must consume the reviewed contact/directory files, queued locales and matching generated-source inputs. This NAS source integration does not imply those phone inputs have been consumed. |
| Harmony Back | Separate reviewed consumer `5445b299657dfe092083c916f146b1e5ba078844`, dependent on fixed-contact shared navigation. Not merged into NAS source. |
| Harmony artwork | Separate owner reports `bd0b3933d24d944973fdca8e7d22c6f6b2a507bc`, parent5445, tree `45a2916d09d84ea81760faa302c3533a28702c1b`. Prior independent resource-tree review is retained; this is not a remotely read-back commit or a built/signed package. |

## Remaining requirements and rollback

Durable main-Bot summary publication/unknown notification is the dedicated Bot
owner's next increment and is not included. Receipt completion idempotence is not
proof of user-summary publication exactly once. Native tool activity is still
visible in the primary chat. Task navigation opens the original execution
session rather than an exact historical run anchor. Exec/video/dependency kinds
outside managed spawn/send, late-after-stop behavior and broader publication
collision/restart barriers remain unverified or incomplete.

Restored work rechecks account, ancestry, Bot grants and selection. Remote work
without the original trusted host binding fails closed; no binding is fabricated.
Already-claimed/unknown work is retained and not automatically rerun. An unresolved
claim may conservatively block successors. The original history is not merged,
deleted or rewritten into the contact.

Schema remains179: do not use migration down or an old dump as rollback. Preserve
the current database and task records. New `internal_command` payloads and v3
resume records require compatible consumers; old binaries reject them and do not
provide queue/result liveness. Older code also cannot restore native queue
receipts. Stop/drain affected work before any application retreat and retain a
compatible queue/receipt consumer; no seamless active-task rollback is established.
No production rollback or data deletion was performed.
