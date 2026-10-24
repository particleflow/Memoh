"""Real client/Vite UI with isolated synthetic HTTP; not native/production acceptance."""
import hashlib
import json
import re
import sys
import tempfile
from pathlib import Path
from playwright.sync_api import sync_playwright

BASE = Path(__import__('os').environ['HARMONY_AUDIT_WORKSPACE']).resolve()
OUT = BASE / 'evidence/client-observation-audit-20261024'
sys.path.insert(0, str(BASE / 'repo/mobile/client/tests/stage2'))
import harmony_auth_persistence as f
f.server.shutdown()
f.base = 'http://127.0.0.1:4179'
f.auto_configure = False
parent = '22222222-2222-4222-8222-222222222222'
delegation = '33333333-3333-4333-8333-333333333333'
target_bot = '55555555-5555-4555-8555-555555555555'
target_session = '66666666-6666-4666-8666-666666666666'
child = '77777777-7777-4777-8777-777777777777'
run = '88888888-8888-4888-8888-888888888888'
content_hash = 'a' * 64
parent_row = {'id': parent, 'bot_id': f.bot_id, 'type': 'chat', 'title': 'Synthetic parent', 'metadata': {}}
handoff = {'delegation_id': delegation, 'request_id': 'synthetic-request', 'source_session_id': parent,
           'target_bot_id': target_bot, 'target_session_id': target_session, 'state': 'completed',
           'delivery_state': 'pending', 'created_at': '2026-10-24T10:00:00Z', 'updated_at': '2026-10-24T10:01:00Z',
           'summary': {'text': '合成成果摘要：等待回收确认', 'source': 'final_assistant', 'truncated': False},
           'attachments': [{'content_hash': content_hash, 'url': f'/bots/{f.bot_id}/sessions/{parent}/delegations/{delegation}/attachments/{content_hash}'}]}
task = {'task_type': 'subagent', 'task_id': child, 'session_id': child, 'parent_session_id': target_session,
        'run_id': run, 'status': 'running', 'summary': 'Latest child run state', 'business_state': 'unknown',
        'content_available': True, 'attachments_available': False, 'created_at': '2026-10-24T10:00:00Z',
        'updated_at': '2026-10-24T10:01:00Z', 'result_summary': {'text': '合成子任务可见回复：正在处理', 'source': 'subagent_assistant', 'truncated': False}}
mode = 'ok'
observed = []

def api(path, request):
    global mode
    if path == f'/bots/{f.bot_id}/sessions': return 200, {'items': [parent_row]}
    if path == f'/bots/{f.bot_id}/sessions/{parent}': return 200, parent_row
    if '/delegations' in path or '/tasks' in path:
        observed.append({'path': path, 'method': request.method, 'mode': mode})
        assert request.method == 'GET', 'Observation must not dispatch or retry delivery'
        if '/delegations' in path:
            if mode in ('503', '403'): return int(mode), {'status': int(mode), 'code': 'http.forbidden' if mode == '403' else 'internal.error'}
            if mode == 'offline': return 503, {'status': 503, 'code': 'internal.error'}
            return 200, {'items': [handoff]} if path.endswith('/delegations') else handoff
        if path == f'/bots/{target_bot}/sessions/{target_session}/tasks': return 200, {'items': [task]}
        if path == f'/bots/{target_bot}/sessions/{target_session}/tasks/subagent/{child}': return 200, task
        if path.endswith('/tasks'): return 200, {'items': []}
        raise AssertionError('Unexpected task scope')
    if path.endswith('/messages'): return 200, {'items': []}
    return None

f.extra_api_handler = api

def start(pw, profile):
    context = pw.chromium.launch_persistent_context(str(profile), executable_path='/usr/bin/chromium', headless=True,
        viewport={'width': 390, 'height': 844}, locale='zh-CN', service_workers='block',
        args=['--proxy-server=http://127.0.0.1:9', '--proxy-bypass-list=127.0.0.1;localhost'])
    for page in context.pages: page.close()
    context.route('**/*', f.intercept)
    context.route_web_socket('**/*', lambda socket: socket.on_message(lambda message: None))
    context.add_init_script("localStorage.setItem('locale','zh')")
    page = context.new_page()
    page.on('pageerror', lambda error: f.page_errors.append({'message': re.sub(r'https?://\S+|eyJ[A-Za-z0-9._-]+', '[redacted]', str(error))[:300]}))
    page.goto(f.base + '/#/', wait_until='domcontentloaded')
    return context, page

