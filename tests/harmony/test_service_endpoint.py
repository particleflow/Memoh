"""Explicit endpoint validation; no DNS, TLS connection or automatic route switch."""
import importlib.util
from pathlib import Path
import unittest

REPO = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location(
    'client_config', REPO / 'mobile/client/mobile/client_config.py')
config = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(config)


class EndpointValidationTest(unittest.TestCase):
    def test_explicit_https_lan_addresses(self):
        for host in ('10.0.0.2', '172.16.0.2', '172.31.255.254', '192.168.1.1',
                     '[fd12::1]', '[fc12::2]', 'nas.local', 'nas.lan'):
            with self.subTest(host=host):
                value = 'https://' + host + '/api'
                self.assertEqual(config.public_url(value, 'apiOrigin'), value)
                with self.assertRaises(ValueError):
                    config.public_url('http://' + host + '/api', 'apiOrigin')

    def test_global_endpoint_compatibility(self):
        for value in ('https://alpha.example/api', 'https://[2606:4700::1111]/api',
                      'http://alpha.example:18000/api'):
            self.assertEqual(config.public_url(value, 'apiOrigin'), value)

    def test_rejects_implicit_local_and_embedded_authorization(self):
        for value in ('https://localhost/api', 'https://localhost.localhost/api',
                      'https://127.0.0.1/api', 'https://[::1]/api',
                      'https://169.254.1.1/api', 'https://[fe80::1]/api',
                      'https://0.0.0.0/api', 'https://224.0.0.1/api', 'https://[ff02::1]/api',
                      'https://0x0a000002/api', 'https://167772162/api',
                      'https://user:password@nas.lan/api', 'https://nas.lan/api?token=synthetic',
                      'https://nas.lan/api#fragment', 'https://nas.lan./api'):
            with self.subTest(value=value):
                with self.assertRaises(ValueError):
                    config.public_url(value, 'apiOrigin')

    def test_rejects_encoded_dot_and_non_api_paths_and_invalid_ports(self):
        for value in ('https://alpha.example/%61pi', 'https://alpha.example/foo/../api',
                      'https://alpha.example/foo/./api', 'https://alpha.example//api',
                      'https://alpha.example/api//', 'https://alpha.example/not-api',
                      'https://alpha.example:65536/api', 'https://alpha.example:no/api',
                      'https://alpha.example:-1/api', 'https://alpha.example:1.2/api',
                      'https://alpha.example:0/api', 'https://alpha.example:/api'):
            with self.subTest(value=value):
                with self.assertRaises(ValueError):
                    config.public_url(value, 'apiOrigin')

    def test_matches_normalized_origin_and_installer_path_rules(self):
        cases = (
            ('https://ALPHA.example:443/gateway/api/', 'apiOrigin', 'https://alpha.example/gateway/api'),
            ('http://alpha.example:80/api/', 'apiOrigin', 'http://alpha.example/api'),
            ('https://nas.lan:8443/api', 'apiOrigin', 'https://nas.lan:8443/api'),
            ('https://[2606:4700:0:0:0:0:0:1111]:443/api/', 'apiOrigin', 'https://[2606:4700::1111]/api'),
            ('https://download.example:443/releases/client.hap', 'hapDownloadUrl', 'https://download.example/releases/client.hap'),
        )
        for value, field, expected in cases:
            with self.subTest(value=value):
                self.assertEqual(config.public_url(value, field), expected)


if __name__ == '__main__':
    unittest.main()
