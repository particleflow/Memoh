#!/usr/bin/env bash
set -euo pipefail

: "${TEST_POSTGRES_DSN:?TEST_POSTGRES_DSN is required for the Pi history gate}"
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
history_test_log=$(mktemp)
trap 'rm -f "$history_test_log"' EXIT

go test -json -race -tags=integration -count=1 -timeout 90s \
  -run '^TestPiBatchHistoryPriorEvidenceIsScopedAndLatest$' ./internal/db | tee "$history_test_log"

# Exit zero alone also allows an empty selection or a skipped integration test.
python3 - "$history_test_log" <<'PY'
import json
import sys

name = "TestPiBatchHistoryPriorEvidenceIsScopedAndLatest"
with open(sys.argv[1], encoding="utf-8") as stream:
    events = [json.loads(line) for line in stream if line.strip()]
outcomes = [e["Action"] for e in events if e.get("Test") == name
            and e["Action"] in ("pass", "skip", "fail")]
bad_children = [e for e in events if e.get("Test", "").startswith(name + "/")
                and e["Action"] in ("skip", "fail")]
if outcomes != ["pass"] or bad_children:
    sys.exit("Required Pi batch history PostgreSQL test did not pass without skips")
print("Required Pi batch history PostgreSQL scope/latest-evidence test passed without skips")
PY
