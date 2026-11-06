# PR60 + memory lifecycle + file references + Web types

This is a source-integration checkpoint, not a release approval. The known legacy
media-read security finding remains open and is not repaired by this branch.
No production, NAS, Windows, paid model, new production credential, private
mobile source, Agent core change, PR creation, or release was involved.

## Fixed inputs and merge decisions

| Input | Fixed SHA | Merge |
| --- | --- | --- |
| PR60 base (includes 834 and PR57/56) | `e61459dab9c2f8bcd2a56166f6dac46d0756ba3c` | base |
| PR55 memory lifecycle | `75bbf15870ab931788e37b2b8caed99cae96a60b` | `a5e3a65b` |
| PR58 canonical memory file references | `a1df904c38e19c08cb78f1cb509697251d05936c` | `21ee4660` |
| PR59 Web typecheck | `b0f686e7862fc953f57d0dd39b0525340ebcc1a5` | `c6a80ec1` |

The three merges retain both parents. No frozen source branch was updated.
Code checkpoint tree: `01cd5ee2ed8f7d28e9d09d841dd63adea50dbe11`.

- `.github/workflows/go-ci.yml`: keep the full lifecycle suite and its six
  named PASS requirements; keep the separate file-reference PASS requirement;
  keep the transport/disconnect PostgreSQL selection including all
  `ComputerDeliveryPostgres.*` cases. The hard Web typecheck remains in ESLint CI.
- `packages/sdk/src/index.ts`: regenerate from the combined handlers using
  `go generate ./internal/handlers` and `pnpm run generate-sdk`; no hand-selected
  generated exports. All 354 method/path pairs from PR60 and all 351 from each
  other input remain. File-reference types and transfer-credential operations
  both compile in the combined SDK.
- `bot-memory.vue`: retain `until` for attachment-preview readiness and
  `shallowRef` for the public UI DateRange type. Neither feature is removed.

## Local verification (2026-10-22)

These are combined-checkout local results, not reused input PR CI results.
No combined GitHub Actions run is claimed.

| Check | Result |
| --- | --- |
| Frozen pnpm install, existing dependency store | exit 0; not a new clean runner |
| Web, SDK, Desktop typecheck | all exit 0; Web zero diagnostics |
| Web ESLint / UI contract | exit 0 / exit 0 (existing lint warnings remain) |
| Web production build / server build | exit 0 / exit 0 |
| Memory PostgreSQL race suite | 233 test/subtest PASS, zero skips |
| Connector disconnect + computer delivery PostgreSQL race selection | 24 PASS, zero skips |
| Related Web suites | 13 files, 207 tests PASS |
| Memory tool race selection | 19 PASS, zero skips |
| memory/media/computertask race packages without DSN | 501 PASS; 7 PG skips subsequently covered above; one opt-in read-volume skip |
| storage/handlers/server/apperror race packages | initial 1,231 PASS, one failure, one opt-in read-volume skip; failed upload case subsequently passed in isolation |
| Official Agent boundary | runtime/contextview match `bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046`; packages/runtime also unchanged |

PG used a new disposable database and the full embedded migrations through 172,
`dirty=false`. Both CI PASS-verification scripts were executed against the real
combined memory test log and passed. Upgrade/downgrade fixtures create their own
databases; no existing application database was migrated backward.

The first command included a nonexistent `internal/filestore/...` path. That
command exited 1 even though the other packages passed; the actual
`internal/storage/...` packages were then run. The initial boundary-suite failure
was `TestMediaHTTPUploadConfiguredAbove200MiB`, HTTP 500 while temporary free space
was low. After deleting only inactive outputs/caches from this same cloud task,
that unchanged test passed alone. The fixture discards the underlying error, so
this does not prove ENOSPC was the cause or establish a green rerun of the entire
initial command. No assertion, limit, or security check was relaxed.

## Real application check

Combined server: local port 18100. Current Vite source/dependencies: local port
18102. A separate UI database was migrated to 172, with a synthetic local admin
and one synthetic Bot. No provider/model was configured. Memories were seeded in
PostgreSQL; a 35-byte text original was uploaded through the real authenticated
HTTP media endpoint. There were no mocked HTTP responses.

The browser logged in, opened the New Bot dialog and entered a draft without
submitting, opened Bot General settings, read located/unavailable memory file
references, and opened the real text original in the existing preview. Narrow
and dark memory states were captured. No uncaught page error occurred. This is
Agent-run browser evidence, not human QA, natural-language/model acceptance, or
proof of Bot workspace provisioning. The two-node graph's initial fit can put
nodes outside the viewport (already reproduced at the original main in PR59);
this integration does not fix that separate issue.

- [Bot creation draft](bot-create.png)
- [Bot settings](bot-settings.png)
- [Available and unavailable references](memory-refs.png)
- [Actual file preview](memory-preview.png)
- [Narrow dark memory list](memory-narrow-dark.png)

See [results.json](results.json) for local test counts and log hashes. Raw local
logs remain in the cloud handoff; screenshots contain only synthetic data.
The later CrossBot / shared-delegation-UI integration must have separate evidence;
these results do not cover those additional inputs.
