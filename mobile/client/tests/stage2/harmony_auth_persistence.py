"""Actual compiled UI + persistent Chromium profile, synthetic auth only.

Closes/reopens browser processes and swaps packaged resources at the same URL.
This is NOT a Harmony bundle install, ArkWeb runtime or a real-phone upgrade.
"""
import base64
import functools
import hashlib
import hmac
import http.server
import json
import os
import shutil
from pathlib import Path
import secrets
import re
import tempfile
import threading
import time
from urllib.parse import urlsplit
from playwright.sync_api import sync_playwright

ROOT = Path(__file__).resolve().parents[2]
def legacy_source(variable):
    value = os.environ.get(variable)
    if not value:
        raise RuntimeError(variable + ' must name an explicitly supplied local legacy fixture; it is not included in Git')
    source = Path(value).absolute()
    if not (source / 'mobile/.build/memoh-web/official-dist/index.html').is_file():
        raise RuntimeError(variable + ' has no compiled legacy entry')
    return source

def candidate_label():
    """A source-only UI checkpoint must not invent an installable version."""
    label = os.environ.get('HARMONY_TEST_CHECKPOINT')
    if label:
        return label
    app = ROOT / 'mobile/harmony/app/AppScope/app.json5'
    return json.loads(app.read_text())['app']['versionName']

OUT = ROOT / 'docs/evidence/browser'
OUT.mkdir(parents=True, exist_ok=True)
old_dist = None
new_dist = ROOT / 'mobile/.build/memoh-web/official-dist'
current_dist = new_dist
secret = secrets.token_bytes(32)
clock_offset = 0
revoked = False
network_offline = False
counts = {'login': 0, 'refresh': 0, 'validatedRequests': 0, 'rejectedRequests': 0}
blocked = []
page_errors = []
api_events = []
extra_api_handler = None
auto_configure = True
synthetic_origin = 'https://alpha.example/api'
bot_id = '11111111-1111-4111-8111-111111111111'
user = {'id': 'synthetic-user', 'username': 'synthetic-user', 'display_name': 'Synthetic user',
        'role': 'admin', 'timezone': 'UTC', 'metadata': {'onboarding_completed': True}}
bot = {'id': bot_id, 'name': 'Synthetic Bot', 'status': 'ready', 'type': 'personal',
       'current_user_permissions': ['read', 'chat', 'workspace_read']}

def encode(value):
    return base64.urlsafe_b64encode(value).decode().rstrip('=')

def issue_token():
    now = int(time.time()) + clock_offset
    payload = {'sub': 'synthetic-user', 'user_id': 'synthetic-user', 'iat': now, 'exp': now + 3600}
    value = encode(b'{"alg":"HS256","typ":"JWT"}') + '.' + encode(json.dumps(payload).encode())
    return value + '.' + encode(hmac.new(secret, value.encode(), hashlib.sha256).digest())

def valid_token(token):
    try:
        header, body, signature = token.split('.')
        expected = encode(hmac.new(secret, (header + '.' + body).encode(), hashlib.sha256).digest())
        claims = json.loads(base64.urlsafe_b64decode(body + '=' * (-len(body) % 4)))
        return hmac.compare_digest(expected, signature) and claims['exp'] > time.time() + clock_offset and not revoked
    except (ValueError, KeyError, TypeError):
        return False

class Handler(http.server.SimpleHTTPRequestHandler):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, directory=str(current_dist), **kwargs)
    def log_message(self, *_):
        pass

server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
threading.Thread(target=server.serve_forever, daemon=True).start()
base = f'http://127.0.0.1:{server.server_port}'

