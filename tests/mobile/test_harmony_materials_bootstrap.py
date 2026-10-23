"""Offline integration checks using real verified build inputs, never fabricated receipts.

Prerequisites: the existing Harmony Web package and locked LibRaw source.
Run from the repository with: python3 tests/mobile/test_harmony_materials_bootstrap.py
Only disposable copies are modified; no SDK, Web build, network or Kit mocks.
"""
import base64
from contextlib import contextmanager
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


class MaterialsBootstrap(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        repo = Path(__file__).resolve().parents[2]
        source = repo / 'mobile/client'
        cls.scratch = tempfile.TemporaryDirectory(prefix='harmony-materials-')
        cls.addClassCleanup(cls.scratch.cleanup)
        cls.root = Path(cls.scratch.name) / 'client'
        paths = subprocess.check_output(['git', 'ls-files', '-z', '--', 'mobile/client'], cwd=repo)
        for name in paths.decode().rstrip('\0').split('\0'):
            relative = Path(name).relative_to('mobile/client')
            target = cls.root / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(source / relative, target)
        lock = json.loads((source / 'config/memoh_web.lock.json').read_text())
        dependency = json.loads((source / 'mobile/harmony/raw-codec.lock.json').read_text())
        for name in [lock['archive'], dependency['directory'],
                     'mobile/.build/memoh-web/build.json',
                     'mobile/.build/memoh-web/resource-package.json',
                     'mobile/.build/memoh-web/official-dist',
                     'mobile/harmony/app/entry/src/main/ets/common/ServiceUrl.ets',
                     'mobile/harmony/app/entry/src/main/ets/common/WebResourceManifest.ets',
                     'mobile/harmony/app/entry/src/main/resources/rawfile/client-web',
                     'mobile/harmony/app/entry/src/main/resources/rawfile/ui-theme.json']:
            target = cls.root / name
            target.parent.mkdir(parents=True, exist_ok=True)
            if (source / name).is_dir():
                shutil.copytree(source / name, target)
            else:
                shutil.copyfile(source / name, target)
        cls.base = cls.root / 'mobile/harmony'
        cls.raw = cls.base / 'app/entry/src/main/resources/rawfile'
        cls.generated = [cls.base / 'materials/source-build.json',
                         cls.raw / 'source-build.json', cls.raw / 'raw-codec-license.txt']
        cls.dependency = cls.root / dependency['directory']
        cls.sequence = 0
        assert not any(p.exists() for p in cls.generated)
        assert not (cls.root / 'validation/harmony/.build/commandline-tools').exists()

    def setUp(self):
        for path in self.generated:
            path.unlink(missing_ok=True)

    def run_python(self, args):
        return subprocess.run([sys.executable, '-B', *args], cwd=self.root,
                              env=dict(os.environ, PYTHONDONTWRITEBYTECODE='1'),
                              capture_output=True, text=True, timeout=60)

    def generate(self, success=True):
        type(self).sequence += 1
        output = f'docs/evidence/development-runs/bootstrap-{self.sequence}'
        result = self.run_python(['mobile/harmony/tools/materials.py', '--output', output])
        if success:
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertFalse(json.loads(result.stdout)['readyForHumanAcceptance'])
        else:
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse((self.root / output / 'current-source-raw.json').exists())
        return self.root / output, result

    def verify(self, success=True, raw_only=False):
        code = "import sys,json;sys.path.insert(0,'mobile/harmony/tools');import materials as m;"
        code += ("m.verify_web_package()" if raw_only else
                 "m.verify_snapshot(json.loads((m.BASE/'materials/source-build.json').read_text()))")
        result = self.run_python(['-c', code])
        if success:
            self.assertEqual(result.returncode, 0, result.stderr)
        else:
            self.assertNotEqual(result.returncode, 0)
        return result

    @contextmanager
    def changed(self, path, suffix=b'\nchanged-for-regression\n'):
        original = path.read_bytes()
        try:
            path.write_bytes(original + suffix)
            yield
        finally:
            path.write_bytes(original)

    def test_clean_generation_binds_actual_files_and_license_bytes(self):
        output, _ = self.generate()
        self.assertEqual(self.generated[0].read_bytes(), self.generated[1].read_bytes())
        snapshot = json.loads(self.generated[0].read_bytes())
        license_data = self.generated[2].read_bytes()
        for name in ['COPYRIGHT', 'LICENSE.CDDL', 'LICENSE.LGPL']:
            self.assertIn((self.dependency / name).read_bytes(), license_data)
        for name, digest in snapshot['files'].items():
            self.assertEqual(hashlib.sha256((self.root / name).read_bytes()).hexdigest(), digest)
        exported = json.loads((output / 'current-source-raw.json').read_text())
        self.assertEqual({item['path'] for item in exported}, set(snapshot['files']))
        for item in exported:
            data = base64.b64decode(item['rawBytesBase64'])
            self.assertEqual(data, (self.root / item['path']).read_bytes())
            self.assertEqual(hashlib.sha256(data).hexdigest(), item['sha256'])
            self.assertEqual(len(data), item['bytes'])
        self.verify()

    def test_repeat_generation_is_deterministic_without_old_metadata(self):
        self.generate()
        first = [p.read_bytes() for p in self.generated]
        self.setUp()
        self.generate()
        self.assertEqual([p.read_bytes() for p in self.generated], first)

    def test_partial_previous_generation_is_replaced_from_current_inputs(self):
        self.generate()
        self.generated[1].unlink()
        self.generated[2].unlink()
        self.generate()
        self.verify()

    def test_both_metadata_copies_are_verified(self):
        self.generate()
        for path in self.generated[:2]:
            with self.subTest(path=path.name), self.changed(path, b'\n'):
                self.assertIn('Generated source metadata drift', self.verify(False).stderr)

    def test_generated_license_tampering_is_rejected(self):
        self.generate()
        with self.changed(self.generated[2]):
            self.verify(False)
            self.assertIn('Physical rawfile set or digest drift', self.verify(False, raw_only=True).stderr)

    def test_locked_license_tampering_stops_before_generation(self):
        with self.changed(self.dependency / 'LICENSE.CDDL'):
            _, result = self.generate(False)
            self.assertIn('Pinned RAW decoder source drift', result.stderr)
            self.assertFalse(any(p.exists() for p in self.generated))

    def test_web_package_tampering_stops_before_generation(self):
        with self.changed(self.raw / 'client-web/index.html'):
            _, result = self.generate(False)
            self.assertIn('Packaged client resource drift', result.stderr)
            self.assertFalse(any(p.exists() for p in self.generated))

    def test_unknown_rawfile_is_not_adopted_by_new_snapshot(self):
        path = self.raw / 'unexpected.txt'
        try:
            path.write_text('unowned test input')
            _, result = self.generate(False)
            self.assertIn('Physical rawfile set or digest drift', result.stderr)
        finally:
            path.unlink()

    def test_current_native_source_changes_require_new_snapshot(self):
        self.generate()
        first = json.loads(self.generated[0].read_bytes())['sourceSha256']
        path = self.base / 'app/entry/src/main/ets/common/NativeClient.ets'
        with self.changed(path, b'\n// regression-only source change\n'):
            self.assertIn('Formal source set or digest drift', self.verify(False).stderr)
            self.generate()
            second = json.loads(self.generated[0].read_bytes())['sourceSha256']
            self.assertNotEqual(first, second)
            self.verify()


if __name__ == '__main__':
    unittest.main(verbosity=2)
