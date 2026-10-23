"""Restore one pinned native RAW dependency inside the project; no global install."""
from pathlib import Path
import hashlib, json, tarfile, urllib.request

ROOT = Path(__file__).resolve().parents[3]
BASE = ROOT / 'mobile/harmony'
lock = json.loads((BASE / 'raw-codec.lock.json').read_text('utf8'))
source = ROOT / lock['directory']
assert source.resolve().is_relative_to(BASE / '.build')
for path in (source, *source.parents):
    if path.is_relative_to(ROOT): assert not path.is_symlink() and not path.is_junction()
if not source.exists():
    source.parent.mkdir(parents=True, exist_ok=True)
    archive_path = source.parent / ('LibRaw-' + lock['version'] + '.tar.gz')
    if not archive_path.exists():
        with urllib.request.urlopen(lock['source'], timeout=45) as response:
            archive_path.write_bytes(response.read())
    assert hashlib.sha256(archive_path.read_bytes()).hexdigest() == lock['archive_sha256']
    with tarfile.open(archive_path) as archive:
        for member in archive.getmembers():
            assert (source.parent / member.name).resolve().is_relative_to(source.resolve())
            assert not member.issym() and not member.islnk()
        archive.extractall(source.parent, filter='data')
assert not any(p.is_symlink() or p.is_junction() for p in source.rglob('*'))
actual = {p.relative_to(source).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
          for p in source.rglob('*') if p.is_file()}
assert actual == lock['files'], 'Pinned RAW source missing or changed'
print(json.dumps({'version': lock['version'], 'verifiedFiles': len(actual), 'globalInstall': False}))
