import json,re,time
from pathlib import Path
from urllib.parse import urlparse
from playwright.sync_api import sync_playwright

out=Path('/tmp/unc-integration/contact-ui-proof');out.mkdir(exist_ok=True)
BOT='11111111-1111-4111-8111-111111111111';EXPERT='22222222-2222-4222-8222-222222222222';OTHER='33333333-3333-4333-8333-333333333333'
ROOT='aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa';A='bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb';B='cccccccc-cccc-4ccc-8ccc-cccccccccccc';ES='dddddddd-dddd-4ddd-8ddd-dddddddddddd'
stamp='2026-10-27T06:00:00Z'
bots=[dict(id=BOT,name='supervisor',display_name='主管 Bot',current_user_permissions=['chat','manage','workspace_exec'],is_active=True),dict(id=EXPERT,name='expert',display_name='图像专家',current_user_permissions=['chat','manage'],is_active=True),dict(id=OTHER,name='other',display_name='无关 Bot',current_user_permissions=['chat','manage'],is_active=True)]
sessions={ROOT:dict(id=ROOT,bot_id=BOT,title='合成会话',type='chat',runtime_type='model',created_at=stamp,updated_at=stamp),A:dict(id=A,bot_id=BOT,title='核对 Agent',type='subagent',runtime_type='model',parent_session_id=ROOT,created_at=stamp,updated_at=stamp),B:dict(id=B,bot_id=BOT,title='子 Agent',type='subagent',runtime_type='model',parent_session_id=A,created_at=stamp,updated_at=stamp),ES:dict(id=ES,bot_id=EXPERT,title='专家原会话',type='chat',runtime_type='model',created_at=stamp,updated_at=stamp)}
OLD='eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee'
sessions[OLD]=dict(id=OLD,bot_id=BOT,title='旧历史',type='chat',runtime_type='model',created_at=stamp,updated_at=stamp)
calls=[];errors=[];ws_calls=[]
def task(s,parent):return dict(task_id=s,task_type='subagent',session_id=s,parent_session_id=ROOT,direct_parent_session_id=parent,status='running',summary='DO_NOT_RENDER_BODY',content_available=False,attachments_available=False,created_at=stamp,updated_at=stamp)
def handle(route):
    req=route.request
    if not urlparse(req.url).path.startswith('/api/'): return route.continue_()
    path=urlparse(req.url).path.removeprefix('/api');calls.append(dict(method=req.method,path=path))
    if req.method=='POST' and path.endswith('/sessions/contact'):
        return route.fulfill(status=200,json=sessions[ROOT if path.split('/')[2]==BOT else ES])
    if req.method!='GET':return route.fulfill(status=403,json=dict(code='fixture.write_disabled'))
    data={}
    if path=='/users/me':data=dict(id='fixture-user',username='fixture',display_name='合成用户',role='admin',metadata=dict(onboarding_completed=True))
    elif path=='/bots':data=dict(items=bots)
    elif path=='/models':data=[dict(id='fixture-model',name='合成模型',model_id='fixture-model',type='chat',config={})]
    elif path in ['/providers','/search-providers','/users/me/runtimes']:data=[]
    elif path=='/acp/profiles':data=dict(items=[])
    elif path in ['/settings','/system/status','/search-providers']:data=dict(items=[])
    elif re.fullmatch('/bots/[^/]+',path):data=next((b for b in bots if b['id']==path.split('/')[2] or b['name']==path.split('/')[2]),bots[0])
    elif path.endswith('/sessions'):data=dict(items=[v for v in sessions.values() if v['bot_id']==path.split('/')[2] and v['type']=='chat' and v['id']!=ROOT])
    elif re.fullmatch('/bots/[^/]+/sessions/[^/]+',path):data=sessions.get(path.split('/')[-1],{})
    elif path.endswith('/tasks'):data=dict(items=[task(A,ROOT),task(B,A)] if ROOT in path else [])
    elif path.endswith('/delegations'):data=dict(items=[dict(delegation_id='linked-only',source_session_id=ROOT,target_bot_id=EXPERT,target_session_id=ES,target_run_state='waiting_decision',state='running',delivery_state='accepted',request_id='fixture-request',attachments=[],created_at=stamp,updated_at=stamp)] if ROOT in path else [])
    elif path.endswith('/messages'):data=dict(items=[])
    elif path.endswith('/settings'):data=dict(chat_runtime='model',chat_model_id='fixture-model',timezone='UTC')
    elif path.endswith('/model-catalog'):data=dict(models=[])
    elif path.endswith('/agents'):data=dict(items=[])
    elif path.endswith('/workspace-targets'):data=dict(items=[dict(id='native',name='云端',provider='memoh',kind='native',available=True)])
    elif path.endswith('/workspace'):data=dict(status='running',state='running',workspace_base='/data',workdir='/data')
    elif path.endswith('/queue'):data=dict(steer=[],follow_up=[],steer_items=[],follow_up_items=[])
    elif path.endswith('/events'):return route.fulfill(status=200,content_type='text/event-stream',body='event: ping\ndata: {}\n\n')
    elif any(path.endswith(s) for s in ['/workdirs','/models','/runtimes','/activity','/skills/catalog','/memory/providers']):data=dict(items=[])
    elif '/runtime/' in path or path.endswith('/runtime'):data=dict(current_run=None,head_seq=0)
    route.fulfill(status=200,content_type='application/json',body=json.dumps(data))

