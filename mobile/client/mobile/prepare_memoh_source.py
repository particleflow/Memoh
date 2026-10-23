"""Recreate the pinned public Memoh + UI source archive; no private inputs."""
import copy
import gzip
import hashlib
import io
import json
import posixpath
import tarfile
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def prepare(lock):
    output = ROOT / lock['archive']
    output.parent.mkdir(parents=True, exist_ok=True)
    parts = []
    for item in lock['sourceArchives']:
        with urllib.request.urlopen(item['archiveURL'], timeout=60) as response:
            data = response.read(50 * 1024 * 1024 + 1)
        assert hashlib.sha256(data).hexdigest() == item['archiveSHA256'], 'Public source archive drift'
        parts.append((item, data))
    seen = set()
    temporary = output.with_suffix('.pending')
    with temporary.open('wb') as raw, gzip.GzipFile(filename='', mode='wb', fileobj=raw, mtime=0) as gz:
        with tarfile.open(mode='w', fileobj=gz) as target:
            for item, data in parts:
                prefix, under = item['archivePrefix'], item['destination']
                with tarfile.open(fileobj=io.BytesIO(data), mode='r:gz') as source:
                    for member in source:
                        assert member.name.startswith(prefix) or member.name == prefix[:-1]
                        name = member.name[len(prefix):]
                        if not name or (not under and name.rstrip('/') == 'packages/ui'):
                            continue
                        assert not name.startswith('/') and '..' not in Path(name).parts
                        assert member.isfile() or member.isdir() or member.issym()
                        entry = copy.copy(member)
                        entry.name = lock['archivePrefix'] + under + name
                        if member.issym():
                            link = posixpath.normpath(posixpath.join(posixpath.dirname(under + name), member.linkname))
                            assert not member.linkname.startswith('/') and link != '..' and not link.startswith('../')
                        entry.uid = entry.gid = entry.mtime = 0
                        entry.uname = entry.gname = ''
                        assert entry.name not in seen
                        seen.add(entry.name)
                        target.addfile(entry, source.extractfile(member) if member.isfile() else None)
    assert hashlib.sha256(temporary.read_bytes()).hexdigest() == lock['archiveSHA256'], 'Combined archive drift'
    temporary.replace(output)
    return output


if __name__ == '__main__':
    print(prepare(json.loads((ROOT / 'config/memoh_web.lock.json').read_text())).name)
