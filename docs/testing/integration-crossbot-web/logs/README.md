# Existing local output for independent review

These files were collected from the completed local combination checks. No product
command, model, browser flow or CI was rerun to prepare this documentation-only
supplement. They are not remote CI logs or third-party attestations.

## Evidence map

| Claim / boundary | Existing output |
| --- | --- |
| 258 Web files / 2,464 tests PASS | [Final Vitest reporter](web-tests-retry.txt) |
| Service 222 PASS | [Complete Go JSON events](go-service.jsonl) |
| Host handler 62 PASS, followed by two build failures | [First corrected host command](go-host-retry2.jsonl) |
| Host application/tool targeted rerun 86 PASS | [Rerun events](go-host-retry3.jsonl) |
| Memory PG 233 PASS | [Complete Go JSON events](pg-memory.jsonl) |
| Transport/delegation/task PG 26 PASS | [Successful targeted selection](pg-delivery-retry.jsonl) |
| Initial PG selection: 20 PASS / 6 FAIL test/subtest events | [Failure output](pg-delivery.jsonl) |
| Initial host compilation failures | [Build-failure events](go-host.jsonl) |
| Mistyped `-p1` command selected the empty root package | [Setup failure](go-host-retry.jsonl) |
| Initial Web: 39 failed files / 10 failed tests | [Full initial reporter](web-tests.txt) |
| Typechecks, Web ESLint, UI guard, build warning, browser script | [Assembled selected check outputs](quality-checks.txt) |
| Earlier DSN skips and still-unrun opt-in read-volume tests | [First-checkpoint selected skip events](first-checkpoint-skips.jsonl) |
| Browser's container 404 / workspace 503 responses | [Projection from existing request capture](browser-observation.json) |

Go PASS counts count named tests **and subtests**. They do not include package PASS
events. The host total 62 + 86 is a union of successful packages from two commands;
the first command itself failed. It is not a full boundary suite rerun. The service,
final memory-PG and final transport-PG logs have no named test skips or failures.

The compact final Vitest reporter records the aggregate 2,464 result, not 2,464
individual named PASS lines. Its original console output cannot provide a per-test
listing retroactively. No new run was made to invent a more detailed report.

Initial Web failures include explicit ENOSPC loads, two 15-second timeouts and a
real missing-label assertion for four CrossBot tools. The latter was repaired in
the already-frozen product checkpoint. Do not attribute every failure to disk or
ignore the failures because a later run passed. Initial PG failures have their
actual unknown/capacity/store/HTTP errors preserved; their individual root causes
are not all proven ENOSPC. The host linker explicitly reports ENOSPC.

The first-checkpoint skip excerpt contains seven missing-DSN tests later exercised
by the real PG suite, plus `TestSeekableReadVolume` and
`TestColdScanLargeResumeReadVolume`. Those **two opt-in volume checks remain skipped**;
the later targeted selections do not turn them into passes. It is an excerpt of
the earlier checkpoint, not the complete output of its broader commands.

## Transformation and confidentiality review

[The manifest](../log-manifest.json) identifies every source, raw SHA-256 and
published transformation. All 14 hashes previously recorded in `results.json`
were checked against the retained local originals before export and matched.
Original files remain local and unchanged; no configuration, environment dump,
server log, production capture or directory-wide export was included.

- Each Go `.jsonl` contains every event from its named source, in source order.
  `Output` remains JSON-escaped; it was **not decoded, summarized or filtered**.
  The manifest says whether the copy is byte-identical. Where present, the only
  replacements are `/workspace/Memoh` → `<repo>` and
  `/workspace/.memoh-cloud/go` → `<cloud-go-toolchain>`. Path-normalized copies are
  **not original byte-for-byte JSONL**.
- The two Web reporter copies have the same literal path normalization, with no
  omitted failure, warning or summary lines.
- `quality-checks.txt` is assembled text with source section headers. It includes
  each selected small log in full; for the large Web build output only the final
  warning/success tail is retained. Exact original line ranges and hashes are in
  the manifest. The empty server-build log does not independently prove exit 0;
  the prior observed process result remains a narrative record.
- `first-checkpoint-skips.jsonl` selects all events for each skipped named test,
  then parses/reserializes JSON inside a `source`/`source_line`/`event` envelope.
  `Output` values are unchanged. This is a **processed excerpt**, not raw JSONL.
- `browser-observation.json` groups existing request records by path class,
  status and method, omitting individual paths/fixture identities/cursor fields.
  It is a **projection**, not a raw capture. The recorded 4 container 404s and
  8 workspace-list 503s remain visible. Seeded summaries and successful GETs do
  not prove specialist execution or supervisor consumption of a delivery.

Every chosen source was checked for credential assignments, credential/JWT/key
forms, private network addresses, endpoint URLs and matches to local fixture
credential values. Go's non-boundary diagnostic output and the text logs were also
reviewed: output consists of repository test names, synthetic fixture identities,
assertions, local tool diagnostics and public documentation links. No actual user
file, private conversation, NAS address or production credential is included.
This review is specific to these files, not a claim of universal secret detection.

## Independently recount without running the product

From the repository root:

```sh
python3 - <<'PY'
import collections, hashlib, json
from pathlib import Path
root = Path('docs/testing/integration-crossbot-web')
manifest = json.loads((root / 'log-manifest.json').read_text())
for source in manifest['sources']:
    if 'published_sha256' in source:
        assert hashlib.sha256((root / source['published']).read_bytes()).hexdigest() == source['published_sha256']
for name in ('go-service', 'go-host-retry2', 'go-host-retry3', 'pg-memory',
             'pg-delivery', 'pg-delivery-retry'):
    events = [json.loads(line) for line in (root / 'logs' / (name + '.jsonl')).read_text().splitlines()]
    counts = collections.Counter(e['Action'] for e in events
                                 if e.get('Test') and e['Action'] in {'pass', 'fail', 'skip'})
    packages = [(e['Package'], e['Action']) for e in events
                if e.get('Package') and not e.get('Test') and e['Action'] in {'pass', 'fail', 'skip'}]
    print(name, dict(counts), packages)
PY
```

For readable Go assertions, decode `Output` while preserving its event association:

```sh
jq -r 'select(.Output != null) | [.Test // .Package // .ImportPath, .Output] | @tsv' \
  docs/testing/integration-crossbot-web/logs/go-host-retry2.jsonl
```

The known legacy media-read P1 remains open. This supplement adds reviewable evidence,
not a release approval, new coverage, production acceptance or whole-repository lint.