def websocket(ws):
    def receive(raw):
        ws_calls.append(raw)
        data=json.loads(raw)
        if data.get('type')=='runtime_subscribe':
            sid=data['session_id'];snapshot=dict(bot_id=sessions[sid]['bot_id'],session_id=sid,epoch='contact-fixture',seq=1,updated_at=stamp)
            ws.send(json.dumps(dict(type='runtime_snapshot',session_id=sid,epoch='contact-fixture',seq=1,snapshot=snapshot)))
    ws.on_message(receive)
with sync_playwright() as p:
    browser=p.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox'])
    def client():
        context=browser.new_context(viewport=dict(width=1280,height=900))
        context.add_init_script("localStorage.setItem('token','synthetic-contact-account');localStorage.setItem('language','zh');")
        page=context.new_page();page.on('pageerror',lambda error:errors.append(str(error)))
        page.route('**/api/**',handle)
        page.route_web_socket('**/*',websocket)
        page.goto('http://127.0.0.1:18091/chat/supervisor',wait_until='domcontentloaded',timeout=60000)
        try: page.get_by_text('合成会话',exact=True).first.wait_for(timeout=12000)
        except Exception:
            (out/'failure.json').write_text(json.dumps(dict(calls=calls,errors=errors,url=page.url,body=page.locator('body').inner_text()),ensure_ascii=False,indent=2))
            page.screenshot(path=str(out/'failure.png'),full_page=True)
            raise
        page.wait_for_timeout(1000)
        return context,page
    context,page=client()
    (out/'state.json').write_text(json.dumps(dict(calls=calls,ws=ws_calls,body=page.locator('body').inner_text(),selection=page.evaluate("Object.entries(sessionStorage).filter(([key])=>key.includes('chat-session') || key.includes('chat-explicit'))")),ensure_ascii=False,indent=2))
    page.screenshot(path=str(out/'contact-desktop.png'),full_page=True)
    assert any(c['method']=='POST' and c['path'].endswith('/sessions/contact') for c in calls)
    assert any(json.loads(raw).get('session_id')==ROOT for raw in ws_calls)
    page.screenshot(path=str(out/'contact-desktop.png'),full_page=True)
    start=len(calls); ws_start=len(ws_calls); other,second=client()
    assert any(json.loads(raw).get('session_id')==ROOT for raw in ws_calls[ws_start:])
    second.set_viewport_size(dict(width=390,height=844));second.screenshot(path=str(out/'contact-narrow.png'),full_page=True)
    # Explicit historical navigation stays distinct and is retained on reload.
    page.get_by_text('旧历史',exact=True).first.click();page.wait_for_timeout(700)
    start=len(calls);ws_start=len(ws_calls);page.reload(wait_until='domcontentloaded');page.wait_for_timeout(1000)
    assert any(json.loads(raw).get('session_id')==OLD for raw in ws_calls[ws_start:])
    assert not any(c['method']=='POST' and c['path'].endswith('/sessions/contact') for c in calls[start:])
    assert not any(c['method']!='GET' and not c['path'].endswith('/sessions/contact') for c in calls)
    assert not errors,errors
    (out/'proof.json').write_text(json.dumps(dict(api='synthetic intercepted API; no real PG/model/device',calls=calls,runtime_subscriptions=ws_calls,page_errors=errors),ensure_ascii=False,indent=2))
    print('PASS actual production Web: contact on entry, second client same ID, explicit old history retained, desktop/390px, no model/task/message writes, zero page errors')
    browser.close()
