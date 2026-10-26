"""Private-slot parser contract only: no HAP, credentials, network or device IO."""
import hashlib
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / 'mobile'))
import client_config as config

checks = 0


def check(condition):
    global checks
    assert condition
    checks += 1


def rejected(value):
    global checks
    try:
        config.network_policy(value)
    except (ValueError, TypeError):
        checks += 1
        return
    raise AssertionError('invalid policy accepted')


policy = {'version': 1, 'lan_addresses': ['192.0.2.10', '2001:db8::10']}
seed = {'apiOrigin': 'https://nas.example:18443/api', 'productName': 'Synthetic', 'networkPolicy': policy}
check(config.deployment_config(seed)['networkPolicy'] == policy)
check(config.network_policy({'version': 1, 'lan_addresses': ['::ffff:192.0.2.10', '2001:0DB8::10', 'fe80::1%fixture']}) == {'version': 1, 'lan_addresses': ['192.0.2.10', '2001:db8::10', 'fe80::1%fixture']})
for value in [None, [], {}, {'version': True, 'lan_addresses': ['192.0.2.1']}, {'version': 2, 'lan_addresses': ['192.0.2.1']}, {'version': 1, 'lan_addresses': []}, {**policy, 'url': 'https://other.example/api'}]:
    rejected(value)
for address in ['0.0.0.0', '224.0.0.1', '::', 'ff02::1', '::ffff:0.0.0.0', '::ffff:224.0.0.1', 'nas.example', 'https://192.0.2.10', '192.0.2.10:443', '192.00.2.10', 'fe80::1%', '2001:::1']:
    rejected({'version': 1, 'lan_addresses': [address]})
rejected({'version': 1, 'lan_addresses': ['192.0.2.10', '::ffff:c000:20a']})
rejected({'version': 1, 'lan_addresses': ['fe80::1%' + '界' * 3000]})
payload = config.deployment_bytes(seed)
check(len(payload) == config.DEPLOYMENT_BYTES == 8192)
check(json.loads(payload)['apiOrigin'] == seed['apiOrigin'])
check(json.loads(payload)['networkPolicy'] == policy)
old = 'http://nas.example:18068/api'
binding = {'schemaVersion': 1, 'bundleName': config.DEPLOYMENT_BUNDLE, 'versionCode': 1000006, 'sourceHapSHA256': 'a' * 64, 'apiBaseSHA256': hashlib.sha256(old.encode()).hexdigest(), 'apiOrigin': old}
check(config.deployment_config({**seed, 'legacyBinding': binding})['legacyBinding'] == binding)
check('networkPolicy' not in config.deployment_config({'apiOrigin': seed['apiOrigin']}))
check('networkPolicy' not in json.loads(config.public_deployment_bytes()))
for value in [{**seed, 'apiOrigin': ''}, {**seed, 'apiOrigin': 'http://nas.example/api'}, {**seed, 'credentials': 'forbidden'}, {**seed, 'networkPolicy': None}]:
    try:
        config.deployment_bytes(value, allow_empty=True)
    except ValueError:
        checks += 1
    else:
        raise AssertionError('unsafe deployment accepted')
print(json.dumps({'passed': True, 'checks': checks, 'slot_bytes': 8192, 'original_origin_and_legacy_binding_preserved': True, 'public_seed_contains_no_policy': True, 'hap_built_or_changed': False, 'network_requests': 0}))
