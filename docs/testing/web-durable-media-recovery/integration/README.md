# Unified integration checkpoint

Product commit: `7873803f1901ddd5f02193427cc45a1f545051ce`. Parent: `2d03f6cbabad67b242d33180cc36bf02d423e6d5`. Its complete tree is exactly the independently reviewed handoff tree `ed3244a75033d4f038e616cbdb439d7c992c9831` (38 changed files). Source patch SHA256: `14e432485e0bafa47463aa427a0a49a2e599d344420c2f0e82f72813fff5429b`. No backend, schema, SDK, native phone source or official Agent core changes were added.

The handoff README records its historical pre-submission state. The user subsequently explicitly authorized submission for this task with Human QA still unverified; repository rules were not changed. The existing PR remains draft and **Human QA is not passed**. This evidence-only follow-up does not change the reviewed product.

## Necessary combination validation

- `pnpm --filter @memohai/web typecheck`: exit 0.
- `sh .husky/pre-commit`: exit 0; complete Web ESLint 0 errors / 39 existing warnings, staged size check passed, no Go packages selected. The 160 scoped source tests and the earlier build were reused, not rerun or represented as a fresh final-tree build.
- Existing isolated loopback Vite `18402` with Server `18400` / PostgreSQL: authenticated account GET 200, actual composer file selection at 620px/Chinese/dark, current transfer/storage module imports, SHA256 of `abc` correct. Zero media/business writes and zero page errors. [Screenshot](narrow-zh-dark.png) was captured and inspected. This small combination smoke does not repeat or replace the source process-kill recovery proof.
- Independent focused review found no determinate P0/P1; all 38 files matched the handoff tree byte-for-byte and the source patch hash matched. Secret/private-address review found no actual credentials or private network addresses.

## Environment failures retained as limits

Temporary disk exhaustion initially interrupted `git apply` at the lockfile. No commit occurred then. Only this task's generated context archive and partial generated source were removed; source, databases, originals and prior evidence were retained. The 38 approved files were restored from the fixed reviewed Git tree and byte-compared before submission.

A local frozen offline install could not use the stale PNPM-home/store paths (ENOENT, then ENOTDIR on the dangling task-cache link). No lockfile regeneration or dependency upgrade was used to hide that failure. Local validation linked the already installed exact locked `@noble/hashes@2.2.0` package; normal fresh frozen installation remains a remote-CI check. No claim of a successful local clean install is made.

No field NAS/Windows/Harmony acceptance, real confirmed chat delivery, actual LAN/IPv6 selection, production deployment, HAP build, main merge or candidate image publication occurred. The source's isolated multi-process byte proof remains separately scoped in the parent README.

## First integration CI failure and exact correction

The first submitted head `e55cea1af21c4e1c86abbdcb0081cb4927e94a2c` failed the existing task-observation test step in [ESLint run 40275432093](https://github.com/particleflow/Memoh/actions/runs/40275432093), job `121417071383`: `session-task-results.test.ts:97` expected a bare anchor click for a dialog-only host. 295 other tests in that step passed; subsequent steps were skipped. Its Lint job and both fresh dependency installations passed. No test or lint rule was disabled.

The same single assertion failure was reproduced locally (7 passed / 1 failed). The product intentionally changed that fallback to the shared durable downloader; the omitted integration fixture still expected the superseded direct URL behavior. Only that test fixture was updated to mock the real downloader boundary and assert exactly one call with the reader Bot, original hash, size 42, original undefined delegation path, name and AbortSignal; both hosts now forbid the old bare anchor click. The preview host must still perform no download, and pending/denial/global-Bot checks remain intact.

Necessary combination regression: the task-results, attachment-block, delegation-download and media-transfer files passed **48/48**, exit 0. Product code and the previously inspected real UI were unchanged by this correction. See [red output](task-red.txt) and [green output](task-green.txt). Existing in-progress CI is retained rather than relabelled successful or blindly rerun. The next normal source push triggers checks for its exact new head.
