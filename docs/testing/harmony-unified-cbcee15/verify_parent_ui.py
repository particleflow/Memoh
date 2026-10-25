"""Generated phone page, synthetic authorized runtime events; never a live task."""
import json
import os
import re
from pathlib import Path
import sys
import tempfile
from urllib.parse import urlsplit
from playwright.sync_api import sync_playwright

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / 'mobile/client/tests/stage2'))
import harmony_auth_persistence as fixture

OUT = Path(os.environ['HARMONY_TEST_EVIDENCE'])
OUT.mkdir(parents=True, exist_ok=True)
fixture.base = os.environ.get('HARMONY_TEST_DEV_URL', 'http://127.0.0.1:4183')
assert urlsplit(fixture.base).hostname in ('127.0.0.1', 'localhost')
fixture.auto_configure = False
session = '22222222-2222-4222-8222-222222222222'
child = '33333333-3333-4333-8333-333333333333'
run_id = '44444444-4444-4444-8444-444444444444'
row = {'id': session, 'bot_id': fixture.bot_id, 'type': 'chat', 'title': 'Synthetic parent'}
stamp = '2026-10-25T00:00:00Z'
run = {'run_id': run_id, 'turn_id': 'synthetic-turn', 'generation': 'synthetic-generation',
       'status': 'running', 'started_at': stamp, 'updated_at': stamp,
       'messages': [{'id': 1, 'type': 'tool', 'name': 'generate_image',
                     'tool_call_id': 'synthetic-image-call', 'running': True,
                     'input': {'prompt': 'Synthetic snow scene <img src=x onerror=alert(1)>'}}]}
sockets = []
frames = []
allow_snapshot = True
sequence = 0

def api(path, request):
    if path == f'/bots/{fixture.bot_id}/sessions': return 200, {'items': [row]}
    if path == f'/bots/{fixture.bot_id}/sessions/{session}': return 200, row
    if path.endswith('/tasks'):
        return 200, {'items': [{'task_type': 'subagent', 'task_id': child,
          'session_id': child, 'parent_session_id': session, 'status': 'running',
          'summary': 'Synthetic child task', 'created_at': stamp, 'updated_at': stamp,
          'content_available': False, 'attachments_available': False}]}
    if path.endswith('/delegations') or path.endswith('/messages'): return 200, {'items': []}
    return None

def snapshot(socket):
    global sequence
    sequence += 1
    socket.send(json.dumps({'type': 'runtime_snapshot', 'session_id': session,
      'epoch': 'synthetic-epoch', 'seq': sequence,
      'snapshot': {'bot_id': fixture.bot_id, 'session_id': session,
        'epoch': 'synthetic-epoch', 'seq': sequence, 'current_run_view': run, 'updated_at': stamp}}))

def runtime_socket(socket):
    def receive(raw):
        if not raw.startswith('{'): return
        data = json.loads(raw)
        if data == {'type': 'ping'}:
            socket.send(json.dumps({'type': 'pong'}))
            return
        assert data.get('type') in ('runtime_subscribe', 'runtime_unsubscribe'), data
        assert data.get('session_id') == session, data
        frames.append(data)
        if data['type'] == 'runtime_subscribe':
            if socket not in sockets: sockets.append(socket)
            if allow_snapshot: snapshot(socket)
    socket.on_message(receive)

fixture.extra_api_handler = api
try:
    with tempfile.TemporaryDirectory(prefix='parent-ui-', dir=OUT) as profile, sync_playwright() as pw:
        context, page = fixture.start(pw, Path(profile), expected_entry='/@vite/client')
        context.route_web_socket('**/*', runtime_socket)
        context.add_init_script("if(location.protocol!=='about:')localStorage.setItem('memoh:harmony:service-config:v1',JSON.stringify({version:1,apiOrigin:'https://alpha.example/api',hapDownloadUrl:'',resetPending:false}))")
        context.add_init_script("window.begoniaNative={request:async raw=>JSON.stringify({ok:true,data:JSON.parse(raw).command==='upload.list'?[]:undefined})}")
        page.reload(wait_until='domcontentloaded')
        fixture.login(page)
        page.wait_for_function("document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.has('workspace-tabs')")
        page.evaluate("id=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('workspace-tabs').openSessionChat({sessionId:id,title:'Synthetic parent'})", session)
        page.get_by_role('button', name='任务', exact=True).click()
        parent_view = page.get_by_role('region', name='本会话最近一次 Bot 运行', exact=True)
        parent_view.get_by_text(run_id, exact=False).wait_for()
        child_button = page.get_by_role('button', name=re.compile(child[:8]))
        child_button.wait_for()
        assert child[:8] not in parent_view.inner_text()
        assert run_id not in child_button.inner_text()
        parent_view.get_by_role('button', name='查看运行内容与工具', exact=True).click()
        parent_view.get_by_text('synthetic-image-call', exact=False).first.wait_for()
        parent_view.get_by_text('工具正在运行', exact=True).wait_for()
        assert parent_view.locator('img').count() == 0
        page.screenshot(path=str(OUT / 'parent-running-390.png'))

        run['status'] = 'aborted'
        snapshot(sockets[-1])
        parent_view.get_by_text('工具结果未确认', exact=True).wait_for()
        assert parent_view.get_by_text('工具正在运行', exact=True).count() == 0
        assert parent_view.get_by_text('已收到工具结果', exact=True).count() == 0
        page.screenshot(path=str(OUT / 'parent-terminal-unconfirmed-390.png'))

        allow_snapshot = False
        sockets[-1].send(json.dumps({'type': 'runtime_dropped', 'session_id': session}))
        parent_view.get_by_text('运行状态尚未同步，会话重新连接后会更新。', exact=True).wait_for()
        assert run_id not in parent_view.inner_text()
        assert 'synthetic-image-call' not in parent_view.inner_text()
        allow_snapshot = True
        snapshot(sockets[-1])
        parent_view.get_by_text(run_id, exact=False).wait_for()
        parent_view.get_by_role('button', name='查看运行内容与工具', exact=True).click()
        parent_view.get_by_text('工具结果未确认', exact=True).wait_for()
        page.screenshot(path=str(OUT / 'parent-resynchronized-390.png'))
        page.keyboard.press('Escape')
        page.reload(wait_until='domcontentloaded')
        page.get_by_role('button', name='任务', exact=True).wait_for()
        assert fixture.counts['login'] == 1
        assert not fixture.page_errors, fixture.page_errors
        assert all(f['type'] in ('runtime_subscribe', 'runtime_unsubscribe') for f in frames)
        report = {'sharedCommit': 'cbcee15a17dc87fd02eb4c198fba0aa45eb3fa45',
          'environment': 'actual generated Vite phone UI; synthetic API/native/runtime boundaries',
          'viewport': '390x844', 'parentRunSeparateFromChild': True, 'toolIdentityVisible': True,
          'terminalDoesNotClaimToolSuccess': True, 'runtimeDroppedClearsStaleRun': True,
          'resynchronizationRestoresSameRun': True, 'reloadKeepsLogin': True,
          'executionWrites': 0, 'pageErrors': fixture.page_errors, 'nativeDevice': False}
        (OUT / 'parent-ui.json').write_text(json.dumps(report, indent=2) + '\n')
        print(json.dumps(report))
        context.close()
finally:
    fixture.server.shutdown()
