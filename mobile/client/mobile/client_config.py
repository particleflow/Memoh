"""Private Windows deployment inputs; persisted phone configuration has priority."""
import argparse
import hashlib
import ipaddress
import json
import os
import re
import struct
import zlib
import zipfile
from pathlib import Path
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[1]
DEPLOYMENT_BYTES = 8192
DEPLOYMENT_MEMBER = 'resources/rawfile/client-web/memoh_client.json'
DEPLOYMENT_BUNDLE = 'cloud.begonia.workassistant'


def public_url(value, field):
    if not isinstance(value, str) or not value or len(value) > 2048 or value != value.strip():
        raise ValueError(field + ' must be deliberately configured')
    if re.search(r'[\\\x00-\x20%?#]', value) or not re.match(r'^https?://', value):
        raise ValueError(field + ' contains an invalid URL character or scheme')
    parsed = urlsplit(value)
    if parsed.scheme not in ('https', 'http') or not parsed.hostname or parsed.username or parsed.password or parsed.query or parsed.fragment:
        raise ValueError(field + ' must be an explicit HTTP(S) URL without credentials/query/fragment')
    if not re.fullmatch(r'(?:\[[0-9a-fA-F:]+\]|[A-Za-z0-9.-]+)(?::[0-9]+)?', parsed.netloc):
        raise ValueError(field + ' must use a canonical host and numeric port')
    # Accessing .port rejects non-numeric and out-of-range ports. urlsplit alone
    # does not validate them, unlike the Web URL parser.
    port = parsed.port
    if port == 0:
        raise ValueError(field + ' port must be in 1..65535')
    host = parsed.hostname.lower()
    if host.endswith('.') or host == 'localhost' or host.endswith('.localhost'):
        raise ValueError(field + ' must not use a loopback/development endpoint')
    lan = host.endswith(('.local', '.lan'))
    address = None
    try:
        address = ipaddress.ip_address(host)
    except ValueError:
        if not re.fullmatch(r'(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z0-9-]{1,62}', host):
            raise ValueError(field + ' must use a qualified hostname or canonical IP address')
    else:
        ranges = ('10.0.0.0/8', '172.16.0.0/12', '192.168.0.0/16', 'fc00::/7')
        lan = any(address in ipaddress.ip_network(network) for network in ranges)
        if address.is_multicast or address.is_unspecified or address.is_loopback or address.is_link_local or (not lan and not address.is_global):
            raise ValueError(field + ' must not use a loopback/link-local/reserved address')
    if lan and parsed.scheme != 'https':
        raise ValueError(field + ' LAN endpoints require HTTPS with normal certificate validation')
    raw_path = parsed.path
    if re.search(r'/\.{1,2}(/|$)', raw_path) or '//' in raw_path or not re.fullmatch(r'(?:/[A-Za-z0-9_.~-]+)*/?', raw_path):
        raise ValueError(field + ' must use an unencoded path without dot segments')
    path = raw_path.rstrip('/')
    if field != 'hapDownloadUrl' and not path.endswith('/api'):
        raise ValueError(field + ' must end in /api')
    authority = '[' + address.compressed + ']' if isinstance(address, ipaddress.IPv6Address) else host
    if port is not None and port != {'http': 80, 'https': 443}[parsed.scheme]:
        authority += ':' + str(port)
    return parsed.scheme + '://' + authority + path


def read_config(require_api=True):
    config = json.loads((ROOT / 'config/memoh_client.json').read_text())
    if require_api:
        config['apiOrigin'] = public_url(config.get('apiOrigin'), 'apiOrigin')
    if config.get('hapDownloadUrl'):
        config['hapDownloadUrl'] = public_url(config['hapDownloadUrl'], 'hapDownloadUrl')
    return config


