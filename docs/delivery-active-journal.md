# Bounded active delivery journal

This increment addresses metadata growth inside the existing private queue. A
64 KiB journal previously exhausted at the 88th synthetic state update even
though the payload was only 64 KiB. Reopening also replayed every accumulated
frame. Raising the payload limit would amplify that cost. No NAS receipt policy,
payload quota, task identity, scheduler or production setting changes here.

Every new state write uses queue record version 4, preserving the entire job,
receipt, offset, retry budget/due, cancel intent, snapshot seal and association
outcome. Existing v2/v3 records remain readable. Old strict readers reject v4:
stop old writers before using this component and do not downgrade a directory
after its first new write. Released receipts also retain v4. No directory move
or locator fallback is introduced.

When an active journal reaches min(64 KiB, half its configured journal limit),
the same job lock protects this finite sequence:

1. Append/sync the newest v4 record, fencing old writers before truncation.
2. Create one private `.checkpoint` containing a checksummed complete state and
   the journal's native object identity; sync file and directory.
3. Truncate/sync the same journal handle and write/sync the one compact record.
4. Remove only the checkpoint and sync the directory. Never address a source
   path, snapshot/part, NAS original or published download.

Recovery reconciles a complete checkpoint before any later append. It requires
the same logical job, locator and native journal identity. A partial checkpoint
cannot authorize truncation; it is removed only if the complete original v4
journal still validates. A complete corrupt checkpoint fails closed and is kept.
Admission enumeration uses a valid checkpoint in the truncate window so the
reservation cannot disappear. File/link privacy checks and Windows hashed
physical names apply to the checkpoint too.

Each normal active reopen is now bounded by the threshold plus one maximum
16 KiB record, rather than the number of lifetime chunks. This does not eliminate
fsync or prove network throughput. Already-full legacy journals without room to
append the v4 fence remain explicitly blocked and untouched; this increment does
not invent an unsafe automatic repair or drop older evidence.

Tests use a 64 KiB synthetic task, a 64 KiB journal and 2048 metadata updates;
there is no large data set. Subprocess exit covers after-record, after-checkpoint,
after-truncate, after-compact and after-finish. Unknown creation, unknown
association, cancel intent, retry due/budget, legacy records, partial/corrupt
checkpoints and foreign object identity retain their original meaning. These
are process-exit tests with a live OS/filesystem, not physical power-cut evidence.
Native Windows results must come from the exact increment's actual workflow.

The separate capacity limits remain: 1024 local records, 128 native publication
receipts/retained byte budget, NAS's 128 unexpired upload receipts, and 24 hours
from upload creation (not last activity). Their lifecycle requires durable
operation-ledger proof and consumer ownership before any safe retirement; this
metadata compaction does not claim to provide that lifecycle or unlimited scale.
