"""Actual injection helper with synthetic ZIP/config inputs; no real signing."""
import hashlib
import json
import struct
import sys
import tempfile
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / 'mobile/client/mobile'))
import client_config as config

checks = 0
def check(condition):
    global checks
    assert condition
    checks += 1

def reject(call):
    global checks
    try:
        call()
    except (ValueError, FileExistsError):
        checks += 1
    else:
        raise AssertionError('invalid input accepted')

template = config.public_deployment_bytes()
check(len(template) == config.DEPLOYMENT_BYTES)
check(json.loads(template)['apiOrigin'] == '')
check(json.loads(template)['legacyBinding'] is None)
with tempfile.TemporaryDirectory(prefix='windows-config-injection-') as folder:
    base = Path(folder)
    old, source, private, output = [base / name for name in ('old.hap', 'generic.hap', 'private.json', 'configured.hap')]
    with zipfile.ZipFile(old, 'w') as z:
        z.writestr('module.json', json.dumps({'app': {'bundleName': config.DEPLOYMENT_BUNDLE, 'versionCode': 1000006}}))
    with zipfile.ZipFile(source, 'w') as z:
        z.writestr('module.json', json.dumps({'app': {'bundleName': config.DEPLOYMENT_BUNDLE, 'versionCode': 1000008}}))
        entry = zipfile.ZipInfo(config.DEPLOYMENT_MEMBER)
        entry.extra = struct.pack('<HH8s', 0xCAFE, 8, b'pad-data')
        z.writestr(entry, template)
        z.writestr('ets/modules.abc', b'synthetic immutable native payload')
        z.comment = b'synthetic archive metadata'
    source_bytes = source.read_bytes()
    source_hash = hashlib.sha256(source_bytes).hexdigest()
    value = {
        'apiOrigin': 'https://alpha.example/api/',
        'hapDownloadUrl': '', 'productName': 'Garnet-Assistant',
        'legacyBinding': {'schemaVersion': 1, 'bundleName': config.DEPLOYMENT_BUNDLE,
                          'versionCode': 1000006,
                          'sourceHapSHA256': hashlib.sha256(old.read_bytes()).hexdigest(),
                          'apiBaseSHA256': hashlib.sha256(b'https://alpha.example/api').hexdigest()},
    }
    private.write_text(json.dumps(value))
    reject(lambda: config.inject_hap_config(source, private, output, '0' * 64, old))
    check(not output.exists())
    reject(lambda: config.inject_hap_config(source, private, output, source_hash))
    check(not output.exists())
    for invalid in [
        {**value, 'apiOrigin': 'https://beta.example/api'},
        {**value, 'token': 'synthetic-unwanted'},
        {**value, 'legacyBinding': {**value['legacyBinding'], 'sourceHapSHA256': '0' * 64}},
    ]:
        private.write_text(json.dumps(invalid))
        reject(lambda: config.inject_hap_config(source, private, output, source_hash, old))
        check(not output.exists())
        check(source.read_bytes() == source_bytes)
    private.write_text(json.dumps(value))
    result = config.inject_hap_config(source, private, output, source_hash, old)
    check(result['configured'] and result['legacyPackageChecked'] and not result['signed'])
    check(result['modifiedPayloadMembers'] == 1)
    check(source.read_bytes() == source_bytes)
    check(len(output.read_bytes()) == len(source_bytes))
    with zipfile.ZipFile(source) as before, zipfile.ZipFile(output) as after:
        check(after.testzip() is None)
        check(before.namelist() == after.namelist())
        check(before.comment == after.comment)
        check(before.read('ets/modules.abc') == after.read('ets/modules.abc'))
        check(json.loads(after.read(config.DEPLOYMENT_MEMBER))['apiOrigin'] == 'https://alpha.example/api')
        for entry in before.infolist():
            check(entry.header_offset == after.getinfo(entry.filename).header_offset)
    reject(lambda: config.inject_hap_config(source, private, output, source_hash, old))
    # Reject the existing real 775b without an injection slot. Never modify its
    # JavaScript/bytecode, relax the resource manifest or relabel it as new.
    existing = ROOT / 'artifacts/harmony/1.0.7-775b494d/memoh-harmony-1.0.7-1000007-775b494d-api26-unsigned.hap'
    if existing.exists():
        reject(lambda: config.inject_hap_config(existing, private, base / 'forbidden-old.hap',
                                               hashlib.sha256(existing.read_bytes()).hexdigest(), old))
        check(not (base / 'forbidden-old.hap').exists())
print(json.dumps({'checks': checks, 'passed': True, 'syntheticInjection': True,
                  'real775bCorrectlyRejected': existing.exists(),
                  'fullHAPBuild': False, 'WindowsExecuted': False}))
