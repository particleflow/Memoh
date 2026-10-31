#!/usr/bin/env bash
set -euo pipefail

# This gate must execute the exact operation CAS/ownership/RLS contract.
# A successful go test exit alone also permits skips or an empty selection.
: "${TEST_POSTGRES_DSN:?TEST_POSTGRES_DSN is required for the computer operation gate}"
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
test_log=$(mktemp)
trap 'rm -f "$test_log"' EXIT

# The unchanged aggregate has passed at 82s and exhausted 90s after 13 passes;
# retain every contract with bounded headroom for PostgreSQL fixture I/O.
go test -json -race -tags=integration -count=1 -timeout 180s \
  -run '^Test(Computer(OperationsMigrationAndDurableIsolation|Admission.*)|NativeHerdr.*|NativeProducerCLIUsesPinnedDirectoryPairAndRecoversUnknownCommit|NativeRuntimeCutoverStopsLegacyAutomaticOAuthPaths)$' ./internal/db | tee "$test_log"

# Keep the same-operation dispatch transaction checks in a separate bounded
# process; preserve the existing contracts and their original time budget.
go test -json -race -tags=integration -count=1 -timeout 90s \
  -run '^TestComputerOperationAdmission' ./internal/db | tee -a "$test_log"

python3 - "$test_log" <<'PY'
import json
import sys

names = ["TestComputerOperationsMigrationAndDurableIsolation", "TestComputerAdmissionDurabilityCoverageAndReservations", "TestComputerAdmissionPreservesAlreadyReservedFollowup", "TestComputerAdmissionPauseSerializesWithClaims", "TestComputerAdmissionRollbackRefusesLivePause", "TestComputerAdmissionRLSAndUnknownMapping"]
names += ["TestNativeHerdrAdmissionPersistsAndFencesNewWork", "TestNativeHerdrConfigAndTransferOwnership", "TestNativeHerdrAdmissionRLSAndRollback", "TestNativeHerdrPauseSerializesWithClaims", "TestNativeHerdrCanonicalAdmissionDefinition", "TestNativeHerdrDeliveryUsesTransactionalRegistry", "TestNativeRuntimeCutoverStopsLegacyAutomaticOAuthPaths", "TestNativeProducerCLIUsesPinnedDirectoryPairAndRecoversUnknownCommit"]
names.extend([
    "TestComputerOperationAdmissionCASPreservesScopeAndDelivery",
    "TestComputerOperationAdmissionConcurrentReservationsAndRLS",
    "TestComputerOperationAdmissionSerializesPauseAndCancellation",
])
with open(sys.argv[1], encoding="utf-8") as stream:
    events = [json.loads(line) for line in stream if line.strip()]
for name in names:
    terminal = [e["Action"] for e in events if e.get("Test") == name
                and e["Action"] in ("pass", "fail", "skip")]
    bad = [e for e in events if e.get("Test", "").startswith(name + "/")
           and e["Action"] in ("fail", "skip")]
    if terminal != ["pass"] or bad:
        sys.exit("Required computer operation PostgreSQL test did not pass without skips: " + name)
print("Required computer operation PostgreSQL CAS/ownership/RLS test passed without skips")
PY
