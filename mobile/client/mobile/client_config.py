"""Private client inputs. Distribution never supplies or overrides the API base."""
import ipaddress
import json
from pathlib import Path
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[1]


def public_url(value, field):
    if not isinstance(value, str) or not value or value != value.strip():
        raise ValueError(field + ' must be deliberately configured')
    parsed = urlsplit(value)
    if parsed.scheme not in ('https', 'http') or not parsed.hostname or parsed.username or parsed.password or parsed.query or parsed.fragment:
        raise ValueError(field + ' must be an explicit HTTP(S) URL without credentials/query/fragment')
    host = parsed.hostname.lower().rstrip('.')
    if host == 'localhost' or host.endswith(('.localhost', '.local', '.lan')):
        raise ValueError(field + ' must not use a LAN/development fallback')
    try:
        address = ipaddress.ip_address(host)
    except ValueError:
        if '.' not in host:
            raise ValueError(field + ' must not use a local hostname')
    else:
        if not address.is_global:
            raise ValueError(field + ' must not use a private/loopback/link-local address')
    return value.rstrip('/')


def read_config(require_api=True):
    config = json.loads((ROOT / 'config/memoh_client.json').read_text())
    if require_api:
        config['apiOrigin'] = public_url(config.get('apiOrigin'), 'apiOrigin')
    if config.get('hapDownloadUrl'):
        public_url(config['hapDownloadUrl'], 'hapDownloadUrl')
    return config
