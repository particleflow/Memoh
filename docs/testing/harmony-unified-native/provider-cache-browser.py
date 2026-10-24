"""Real Harmony/Vite page with intercepted synthetic API; no device/live service.

Usage: python3 provider-cache-browser.py http://127.0.0.1:4181 OUTPUT_DIR
Add --refresh-failure to reject the post-save catalog refresh once until checked.
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
out = Path(sys.argv[2]).resolve()
out.mkdir(parents=True, exist_ok=True)
failure_case = '--refresh-failure' in sys.argv
reject_refresh = False
failed_reads = 0
writes = []
provider_id = 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'
provider = {
    'id': provider_id, 'name': 'Synthetic Provider', 'client_type': 'openai-completions',
    'enable': True, 'config': {'base_url': 'https://provider.example.invalid/v1',
    'api_key': 'synthetic********', 'images_api': {'protocol': 'previous-protocol', 'edits': True}},
}


def api(path, request):
    global reject_refresh, failed_reads
    if path == '/providers':
        if reject_refresh:
            failed_reads += 1
            return 503, {'status': 503, 'code': 'internal.error'}
        return 200, [provider]
    if path in ('/models', '/provider-templates'):
        return 200, []
    if path == '/providers/' + provider_id:
        if request.method == 'PUT':
            payload = request.post_data_json
            assert 'api_key' not in payload.get('config', {})
            writes.append(payload)
            provider.update({k: v for k, v in payload.items() if k != 'config'})
            provider['config'].update(payload.get('config', {}))
            reject_refresh = failure_case and len(writes) == 2
        return 200, provider
    return None


f.extra_api_handler = api
with tempfile.TemporaryDirectory(prefix='provider-cache-', dir=out) as directory, sync_playwright() as pw:
    context = pw.chromium.launch_persistent_context(
        str(Path(directory) / 'profile'), executable_path='/usr/bin/chromium', headless=True,
        viewport={'width': 390, 'height': 844}, locale='zh-CN', service_workers='block',
        args=['--proxy-server=http://127.0.0.1:9', '--proxy-bypass-list=127.0.0.1;localhost'])
    context.route('**/*', f.intercept)
    context.route_web_socket('**/*', lambda socket: socket.on_message(lambda message: None))
    context.add_init_script("localStorage.setItem('locale','zh')")
    page = context.new_page()
    page.on('pageerror', lambda error: f.page_errors.append(str(error)))
    page.goto(f.base + '/#/', wait_until='domcontentloaded')
    page.locator('#service-api').fill('https://alpha.example/api')
    page.get_by_role('button', name='保存并继续', exact=True).click()
    f.login(page)
    identity = f.fingerprint(page)
    page.goto(f.base + '/#/settings/providers?provider=' + provider_id, wait_until='domcontentloaded')
    name = page.locator('form input[type=text]').first
    name.wait_for()
    assert page.locator('input[type=password]').input_value() == ''
    name.fill('Renamed Synthetic Provider')
    page.locator('form button[type=submit]').click()
    page.wait_for_timeout(700)
    assert len(writes) == 1 and 'images_api' not in writes[0].get('config', {})
    protocol = page.locator('button[aria-label="图片生成协议"]')
    protocol.click()
    page.get_by_role('option', name='OpenAI Images', exact=True).click()
    page.locator('form button[type=submit]').click()
    page.wait_for_timeout(700)
    assert len(writes) == 2
    assert writes[1]['config']['images_api'] == {'protocol': 'openai-images', 'edits': True}
    if failure_case:
        assert failed_reads > 0
        assert '保存失败' not in page.locator('body').inner_text()
    cache = page.evaluate("localStorage.getItem('memoh:query-cache') || ''")
    assert 'synthetic********' not in cache and '"api_key"' not in cache
    assert 'previous-protocol' not in cache
    reject_refresh = False
    page.reload(wait_until='domcontentloaded')
    protocol.wait_for()
    page.wait_for_timeout(1000)
    assert 'OpenAI Images' in protocol.inner_text()
    assert page.locator('input[type=password]').input_value() == ''
    assert f.fingerprint(page) == identity and f.counts['login'] == 1
    page.evaluate("document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('settings').theme='dark'")
    page.wait_for_function("document.documentElement.classList.contains('dark')")
    page.wait_for_timeout(2000)  # Let the existing theme transition finish before capture.
    assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
    page.screenshot(path=str(out / 'provider-current.png'))
    result = {'refreshFailureCase': failure_case, 'ordinarySaveOmitsKeyAndImages': True,
              'explicitImagesPreservesPolicies': True, 'quickReloadRetainsChoice': True,
              'diskCacheStripsKey': True, 'loginCalls': f.counts['login'], 'loginRetained': True,
              'pageErrors': f.page_errors, 'nativeDevice': False, 'liveAPI': False}
    (out / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(result), flush=True)
    assert not f.page_errors
    context.close()
