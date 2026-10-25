# Windows large-byte recovery fixture

This increment builds on `a51ddcb1cab8e246ee32fe50762e633f221ef271` without
changing production code, storage layout, protocol, configuration or limits.
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
between finite invocations. Its explicit 2 GiB working-set allowance is test
setup for one download, not a product backlog limit or a new user step.

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
