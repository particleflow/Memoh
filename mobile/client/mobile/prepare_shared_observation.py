"""Apply the fixed shared observation and optional delivery dependency after Web extraction.

Called by the real builder with the freshly extracted baseline and a pinned Git object.
Does not fetch, upload, build, install dependencies, or change the upstream source.
"""
from pathlib import Path
import argparse
import hashlib
import json
import re
import subprocess

ROOT = Path(__file__).resolve().parents[1]


def sha(data):
    return hashlib.sha256(data).hexdigest()


def apply(checkout, repository):
    lock = json.loads((ROOT / 'config/memoh_web.lock.json').read_text())['sharedObservation']
    commit = lock['commit']
    upstream = {}
    for name, digest in lock['files'].items():
        data = subprocess.check_output(['git', '-C', str(repository), 'show', commit + ':' + name])
        if sha(data) != digest:
            raise ValueError('Shared dependency drift: ' + name)
        upstream[name] = data
    # Validate every precondition before writing. Keep unrelated SDK contracts on
    # the pinned baseline; importing the entire newer SDK changes OAuth inputs.
    for name, digest in lock['baseSDKFiles'].items():
        if sha((checkout / name).read_bytes()) != digest:
            raise ValueError('Expected unchanged baseline SDK: ' + name)
    types = upstream['packages/sdk/src/types.gen.ts'].decode()
    sdk = upstream['packages/sdk/src/sdk.gen.ts'].decode()
    declarations = []
    for name in lock['types']:
        match = re.search(r'^export type ' + re.escape(name) + r' = .*?(?=\n\n|\Z)', types, re.M | re.S)
        if not match:
            raise ValueError('Missing generated type: ' + name)
        declarations.append(match.group())
    for name in lock['functions']:
        match = re.search(r'^export const ' + re.escape(name) + r' = [^\n]+', sdk, re.M)
        if not match:
            raise ValueError('Missing generated read function: ' + name)
        declarations.append(match.group())
    generated = ('// Extracted verbatim declarations from @hey-api/openapi-ts output.\n'
                 '// Fixed upstream commit: ' + commit + '\n'
                 "import type { RequestResult } from './client';\n"
                 "import { client } from './client.gen';\n"
                 "import type { Options } from './sdk.gen';\n"
                 "import type { ServerProblem } from './types.gen';\n\n"
                 + '\n\n'.join(declarations) + '\n')
    writes = {checkout / 'packages/sdk/src/harmony-delegation.gen.ts': generated.encode()}
    index = checkout / 'packages/sdk/src/index.ts'
    writes[index] = index.read_bytes() + b"\nexport * from './harmony-delegation.gen';\n"
    for name in [*lock['sharedModules'], *lock.get('testModules', [])]:
        writes[checkout / name] = upstream[name]
    for locale in ('en', 'zh', 'ja'):
        name = f'apps/web/src/i18n/locales/{locale}.json'
        value = json.loads((checkout / name).read_text())
        value['chat']['delegations'] = json.loads(upstream[name])['chat']['delegations']
        writes[checkout / name] = (json.dumps(value, ensure_ascii=False, indent=2) + '\n').encode()
    writes[checkout / 'apps/web/src/lib/HarmonyTasks.vue'] = (ROOT / 'mobile/memoh/HarmonyTasks.vue').read_bytes()
    for path, data in writes.items():
        if path.is_symlink():
            raise ValueError('Linked output: ' + path.name)
    for path, data in writes.items():
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
    return {'sharedCommit': commit, 'inputFiles': lock['files'],
            'baseSDKFiles': lock['baseSDKFiles'], 'sdkFunctions': lock['functions'],
            'outputFiles': {path.relative_to(checkout).as_posix(): sha(data)
                            for path, data in writes.items()}}


def verify(checkout, receipt):
    for name, expected in receipt['outputFiles'].items():
        if sha((checkout / name).read_bytes()) != expected:
            raise ValueError('Shared observation output drift: ' + name)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--checkout', type=Path, required=True)
    parser.add_argument('--repository', type=Path, required=True)
    args = parser.parse_args()
    receipt = apply(args.checkout.resolve(), args.repository.resolve())
    verify(args.checkout.resolve(), receipt)
    print(json.dumps(receipt, indent=2))
