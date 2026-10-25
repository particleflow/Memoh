"""Current Harmony/Vite client, intercepted synthetic HTTP and actual login store.

This checks delayed responses across login boundaries, not device installation.
Usage: python3 auth-race-browser.py http://127.0.0.1:4181 OUTPUT_DIR
"""
import json
import sys
import tempfile
from pathlib import Path
from urllib.parse import urlsplit
from playwright.sync_api import sync_playwright

REPO = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO / 'mobile/client/tests/stage2'))
import harmony_auth_persistence as f

f.server.shutdown()
f.base = sys.argv[1].rstrip('/')
assert urlsplit(f.base).hostname in ('127.0.0.1', 'localhost')
out = Path(sys.argv[2]); out.mkdir(parents=True, exist_ok=True)
held = []
errors = []
headers = {'Access-Control-Allow-Origin': f.base,
           'Access-Control-Allow-Headers': 'Authorization, Content-Type',
           'Access-Control-Allow-Methods': 'GET, OPTIONS'}

def delayed(route):
    if route.request.method == 'OPTIONS':
        route.fulfill(status=204, headers=headers)
    else:
        assert route.request.method == 'GET'
        held.append(route)

with tempfile.TemporaryDirectory(prefix='auth-race-', dir=out) as temp, sync_playwright() as pw:
    context = pw.chromium.launch_persistent_context(
        str(Path(temp) / 'profile'), executable_path='/usr/bin/chromium', headless=True,
        viewport={'width': 390, 'height': 844}, locale='zh-CN', service_workers='block',
        args=['--proxy-server=http://127.0.0.1:9', '--proxy-bypass-list=127.0.0.1;localhost'])
    context.route('**/*', f.intercept)
    context.route('https://alpha.example/api/audit/late401', delayed)
    context.route_web_socket('**/*', lambda socket: socket.on_message(lambda message: None))
    context.add_init_script("localStorage.setItem('locale','zh')")
    page = context.new_page()
    page.on('pageerror', lambda error: errors.append(str(error)))
    page.goto(f.base + '/#/', wait_until='domcontentloaded')
    page.locator('#service-api').fill('https://alpha.example/api')
    page.get_by_role('button', name='保存并继续', exact=True).click()
    page.locator('#username').wait_for()

    def start_request(authenticated):
        page.evaluate('''async authenticated => {
            const api = await import('/src/lib/api-client.ts');
            window.__raceDone = false;
            void api.createApiFetch()('https://alpha.example/api/audit/late401', {
                headers: authenticated ? {Authorization: 'Bearer '+localStorage.getItem('token')} : {},
            }).then(() => { window.__raceDone = true });
        }''', authenticated)
        for _ in range(100):
            if held: break
            page.wait_for_timeout(50)
        assert len(held) == 1

    def finish_request():
        held.pop().fulfill(status=401, content_type='application/json', headers=headers,
                           body=json.dumps({'status': 401, 'code': 'http.unauthorized'}))
        page.wait_for_function('window.__raceDone === true')

    # Anonymous request starts before a normal UI login completes.
    start_request(False)
    f.login(page)
    identity = f.fingerprint(page)
    finish_request()
    assert f.fingerprint(page) == identity
    assert '/login' not in page.evaluate('location.hash')

    # Use the real login store to model a later login with the same token bytes.
    start_request(True)
    page.evaluate('''() => {
        const user = document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('user');
        user.login({...user.userInfo}, localStorage.getItem('token'));
    }''')
    finish_request()
    assert f.fingerprint(page) == identity
    assert '/login' not in page.evaluate('location.hash')
    page.reload(wait_until='domcontentloaded'); f.wait_chat(page)
    assert f.fingerprint(page) == identity and f.counts['login'] == 1
    page.wait_for_timeout(350)
    page.screenshot(path=str(out / 'login-retained.png'))

    # A genuine 401 for the current session must still clear it and show login.
    start_request(True)
    finish_request()
    page.locator('#username').wait_for()
    assert page.evaluate("!localStorage.getItem('token')")
    page.wait_for_timeout(350)
    page.screenshot(path=str(out / 'current-401-login.png'))
    result = {'anonymousLate401KeepsNewLogin': True, 'sameTokenPriorSession401Ignored': True,
              'reloadKeepsLogin': True, 'loginHTTPCalls': f.counts['login'],
              'currentSession401StillClearsLogin': True, 'pageErrors': errors,
              'runtime': 'existing Vite + real UI/store + intercepted synthetic HTTP',
              'nativeDevice': False, 'liveAPI': False}
    (out / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(result), flush=True)
    assert not errors
    context.close()
