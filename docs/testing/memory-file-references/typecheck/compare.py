#!/usr/bin/env python3
"""Compare raw TS diagnostic first lines; retain raw logs and multiplicities."""
import collections
import hashlib
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parent

def read(name):
    path = ROOT / name
    raw = path.read_bytes()
    lines = [line for line in raw.decode().splitlines() if 'error TS' in line]
    normalized = collections.Counter(re.sub(r'\(\d+,\d+\)', '(LINE,COL)', line) for line in lines)
    return {
        'file': name,
        'sha256': hashlib.sha256(raw).hexdigest(),
        'raw_diagnostic_first_lines': len(lines),
        'distinct_normalized_diagnostics': len(normalized),
        'raw_counts_by_ts_code': dict(sorted(collections.Counter(re.search(r'error (TS\d+)', line)[1] for line in lines).items())),
        'normalized_with_multiplicity': dict(sorted(normalized.items())),
    }

base = read('main-851e8da.log')
head = read('head-63c4bb.log')
result = {
    'base_sha': '851e8da4284c1f01e7e95f9547b16e7250bf553b',
    'head_sha': '63c4bb2592a73e2ce76e39f23999cdb6cf6a8f82',
    'command': 'pnpm --filter @memohai/web typecheck',
    'definition': 'Count diagnostic first lines, excluding continuation prose. Normalize only line/column positions; retain path, TS code, full first-line message. Distinct count is not the number of TS error codes.',
    'base': base,
    'head': head,
    'normalized_multisets_equal': base['normalized_with_multiplicity'] == head['normalized_with_multiplicity'],
}
print(json.dumps(result, indent=2))
raise SystemExit(0 if result['normalized_multisets_equal'] else 1)
