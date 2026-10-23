"""Compiled Harmony HTML dialog + unmodified PR54 reader/body; synthetic API only."""
import json
import re
import tempfile
from pathlib import Path
from urllib.parse import parse_qs, urlsplit
from playwright.sync_api import sync_playwright
import harmony_auth_persistence as f

parent = '22222222-2222-4222-8222-222222222222'
other = '22222222-2222-4222-8222-222222222223'
task = '33333333-3333-4333-8333-333333333333'
target = '44444444-4444-4444-8444-444444444444'
target_b = '44444444-4444-4444-8444-444444444445'
bot_b = '11111111-1111-4111-8111-111111111112'
visible_text = '<img src="https://example.invalid/no-fetch"> <script>window.__unsafe=true</script>\n' + '最终回复😀 ' * 100
row = {'delegation_id': task, 'request_id': 'synthetic-request', 'source_session_id': parent,
       'target_bot_id': '55555555-5555-4555-8555-555555555555', 'state': 'completed',
       'delivery_state': 'accepted', 'created_at': '2026-10-22T00:00:00Z', 'updated_at': '2026-10-22T00:01:00Z',
       'attachments': [{'content_hash': 'a' * 64, 'url': '/scoped-unavailable'}],
       'summary': {'source': 'final_assistant', 'text': visible_text, 'truncated': True}}
parent_row = {'id': parent, 'bot_id': f.bot_id, 'type': 'chat', 'title': 'Synthetic parent',
              'metadata': {'workspace_target_id': target}}
mode = 'ok'
requests = []


def api(path, request):
    if path == '/bots':
        return 200, {'items': [f.bot, {**f.bot, 'id': bot_b, 'name': 'Synthetic Bot B'}]}
    if path == f'/bots/{bot_b}':
        return 200, {**f.bot, 'id': bot_b, 'name': 'Synthetic Bot B'}
    if path == f'/bots/{f.bot_id}/sessions':
        return 200, {'items': [parent_row, {**parent_row, 'id': other}]}
    if path == f'/bots/{f.bot_id}/sessions/{parent}':
        return 200, parent_row
    if path == f'/bots/{f.bot_id}/sessions/{other}':
        return 200, {**parent_row, 'id': other}
    if '/delegations' in path or '/tasks' in path:
        cursor = parse_qs(urlsplit(request.url).query).get('cursor', [''])[0]
        requests.append({'path': path, 'method': request.method, 'cursor': cursor})
        assert request.method == 'GET' and request.headers.get('authorization', '').startswith('Bearer ')
        if path.endswith('/tasks'):
            return 200, {'items': []}
        if mode == 'unsupported':
            return 404, {'code': 'http.not_found', 'status': 404}
        if mode == 'inaccessible':
            return 404, {'code': 'session.not_found', 'status': 404}
        if mode in ('403', '401', '500'):
            return int(mode), {'status': int(mode), 'code': 'http.forbidden' if mode == '403' else 'internal'}
        if path.endswith('/delegations'):
            return 200, {'items': [row], **({'next_cursor': 'delegation-page-two'} if not cursor else {})}
        return 200, row
    if path.endswith('/messages'):
        return 200, {'items': []}
    return None