def deployment_config(value, allow_empty=False):
    """One existing config format; reject credentials and unknown extra fields."""
    if not isinstance(value, dict) or set(value) - {'apiOrigin', 'hapDownloadUrl', 'productName', 'legacyBinding'}:
        raise ValueError('DEPLOYMENT_CONFIG_FIELDS')
    api = value.get('apiOrigin')
    download = value.get('hapDownloadUrl', '')
    name = value.get('productName', 'Garnet-Assistant')
    if not isinstance(name, str) or not name or len(name) > 128 or not isinstance(download, str):
        raise ValueError('DEPLOYMENT_CONFIG_INVALID')
    api = '' if allow_empty and api == '' else public_url(api, 'apiOrigin')
    download = public_url(download, 'hapDownloadUrl') if download else ''
    binding = value.get('legacyBinding')
    if binding is not None:
        fields = {'schemaVersion', 'bundleName', 'versionCode', 'sourceHapSHA256', 'apiBaseSHA256'}
        if not isinstance(binding, dict) or set(binding) != fields or binding['schemaVersion'] != 1 or binding['bundleName'] != DEPLOYMENT_BUNDLE or binding['versionCode'] != 1000006:
            raise ValueError('DEPLOYMENT_LEGACY_IDENTITY')
        if any(not isinstance(binding[k], str) or not re.fullmatch('[a-f0-9]{64}', binding[k]) for k in ('sourceHapSHA256', 'apiBaseSHA256')):
            raise ValueError('DEPLOYMENT_LEGACY_DIGEST')
        if not api or hashlib.sha256(api.encode()).hexdigest() != binding['apiBaseSHA256']:
            raise ValueError('DEPLOYMENT_LEGACY_ORIGIN_MISMATCH')
    return {'apiOrigin': api, 'hapDownloadUrl': download, 'productName': name, 'legacyBinding': binding}


def deployment_bytes(value, allow_empty=False):
    data = json.dumps(deployment_config(value, allow_empty), ensure_ascii=False, separators=(',', ':')).encode()
    if len(data) > DEPLOYMENT_BYTES:
        raise ValueError('DEPLOYMENT_CONFIG_SIZE')
    return data + b' ' * (DEPLOYMENT_BYTES - len(data))


def public_deployment_bytes():
    value = read_config(require_api=False)
    if value.get('apiOrigin') != '' or value.get('hapDownloadUrl', '') != '' or value.get('legacyBinding') is not None:
        raise ValueError('PUBLIC_BUILD_MUST_NOT_CONTAIN_PRIVATE_CONFIG')
    return deployment_bytes(value, allow_empty=True)


