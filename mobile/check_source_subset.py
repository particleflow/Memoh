"""Offline source-manifest and public-import checks; never builds or uploads."""
import ast
import hashlib
import json
import struct
import zlib
from pathlib import Path
import subprocess

HERE = Path(__file__).resolve().parent
REPO = HERE.parent
manifest = json.loads((HERE / 'SOURCE-PROVENANCE.json').read_text())
entries = manifest['entries']
# Two admitted photo regression inputs extend the previous176 text sources.
assert len(entries) == 178
assert {
    'mobile/client/tests/stage2/harmony_photo_dual_mode.cjs',
    'mobile/client/tests/stage2/harmony_photo_fixtures.py',
} <= {item['path'] for item in entries}
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

# These seven approved PNGs are the only binary source inputs. Existing
# source entries still require NUL-free UTF-8; SDK/CLT/HAP files are not admitted.
images = manifest['approvedArtworkImages']
art = 'mobile/client/mobile/harmony/'
expected_images = {art + name for name in (
    'app/AppScope/resources/base/media/app_icon.png',
    'app/entry/src/main/resources/base/media/app_icon.png',
    'app/entry/src/main/resources/base/media/startup_art.png',
    'artwork/approved/whale.png', 'artwork/approved/koi-lotus.png',
    'artwork/watermark-light.png', 'artwork/watermark-dark.png')}
assert len(images) == 7 and {item['path'] for item in images} == expected_images
for item in images:
    path = REPO / item['path']
    assert path.is_file() and not path.is_symlink() and item['mediaType'] == 'image/png'
    data = path.read_bytes()
    assert len(data) == item['bytes'] and hashlib.sha256(data).hexdigest() == item['sha256'], item['path']
    assert data[:8] == b'\x89PNG\r\n\x1a\n', item['path']
    offset, chunks = 8, []
    while offset < len(data):
        length = struct.unpack_from('>I', data, offset)[0]
        kind = data[offset + 4:offset + 8]
        assert offset + length + 12 <= len(data), item['path']
        payload = data[offset + 8:offset + 8 + length]
        crc = struct.unpack_from('>I', data, offset + 8 + length)[0]
        assert zlib.crc32(kind + payload) == crc, item['path']
        if not chunks:
            assert kind == b'IHDR' and length == 13
            assert struct.unpack('>IIBBBBB', payload)[:2] == (1254, 1254)
        chunks.append(kind)
        offset += length + 12
        if kind == b'IEND':
            assert length == 0 and offset == len(data)
            break
    assert chunks[-1] == b'IEND' and b'IDAT' in chunks, item['path']

allowed = {item['path'] for item in entries} | expected_images | {
    'mobile/README.md', 'mobile/PROVENANCE-AND-LICENSES.md',
    'mobile/REGRESSIONS.md', 'mobile/SOURCE-PROVENANCE.json',
    'mobile/check_source_subset.py', 'mobile/.gitignore',
    'mobile/client/tests/unit/service-config.test.cjs',
    'mobile/SHARED-OBSERVATION.md', 'mobile/BUILD-REPRODUCTION.md',
}
# Check the publishable source set without traversing generated dependency trees.
# Include untracked, non-ignored files so accidental source additions still fail.
files = set(subprocess.check_output(
    ['git', 'ls-files', '--cached', '--others', '--exclude-standard', '-z', '--', 'mobile'],
    cwd=REPO).decode('utf8').rstrip('\0').split('\0'))
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
                  'unverifiedHistoricalLaterChanges': manifest['archiveRestoration']['historicalLaterChangedFilesUnverified'],
                  'files': len(files), 'approvedPNGInputs': len(images), 'pythonSyntax': python_count, 'cjsSyntax': cjs_count,
                  'passed': True, 'fullClientBuild': False}))