f.extra_api_handler = api
try:
    with tempfile.TemporaryDirectory(prefix='shared-body-', dir=f.OUT) as profile, sync_playwright() as pw:
        context, page = f.start(pw, Path(profile))
        f.login(page)

        def open_parent():
            page.wait_for_function("document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.has('workspace-tabs')")
            page.evaluate("id=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('workspace-tabs').openSessionChat({sessionId:id,title:'Synthetic parent'})", parent)
            page.get_by_role('button', name='任务列表', exact=True).wait_for()

        open_parent()
        opener = page.get_by_role('button', name='任务列表', exact=True)
        dialog = page.get_by_role('dialog')

        def observe():
            opener.click()
            dialog.get_by_role('tab', name='专业 Bot 委派', exact=True).click()

        def first_row():
            return dialog.get_by_role('button', name=re.compile('目标 Bot 55555555'))

        observe()
        first_row().wait_for()
        assert dialog.evaluate('el=>el.tagName === "DIALOG" && el.open')
        assert dialog.get_attribute('aria-describedby') == 'harmony-delegations-description'
        assert dialog.locator('#harmony-delegations-description').is_visible()
        dialog.get_by_role('button', name='下一页', exact=True).click()
        page.wait_for_function('true')
        first_row().wait_for()
        assert any(r['cursor'] == 'delegation-page-two' for r in requests)
        dialog.get_by_role('tab', name='执行任务', exact=True).click()
        dialog.get_by_role('tab', name='专业 Bot 委派', exact=True).click()
        first_row().wait_for()
        assert [r for r in requests if r['path'].endswith('/delegations')][-1]['cursor'] == ''
        assert all(not r['cursor'] for r in requests if r['path'].endswith('/tasks'))
        first_row().evaluate('el=>{el.click();el.click()}')
        dialog.get_by_text(visible_text, exact=True).wait_for()
        assert len([r for r in requests if r['path'].endswith('/delegations/' + task)]) == 1
        assert not dialog.locator('img,iframe,a,script').count()
        assert not page.evaluate('Boolean(window.__unsafe)')
        assert '暂不提供下载' in dialog.inner_text()
        assert '不代表主管 Bot 已处理结果' in dialog.inner_text()
        assert not dialog.get_by_role('button', name=re.compile('取消|重试|下载')).count()
        assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
        for _ in range(8):
            page.keyboard.press('Tab')
            assert page.evaluate('!document.activeElement || document.activeElement === document.body || !!document.activeElement.closest("dialog")')
        page.screenshot(path=str(f.OUT / 'shared-body-native-390.png'))
        page.keyboard.press('Escape')
        first_row().wait_for()
        page.keyboard.press('Escape')
        assert not dialog.is_visible()
        assert opener.evaluate('el=>el===document.activeElement')

        for state, label in [('running', '运行中'), ('future_enum', '执行状态未知')]:
            row['state'] = state
            observe()
            first_row().click()
            dialog.get_by_text('暂无最终可见回复。', exact=True).wait_for()
            assert label in dialog.inner_text()
            assert visible_text not in dialog.inner_text()
            dialog.get_by_role('button', name='父对话', exact=True).click()
        row['state'] = 'completed'

        for next_mode, expected in [('unsupported', '此服务器暂不支持'), ('inaccessible', '当前委派或会话已无法访问'), ('403', '权限'), ('500', '服务器出错了，请重试。')]:
            mode = next_mode
            observe()
            dialog.get_by_text(expected, exact=False).wait_for()
            assert visible_text not in dialog.inner_text()
            assert '没有专业 Bot 委派记录' not in dialog.inner_text()
            if mode == 'unsupported':
                count = len(requests)
                page.wait_for_timeout(5300)
                assert len(requests) == count
                page.screenshot(path=str(f.OUT / 'shared-body-unsupported-390.png'))
            dialog.get_by_role('button', name='返回父对话', exact=True).click()
        mode = 'ok'
        f.network_offline = True
        observe()
        dialog.get_by_role('button', name='重新读取', exact=True).wait_for()
        assert visible_text not in dialog.inner_text()
        f.network_offline = False
        dialog.get_by_role('button', name='重新读取', exact=True).click()
        first_row().wait_for()
        page.evaluate("document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('settings').theme='dark'")
        page.wait_for_function("document.documentElement.classList.contains('dark')")
        page.screenshot(path=str(f.OUT / 'shared-body-dark-390.png'))
        page.evaluate("window.dispatchEvent(new PageTransitionEvent('pagehide'))")
        assert not dialog.is_visible()

        pending = []
        def hold(route):
            pending.append(route)
        context.route('**/delegations/' + task, hold)

        def start_held():
            observe()
            first_row().click()
            page.wait_for_timeout(100)
            assert pending

        def deliver_late():
            for route in pending:
                try:
                    route.fulfill(status=200, content_type='application/json', body=json.dumps(row))
                except Exception:
                    pass
            pending.clear()
            page.wait_for_timeout(100)
            assert not dialog.is_visible()
            assert visible_text not in page.locator('body').inner_text()

        start_held()
        page.evaluate("id=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('workspace-tabs').openSessionChat({sessionId:id,title:'Synthetic second'})", other)
        deliver_late()
        open_parent()
        start_held()
        page.evaluate("id=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('chat').selectBot(id)", bot_b)
        deliver_late()
        page.evaluate("id=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('chat').selectBot(id)", f.bot_id)
        open_parent()
        selection = {'botId': f.bot_id, 'sessionId': parent, 'viewId': 'synthetic'}
        start_held()
        page.evaluate("([view,id])=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('chat').setWorkspaceTargetSelection(view,id)", [selection, target_b])
        deliver_late()
        before = len(requests)
        observe()
        dialog.get_by_text('电脑选择尚未保存到服务端。', exact=False).wait_for()
        page.wait_for_timeout(100)
        assert len(requests) == before
        dialog.get_by_role('button', name='返回父对话', exact=True).click()
        page.evaluate("([view,id])=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('chat').setWorkspaceTargetSelection(view,id)", [selection, target])
        start_held()
        f.logout(page)
        f.login(page)
        open_parent()
        deliver_late()
        context.unroute('**/delegations/' + task, hold)
        mode = '401'
        observe()
        page.wait_for_selector('#username')
        assert not dialog.is_visible()
        assert visible_text not in page.locator('body').inner_text()
        assert all(r['method'] == 'GET' for r in requests)
        assert not f.page_errors
        report = {'checkpoint': f.candidate_label(), 'sharedCommit': 'be6862497a58a01ef8c6f4168f02d5624cd44061',
                  'compiledHarmonyHTMLDialog': True, 'viewport': '390x844', 'sharedReaderAndBodyUnmodified': True,
                  'plainTextAndAttachmentUnavailable': True, 'independentGroupCursors': True,
                  'duplicateDetailOneRequest': True, 'escapeReturnsAndRestoresFocus': True,
                  'unsupportedDistinctFromScoped404': True, 'unsupportedPollingStopped': True,
                  'denialClearsContent': [401, 403, 404, 500], 'networkRecovery': True,
                  'lateScopesDiscarded': ['Bot', 'session', 'computer', 'identity'], 'unsavedSelectionNoReads': True,
                  'pagehideClears': True, 'readOnly': True, 'pageErrors': f.page_errors,
                  'nativeDevice': False, 'liveAPI': False, 'newHAP': False}
        (f.OUT / 'shared-observation-ui.json').write_text(json.dumps(report, indent=2) + '\n')
        print(json.dumps(report))
        context.close()
finally:
    f.server.shutdown()
