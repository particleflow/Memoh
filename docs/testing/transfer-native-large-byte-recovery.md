# Windows large-byte recovery fixture

The original test-only increment built on
`a51ddcb1cab8e246ee32fe50762e633f221ef271`. Its later default-CLI regression
fix is described below; the original results and failure remain historical evidence.
The prior Windows Server/PostgreSQL/NTFS test used a short string; the 65 MiB
admission test did not exercise this combined transfer route. The Linux 1 GiB
and 512-file evidence remains separate and is not rerun by this fixture.

`TestWindowsNativeLargeByteRecovery` runs on Windows with a fresh, fully migrated
PostgreSQL database, the real device verifier and Server/Media handlers, real
HTTPS, the existing configured finite Herdr action and native NTFS publication.
A private fixture credential and process-local RootCAs are synthetic. No OS trust
store, installed PostgreSQL service, production device, Agent, or NAS is changed.

The fixture writes 1 GiB of random non-sparse source bytes in 64 KiB buffers,
Syncs them, verifies NTFS and reports allocation using `FILE_STANDARD_INFO`.
It requires 4 GiB free before starting and retains a 1 GiB free-space reserve
between finite invocations. The current test uses the existing official bundle
CLI test executable and actual command dispatcher with installed defaults,
without `queue-budget.json` or a test-only byte allowance. Only its test process
receives the synthetic CA; the shipped EXE does not receive a new trust path.

A streaming wrapper pauses the first **real Media** response after 256 MiB;
the test kills its client subprocess and inspects the durable queue offset.
The next subprocess must use precisely that offset in Range, without resending
any confirmed prefix. Local publication must still be absent before recovery.
The assertions then cover the complete SHA256 and size, unchanged original
file identity and hash, durable committed PostgreSQL reference, stable
operation/attempt/delivery/host binding, and two fresh-process re-enqueues with
no added GET or Commit requests and unchanged ledger data. Bytes sent beyond
the durable checkpoint are reported separately as unconfirmed tail replay.
Download has no upload receipt: the retained proof is its original delivery,
queue/publication state and PostgreSQL reference, not an invented upload ID.

Run using the existing disposable PostgreSQL bootstrap:

```powershell
python scripts/build-transferqueue-bundle.py --output "$env:RUNNER_TEMP/default-delivery-windows-amd64"
$env:MEMOH_NATIVE_LARGE_CLI_TEST = "$env:RUNNER_TEMP/default-delivery-windows-amd64/transferqueue-cli.test.exe"
./scripts/test-native-delivery-postgres.ps1 -TestPattern '^TestWindowsNativeLargeByteRecovery$' -TestTimeout '12m'
```

The script's default selector and 90-second timeout are unchanged. The new
`Native large-byte recovery` workflow executes only this added scenario. Normal
PR checks can independently run their required regression suites; their green
status is not evidence of this 1 GiB case. Read the dedicated job's named PASS
and measured counters before claiming execution. Cross-compilation is not a
Windows runtime result.

This tests NAS-module-to-Windows **download**, with a preseeded synthetic source:
upload admission is Unix-only. It is not a new Windows NAS server product,
physical power-loss test, Internet/LAN throughput benchmark, Windows 512-file
batch, phone acceptance, or formal `transferqueue.exe`/Herdr trust acceptance.
No certificate import is attempted. Source/binary provenance remains the fixed
commit tested by Actions; no blocked artifact is downloaded through another route.

## Initial execution retained

