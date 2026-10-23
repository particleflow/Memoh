"""Actual old/new compiled Harmony Web bundles, intercepted synthetic API only."""
import json
from pathlib import Path
import tempfile
from playwright.sync_api import sync_playwright
import harmony_auth_persistence as fixture

fixture.old_dist = fixture.legacy_source('HARMONY_LEGACY_103_ROOT') / 'mobile/.build/memoh-web/official-dist'
OUT = fixture.OUT
shots = OUT / 'bot-ui-internal'
shots.mkdir(exist_ok=True)
fixture.bot.update(name='synthetic-bot', display_name='Synthetic Bot', current_user_permissions=['read','chat','manage','workspace_read'])
mode = 'normal'
writes = []
responses = []

def api(path, request):
    if path == '/bots/name-availability':
        return 200, {'available': True, 'name': 'synthetic-new-bot'}
    if path == '/bots/' + fixture.bot_id:
        responses.append(mode)
        if mode in ['403', '404', '500']:
            return int(mode), {'message': 'Synthetic controlled error', 'status': int(mode)}
    if request.method not in ['GET','HEAD']:
        writes.append({'method': request.method, 'path': path})
        # Deliberate server refusal verifies the client does not bypass permission.
        return 403, {'message': 'Synthetic create denied', 'status': 403, 'code':'http.forbidden'}
    if path.endswith('/settings'):
        return 200, {'chat_model_id':'synthetic-model','memory_enabled':True,'timezone':'UTC'}
    if path.endswith('/container'):
        return 200, {'status':'stopped','workspace_backend':'container'}
    if path.endswith('/agents') or path.endswith('/apps') or path.endswith('/user-access'):
        return 200, {'items': []}
    return None

fixture.extra_api_handler = api

def go(page, path):
    page.goto(fixture.base + '/#' + path, wait_until='domcontentloaded')

def narrow(page):
    assert page.evaluate('document.documentElement.scrollWidth <= window.innerWidth')

def shot(page, name):
    page.wait_for_timeout(650)
    narrow(page)
    page.screenshot(path=str(shots / name), full_page=True)

try:
    with tempfile.TemporaryDirectory(prefix='bot-ui-synthetic-', dir=fixture.OUT) as directory, sync_playwright() as playwright:
        profile = Path(directory) / 'profile'
        fixture.current_dist = fixture.old_dist
        context, page = fixture.start(playwright, profile)
        fixture.login(page)
        baseline = []
        for target in ['/settings/bots', '/settings/bots/new', '/settings/bots/' + fixture.bot_id]:
            go(page, target)
            page.wait_for_function("!location.hash.startsWith('#/settings/')")
            baseline.append({'route':target, 'redirectedToChat':True, 'classification':'client guard redirect; not HTTP 404 or backend rejection'})
        context.close()
        print('old candidate Bot route regression reproduced', flush=True)

        fixture.current_dist = fixture.new_dist
        context, page = fixture.start(playwright, profile)
        fixture.wait_chat(page)
        go(page, '/settings')
        page.get_by_text('Bot', exact=True).click()
        page.wait_for_url('**/#/settings/bots')
        page.get_by_text('新建 Bot', exact=True).wait_for(state='visible')
        shot(page,'bot-list-390.png')
        # Existing Bot detail route stays in settings and opens the actual general panel.
        go(page, '/settings/bots/' + fixture.bot_id)
        page.get_by_text('通用', exact=True).click()
        page.wait_for_url('**tab=general')
        page.get_by_text('URL 名称', exact=True).wait_for(state='visible')
        page.wait_for_function("location.hash.includes('/settings/bots/') && document.body.innerText.includes('Synthetic Bot')")
        page.wait_for_timeout(500)
        shot(page,'bot-settings-390.png')
        assert page.evaluate("!!localStorage.getItem('token')")
        go(page, '/settings/bots')
        page.get_by_text('新建 Bot', exact=True).click()
        page.wait_for_selector('#bot-create-name')
        shot(page,'bot-create-390.png')
        # Cancel through the real form. Reopen and cancel again; no POST is sent.
        page.get_by_role('button',name='取消',exact=True).click()
        page.wait_for_url('**/#/settings/bots')
        page.get_by_text('新建 Bot', exact=True).click()
        page.wait_for_selector('#bot-create-name')
        page.get_by_role('button',name='取消',exact=True).click()
        page.wait_for_url('**/#/settings/bots')
        assert not writes
        print('new Bot list/settings/create/cancel navigation passed', flush=True)

        # Same page supports repeated navigation without multiplying create calls.
        go(page, '/settings/bots/new')
        page.wait_for_selector('#bot-create-name')
        # Real input events drive the name-availability query and form validation.
        page.locator('form input[type=text]').first.fill('Synthetic New Bot')
        page.locator('#bot-create-name').fill('synthetic-new-bot')
        page.wait_for_function("!document.querySelector('button[type=submit]').disabled")
        page.locator('form').evaluate("form => { form.requestSubmit(); form.requestSubmit(); }")
        page.wait_for_function("location.hash.includes('/settings/bots/new/progress')")
        page.wait_for_timeout(600)
        assert len([w for w in writes if w['method']=='POST' and w['path']=='/bots']) == 1
        assert page.evaluate("!!localStorage.getItem('token')")
        shot(page,'bot-create-403-390.png')

        classifications = []
        original_bot_id = fixture.bot_id
        for status in ['403','404','500']:
            mode = status
            # A distinct synthetic ID avoids exercising cached successful detail
            # data when this case is meant to observe an actual API error response.
            fixture.bot_id = '22222222-2222-4222-8222-000000000' + status
            with page.expect_response(lambda response: response.status == int(status) and response.url.split('?')[0].endswith('/bots/' + fixture.bot_id)):
                go(page, '/settings/bots/' + fixture.bot_id)
            page.wait_for_timeout(750)
            assert page.evaluate('location.hash').startswith('#/settings/bots/')
            assert page.evaluate("!!localStorage.getItem('token')")
            assert status in responses
            classifications.append({'apiStatus':int(status),'clientRedirectedToChat':False,'loginRetained':True})
        mode = 'normal'
        fixture.bot_id = original_bot_id
        go(page, '/')
        fixture.wait_chat(page)
        shot(page,'back-to-chat-390.png')
        context.close()
        report = {'scope':__doc__, 'oldVersion':'1.0.3-rc.66b32f8c','newVersion':fixture.candidate_label(),
                  'baseline':baseline,'botSettingsRestored':True,'createEntryRestored':True,
                  'cancelWithoutCreate':True,'duplicateSubmitSinglePOST':True,'server403NotBypassed':True,
                  'errorClassifications':classifications,'returnedToChat':True,'viewport':'390x844',
                  'nativeDevice':False,'liveAPI':False,'pageErrors':fixture.page_errors,'writes':writes}
        (OUT/'bot-navigation.json').write_text(json.dumps(report,indent=2)+'\n')
        print(json.dumps(report),flush=True)
finally:
    fixture.server.shutdown()
