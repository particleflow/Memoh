"""Real UI loads the injected local resource automatically; synthetic data only."""
import hashlib
import json
import sys
import tempfile
from pathlib import Path
from urllib.parse import urlsplit
from playwright.sync_api import sync_playwright

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / 'mobile/client/tests/stage2'))
sys.path.insert(0, str(ROOT / 'mobile/client/mobile'))
import harmony_auth_persistence as f
import client_config as config

f.server.shutdown(); f.auto_configure = False
f.base = sys.argv[1].rstrip('/')
assert urlsplit(f.base).hostname in ('localhost', '127.0.0.1')
slot = Path(sys.argv[2]).resolve() / 'memoh_client.json'
out = Path(sys.argv[3]); out.mkdir(parents=True, exist_ok=True)
original = slot.read_bytes() if slot.exists() else None
deployment = {
    'apiOrigin': 'https://alpha.example/api', 'hapDownloadUrl': '',
    'productName': 'Garnet-Assistant',
    'legacyBinding': {'schemaVersion': 1, 'bundleName': config.DEPLOYMENT_BUNDLE,
                      'versionCode': 1000006,
                      'sourceHapSHA256': hashlib.sha256(b'synthetic old package').hexdigest(),
                      'apiBaseSHA256': hashlib.sha256(b'https://alpha.example/api').hexdigest()},
}
events, errors, config_reads = [], [], []
original_intercept = f.intercept
def intercept(route):
    if urlsplit(route.request.url).path == '/memoh_client.json':
        config_reads.append(True)
        assert not route.request.headers.get('authorization')
    return original_intercept(route)
f.intercept = intercept
def monitor(path, request):
    events.append({'host': urlsplit(request.url).hostname,
                   'authorized': bool(request.headers.get('authorization'))})
    return None
f.extra_api_handler = monitor
try:
    slot.write_bytes(config.public_deployment_bytes())
    with tempfile.TemporaryDirectory(prefix='injected-config-', dir=out) as folder, sync_playwright() as pw:
        profile = Path(folder) / 'upgraded'
        context = pw.chromium.launch_persistent_context(
            str(profile), executable_path='/usr/bin/chromium', headless=True,
            viewport={'width': 390, 'height': 844}, locale='zh-CN', service_workers='block',
            args=['--proxy-server=http://127.0.0.1:9', '--proxy-bypass-list=127.0.0.1;localhost'])
        context.route('**/*', f.intercept)
        context.route_web_socket('**/*', lambda socket: socket.on_message(lambda message: None))
        context.add_init_script("""if(location.protocol !== 'about:' && !localStorage.getItem('fixture-seeded')) {
            localStorage.setItem('fixture-seeded','true'); localStorage.setItem('language','zh');
            localStorage.setItem('token',%s);
            localStorage.setItem('chat-input-drafts',JSON.stringify({'synthetic-draft':'keep locally'}));
            localStorage.setItem('pinned-bot-ids',JSON.stringify(['synthetic-bot']));
        }
        if(location.protocol !== 'about:' && localStorage.getItem('fixture-quota')) {
            const original = Storage.prototype.setItem;
            Storage.prototype.setItem = function(key, value) {
                if(key === 'memoh:harmony:service-config:v1') throw new DOMException('Synthetic quota','QuotaExceededError');
                original.call(this,key,value);
            };
        }""" % json.dumps(f.issue_token()))
        page = context.new_page()
        page.on('pageerror', lambda error: errors.append(str(error)))
        page.goto(f.base + '/#/', wait_until='domcontentloaded')
        page.get_by_role('heading', name='连接服务').wait_for()
        before = page.evaluate('JSON.stringify({...localStorage})')
        token = f.fingerprint(page)
        assert page.get_by_role('button', name='保存并继续').is_disabled() and not events

        # Deliberately malformed resource bypasses the injection helper so the
        # actual client must independently refuse a different destination.
        bad = json.dumps({**deployment, 'apiOrigin': 'https://beta.example/api'}).encode()
        slot.write_bytes(bad + b' ' * (config.DEPLOYMENT_BYTES - len(bad)))
        page.reload(wait_until='domcontentloaded')
        page.get_by_role('alert').get_by_text('未能通过核对', exact=False).wait_for()
        assert page.evaluate('JSON.stringify({...localStorage})') == before and not events
        page.screenshot(path=str(out / 'different-base-retained.png'))

        slot.write_bytes(config.deployment_bytes(deployment))
        page.evaluate("localStorage.setItem('fixture-quota','1')")
        before_failure = page.evaluate('JSON.stringify({...localStorage})')
        page.reload(wait_until='domcontentloaded')
        page.get_by_role('alert').get_by_text('未能通过核对', exact=False).wait_for()
        assert page.evaluate('JSON.stringify({...localStorage})') == before_failure and not events
        page.evaluate("localStorage.removeItem('fixture-quota')")
        page.reload(wait_until='domcontentloaded'); f.wait_chat(page)
        assert f.fingerprint(page) == token and f.counts['login'] == 0
        assert page.locator('#service-api').count() == 0
        assert page.evaluate("JSON.parse(localStorage.getItem('chat-input-drafts'))['synthetic-draft']") == 'keep locally'
        assert 'synthetic-bot' in page.evaluate("JSON.parse(localStorage.getItem('pinned-bot-ids'))")
        assert all(event['host'] == 'alpha.example' for event in events)
        assert any(event['authorized'] for event in events)
        reads = len(config_reads)
        slot.write_bytes(b'invalid replacement config')
        page.reload(wait_until='domcontentloaded'); f.wait_chat(page)
        assert f.fingerprint(page) == token and len(config_reads) == reads
        context.close()
        context, page = f.start(pw, profile, '/@vite/client'); f.wait_chat(page)
        assert f.fingerprint(page) == token and f.counts['login'] == 0 and len(config_reads) == reads
        page.screenshot(path=str(out / 'verified-login-retained.png'))
        context.close()

        # A new installation receives the same local seed without old binding
        # metadata and reaches login directly, with no manual address step.
        slot.write_bytes(config.deployment_bytes({**deployment, 'legacyBinding': None}))
        boundary = len(events)
        context, page = f.start(pw, Path(folder) / 'fresh', '/@vite/client')
        page.wait_for_selector('#username')
        assert page.locator('#service-api').count() == 0
        assert not page.evaluate("localStorage.getItem('token')")
        assert all(not event['authorized'] for event in events[boundary:])
        context.close()
        assert not errors and not f.page_errors
        result = {'runtime': 'real Harmony Vite entry/UI; synthetic local deployment config and old state',
                  'blankPublicTemplatePreservesUnboundState': True,
                  'differentBasePreservesOriginalStorageAndSendsNoAPIRequest': True,
                  'writeFailurePreservesOriginalStorageAndSendsNoAPIRequest': True,
                  'matchingBaseAutomaticallyPreservesTokenDraftsAndPins': True,
                  'noManualAddressEntry': True, 'persistedConfigOverridesLaterPackage': True,
                  'processRestartRetainsLogin': True, 'freshInstallAutomaticallyReachesLogin': True,
                  'loginHTTPCalls': f.counts['login'], 'pageErrors': errors + f.page_errors,
                  'actual106Package': False, 'actualHAPUpgrade': False, 'nativeDevice': False, 'liveAPI': False}
        (out / 'verified-binding-result.json').write_text(json.dumps(result, indent=2) + '\n')
        print(json.dumps(result))
finally:
    if original is None: slot.unlink(missing_ok=True)
    else: slot.write_bytes(original)