def intercept(route):
    request = route.request
    parsed = urlsplit(request.url)
    if parsed.netloc == urlsplit(base).netloc:
        route.continue_()
        return
    if '/api/' not in parsed.path:
        blocked.append({'kind': 'non-api', 'path': parsed.path})
        route.abort()
        return
    headers = {'Access-Control-Allow-Origin': base, 'Access-Control-Allow-Headers': 'Authorization, Content-Type',
               'Access-Control-Allow-Methods': 'GET, HEAD, POST, PUT, OPTIONS'}
    if request.method == 'OPTIONS':
        route.fulfill(status=204, headers=headers)
        return
    path = parsed.path.split('/api', 1)[1]
    api_events.append({'path': path, 'method': request.method, 'hasBearer': bool(request.headers.get('authorization'))})
    if network_offline:
        route.abort('internetdisconnected')
        return
    status = 200
    data = []
    if path == '/auth/login':
        counts['login'] += 1
        body = request.post_data_json
        if body != {'username': 'synthetic-user', 'password': 'synthetic-test-password'}:
            status, data = 401, {'message': 'Invalid synthetic credentials'}
        else:
            data = {'access_token': issue_token(), 'token_type': 'Bearer', 'user_id': user['id'],
                    'username': user['username'], 'display_name': user['display_name'], 'role': user['role']}
    elif not valid_token(request.headers.get('authorization', '').removeprefix('Bearer ')):
        counts['rejectedRequests'] += 1
        status, data = 401, {'message': 'Synthetic session expired or revoked'}
    elif extra_api_handler and (override := extra_api_handler(path, request)) is not None:
        status, data = override
    else:
        counts['validatedRequests'] += 1
        if path == '/users/me':
            data = user
        elif path == '/auth/refresh':
            counts['refresh'] += 1
            data = {'access_token': issue_token(), 'token_type': 'Bearer'}
        elif path == '/bots':
            data = {'items': [bot]}
        elif path == '/bots/' + bot_id:
            data = bot
        elif path.endswith('/sessions') or path.endswith('/messages'):
            data = {'items': []}
        elif path.endswith('/settings'):
            data = {'model_id': 'synthetic-model'}
        elif path.endswith('/container/fs/list'):
            data = {'path': '/data', 'entries': []}
    route.fulfill(status=status, content_type='application/json', headers=headers, body=json.dumps(data))

def start(playwright, profile, expected_entry=None):
    context = playwright.chromium.launch_persistent_context(str(profile), executable_path=os.environ.get('HARMONY_CHROMIUM') or shutil.which('chromium') or shutil.which('chromium-browser'),
        headless=True, viewport={'width': 390, 'height': 844}, locale='zh-CN', service_workers='block',
        args=['--proxy-server=http://127.0.0.1:9', '--proxy-bypass-list=127.0.0.1;localhost'])
    restored_pages = list(context.pages)
    if expected_entry is not None and restored_pages:
        # Vite's restored HMR document can hang Page.close before interception
        # is installed. Navigate it away after installing routes instead.
        page = restored_pages[0]
    else:
        page = context.new_page()
        for restored in restored_pages:
            restored.close()
    context.route('**/*', intercept)
    context.route_web_socket('**/*', lambda socket: socket.on_message(lambda message: None))
    context.add_init_script("if(location.protocol !== 'about:') localStorage.setItem('locale','zh')")
    if auto_configure and current_dist == new_dist:
        context.add_init_script("if (!localStorage.getItem('memoh:harmony:service-config:v1')) localStorage.setItem('memoh:harmony:service-config:v1', JSON.stringify({version:1,apiOrigin:'https://alpha.example/api',hapDownloadUrl:'',resetPending:false}))")
    # A persistent profile may restore the last document before routing attaches.
    # Navigating only its hash can keep the OLD compiled entry alive. Open a new
    # document after interception is installed; retain the same storage profile.
    page.on('pageerror', lambda error: page_errors.append({
        'message': re.sub(r'https?://\S+|eyJ[A-Za-z0-9._-]+', '[redacted]', str(error))[:400],
        'offlineProbe': network_offline}))
    if expected_entry is not None:
        page.goto('about:blank', wait_until='domcontentloaded')
    page.goto(base + '/#/', wait_until='domcontentloaded')
    if expected_entry is None:
        expected_entry = re.search(r'<script[^>]+src="([^"]+)"', (current_dist / 'index.html').read_text()).group(1)
    actual_entry = page.locator('script[src]').first.get_attribute('src')
    assert actual_entry == expected_entry, 'Wrong compiled entry loaded during resource migration'
    return context, page

def login(page):
    page.wait_for_selector('#username')
    page.locator('#username').fill('synthetic-user')
    page.locator('#password').fill('synthetic-test-password')
    page.locator('button[type=submit]').click()
    wait_chat(page)

def wait_chat(page):
    try:
        page.wait_for_function("document.body.innerText.includes('Synthetic Bot') && !location.hash.includes('/login')")
    except Exception:
        print(json.dumps({'debug': 'chat-wait', 'hash': page.evaluate('location.hash'),
            'tokenPresent': page.evaluate("!!localStorage.getItem('token')"),
            'syntheticTokenValid': valid_token(page.evaluate("localStorage.getItem('token') || ''")),
            'body': page.locator('body').inner_text()[:400], 'api': api_events[-12:], 'errors': page_errors}), flush=True)
        raise

