"""Prepare the public Harmony Web test context; never compile a HAP or use a SDK."""
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tarfile

REPO = Path(__file__).resolve().parents[1]
CLIENT = REPO / 'mobile/client'
BASE = CLIENT / 'tests/.isolated/ci-web'
sys.path.insert(0, str(CLIENT / 'mobile'))
import build_memoh_web as builder
import prepare_memoh_source


def main():
    subprocess.run([sys.executable, str(REPO / 'mobile/check_source_subset.py')],
                   cwd=REPO, check=True)
    lock = json.loads(builder.PIN.read_text())
    baseline = json.loads((REPO / 'docs/testing/harmony-native-runtime-fa77/generated-web-baseline.json').read_text())
    assert lock['commit'] == baseline['sharedCommit'], 'Review the baseline when updating shared source'
    archive = CLIENT / lock['archive']
    if not archive.exists():
        prepare_memoh_source.prepare(lock)
    builder.verify_modules({lock['archive']: lock['archiveSHA256']})
    BASE.mkdir(parents=True, exist_ok=True)
    context, patches = builder.context_archive(lock, BASE, 'harmony')
    test_files = {'apps/web/src/pages/home/components/session-task-results.test.ts',
                  'apps/web/src/pages/home/components/session-tasks.test.ts'}
    assert test_files.issubset(patches), 'Phone attachment assertions missing'
    existing = {name: values for name, values in patches.items() if name not in test_files}
    assert len(existing) == 43 and existing == baseline['existingPhonePatches'], 'Reviewed phone adaptation drift'
    checkout = BASE / 'checkout'
    checkout.mkdir(exist_ok=True)
    with tarfile.open(context) as source:
        config = json.load(source.extractfile('apps/web/public/memoh_client.json'))
        assert config['apiOrigin'] == '' and config['hapDownloadUrl'] == ''
        source.extractall(checkout, filter='data')
    report = {
        'sharedCommit': lock['commit'],
        'sourceSHA256': hashlib.sha256(archive.read_bytes()).hexdigest(),
        'reviewedPhoneAdaptations': len(existing),
        'testAdaptations': len(test_files),
        'publicConfigurationEmpty': True,
        'fullTypecheckCommand': 'pnpm --filter @memohai/web typecheck',
        'hapCompiled': False,
    }
    (BASE / 'source-check.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report))


if __name__ == '__main__':
    main()
