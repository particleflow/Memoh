# Workspace observation and image-reference integration

This integration combines the existing `4521d323050e27d12a0e496ff51f0bd9bea938f6`
baseline with workspace observation from
`c4b77db84555080aba49fbfab132ebf351d0643e` and cloud image references from
`a43a77f2dfb090426ef2695a3a179975fa35ccb6`.
Implementation merge: `0413e0aac8274681fb8ee818fb4f8daed86b7e37`.
Later validation documentation and screenshots do not change application code.

## Merge review and boundaries

- Both source heads and the existing task-result/Range baseline remain ancestors.
  All 64 changed source-tree files preserve their source-branch contents.
  The only overlapping files are the three locale JSON files; their changed
  keys are disjoint and the combined values equal their exact union.
- No merge conflict or manual business-code edit was needed. Existing current
  target grants, PC-session scope and operation fingerprints remain enforced.
  Recovery reads original references; it does not submit work, recreate panes,
  infer completion from missing panes, or overwrite legacy receipts.
- Image queue Origin, current input references and background result projection
  stay in the existing outer host/tool/media adapters. The background queue
  remains in memory. Its image publication receipts are durable; this is not a
  new durable scheduler or automatic recovery of background conversations.
- No migration changes; the local database reports schema 170, clean. The
  frozen mobile task HTTP schema remains unchanged. Image `artifact_refs` are
  returned by the existing background tool, not added as general task-detail
  attachments. Task-detail attachments and cancel/retry controls remain absent.
- `scripts/check-agent-core-boundary.sh` passes against official baseline
  `bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046` for the complete Agent runtime and
  contextview directories. No upstream Agenvo connector change is included.

## Combined verification

- Current-source Go server, Vite and PostgreSQL were started and checked for
  health, real login and authenticated queries. Browser URL:
  `http://127.0.0.1:18082`; server URL: `http://127.0.0.1:18080`.
  All fixtures and screenshots are synthetic. No production PC, model, private
  file, credential change, production deployment or real native pane was used.
- Real PostgreSQL race integration tests passed for
  `TestImageToolPostgresParentChildEditReferences` and
  `TestSessionTasksPostgresOwnedTreeAndStableCursor`.
  The image test exercises registered tools and actual outer spawn/queue
  promotion with a synthetic child callback and HTTP edit provider. It verifies
  exact input bytes, not-yet-linked current inputs, two root-turn identities,
  same-turn replay through a replacement child, one provider call per admitted
  turn, ready-only stable hashes in the parent result, revoked/wrong-parent
  denial, and unknown result reads without another provider request. This is
  not production-model or browser end-to-end image-generation acceptance.
- Race suites passed for agenvo, computertask, imagejob, media, background,
  application, handlers, sessiontask, server, userruntime, workspace and core.
  Focused computer/image tool and queued-background race tests also passed.
  Source tests cover
  read-only native workspace/recovery authorization, mid-read revocation,
  legacy unknown receipts and immutable operation records.
- Six actual runtime/target HTTP checks plus real account/Bot browser flows
  distinguish HTTP-401 authentication unavailability, explicit device offline
  and native runtime unavailability. Only native `workspace.list` reached the
  temporary loopback MCP fixture. Operation records were unchanged and fixture
  configuration was restored.
- Sixteen task HTTP checks across eight stored outcomes, seventeen scope /
  revocation / cursor checks, eighteen OAuth no-store checks and eight Range
  checks passed. These used the real local server. Cross-block reads, disconnect
  resume with matching SHA-256, and unchanged operation records were checked.
- Real task UI verification checked list/detail, honest unknown and cancel
  intent, narrow Chinese dark layout, revocation clearing, and a delayed real
  detail response after an auth-clear event. A synthetic HTML/JS/Markdown probe
  rendered as literal summary text with no active elements or outbound probe
  requests. The file control selected a synthetic attachment without sending a
  message; an authenticated cross-block media read returned the expected bytes.
- The root Vitest configuration passed all 28 selected tests across five files.
  Scoped Go lint, ESLint, SDK typecheck, server build and Web build passed.
  The UI guard passed with its three existing warnings; Web build retains its
  existing large-chunk warning.

## Failed checks and limits

- The initial broad local race run failed seven existing network-image tests
  because the environment proxy resolves to a restricted address rejected by
  the existing SSRF policy. These are the same failures documented and baseline-
  reproduced by the image source branch in
  [background-and-browser.md](../cloud-image-foundation/background-and-browser.md).
  Neither proxy settings nor SSRF checks were weakened. Do not describe the
  complete local Go run as passing.
- That loaded run also hit animated-sticker and WebSocket deadlines. Both exact
  failing tests passed an isolated race rerun, followed by passing complete
  application and handlers race suites with package parallelism reduced to one.
  The first Web test invocation
  incorrectly used the app Vite configuration; rerunning with the documented
  repository-root Vitest configuration passed all 28 tests without code or
  dependency changes.
- Full Web typecheck still reports **442 existing errors**. Its report-only CI
  job must not be described as a successful full typecheck.
- Real PC reconnect, real Pi receipt recovery, production provider image edits,
  mobile integration and human QA were not performed by this integration.
  Prior source-branch browser evidence remains source-branch evidence, not a
  claim that its full image-generation browser flow was repeated here.

## Fresh browser screenshots

Screenshots were captured from the combined running application and visually
inspected. They are agent verification, not human QA.

- [Authentication unavailable](workspace-auth.png) versus
  [confirmed device offline](workspace-offline.png).
- [Chinese dark narrow layout](workspace-auth-zh-narrow.png).
- [Stored final summary and cancellation intent](result-light.png).
- [Literal summary probe](result-literal-summary.png) and
  [cleared detail after identity event](result-auth-cleared.png).
- [Attachment selected and authenticated Range checked](combined-range-selected.png).

The screenshots support UI observations only. Authorization, immutability and
queue/provider-call assertions come from the tests described above.
