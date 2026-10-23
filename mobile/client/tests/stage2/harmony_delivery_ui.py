"""Actual compiled Harmony host + fixed shared delivery action; synthetic HTTP only."""
import json
import tempfile
from pathlib import Path
from playwright.sync_api import sync_playwright
import harmony_auth_persistence as f

parent = '22222222-2222-4222-8222-222222222222'
other = '22222222-2222-4222-8222-222222222223'
task = '33333333-3333-4333-8333-333333333333'
target = '44444444-4444-4444-8444-444444444444'
target_b = '44444444-4444-4444-8444-444444444445'
bot_b = '11111111-1111-4111-8111-111111111112'
row = {'delegation_id': task, 'request_id': 'synthetic-request', 'source_session_id': parent,
       'target_bot_id': '55555555-5555-4555-8555-555555555555', 'state': 'completed',
       'delivery_state': 'unknown', 'created_at': '2026-10-22T00:00:00Z',
       'updated_at': '2026-10-22T00:01:00Z', 'attachments': [],
       'summary': {'source': 'final_assistant', 'text': 'Synthetic visible final result', 'truncated': False}}
parent_row = {'id': parent, 'bot_id': f.bot_id, 'type': 'chat', 'title': 'Synthetic parent',
              'metadata': {'workspace_target_id': target}}
mode = 'ok'
post_status = 200
posts = []
reads = []
pending = []


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
    if '/delegations' in path:
        assert request.headers.get('authorization', '').startswith('Bearer ')
        if request.method == 'POST':
            assert path == f'/bots/{f.bot_id}/sessions/{parent}/delegations/{task}/delivery'
            assert request.post_data in (None, '')
            posts.append({'path': path, 'method': request.method})
            if post_status != 200:
                return post_status, {'status': post_status, 'code': 'session.not_found' if post_status == 404 else 'http.forbidden', 'detail': 'PRIVATE_DIAGNOSTIC'}
            return 200, {**row, 'delivery_state': 'accepted'}
        reads.append(path)
        if mode == 'unsupported':
            return 404, {'status': 404, 'code': 'http.not_found'}
        if mode in ('403', '404'):
            return int(mode), {'status': int(mode), 'code': 'session.not_found' if mode == '404' else 'http.forbidden'}
        return 200, {'items': [row]} if path.endswith('/delegations') else row
    if path.endswith('/tasks'):
        return 200, {'items': []}
    if path.endswith('/messages'):
        return 200, {'items': []}
    return None


