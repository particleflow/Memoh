"""Private client inputs. Distribution never supplies or overrides the API base."""
import ipaddress
import json
import re
from pathlib import Path
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[1]


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
