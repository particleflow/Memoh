# Admission pause and LAN increment integration

The admission product checkpoint is `1dce64648fd6c33c282e3465a3f1bed0905a0074`
(tree `60d0a365baf7a9cc9fe847d60bf1bea282bf30f5`), based on
`acdb3c7aba0931929243921271abbb63afc54024`. Its [contract and verification](README.md)
remain unchanged by the transfer integration.

Only these two fixed transfer increments were cherry-picked with their source
references retained; the moving transfer branch was not merged:

- `393e90871b01e23027301f0670df9743ec014b07` became
  `1b116939d487e64b8c9f90d7bbc288ac1664d99d`: optional LAN route policy in the
  existing private transfer configuration directory.
- `a51ddcb1cab8e246ee32fe50762e633f221ef271` became
  `222ca19055175d5187d69ea4f140d2ae5ed5a1e2`: bounded Windows CI/package test
  grouping, retaining the original tests and their durability checks.

The only merge conflict was a modify/delete conflict for
`internal/transferclient/network_recovery_test.go`. The unified base did not have
this Linux dual-stack fixture, which the new policy tests use. The exact file
from `393e9087` was retained (SHA256
`9edd0bd58988e45cb95856a4edc0a9bdcc107b21972f2d09f2bb3d75335fdb5a`).
No previous transfer protocol or generated API tree was imported. Admission
Swagger/SDK output is unchanged by both transfer commits. Subsequent concurrent
API changes must regenerate from the combined handler sources rather than replace
these generated files with another branch's copies.

## Combined checks

These checks ran on `222ca190` plus removal of one pre-existing redundant blank
line in `producer_feedback.go`. The blank line was the sole combined package lint
finding, originating in earlier `b4564e7f`; removing it changes no behavior.
The resulting fixed product checkpoint is
`39890d2742202da1f8f85ed1a9b5981c52142765`, tree
`b4d4bdb7bd780a1d24ceb8e13aa317ccd84eb4e1`. The subsequent evidence commit changes
only this documentation directory.

| Command | Result |
| --- | --- |
| `go test -v -race -count=1 -timeout=90s -run 'Test(HostNetwork\|ConfiguredLAN\|DualStack)' ./internal/transferclient` | Exit 0, 8 top-level / 12 named PASS, zero SKIP, 1.179 seconds. [Output](evidence/lan-combined-race.log). |
| `golangci-lint run ./internal/transferclient/...` (2.10.1) | First exit 1 for the single redundant blank line; [original output](evidence/lan-combined-lint.log). After its removal, exit 0 with [empty successful stdout](evidence/lan-combined-lint-final.log). No lint rule or dependency changed. |
| `go test -count=1 -timeout=90s ./cmd/transferqueue` | Exit 0, 0.474 seconds. [Output](evidence/lan-combined-cli.log). This is the Linux CLI package, not the Windows package/NTFS suite. |
| `go build -o /tmp/computer-admission/transferqueue ./cmd/transferqueue` | Exit 0; [empty successful stdout](evidence/lan-combined-build.log). |
| `scripts/check-agent-core-boundary.sh` | Exit 0; [output](evidence/combined-core.log). Official Agent runtime/contextview unchanged. |
| `git diff --check` | Product/source diff exit 0. Adding raw outputs reports four terminal blank-line warnings in typecheck/Vitest logs; those original bytes are retained, not represented as a clean full evidence diff. |

The real TCP/TLS fixtures cover configured IPv4/IPv6 ordering, TCP-failure fallback
to the original authority's IPv6, later LAN recovery, cancellation, and refusal to
reroute on certificate or HTTP 403 failure. Original canonical authority,
HostBinding, credential scope, task/attempt/delivery identity, receipt and hash
remain unchanged. The payload is synthetic, 16 bytes; this is not a field network
test or a throughput claim. The routing option was not enabled on any real device.

The admission PG/race/HTTP and real isolated Server restart/browser outputs were
obtained before these disjoint transfer increments and are retained, not rerun or
relabeled as a second whole-suite pass. Web/SDK/Desktop source and types have not
changed since those checks. Windows grouping's upstream CI evidence remains in
[its source record](../transfer-native-ci-grouping.md); this environment did not
execute Windows. Final integrated remote checks still require the normal PR run.

## Cross-end handoff remains incomplete

Migration 0175 fences new admission, not host shutdown. Existing operations and
already reserved follow-ups still need reconciliation; accepted work may dispatch
after pause. Read-only reconciliation alone is not stop authorization. The owner
control path needs an explicitly configured endpoint program/hostRoot and a
Windows-aligned host run/configuration/native-ownership binding plus durable
acknowledgement. None is inferred from task workspace paths, names, local JSON or
file-transfer credentials. No stop command, new control pane, credential,
production mutation, image publication or deployment is performed by this patch.

All listed outputs are selected synthetic evidence. Existing failed runs are
retained. The earlier `*.log` files were ignored by the repository's global log
pattern; this evidence commit explicitly adds only the files listed in the
[checksum manifest](evidence/checksums.json), not an entire private work directory.