f.extra_api_handler = api
try:
    with tempfile.TemporaryDirectory(prefix='delivery-ui-', dir=f.OUT) as profile, sync_playwright() as pw:
        context, page = f.start(pw, Path(profile))
        f.login(page)
        dialog = page.get_by_role('dialog')
        opener = page.get_by_role('button', name='任务列表', exact=True)
        retry = dialog.get_by_role('button', name='重试结果投递', exact=True)

        def open_parent():
            page.wait_for_function("document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.has('workspace-tabs')")
            page.evaluate("id=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('workspace-tabs').openSessionChat({sessionId:id,title:'Synthetic parent'})", parent)
            opener.wait_for()

        def observe(detail=True):
            opener.click()
            dialog.get_by_role('tab', name='专业 Bot 委派', exact=True).click()
            if detail:
                dialog.get_by_role('button', name='目标 Bot 55555555 · 33333333', exact=False).click()
                dialog.get_by_text(row['summary']['text'], exact=True).wait_for()

        def close():
            dialog.get_by_role('button', name='父对话', exact=True).click()

        def hold(route):
            assert route.request.method == 'POST' and route.request.post_data in (None, '')
            assert route.request.headers.get('authorization', '').startswith('Bearer ')
            posts.append({'path': route.request.url.split('/api', 1)[1], 'method': 'POST'})
            pending.append(route)

        def release():
            for route in pending:
                try:
                    route.fulfill(status=200, content_type='application/json', body=json.dumps({**row, 'delivery_state': 'accepted'}))
                except Exception:
                    pass
            pending.clear()
            page.wait_for_timeout(120)

        open_parent()
        mode = 'unsupported'
        observe(False)
        dialog.get_by_text('此服务器暂不支持', exact=False).wait_for()
        assert '离线' not in dialog.inner_text() and not retry.count() and not posts
        dialog.get_by_role('button', name='返回父对话', exact=True).click()
        mode = 'ok'
        # Existing read-only scheduled-session semantics drive the host capability.
        parent_row['type'] = 'schedule'
        page.reload(wait_until='domcontentloaded')
        opener.wait_for()
        assert f.fingerprint(page)
        open_parent()
        observe()
        assert not retry.count() and not posts
        close()
        parent_row['type'] = 'chat'
        page.reload(wait_until='domcontentloaded')
        opener.wait_for()
        assert f.fingerprint(page)
        open_parent()
        observe()
        retry.wait_for()
        assert not posts  # Opening/reading never retries a result.
        context.route('**/delegations/*/delivery', hold)
        retry.evaluate('el=>{el.click();el.click();el.click()}')
        page.wait_for_timeout(120)
        assert len(pending) == 1 and len(posts) == 1 and retry.is_disabled()
        release()
        dialog.get_by_text('不代表主管 Bot 已处理结果', exact=False).wait_for()
        # A POST acknowledgement does not overwrite the subsequent unknown GET.
        assert '返回投递 · 投递状态未知' in dialog.inner_text()
        assert 'Synthetic visible final result' in dialog.inner_text()
        assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
        page.screenshot(path=str(f.OUT / 'delivery-ack-390.png'))
        context.unroute('**/delegations/*/delivery', hold)
        close()
        row['delivery_state'] = 'accepted'
        observe()
        assert not retry.count()
        close()
        row['delivery_state'] = 'unknown'

        # Mutation rejection remains unconfirmed; no hidden retries or private text.
        for status in [403, 404, 405, 409, 500]:
            post_status = status
            observe()
            before = len(posts)
            retry.click()
            page.wait_for_timeout(250)
            assert len(posts) == before + 1
            assert 'PRIVATE_DIAGNOSTIC' not in dialog.inner_text()
            assert '投递请求响应' not in dialog.inner_text()
            if status in [403, 404, 405]:
                assert retry.is_disabled()
            close()
        post_status = 200
        observe()
        f.network_offline = True
        before = len(posts)
        retry.click()
        dialog.get_by_text('网络', exact=False).first.wait_for()
        page.wait_for_timeout(100)
        assert len(posts) == before
        assert 'Synthetic visible final result' not in dialog.inner_text()
        f.network_offline = False
        dialog.get_by_role('button', name='返回任务列表', exact=True).click()
        dialog.get_by_role('button', name='返回父对话', exact=True).click()

        # POST replies after Bot/session/computer/auth/visibility changes cannot revive UI.
        context.route('**/delegations/*/delivery', hold)
        selection = {'botId': f.bot_id, 'sessionId': parent, 'viewId': 'synthetic'}
        for change in ['session', 'Bot', 'computer', 'identity', 'pagehide']:
            open_parent()
            observe()
            retry.click()
            page.wait_for_timeout(100)
            assert pending
            if change == 'session':
                page.evaluate("id=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('workspace-tabs').openSessionChat({sessionId:id,title:'Other'})", other)
            elif change == 'Bot':
                page.evaluate("id=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('chat').selectBot(id)", bot_b)
            elif change == 'computer':
                page.evaluate("([v,id])=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('chat').setWorkspaceTargetSelection(v,id)", [selection, target_b])
            elif change == 'identity':
                f.logout(page)
                f.login(page)
            else:
                page.evaluate("window.dispatchEvent(new PageTransitionEvent('pagehide'))")
            release()
            assert not dialog.is_visible()
            assert 'Synthetic visible final result' not in page.locator('body').inner_text()
            if change == 'Bot':
                page.evaluate("id=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('chat').selectBot(id)", f.bot_id)
            if change == 'computer':
                before = len(reads)
                observe(False)
                dialog.get_by_text('电脑选择尚未保存到服务端。', exact=False).wait_for()
                assert len(reads) == before and not retry.count()
                dialog.get_by_role('button', name='返回父对话', exact=True).click()
                page.evaluate("([v,id])=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('chat').setWorkspaceTargetSelection(v,id)", [selection, target])
        context.unroute('**/delegations/*/delivery', hold)
        open_parent()
        observe()
        post_status = 401
        retry.click()
        page.wait_for_selector('#username')
        assert not dialog.is_visible()
        assert 'Synthetic visible final result' not in page.locator('body').inner_text()
        assert all(p['path'].endswith('/delivery') and p['method'] == 'POST' for p in posts)
        assert not f.page_errors
        report = {'checkpoint': f.candidate_label(), 'sharedCommit': 'be6862497a58a01ef8c6f4168f02d5624cd44061',
                  'readonlyHostNoAction': True, 'unsupportedNotPCOffline': True,
                  'explicitDeliveryOnly': True, 'duplicateClickSinglePOST': True,
                  'acceptedNotSupervisorProcessing': True, 'freshGETRemainsAuthoritative': True,
                  'denialAndDisconnectNotConfirmed': [401, 403, 404, 405, 409, 500],
                  'lateResponsesDiscarded': ['Bot', 'session', 'computer', 'identity', 'pagehide'],
                  'unsavedSelectionNoRequests': True, 'pageErrors': f.page_errors,
                  'specialistRerun': False, 'nativeDevice': False, 'productionAPI': False}
        (f.OUT / 'delivery-ui.json').write_text(json.dumps(report, indent=2) + '\n')
        print(json.dumps(report))
        context.close()
finally:
    f.server.shutdown()
