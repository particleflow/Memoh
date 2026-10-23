"""Compiled Web on persistent Chromium profile, intercepted synthetic API only; not native install."""
import json, tempfile
from pathlib import Path
from urllib.parse import urlsplit
from playwright.sync_api import sync_playwright
import harmony_auth_persistence as f

f.current_dist = f.new_dist
f.auto_configure = False
key = 'memoh:harmony:service-config:v1'
checks = {}
requests = []

def monitor(path, request):
    requests.append({'host': urlsplit(request.url).hostname, 'authorized': bool(request.headers.get('authorization')), 'path': path})
    return None
f.extra_api_handler = monitor

def open_config(page):
    page.evaluate("sessionStorage.setItem('memoh:harmony:edit-service','1');location.reload()")
    page.get_by_role('heading', name='连接服务').wait_for()

def save(page, api, download=''):
    page.locator('#service-api').fill(api)
    page.locator('#service-download').fill(download)
    page.get_by_role('button', name='保存并继续').click()

try:
 with tempfile.TemporaryDirectory(prefix='service-config-',dir=f.OUT) as profile, sync_playwright() as pw:
    context,page=f.start(pw,Path(profile))
    page.get_by_role('heading',name='连接服务').wait_for()
    page.wait_for_timeout(250)
    assert not requests
    assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
    page.screenshot(path=str(f.OUT/'service-empty-internal.png'))
    checks['emptyConfigurationSendsNoAPIRequest']=True
    # Unbound old installation: no token may be used on first entered server.
    page.evaluate("localStorage.setItem('token','synthetic-unbound-old');localStorage.setItem('chat-input-drafts','synthetic-local-draft')")
    page.reload();page.get_by_text('旧版没有保存服务地址。',exact=False).wait_for()
    for invalid in ['http://127.0.0.1/api','https://user:pass@alpha.example/api','https://alpha.example/api?token=x','https://alpha.example/not-api']:
        save(page,invalid)
        page.get_by_role('alert').get_by_text('无法保存。',exact=False).wait_for()
        assert not requests
        assert page.evaluate("localStorage.getItem('token')")=='synthetic-unbound-old'
    checks['invalidURLsFailWithoutRequestsOrDataLoss']=True
    save(page,'https://alpha.example/api','https://download.example/app-1.0.8.hap')
    page.wait_for_selector('#username')
    assert page.evaluate("!localStorage.getItem('token')")
    assert page.evaluate("JSON.parse(localStorage.getItem('memoh:harmony:unbound-state:v1')).entries['chat-input-drafts']")=='synthetic-local-draft'
    assert page.evaluate("!('token' in JSON.parse(localStorage.getItem('memoh:harmony:unbound-state:v1')).entries)")
    page.get_by_role('button',name='服务地址',exact=True).click();page.get_by_role('heading',name='连接服务').wait_for();page.get_by_role('button',name='返回',exact=True).click();page.wait_for_selector('#username')
    checks['loginConfigurationEntryWorks']=True
    assert all(not r['authorized'] for r in requests)
    checks['legacyUnboundTokenNeverSentAndDataBackedUp']=True
    f.login(page);initial=f.fingerprint(page)
    assert f.counts['login']==1
    assert all(r['host']=='alpha.example' for r in requests)
    checks['distributionAddressDoesNotDriveAPI']=True
    context.close()
    context,page=f.start(pw,Path(profile));f.wait_chat(page)
    assert f.fingerprint(page)==initial and f.counts['login']==1
    checks['processRestartPreservesConfiguredLogin']=True
    # Same-origin package reload is the browser-level upgrade boundary. An
    # actual Harmony overwrite-install remains a device acceptance step.
    page.reload();f.wait_chat(page);assert f.fingerprint(page)==initial
    checks['sameOriginResourceReloadPreservesConfiguredLogin']=True
    open_config(page);save(page,'https://alpha.example/api','https://other-download.example/files')
    f.wait_chat(page);assert f.fingerprint(page)==initial and f.counts['login']==1
    checks['downloadOnlyChangePreservesLogin']=True
    open_config(page);page.get_by_role('button',name='返回',exact=True).click();f.wait_chat(page)
    assert f.fingerprint(page)==initial
    checks['cancelConfigEditPreservesLogin']=True
    boundary=len(requests);open_config(page);save(page,'https://beta.example/api')
    page.wait_for_selector('#username')
    assert page.evaluate("!localStorage.getItem('token')")
    assert all(not r['authorized'] for r in requests[boundary:] if r['host']=='beta.example')
    f.login(page)
    assert any(r['host']=='beta.example' and r['authorized'] for r in requests[boundary:])
    checks['serviceSwitchDoesNotSendPreviousCredential']=True
    f.logout(page);context.close();context,page=f.start(pw,Path(profile));page.wait_for_selector('#username')
    assert page.evaluate("!localStorage.getItem('token')")
    checks['logoutSurvivesRestart']=True
    f.login(page);f.revoked=True;page.reload();page.wait_for_selector('#username');assert page.evaluate("!localStorage.getItem('token')")
    checks['revocationStillClearsLogin']=True
    f.revoked=False;f.login(page);f.clock_offset=7200;page.reload();page.wait_for_selector('#username');assert page.evaluate("!localStorage.getItem('token')")
    checks['realSyntheticExpiryStillClearsLogin']=True
    f.clock_offset=0
    # Invalid stored schema stops startup, explicit repair is possible.
    page.evaluate(f"localStorage.setItem('{key}',JSON.stringify({{version:99}}));location.reload()")
    page.get_by_role('heading',name='连接服务').wait_for();boundary=len(requests)
    page.wait_for_timeout(200);assert len(requests)==boundary
    save(page,'https://alpha.example/api');page.wait_for_selector('#username')
    checks['invalidStoredSchemaFailsClosedAndExplicitRepairWorks']=True
    context.close()
    report={'scope':__doc__,'checks':checks,'passed':all(checks.values()),'nativeDevice':False,'actualHAPUpgrade':False,'liveAPI':False,'refreshCalls':f.counts['refresh'],'pageErrors':f.page_errors}
    (f.OUT/'service-config-browser.json').write_text(json.dumps(report,indent=2)+'\n')
    print(json.dumps(report))
finally:
 f.server.shutdown()
