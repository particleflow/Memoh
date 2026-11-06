# Unified JSON Images, transfer recovery and task observation candidate

This integration retains the fixed histories and evidence of:

| Source | Exact commit | Increment |
| --- | --- | --- |
| PR #76 | `7bcbba5ac85a2ba0769b48ed8d4334db4b7806f7` | Existing unified candidate plus explicit JSON reference-image encoding. |
| PR #77 | `e30c331e43ebbef78fbdddf47a955a21402f10ed` | Synthetic real-server/PG/CLI three-file process interruption/recovery test and its separate bounded CI selection. |
| PR #78 | `439f21ea068faf41e09e2ca32fc914751c51ef64` | Web downstream task observation resumes after parent-read recovery, with existing scope/identity invalidation. |

Both PRs have the exact PR #76 commit as their merge base. Their changed-file
sets are disjoint (5 and 21 files). Merge `a2203698e4da7a5509b9621e68020def9bde1a81`
retains both fixed heads as ancestors and all **26 source blobs byte-for-byte**.
There was no manual conflict resolution, protocol alteration, history rewrite,
source-branch update or generated API replacement. The merge product tree is
`7e131d2b5f889b473e032764d4fed1ca90380419`; this evidence directory is a later
documentation-only addition.

## Checks on the merged checkout

| Command | Actual result |
| --- | --- |
| `pnpm --filter @memohai/web typecheck` | Exit 0; [output](web-typecheck.log). |
| `pnpm --filter @memohai/sdk typecheck` | Exit 0; [output](sdk-typecheck.log). |
| `NODE_OPTIONS=--no-experimental-webstorage pnpm exec vitest run --project web apps/web/src/pages/home/components/session-tasks.test.ts apps/web/src/pages/home/composables/useSessionTasks.test.ts apps/web/src/pages/home/composables/useBotDelegations.test.ts --maxWorkers=2` | Exit 0; 86 passed, no skip; [output](ui-focused.log). |
| `pnpm --filter @memohai/web exec vite build --outDir /tmp/unified-7b-77-78/web-dist` | Exit 0; chunk warning retained; [output](web-build.log). |
| `bash scripts/check-agent-core-boundary.sh` | Exit 0; runtime/contextview match official `bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046`. |
| Fixed-head ancestry, 26 blob comparisons, `git diff --check` | Exit 0. |

Outputs are exact selected cloud logs, checked for private fixture secrets before
publication. The root filesystem is full; the isolated temporary checkout and
existing dependencies were reused on `/tmp` (about 6.6 GiB initially available).
No source, database, original, other-task resource or global cache was deleted.

The original [transfer fault evidence](../transfer-batch-process-recovery.md)
and [real browser/PG observer evidence](../web-observation-reconnect/README.md)
remain unchanged; their old success is not presented as a rerun. There are no
backend/product changes in PR #77 and no overlap with PR #78, so the browser
business scenario was not repeated. The new final head must pass its own Go CI
(including the real PG recovery step), Web lint/test/typecheck and Docker PR
before the existing candidate image workflow can run. The Draft PR links those
final-head results; no child-PR green check substitutes for that gate.

## Delivery boundary

A candidate image build is not a deployment. Keep the old `7bcbba5a` candidate,
all source branches and earlier evidence. Do not merge main, move main/latest
image tags, or deploy NAS from this integration task. A successful candidate
build must have verified revision labels and both linux/amd64 and linux/arm64
manifests for server, web and workspace, with exact digests in the handoff.

The legacy media P1 production hold remains. Real Magpie image-edit behavior,
Windows/NAS joint transfer acceptance, mobile source/signing/distribution,
OAuth recovery and production deployment are not established by these synthetic
checks. The new PG test kills a Linux CLI child, not the server/PG or physical
power; the Web observer polls and does not provide a runnable queue or live tool
log stream. No real provider/PC task, production credential or private conversation
is used here. Human QA remains unperformed.
