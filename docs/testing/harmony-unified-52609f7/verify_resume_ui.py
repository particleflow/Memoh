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

operation='55555555-5555-4555-8555-555555555555'
target='66666666-6666-4666-8666-666666666666'
original={'kind':'herdr_pi_task','task':'Synthetic original native instruction','target_id':target,'work_dir':'C:/synthetic-work'}
run['status']='aborted'
run['messages']=[{'id':1,'type':'tool','name':'computer_task_submit','tool_call_id':'synthetic-native-call','running':False,'input':original,'output':{'structuredContent':{'kind':'herdr_pi_task','operation_id':operation,'target_id':target,'state':'unknown','phase':'batch_creating_workspace'}}}]
posts=[]
base_api=api
def resume_api(path,request):
    if path.endswith('/resume'):
        assert path==f'/bots/{fixture.bot_id}/sessions/{session}/tasks/computer_operation/{operation}'+'/resume'
        assert request.method=='POST' and request.post_data_json==original
        posts.append({'path':path,'body':request.post_data_json})
        return 503, {'message':'Synthetic lost/unknown server outcome'}
    return base_api(path,request)
fixture.extra_api_handler=resume_api
try:
    with tempfile.TemporaryDirectory(prefix='resume-ui-', dir=OUT) as profile,sync_playwright() as pw:
        context,page=fixture.start(pw,Path(profile),expected_entry='/@vite/client')
        context.route_web_socket('**/*',runtime_socket)
        context.add_init_script("if(location.protocol!=='about:')localStorage.setItem('memoh:harmony:service-config:v1',JSON.stringify({version:1,apiOrigin:'https://alpha.example/api',hapDownloadUrl:'',resetPending:false}))")
        context.add_init_script("window.begoniaNative={request:async raw=>JSON.stringify({ok:true,data:JSON.parse(raw).command==='upload.list'?[]:undefined})}")
        page.reload(wait_until='domcontentloaded');fixture.login(page)
        page.wait_for_function("document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.has('workspace-tabs')")
        def open_parent():
            page.evaluate("id=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('workspace-tabs').openSessionChat({sessionId:id,title:'Synthetic parent'})",session)
            page.get_by_role('button',name='任务',exact=True).click()
            view=page.get_by_role('region',name='本会话最近一次 Bot 运行',exact=True)
            view.get_by_text(run_id,exact=False).wait_for()
            view.get_by_role('button',name='查看运行内容与工具',exact=True).click()
            return view
        view=open_parent();button=view.get_by_role('button',name='继续原生批次',exact=True);button.wait_for()
        assert len(posts)==0
        assert operation in view.inner_text() and target in view.inner_text()
        button.scroll_into_view_if_needed()
        box=button.bounding_box();assert box and 0<=box['y'] and box['y']+box['height']<=844
        page.screenshot(path=str(OUT/'native-resume-action-visible-390.png'))
        button.click();view.get_by_text('继续请求的结果未知，此请求不会重复发送。请查看此任务的当前状态和原生终端。',exact=True).wait_for()
        assert len(posts)==1 and view.get_by_role('button',name='继续原生批次',exact=True).count()==0
        page.screenshot(path=str(OUT/'native-resume-unknown-390.png'))
        page.keyboard.press('Escape');page.reload(wait_until='domcontentloaded')
        page.get_by_role('button',name='任务',exact=True).wait_for();view=open_parent()
        view.get_by_text('继续请求的结果未知，此请求不会重复发送。请查看此任务的当前状态和原生终端。',exact=True).wait_for()
        assert len(posts)==1 and fixture.counts['login']==1 and not fixture.page_errors
        assert all(f['type'] in ('runtime_subscribe','runtime_unsubscribe') for f in frames)
        page.screenshot(path=str(OUT/'native-resume-reload-390.png'))
        report={'sharedCommit':'52609f7db4e59d7e528c2c5f8aa0330aa3ebbbe3','scope':'Actual generated Harmony Vite page, synthetic API/runtime/native boundaries','viewport':'390x844','openPosts':0,'explicitPosts':len(posts),'originalIdTargetBodyPreserved':True,'unknownReloadNoRetry':True,'reloadKeepsLogin':True,'noModelDispatch':True,'pageErrors':fixture.page_errors,'nativeDevice':False}
        (OUT/'native-resume-ui.json').write_text(json.dumps(report,indent=2)+'\n')
        context.close()
finally: fixture.server.shutdown()
