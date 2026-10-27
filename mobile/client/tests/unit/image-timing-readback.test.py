"""The desktop filter must not leak the surrounding service output."""
import importlib.util
import json
from pathlib import Path
import unittest

p = Path(__file__).resolve().parents[2] / 'mobile/image_timing_readback.py'
spec = importlib.util.spec_from_file_location('image_timing_readback', p)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class ReadbackTest(unittest.TestCase):
    def test_numeric_whitelist(self):
        result = module.sanitize('DEVICE_PRIVATE\nGarnetImageTiming:' + json.dumps({
            'version': 1, 'rows': [{'id': 1, 'card': 1, 'bytes': 42, 'url': 'PRIVATE', 'token': 'PRIVATE', 'load': -1}]}))
        self.assertEqual(result['rows'], [{'id': 1, 'card': 1, 'bytes': 42}])
        self.assertNotIn('PRIVATE', json.dumps(result))

    def test_resource_fields_and_unknown_zero(self):
        data = {'version': 1, 'rows': [{'id': 1, 'responseStart': 10, 'responseEnd': 20,
                'encodedBodySize': 42, 'transferSize': 50, 'resourceMatch': 1},
                {'id': 2, 'responseStart': 0, 'encodedBodySize': 0, 'transferSize': 0, 'resourceMatch': 2}]}
        rows = module.sanitize('GarnetImageTiming:' + json.dumps(data))['rows']
        self.assertEqual(rows[0]['encodedBodySize'], 42)
        self.assertEqual(rows[0]['transferSize'], 50)
        self.assertEqual(rows[1], {'id': 2, 'resourceMatch': 2})

    def test_missing_or_oversized_payload(self):
        for text in ['DEVICE_PRIVATE', 'GarnetImageTiming:null', 'GarnetImageTiming:' + json.dumps({'version': 1, 'rows': [{'id': 1}] * 6})]:
            with self.assertRaises((ValueError, TypeError)):
                module.sanitize(text)

    def test_booleans_are_not_numeric_times(self):
        result = module.sanitize('GarnetImageTiming:' + json.dumps({'version': 1, 'rows': [{'id': 1, 'card': True}]}))
        self.assertEqual(result['rows'], [{'id': 1}])


if __name__ == '__main__':
    unittest.main()
