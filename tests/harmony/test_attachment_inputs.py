"""Exercise pinned public attachment preparation without a client builder or SDK."""
import copy
import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

REPO = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location(
    'shared', REPO / 'mobile/client/mobile/prepare_shared_observation.py')
shared = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(shared)
LOCK = json.loads((REPO / 'mobile/client/config/memoh_web.lock.json').read_text())
ATTACHMENTS = LOCK['sharedObservation']['delegatedAttachments']


class AttachmentInputsTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.checkout = Path(self.temporary.name) / 'checkout'
        self.checkout.mkdir()
        for name, expected in ATTACHMENTS['baseFiles'].items():
            if expected is not None:
                data = subprocess.check_output(
                    ['git', '-C', str(REPO), 'show', ATTACHMENTS['baseCommit'] + ':' + name])
                self.assertEqual(shared.sha(data), expected)
                path = self.checkout / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(data)

    def snapshot(self):
        return {p.relative_to(self.checkout).as_posix(): p.read_bytes()
                for p in self.checkout.rglob('*') if p.is_file()}

    def test_applies_exact_upstream_bytes_and_rejects_reapplication(self):
        receipt = shared.apply_delegated_attachments(self.checkout, REPO)
        shared.verify(self.checkout, receipt)
        self.assertEqual(len(receipt['outputFiles']), 8)
        for name, expected in ATTACHMENTS['files'].items():
            self.assertEqual(shared.sha((self.checkout / name).read_bytes()), expected)
        before = self.snapshot()
        with self.assertRaisesRegex(ValueError, 'Attachment baseline drift'):
            shared.apply_delegated_attachments(self.checkout, REPO)
        self.assertEqual(self.snapshot(), before)

    def test_baseline_drift_leaves_every_file_untouched(self):
        path = self.checkout / 'apps/web/src/store/workspace-tabs.ts'
        path.write_bytes(path.read_bytes() + b'\n// local modification\n')
        before = self.snapshot()
        with self.assertRaisesRegex(ValueError, 'Attachment baseline drift'):
            shared.apply_delegated_attachments(self.checkout, REPO)
        self.assertEqual(self.snapshot(), before)

    def test_dependency_digest_failure_leaves_every_file_untouched(self):
        changed = copy.deepcopy(LOCK)
        name = next(reversed(ATTACHMENTS['files']))
        changed['sharedObservation']['delegatedAttachments']['files'][name] = '0' * 64
        config_root = Path(self.temporary.name) / 'client'
        (config_root / 'config').mkdir(parents=True)
        (config_root / 'config/memoh_web.lock.json').write_text(json.dumps(changed))
        before = self.snapshot()
        with patch.object(shared, 'ROOT', config_root):
            with self.assertRaisesRegex(ValueError, 'Attachment dependency drift'):
                shared.apply_delegated_attachments(self.checkout, REPO)
        self.assertEqual(self.snapshot(), before)

    def test_symlink_input_is_rejected_without_touching_its_target(self):
        path = self.checkout / ATTACHMENTS['modules'][0]
        target = Path(self.temporary.name) / 'external.ts'
        path.rename(target)
        expected = target.read_bytes()
        path.symlink_to(target)
        with self.assertRaisesRegex(ValueError, 'Linked attachment input'):
            shared.apply_delegated_attachments(self.checkout, REPO)
        self.assertEqual(target.read_bytes(), expected)

    def test_unexpected_test_file_is_preserved(self):
        name = next(p for p, digest in ATTACHMENTS['baseFiles'].items() if digest is None)
        path = self.checkout / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text('existing local test\n')
        before = self.snapshot()
        with self.assertRaisesRegex(ValueError, 'Expected absent attachment input'):
            shared.apply_delegated_attachments(self.checkout, REPO)
        self.assertEqual(self.snapshot(), before)


if __name__ == '__main__':
    unittest.main()
