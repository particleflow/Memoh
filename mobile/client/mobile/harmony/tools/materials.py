"""Bind only the formal Harmony client and its actual shared-client inputs."""
from pathlib import Path
import argparse
import datetime
import uuid
import base64
import hashlib
import json
import os
import sys

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / 'tools'))
from artifact_paths import checked as checked_artifact_path
BASE = ROOT / 'mobile/harmony'
OUT = ROOT / 'docs/evidence/development-runs'
EXCLUDED = {'build', '.build', '.hvigor', '.cxx', 'oh_modules', 'node_modules', '.idea', '__pycache__', 'materials', 'client-web'}

def sha(path): return hashlib.sha256(path.read_bytes()).hexdigest()

def checked(path):
    return checked_artifact_path(path, ROOT)

def evidence_output(path, filenames):
    path = checked(path)
    if not path.is_relative_to(OUT):
        raise ValueError('Build evidence must remain under development-runs')
    for name in filenames:
        target = checked(path / name)
        if target.exists():
            raise FileExistsError('Never overwrite frozen evidence: ' + name)
    return path

def verify_snapshot(snapshot):
    """Bind the complete current source set and packaged Web input, not a subset."""
    files = {p.relative_to(ROOT).as_posix(): sha(p) for p in inputs()}
    assert snapshot['files'] == files, 'Formal source set or digest drift'
    assert snapshot['sourceSha256'] == hashlib.sha256(json.dumps(files, sort_keys=True).encode()).hexdigest(), 'Invalid source identity'
    package = checked(ROOT / 'mobile/.build/memoh-web/resource-package.json')
    assert snapshot['webResources'] == {'path': package.relative_to(ROOT).as_posix(), 'sha256': sha(package)}, 'Packaged Web input drift'
    verify_web_package()
    return [ROOT / name for name in files]

def verify_web_package():
    directory = str(checked(ROOT / 'mobile'))
    if directory not in sys.path:
        sys.path.insert(0, directory)
    from package_memoh_web import verify_current_package
    package = verify_current_package()
    verify_rawfile_resources(package)
    return package


def verify_rawfile_resources(package):
    rawfile = checked(BASE / 'app/entry/src/main/resources/rawfile')
    expected = {'client-web/' + name: digest for name, digest in package['resources'].items()}
    expected['ui-theme.json'] = sha(checked(ROOT / 'contracts/ui_theme.json'))
    expected.update({name: sha(checked(rawfile / name))
                     for name in ('source-build.json', 'raw-codec-license.txt')})
    actual = {}
    for path in rawfile.rglob('*'):
        checked(path)
        if path.is_file():
            actual[path.relative_to(rawfile).as_posix()] = sha(path)
    assert actual == expected, 'Physical rawfile set or digest drift'
    return expected

def inputs():
    lock = json.loads((BASE / 'raw-codec.lock.json').read_text(encoding='utf-8'))
    dependency = checked(ROOT / lock['directory'])
    assert dependency.resolve().is_relative_to(BASE / '.build')
    assert not any(p.is_symlink() or p.is_junction() for p in dependency.rglob('*')), 'RAW dependency link forbidden'
    actual = {p.relative_to(dependency).as_posix(): sha(p) for p in dependency.rglob('*') if p.is_file()}
    assert actual == lock['files'], 'Pinned RAW decoder source drift or missing dependency'
    files = set()
    for directory, dirs, names in os.walk(BASE, followlinks=False):
        current = Path(directory)
        dirs[:] = [name for name in dirs if name not in EXCLUDED]
        # Refuse links in owned source before descent; generated directories are pruned.
        for name in dirs:
            child = current / name
            if child.is_symlink() or child.is_junction():
                raise RuntimeError('Source must remain in a non-linked workspace')
        files.update(current / name for name in names
                     if name not in ('source-build.json', 'local.properties'))
    files |= {p for p in (ROOT / 'mobile/memoh').rglob('*') if p.is_file()}
    files |= {ROOT / name for name in (
        'mobile/package_memoh_web.py', 'mobile/build_memoh_web.py', 'config/memoh_runtime.lock.json', 'config/memoh_web.lock.json', 'config/memoh_client.json',
        'contracts/client_capabilities.json', 'contracts/harmony_native_assets.json', 'contracts/album_sync.json',
        'contracts/matrix_assets.json', 'contracts/ui_theme.json',
        'mobile/.build/memoh-web/build.json',
        'validation/harmony/tools/toolchain.py',
        'mobile/harmony/raw-codec.lock.json',
        'mobile/harmony/tools/build_api26.py', 'tools/artifact_paths.py')}
    for p in files:
        if not p.resolve().is_relative_to(ROOT) or any(q.is_symlink() or q.is_junction() for q in (p, *p.parents) if q.is_relative_to(ROOT)):
            raise RuntimeError('Source must remain in a non-linked workspace')
    return sorted(files)

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output')
    args = parser.parse_args()
    run_name = datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ') + '-' + uuid.uuid4().hex[:8]
    output = evidence_output(ROOT / args.output if args.output else OUT / ('source-' + run_name), ['current-source-raw.json'])
    metadata = [checked(BASE / 'materials/source-build.json'),
                checked(BASE / 'app/entry/src/main/resources/rawfile/source-build.json')]
    package = checked(ROOT / 'mobile/.build/memoh-web/resource-package.json')
    verify_web_package()
    files = {p.relative_to(ROOT).as_posix(): sha(p) for p in inputs()}
    identity = hashlib.sha256(json.dumps(files, sort_keys=True).encode()).hexdigest()
    snapshot = {'sourceSha256': identity, 'files': files, 'scope': 'formal-shared-chat-client',
                'bundleName': 'cloud.begonia.workassistant', 'readyForHumanAcceptance': False,
                'webResources': {'path': 'mobile/.build/memoh-web/resource-package.json',
                                 'sha256': sha(package)},
                'buildVerification': 'Source binding only; compilation, native execution and complete independent review are recorded separately.'}
    BASE.joinpath('materials').mkdir(exist_ok=True)
    output.mkdir(parents=True, exist_ok=True)
    for p in metadata:
        p.write_text(json.dumps(snapshot, ensure_ascii=False, indent=2)+'\n', encoding='utf-8')
    raw = [{'path': n, 'sha256': h, 'bytes': (ROOT/n).stat().st_size,
            'rawBytesBase64': base64.b64encode((ROOT/n).read_bytes()).decode('ascii')} for n,h in files.items()]
    (output/'current-source-raw.json').write_text(json.dumps(raw, indent=2)+'\n', encoding='utf-8')
    print(json.dumps({'sourceIdentitySha256': identity, 'files': len(files), 'readyForHumanAcceptance': False}))

if __name__ == '__main__': main()