with tempfile.TemporaryDirectory(prefix='client-observation-', dir=OUT) as directory, sync_playwright() as pw:
    profile = Path(directory) / 'profile'
    context, page = start(pw, profile)
    page.locator('#service-api').fill('https://alpha.example/api')
    page.get_by_role('button', name='保存并继续', exact=True).click()
    f.login(page)
    fingerprint = f.fingerprint(page)
    stored = page.evaluate("localStorage.getItem('memoh:harmony:service-config:v1')")
    page.reload(wait_until='domcontentloaded'); f.wait_chat(page)
    assert f.fingerprint(page) == fingerprint and f.counts['login'] == 1
    context.close()
    context, page = start(pw, profile); f.wait_chat(page)
    assert f.fingerprint(page) == fingerprint and f.counts['login'] == 1
    assert page.evaluate("localStorage.getItem('memoh:harmony:service-config:v1')") == stored
    page.wait_for_function("document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.has('workspace-tabs')")
    page.evaluate("id=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('workspace-tabs').openSessionChat({sessionId:id,title:'Synthetic parent'})", parent)
    page.get_by_role('button', name='任务', exact=True).click()
    dialog = page.get_by_role('dialog')
    dialog.get_by_role('tab', name='专业 Bot 委派', exact=True).click()
    dialog.get_by_role('button', name=re.compile('目标 Bot')).click()
    dialog.get_by_text('合成成果摘要：等待回收确认', exact=True).wait_for()
    assert '待投递' in dialog.inner_text()
    assert dialog.get_by_role('button', name='下载成果 ' + 'a' * 12, exact=True).is_enabled()
    page.screenshot(path=str(OUT/'delegation-pending-390.png'))
    dialog.get_by_role('button', name='查看下游任务', exact=True).click()
    dialog.get_by_role('button', name=re.compile('子 Agent')).click()
    dialog.get_by_text(task['result_summary']['text'], exact=True).wait_for()
    assert '执行中' in dialog.inner_text()
    assert '业务目标尚未独立核验' in dialog.inner_text()
    mode = '503'
    dialog.get_by_role('button', name='重新读取', exact=True).wait_for(timeout=10000)
    assert task['result_summary']['text'] not in dialog.inner_text()
    assert '专业 Bot 的任务' in dialog.inner_text()
    page.screenshot(path=str(OUT/'downstream-paused-390.png'))
    mode = 'ok'
    dialog.get_by_role('button', name='重新读取', exact=True).click()
    dialog.get_by_text(task['result_summary']['text'], exact=True).wait_for()
    task['status'] = 'failed'; task['result_summary']['text'] = '合成子任务错误：处理失败'
    dialog.get_by_text(task['result_summary']['text'], exact=True).wait_for(timeout=10000)
    assert '本轮运行失败' in dialog.inner_text()
    page.evaluate("document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('settings').theme='dark'")
    page.wait_for_function("document.documentElement.classList.contains('dark')")
    page.wait_for_timeout(300)
    assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
    page.screenshot(path=str(OUT/'downstream-failed-dark-390.png'))
    mode = '403'
    dialog.get_by_role('button', name='重新读取', exact=True).wait_for(timeout=10000)
    assert '专业 Bot 的任务' not in dialog.inner_text()
    assert task['result_summary']['text'] not in dialog.inner_text()
    assert f.fingerprint(page) == fingerprint and f.counts['login'] == 1
    context.close()
    result = {'sourceUI':'Vite actual client modules; pinned 40f plus exact PR79 observation modules',
              'backend':'Synthetic intercepted HTTP only; no live Server/PG or production', 'viewport':'390x844',
              'loginCalls':f.counts['login'], 'reloadAndBrowserProcessRestartRetainLogin':True,
              'persistedConfigurationUnchanged':True,'delegationToServerReturnedTargetSession':True,
              'runningAndFailedChildVisibleOutput':True,'deliveryPendingNotAccepted':True,
              'scopedAttachmentReferenceShown':True,'attachmentBytesDownloaded':False,
              'transientFailureClearsContentRetainsLocation':True,'manualReloadRestoresSameDetail':True,
              'parent403RetiresDownstream':True,'taskRequests':observed,'taskMutationRequests':0,
              'darkNarrowNoHorizontalOverflow':True,'pageErrors':f.page_errors,
              'nativeUpgrade':False,'nativeDevice':False,'humanQA':False}
    (OUT/'browser-audit.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
    print(json.dumps({k:v for k,v in result.items() if k!='taskRequests'},ensure_ascii=False),flush=True)
    assert not f.page_errors
