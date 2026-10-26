# Unified central-policy integration

The existing PR79 branch starts this increment at `391adba82a2347123f02f29fdbd289cde84d26cb`.
The Library source receipt records both exact reconstructed source trees and every
patch hash. NAS source `e34e99e6356c0e79ecc634a4ea833a9c0a81fb29` matched all 22
manifest entries. The separate phone tree matched all 8 entries and was **not**
applied, compiled, signed or included in the already prepared 1.0.13 bug package.
Integration additionally corrects import grouping in three new Go tests and four
lint findings in the new client test (unused parameter, standard HTTP constants).
No assertion or product behavior changed. Normal Swagger and SDK regeneration
reproduced its five generated outputs.

## Scope and compatibility

This closes a demonstrated configuration-distribution seam by adding the optional
`network_policy` field under the existing `[files]` section and reusing the
original authenticated GETs. The optional
field adds no API operation: all 371 method/path/operation-ID tuples remain
unchanged. There is no migration (schema177), configuration deployment, service,
probe, credential, journal format or official Agent/core change. Existing access
checks, Bot/task identity, receipt, authority, TLS identity and uncertain-write
reconciliation remain. Browser Fetch receives metadata and keeps platform routing.

The policy is visible to callers already authorized for `/users/me`; it is not an
admin-only configuration response. Real address values and their authenticated
visibility must be reviewed before enabling the optional section in production.
Omission keeps existing behavior. No private deployment configuration was read or
changed. Existing Windows binaries do not acquire new code merely from a NAS
image update; their original owner must package/adopt the consumer separately.

## Executed integration checks

- Focused Go media/handler/client policy and existing host-network regressions:
  exit0. The first regex missed the newly named configuration test; its exact
  `TestFilesTransferNetworkConfiguration` was subsequently run and passed. This
  boundary is preserved in the two output files.
- New real-SDK policy regression: 1 passed, 19 unrelated cases intentionally
  skipped by the explicit test-name filter. The old160/129 matrices were not rerun.
- Full Web and SDK type checks, normal Swagger/SDK regeneration and UI contract:
  exit0; UI contract retains 3 existing warnings.
- Current Vite18402 Web was opened with the existing synthetic authenticated
  Server18400/PostgreSQL environment. Its real SDK identity GET accepted the old
  server's omitted policy; the original Bot and canonical origin were preserved,
  with zero business writes and page errors. The screenshot was inspected. This
  tests **old-server omission compatibility**, not the new server response or LAN.
- Source-owner readable outputs are retained separately: 12 Go top-level/9
  subcases and isolated current Server/PG transfer of65,573 bytes with one
  Create/PATCH/Complete and matching hash. This is reused source evidence, not a
  fresh integration transfer, phone device or Windows field test.
- The first staged aggregate failed writing this task's Go cache (ENOSPC), before
  completing lint. Its output is preserved. Only regenerable cache entries under
  the existing task-owned Go-cache directory were reclaimed; sources, database,
  original files, binaries and raw logs were preserved. Recovery results are
  recorded separately, without rewriting the initial failure.
- After cache recovery, the full scoped lint found four test-only issues above;
  its failure is retained. After correction, the same scoped `golangci-lint run
  --concurrency=2` over all seven selected packages exited0; the focused Go
  regressions across four packages also exited0. This checkout has no configured
  `core.hooksPath`, so these were explicitly executed checks, not an automatically
  executed Git hook. No rule, assertion, timeout or workflow was changed. Complete
  repository/Windows/PostgreSQL gates belong to the final-head remote CI.

## Still open

The original canonical authority must be reachable for the bootstrap GET. Browser
Fetch has no TCP hint consumer. Windows retains ordered LAN candidates, a bounded
LAN dialing phase and original-host IPv6 TCP fallback, with no TLS/HTTP fallback;
the existing proxy conflict remains a refusal. No per-block probe was added.

In the separate phone patch, safe JSON GETs can select/cache a route, but HEAD,
file-byte GET and writes remain single-dispatch. A cold HEAD/file request uses the
first candidate once; generic RCP timeouts do not prove that bytes were unsent.
Actual RCP/ArkWeb TLS-error classification, ordered LAN behavior, IPv6 fallback
and physical reconnect remain unverified. No URL/origin substitution or token
movement is authorized by this metadata. An unknown Create/Commit still reconciles
the original ID instead of replaying it.

F01/F02 and Human QA remain unaccepted. This source increment does not move the
published391 images or deploy NAS. Actual security/network configuration,
Windows packaging and a future phone combination remain separate owner actions.
