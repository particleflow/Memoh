# Queue183 after the frozen4425 delivery

This is the next Server-only source candidate. The already-published4425 Server/Web, matching Harmony package and field deployment are unchanged. No second image publication or field action was performed here.

## Source and composition

- Base:4425e3c5f5940d71d65c98b0ba2290db14b97b43, schema182.
- Original durable queue:efac3b837b6c5291c09a714dddb4b73251367361, composed as2d088f39c1a943d5b6c7d5f927ec3ea21df3160d.
- Recovery correction:f13f0ea09374788ea028c02168f7d99874c43570, composed ase173d94b09501e0bb6aab263065895637a6702ae.
- Original-source trailers are retained. Only the canonical0001 EOF append conflicted: both presence and queue definitions are retained. Already-published presence182 is unchanged. The unreleased queue occupies the verified-free183 pair. Existing root migration rules permit renumbering this unmerged increment; no deployed version is rewritten.
- SQLC was actually generated with the repository-declared v1.31.1 command. The offline attempt lacked its module; a local-only compiler attempt correctly rejected Go1.25.7 because the generator requires1.26. The same declared command, normal module verification and module-selected official Go1.26.10 then succeeded. Rust/system Go were not modified. Generated diff is only the queue model/query; no manual generated edits.
- Web, SDK and native resources are byte-identical to4425. No new Harmony/Workspace build is needed for this Server-only increment.

## Necessary new evidence

`forward-fixture.go.txt` is the isolated manual fixture source, not an additional production command/service. It used an explicitly verified new empty loopback database, the exact4425 canonical schema plus the complete original through182 chain, then the combined normal up to183. `forward-182-183.log.txt` records exit0:182clean→183clean; eight original table projections unchanged, including presence claims/published receipts, original messages/run and an unknown computer operation. Presence forced RLS remains on. No down, force/version rewrite or old dump was used. The owned fixture database was closed/dropped afterward.

The same new-schema fixture verifies credential-free persistence without an encryption key; credential-bearing write refusal leaves the prior unknown item intact; the existing public source test's synthetic at-rest capability protects credential bytes and reads the retained unknown/claimed records; absence of that capability cannot decode ciphertext as an empty queue. A repeated up preserves the ciphertext exactly. This does not read, create or configure a real key. Existing auth.agent_credentials_encryption_key is reused; preserve old ciphertext/key access on recovery. JWT rotation is not queue encryption.

The unchanged independent MaxConns1 real-PG counterexample passes on exactf13:0.05s/exit0, TEMP tables only. Author concurrency/error/permission/restart tests retain their exact recorded source limits and are reused instead of rerun. Scoped `go vet ./internal/followupqueue ./cmd/internal/core` passed on the composition. The actual changed-head natural CI result is recorded separately, not borrowed from4425.

## Fixture incident: no false isolation claim

Two initial attempts and their logs are retained. First, wrapping only a canonical file as migration182 conflicted with its168 bootstrap stamp. More seriously, pgx ConnConfig.ConnString retains the original parsed DSN after Database is changed. The next attempt therefore ran forward migrations169–182 on the prepared cloud development database instead of the new empty database; the latter had no schema_migrations and the fixture exited1. That was not a production/NAS target, but it did change an existing development schema. The later read found182clean, users1 and bots/sessions/history/runs/unknown operations0; no pre-event row-hash baseline exists, so byte-for-byte data preservation is not claimed. No down/dump reversal was attempted. See C18 in the compatibility register.

The corrected fixture constructs an explicit target DSN from fields, connects it and verifies current_database equals its newly-created name and schema_migrations is absent before executing any migration. It then uses the full old chain. The final valid run is separate from the failed/mistargeted runs. All owned temporary databases were removed; the development database remains182clean and must not be described as unchanged.

## Later delivery and recovery

Before a later maintenance window the sole writer reads only backend kind, schema/dirty state, at-rest capability availability/validity (never its value), and original pending/claimed/unknown identities. Let already-accepted old memory inputs finish naturally; a public queue GET cannot reconstruct their authority or command. Then stop the old writer normally and apply183 with the qualified new Server. Retain presence182, all queue envelopes/receipts/unknown and the original encryption capability. Old memory-only readers cannot consume pending durable rows; use a183-compatible reader/writer or forward fix. The queue down file is preservation-only, not an operational rollback. Never delete unresolved items, globally down or overlay an older dump.

Real UI/HAP/restricted-account/restart acceptance remains a later sole-writer step. The original destructive-down incident in the author fixture remains historical and is not erased by this successful forward check. Current4425 field acceptance has priority; this source must not trigger another production switch by itself.