def inject_hap_config(source, config, output, expected_sha256, legacy_hap=None):
    """Update only the reserved, stored ZIP entry and its standard ZIP CRCs.

    Keep all offsets, alignment, unrelated payloads and the original HAP intact.
    This happens before the existing official signing step, only on Windows.
    """
    source, output = Path(source), Path(output)
    original = source.read_bytes()
    if hashlib.sha256(original).hexdigest() != expected_sha256:
        raise ValueError('SOURCE_HAP_SHA256_MISMATCH')
    value = deployment_config(json.loads(Path(config).read_text(encoding='utf-8-sig')))
    binding = value['legacyBinding']
    if binding is not None:
        if legacy_hap is None or hashlib.sha256(Path(legacy_hap).read_bytes()).hexdigest() != binding['sourceHapSHA256']:
            raise ValueError('LEGACY_HAP_SHA256_MISMATCH')
        with zipfile.ZipFile(legacy_hap) as old:
            app = json.loads(old.read('module.json'))['app']
            if app.get('bundleName') != DEPLOYMENT_BUNDLE or app.get('versionCode') != 1000006:
                raise ValueError('LEGACY_HAP_IDENTITY_MISMATCH')
    payload = deployment_bytes(value)
    with zipfile.ZipFile(source) as z:
        if '.pages.info' in z.namelist() or z.namelist().count(DEPLOYMENT_MEMBER) != 1 or z.testzip() is not None:
            raise ValueError('UNSIGNED_CONFIG_SLOT_REQUIRED')
        app = json.loads(z.read('module.json'))['app']
        if app.get('bundleName') != DEPLOYMENT_BUNDLE:
            raise ValueError('SOURCE_HAP_IDENTITY_MISMATCH')
        seed = deployment_config(json.loads(z.read(DEPLOYMENT_MEMBER)), allow_empty=True)
        if seed['apiOrigin'] or seed['hapDownloadUrl'] or seed['legacyBinding'] is not None:
            raise ValueError('SOURCE_HAP_IS_ALREADY_CONFIGURED')
        if seed['productName'] != value['productName']:
            raise ValueError('DEPLOYMENT_PRODUCT_CHANGED')
        info = z.getinfo(DEPLOYMENT_MEMBER)
        if info.compress_type != zipfile.ZIP_STORED or info.flag_bits & 9 or info.file_size != DEPLOYMENT_BYTES or info.compress_size != DEPLOYMENT_BYTES:
            raise ValueError('CONFIG_SLOT_LAYOUT_UNSUPPORTED')
        offset = info.header_offset
        header = struct.unpack_from('<IHHHHHIIIHH', original, offset)
        if header[0] != 0x04034b50 or header[2] & 9 or header[3] != 0 or header[7:9] != (DEPLOYMENT_BYTES, DEPLOYMENT_BYTES):
            raise ValueError('CONFIG_SLOT_LOCAL_HEADER')
        start = offset + 30 + header[9] + header[10]
        if original[start:start + DEPLOYMENT_BYTES] != z.read(DEPLOYMENT_MEMBER):
            raise ValueError('CONFIG_SLOT_OFFSET_MISMATCH')
        central = z.start_dir
        found = []
        for _ in z.infolist():
            if original[central:central + 4] != b'PK\x01\x02':
                raise ValueError('CONFIG_CENTRAL_DIRECTORY')
            name_len, extra_len, comment_len = struct.unpack_from('<HHH', original, central + 28)
            name = original[central + 46:central + 46 + name_len]
            if name == DEPLOYMENT_MEMBER.encode():
                if struct.unpack_from('<I', original, central + 42)[0] != offset:
                    raise ValueError('CONFIG_CENTRAL_OFFSET')
                found.append(central)
            central += 46 + name_len + extra_len + comment_len
        if len(found) != 1:
            raise ValueError('CONFIG_CENTRAL_ENTRY')
        changed = bytearray(original)
        changed[start:start + DEPLOYMENT_BYTES] = payload
        crc = zlib.crc32(payload)
        struct.pack_into('<I', changed, offset + 14, crc)
        struct.pack_into('<I', changed, found[0] + 16, crc)
        # Exclusive creation: never replace the public source or an older output.
        fd = os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        try:
            with os.fdopen(fd, 'wb') as handle:
                handle.write(changed); handle.flush(); os.fsync(handle.fileno())
            with zipfile.ZipFile(output) as check:
                if check.testzip() is not None or check.namelist() != z.namelist():
                    raise ValueError('CONFIGURED_HAP_INTEGRITY')
                for entry in z.infolist():
                    expected = payload if entry.filename == DEPLOYMENT_MEMBER else z.read(entry)
                    if check.read(entry.filename) != expected:
                        raise ValueError('UNEXPECTED_HAP_PAYLOAD_CHANGE')
        except BaseException:
            output.unlink(missing_ok=True)
            raise
    return {'configured': True, 'signed': False, 'modifiedPayloadMembers': 1,
            'bytes': len(changed), 'sha256': hashlib.sha256(changed).hexdigest(),
            'legacyPackageChecked': binding is not None}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--hap', required=True, type=Path)
    parser.add_argument('--config', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--expected-sha256', required=True)
    parser.add_argument('--legacy-hap', type=Path)
    args = parser.parse_args()
    try:
        print(json.dumps(inject_hap_config(args.hap, args.config, args.output, args.expected_sha256, args.legacy_hap)))
    except Exception as error:
        # Private paths, config values and decoder excerpts must never be echoed.
        code = str(error) if isinstance(error, ValueError) and re.fullmatch('[A-Z0-9_]+', str(error)) else type(error).__name__
        print(json.dumps({'configured': False, 'error': code}))
        raise SystemExit(1) from None
