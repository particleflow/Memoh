"""Offline source-manifest and public-import checks; never builds or uploads."""
import ast
import hashlib
import json
from pathlib import Path
import subprocess

HERE = Path(__file__).resolve().parent
REPO = HERE.parent
manifest = json.loads((HERE / 'SOURCE-PROVENANCE.json').read_text())
entries = manifest['entries']
assert len(entries) == 42
assert len({item['path'] for item in entries}) == len(entries)
for item in entries:
    relative = Path(item['path'])
    assert not relative.is_absolute() and '..' not in relative.parts
    path = REPO / relative
    assert path.is_file() and not path.is_symlink(), item['path']
    data = path.read_bytes()
    assert hashlib.sha256(data).hexdigest() == item['sha256'], item['path']
    assert b'\0' not in data, item['path']
    data.decode('utf8')

allowed = {item['path'] for item in entries} | {
    'mobile/README.md', 'mobile/PROVENANCE-AND-LICENSES.md',
    'mobile/REGRESSIONS.md', 'mobile/SOURCE-PROVENANCE.json',
    'mobile/check_source_subset.py', 'mobile/.gitignore',
    'mobile/client/tests/unit/service-config.test.cjs',
    'mobile/SHARED-OBSERVATION.md', 'mobile/BUILD-REPRODUCTION.md',
}
files = {p.relative_to(REPO).as_posix() for p in HERE.rglob('*')
         if p.is_file() and '__pycache__' not in p.parts}
assert files == allowed, {'unexpected': sorted(files - allowed), 'missing': sorted(allowed - files)}
config = json.loads((HERE / 'client/config/memoh_client.json').read_text())
assert config['apiOrigin'] == '' and config['hapDownloadUrl'] == ''
python_count = cjs_count = 0
for name in sorted(files):
    path = REPO / name
    if path.suffix == '.py':
        ast.parse(path.read_text(), filename=name)
        python_count += 1
    elif path.suffix == '.cjs':
        subprocess.run(['node', '--check', str(path)], check=True)
        cjs_count += 1
print(json.dumps({'sourceFiles': len(entries), 'heldSources': len(manifest['excludedSourcePaths']),
                  'files': len(files), 'pythonSyntax': python_count, 'cjsSyntax': cjs_count,
                  'passed': True, 'fullClientBuild': False}))