def fingerprint(page):
    # SHA stays inside the harness for equality checks and is never recorded.
    value = page.evaluate("localStorage.getItem('token') || ''")
    assert value
    return hashlib.sha256(value.encode()).digest()

def logout(page):
    page.evaluate("document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('user').exitLogin()")
    page.wait_for_selector('#username')
    assert page.evaluate("!localStorage.getItem('token')")

def main():
    global current_dist, network_offline, revoked, clock_offset
    current_dist = legacy_source('HARMONY_LEGACY_103_ROOT') / 'mobile/.build/memoh-web/official-dist'
    results = {}
    try:
        with tempfile.TemporaryDirectory(prefix='auth-synthetic-', dir=OUT) as directory, sync_playwright() as playwright:
            profile = Path(directory) / 'browser-profile'
            context, page = start(playwright, profile)
            login(page)
            initial = fingerprint(page)
            assert counts['login'] == 1
            context.close()
            print('first-login completed', flush=True)

            context, page = start(playwright, profile)
            wait_chat(page)
            assert fingerprint(page) == initial and counts['login'] == 1
            results['processRestartKeepsValidSession'] = True
            context.close()

            current_dist = new_dist
            context, page = start(playwright, profile)
            wait_chat(page)
            assert fingerprint(page) == initial and counts['login'] == 1
            results['sameOriginResourceUpgradeKeepsValidSession'] = True
            print('restart and resource upgrade retained login', flush=True)

            # HTTP 503/offline errors must not be reclassified as expired credentials.
            network_offline = True
            page.reload(wait_until='domcontentloaded')
            page.wait_for_timeout(600)
            assert fingerprint(page) == initial
            network_offline = False
            page.reload(wait_until='domcontentloaded')
            wait_chat(page)
            results['offlineRestartRetainsCredential'] = True

            logout(page)
            context.close()
            context, page = start(playwright, profile)
            page.wait_for_selector('#username')
            assert page.evaluate("!localStorage.getItem('token')")
            results['explicitLogoutStaysLoggedOutAfterRestart'] = True
            login(page)
            revoked = True
            page.reload(wait_until='domcontentloaded')
            page.wait_for_selector('#username')
            assert page.evaluate("!localStorage.getItem('token')")
            results['serverRevocation401ClearsSession'] = True
            revoked = False
            login(page)
            clock_offset = 7200
            page.reload(wait_until='domcontentloaded')
            page.wait_for_selector('#username')
            assert page.evaluate("!localStorage.getItem('token')")
            results['signedSyntheticJWTExpiry401ClearsSession'] = True
            context.close()
            context, page = start(playwright, profile)
            page.wait_for_selector('#username')
            assert page.evaluate("!localStorage.getItem('token')")
            results['expiredSessionNotResurrectedAfterRestart'] = True
            context.close()
            assert counts['refresh'] == 0
            expected_offline_errors = all(error['offlineProbe'] and error['message'] == 'Failed to fetch' for error in page_errors)
            expected_blocked_fonts = all(item['path'] == '/css2' for item in blocked)
            report = {'scope': __doc__.strip(), 'results': results, 'apiCounts': counts,
                      'credentialsLogged': False, 'localCredentialCopiesPersistedAfterTest': False,
                      'sameBrowserProfileAcrossProcesses': True, 'storageStateImportUsed': False,
                      'oldVersion': '1.0.3-rc.66b32f8c', 'newVersion': json.loads((ROOT / 'mobile/harmony/app/AppScope/app.json5').read_text())['app']['versionName'],
                      'actualHAPUpgrade': False, 'nativeDevice': False, 'liveAPI': False,
                      'pageErrors': page_errors, 'blockedExternal': blocked,
                      'offlineFetchErrorRecorded': bool(page_errors),
                      'authScenariosPassed': all(results.values()) and expected_offline_errors and expected_blocked_fonts}
            (OUT / 'auth-persistence-browser.json').write_text(json.dumps(report, indent=2) + '\n')
            print(json.dumps(report), flush=True)
            # Offline deliberately aborts requests; retain that observed error instead
            # of relabeling it as an authentication failure. Optional fonts stay blocked.
            assert report['authScenariosPassed']
    finally:
        server.shutdown()

if __name__ == '__main__':
    main()
