# Default download compatibility on the observation/OAuth fix

Integration base: `d21b15581656048cb58246530c2131f0983f053e`.
Fixed transfer source: `648e4511f35fe2ce187509e9823d03c1d2e1d341`, delta from the already integrated producer `a51ddcb1cab8e246ee32fe50762e633f221ef271`.

Only the source delta's 27 files were applied. All 27 preimages matched a51; all postimages match 648e4511 byte-for-byte, including the source's retained failure history and its dedicated Windows large-byte test/workflow. There were no conflicts. The manifest records each file and hash. Source commits are e844456f, de05d053, 1fb178a2, 5c3b9837, 510814ee and 648e4511; the combined commit retains their original evidence without replacing any unrelated branch tree.

No Web, SDK/OpenAPI, provider, auth, direct-parent, official Agent core or migration files changed. The preceding OAuth fail-closed and observation activation fix remains intact. There is no new HTTP contract or field for clients to adopt. Therefore no generated files were rewritten and no unchanged Web tests/build were repeated. The current Web and screenshots in [the observation checkpoint](../task-observation-activation/README.md) remain the same source; they are not proof of native file transfer.

## Scope and compatibility

This increment removes the historical default 256 MiB **download** size/cumulative reservation gate. It registers metadata with an empty part, streams under existing worker locks and checks actual filesystem capacity per existing chunk. It does not raise an arbitrary total-file allowance or require a user budget migration. Local capacity waiting retains synced offset, identity and unknown association without spending the network-retry budget. Existing retained objects, bounded reclaim, historical publication migration and overflow/integrity checks remain. Source documentation: [native large-byte recovery](../transfer-native-large-byte-recovery.md).

The producer/CLI/library owns this behavior. Publishing or installing a NAS image alone does not update an installed Windows transfer executable. Windows must adopt the exact compatible producer separately; old writers still apply their old byte checks, so mixed old/new writers are unsupported for these downloads. No queue/locator reset or data migration is introduced. Real disk exhaustion, metadata storage failure and incomplete commits still surface as failures/unknown; metadata admission is not proof that bytes were delivered.

This is independent of Pi execution/results declarations and UI browsing. None of those protocols or semantics was extended.

## Combined checks

All commands used this combined worktree, Go 1.25.7, existing configuration and task-owned cloud test storage:

| Command | Actual result |
| --- | --- |
| `go test -race -p=1 -v -count=1 -run '(Download\|PublicationMetadata\|QueueRelease\|Association)' ./internal/transferclient` | [Exit 0](race.log), 95 named PASS entries counting parents/subtests, zero SKIP, 5.821s. Covers no payload preallocation, repeated capacity waits, offset/hash resume, worker exclusion, unknown association, reclaim process exits, and old publication migration with legacy and zero-quota modes. |
| `go test -p=2 -tags=integration -count=1 -v ./internal/db -run '^TestComputerDeliveryPostgresAuthorizationCASAndPreservation$' -timeout=180s` | [Exit 0](pg.log), 31.712s, zero SKIP. Existing isolated loopback PG and identity; the fixture creates/removes only its own unique synthetic database. |
| `node .husky/checks.mjs` | [Exit 0](aggregate.log), 17.18s: staged frontend script lint, Go lint and complete ordinary tests for `cmd/transferqueue`, `internal/db`, `internal/transferclient`. Ordinary `internal/db` result was cached and excludes opt-in integration tests; the explicit PG run above is separate. |
| `node --test .github/scripts/contribution-policy.test.mjs` | [Exit 1](policy.log): 15 passed, four YAML-loading tests failed solely with `spawnSync ruby ENOENT`. The unchanged Ruby dependency is absent locally. No rule/test was disabled, no fake Ruby shim was used; exact-head remote contribution CI remains required. |

The real PG/HTTPS route test built and invoked the actual CLI upload: 9,800,163 bytes, a lost PATCH response recovered the original receipt, Create/Complete/Commit each one. Library download resumed from offset 8,394,902 and matched the full content/hash. Large/backlog download metadata was accepted without payload GET. Linux CLI publication remains explicitly unavailable; its response was not treated as native success. The same parent test covered three real 17 MiB process-recovery transfers and original IDs, authorized/revoked access, result-page stability and `/api` Range/HEAD/query behavior; see the named outputs for the exact scope.

No Windows execution or 1 GiB transfer was rerun in this Linux environment. The fixed source's Windows evidence is linked in its retained README and was reported green by the parent; that does not replace CI on this combined head. No physical PC, phone, NAS deployment, production key or model request was used. The existing isolated PG instance and source database were preserved. This integration did not clean more cache or storage.

`manifest.json` records the original/published log hashes and normalization. Remote CI status, candidate publication and installation remain separate steps. The security checkpoint d21b1558 remains independently addressable.