[Run 40172148458 / job 121105065246](https://github.com/particleflow/Memoh/actions/runs/40172148458/job/121105065246)
at `e844456f6076283ee508ec6327e50a7e1637c514` failed after actual transfer:
free space 31,507,292,160 bytes; prefix sent and durable offset both 268,435,456.
The fixture then incorrectly asserted that the published reader was an
`*os.File`. The production interface intentionally returns a private wrapper.
Its failed assertion also prevented explicit handle closure and caused a
TempDir cleanup error. The follow-up retains the public reader for hash checks,
closes it on every path, and measures allocation by a read-only census of the
test-owned object directory. No production API is changed or exposed. This
first run is a failure, not full recovery acceptance.

Contribution CI 40172148231 independently failed its expected workflow count
(10 vs actual 11). The catalog assertion is updated by one, retaining all
read-only permissions and no-format-dependency checks. Local full Node tests
could not complete because Ruby is absent; the actual GitHub check must pass.

## Default download limit regression

The earlier successful [Windows run 40172549407](https://github.com/particleflow/Memoh/actions/runs/40172549407/job/121106271229)
at `de05d0532a6d9a46bd9923dce172f739fde82e17` used a fixture-only 2 GiB
allowance. It proved NTFS recovery but did not prove the installed defaults.
`TestCLIDefaultDownloadDoesNotCapSourceSize` reproduced the default command's
1 GiB metadata admission failure before the fix:

```text
exit=1 output={"feedback_status":"","state":"capacity"}
--- FAIL: TestCLIDefaultDownloadDoesNotCapSourceSize (0.00s)
```

Three gates were involved: download admission/replay compared size to the
historical 256 MiB profile, pending downloads reserved cumulative bytes/slots,
and native publication compared all retained objects to the same byte value.
The correction registers metadata and an empty part, then streams bytes under
existing worker locks. Downloads no longer use any of these cumulative gates.
The 64 KiB copy buffer and existing 8 MiB checkpoint chunk are unchanged.
Free space is checked per chunk, including the existing metadata reserve; actual
ENOSPC/EDQUOT remains possible if another process consumes space after a check.
It preserves the synced prefix and records a `waiting` / `capacity` summary with
`next_due_at`, reusing the existing maximum retry delay without consuming the
network retry budget. Insufficient metadata space can still prevent admission
or checkpoint persistence; that error does not invent a successful checkpoint.

No locator, root, credential, transfer identity, HTTP contract or stored record
shape changes. Existing default locators reopen unchanged; no budget migration
or user configuration is needed. The legacy byte field remains bound for older
snapshot/config compatibility, but is not a download size or retained-object
allowance. Published objects and records are preserved and still counted for
integrity and overflow checks. Existing metadata bucket limits remain distinct
from payload space. This is not an unlimited physical disk claim.

All writers must use the corrected component before processing downloads above
an old byte allowance: an older component still rejects those states; rolling
back the binary does not make a large retained object fit its old policy. Do not
run old and new writers together or delete data to make a rollback fit. There is
no on-disk migration or reset step in this change.

Focused regressions cover default 1 GiB registration with no allocated payload,
more pending downloads than worker/admission slots, unchanged locator bytes and
identities after reopen, 13 disk-pressure retries preserving the offset and
retry budget, worker exclusion, full-hash resume, and unknown association
reconciliation. The Windows retained-object case also crosses a legacy byte
allowance without deleting the earlier objects. These small-data tests are
separate from the real 1 GiB Windows job; a pending job is not a pass.

## Default-path regression execution

The first default-path fixed head `5c3b983707611195dc29cf7920c5a2033ac67888`
passed the real [1 GiB case](https://github.com/particleflow/Memoh/actions/runs/40175212658/job/121114334437)
(PASS 40.03s, GET 2 / HEAD 1 / Commit requests 3, durable offset 268435456,
confirmed-prefix replay 0, stable completed reopens 2). This is not an all-green
candidate: the ordinary [Windows regression job](https://github.com/particleflow/Memoh/actions/runs/40175212657/job/121114479380)
failed `TestQueueReleaseProcessCrash/after_receipt`, `/after_truncate` and
`/after_compact`. Subsequent native/CLI/PG/bundle steps were skipped, not passed.

The new bounded reclaim traversal removed a journal and its release witness
while that directory page was still being visited. Windows then visited the
now-missing witness and returned ErrStore. The follow-up collects at most the
existing reclaim batch of confirmed IDs, finishes traversal, and only then
rechecks/releases each under admission -> job locking. It neither swallows
missing/corrupt evidence nor loads the entire backlog. The original process-exit
regressions and their fsync assertions are kept unchanged and must pass on the
new exact head before it is considered integrated.

The subsequent PostgreSQL job [121116022429](https://github.com/particleflow/Memoh/actions/runs/40175732264/job/121116022429)
exposed a separate outdated assertion in the existing real-route roundtrip:
it still required download metadata above 256 MiB or the eighth extra pending
job to fail. The corrected command accepted the metadata (no payload GET), so
the old assertion failed. That fixture now expects metadata acceptance and zero
offset across those boundaries. Its byte transfer, hash, original receipt,
wrong-attempt denial and revocation assertions remain unchanged. This is a test
contract correction for the deliberately removed download gate, not evidence
that the metadata-only extra jobs transferred any content.

The same head's [Native job121115880712](https://github.com/particleflow/Memoh/actions/runs/40175732295/job/121115880712)
confirmed all release-process crash cases had recovered, then exposed one
missed legacy-publication migration check. It interpreted the internal zero
(no cumulative payload quota) as zero capacity. Migration now reuses the same
overflow-safe retained-byte check as normal reads; its old/new copies, version
gate, locking, fsync and nine process-exit assertions are unchanged. The shared
complete-history migration test is exercised with both its legacy nonzero
allowance and current zero mode; zero mode failed before this one-line fix.
