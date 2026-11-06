# Native durability CI: preserve coverage with bounded groups

Base: `393e90871b01e23027301f0670df9743ec014b07` on the existing PR77 branch.
This increment changes only the Native workflow, its packaged verification
script and evidence. No product source, queue limit, filesystem operation,
fsync call, test body or durability assertion changes.

## Failure and comparison

[Run 40163966452 / job121081104769](https://github.com/particleflow/Memoh/actions/runs/40163966452/job/121081104769)
failed at the Windows-prefixed package's **aggregate five-minute timeout**
(300.193s). `TestWindowsNativePublicationBucketBudget` had run only 14 seconds
when interrupted in the existing metadata census / `FlushFileBuffers` path.
The complete CLI, PG/NTFS, build, package verification and artifact steps were
skipped. This was not a certificate/real-device failure or a five-minute failure
of that individual final test.

| Same test or group | Previous passing 8b83ca85 | Failed 393e9087 |
| --- | ---: | ---: |
| Shared transfer package | 98.204s | 297.006s (PASS) |
| Windows ActiveJournal wrapper | 17.17s | 180.95s (PASS) |
| Its `progress` subcase | 13.79s | 168.01s (PASS) |
| Windows package | 116.047s (PASS) | 300.193s (timeout) |

Previous source: [run40162725339 / job121077472110](https://github.com/particleflow/Memoh/actions/runs/40162725339/job/121077472110).
The new configuration tests passed on Windows in the failed run (0.10s, 0.46s,
0.07s). [Selected verbatim lines and the failure stack](evidence/transfer-native-budget-failure.txt)
retain both comparisons and the original failed outcome.

`durable.go`, `active_journal_test.go`, `durable_windows_test.go`, publication
storage/tests and the old workflow were unchanged between those commits. The
progress fixture opens its queue directly; it does not call the new host network
policy or perform network selection. This supports a cumulative budget failure
with variable latency in unchanged flush-heavy code, rather than identifying a
new routing performance regression. The physical runner's I/O cause was not
measured, so this evidence does not establish a hardware diagnosis.

## Exact partition

The source workflow retains `-count=1 -v -timeout=5m` for every process:

| Group | `-run` | `-skip` | Current top-level tests |
| --- | --- | --- | ---: |
| Shared except progress | default | `^Test(Windows\|ActiveJournalBoundedAcrossProgress$)` | 82 |
| Shared progress | `^TestActiveJournalBoundedAcrossProgress$` | none | 1 |
| Native journal | `^TestWindowsNativeActiveJournal$` | none | 1 |
| Remaining Windows | `^TestWindows` | `^TestWindowsNativeActiveJournal$` | 34 |

The shared progress test and its Windows wrapper both existed in the old source
run; both are retained. Child cases, process-crash helpers and 2048 progress
iterations remain unchanged. The package verifier likewise retains its original
`^TestWindowsNative` set, partitioned into the exact journal wrapper and the
remaining native tests, each with `-test.timeout=5m`. Its CLI selection is unchanged.

The overall CI job is bounded at 25 minutes instead of 15. The failed source
passes alone used approximately 597 seconds, and the packaged verifier repeats
the native durability suite before CLI verification. The total job needs room
for both passes, setup, PG and build at the observed slower latency. This is a
CI execution budget, not a product file count/size limit; no individual test
timeout is removed or increased. Permissions remain `contents: read`.

## Coverage proof and execution boundary

For the current Windows/amd64 build, `GOOS=windows GOARCH=amd64 CGO_ENABLED=0
go list -json ./internal/transferclient` selects the test source files. Enumerating
their top-level `Test` functions yields 118 distinct tests, with no Example/Fuzz
functions. Applying the actual workflow selectors assigns each exactly once;
the two actual package-verifier selectors cover exactly the previous 35 native
tests once. [The result includes each source group's full name list and catalog
hash](evidence/transfer-native-partition.txt). This is a selection proof, not
native execution. Workflow YAML parsing and diff checks also passed locally.

The new fixed-head Native CI must execute all groups, CLI, PG/NTFS and packaged
verification successfully before claiming the previous verification gap closed.
Its exact run and outcome are recorded in PR77; the old failing run remains
failure even if the new grouped run succeeds. No workflow was blindly rerun,
no artifact was downloaded, and no production host/configuration was touched.
