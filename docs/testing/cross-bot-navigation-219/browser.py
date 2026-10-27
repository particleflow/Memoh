import json,re,time,sys
from pathlib import Path
from urllib.parse import urlparse,parse_qs
from playwright.sync_api import sync_playwright

entry_name='任务' if len(sys.argv)>1 and sys.argv[1]=='tasks' else 'Bots'
out=Path('/tmp/unc-integration/navigation-219-task-proof' if entry_name=='任务' else '/tmp/unc-integration/navigation-219-proof');out.mkdir(exist_ok=True)
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
    path=urlparse(req.url).path.removeprefix('/api');calls.append(dict(method=req.method,path=path,query=urlparse(req.url).query))
    sid=path.split('/sessions/')[-1].split('/')[0] if '/sessions/' in path else parse_qs(urlparse(req.url).query).get('session_id',[''])[0]
    if sid in sessions and path.startswith('/bots/') and path.split('/')[2]!=sessions[sid]['bot_id']:
        return route.fulfill(status=404,json=dict(code='session.not_found'))
    if req.method=='POST' and path.endswith('/sessions/contact'):
        return route.fulfill(status=200,json=sessions[ROOT if path.split('/')[2]==BOT else ES])
    if req.method!='GET':return route.fulfill(status=403,json=dict(code='fixture.write_disabled'))
    data={}
    if path.endswith('/sessions/contact'): return route.fulfill(status=200,json=dict(session=sessions[ROOT],candidates=[]) if path.split('/')[2]==BOT else dict(candidates=[]))
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
    elif path.endswith('/messages'):data=dict(items=[dict(id='fixture-expert-turn',turn_id='fixture-expert-turn',role='assistant',timestamp=stamp,messages=[dict(id=1,type='text',content='原专家历史正文保持')])] if sid==ES else [])
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
    context=browser.new_context(viewport=dict(width=390,height=844))
    context.add_init_script("localStorage.setItem('token','synthetic-contact-account');localStorage.setItem('language','zh');")
    page=context.new_page();page.on('pageerror',lambda e:errors.append(str(e)))
    cdp=context.new_cdp_session(page);cdp.send('Network.enable');cdp.send('Debugger.enable');cdp.send('Debugger.setAsyncCallStackDepth',dict(maxDepth=32));traces=[]
    cdp.on('Network.requestWillBeSent',lambda e:traces.append(dict(url=e['request']['url'],initiator=e.get('initiator'))) if '/bots/'+EXPERT+'/sessions/'+ROOT in e['request']['url'] else None)
    page.route('**/api/**',handle);page.route_web_socket('**/*',websocket)
    page.goto('http://127.0.0.1:18407/bot/supervisor',wait_until='domcontentloaded',timeout=60000)
    page.get_by_role('button',name=entry_name,exact=True).wait_for(timeout=30000)
    page.get_by_role('button',name=entry_name,exact=True).click()
    dialog=page.get_by_role('dialog');dialog.get_by_text('图像专家',exact=True).wait_for(timeout=30000)
    start=len(calls);start_ws=len(ws_calls)
    dialog.get_by_role('button').filter(has_text='图像专家').click()
    page.wait_for_timeout(2000)
    assert 'session='+ES in page.url,page.url
    assert any(json.loads(raw).get('session_id')==ES for raw in ws_calls[start_ws:]),ws_calls[start_ws:]
    assert not any('/sessions/contact' in c['path'] for c in calls[start:]),calls[start:]
    assert not any(c['method']!='GET' for c in calls[start:]),calls[start:]
    bad=[c for c in calls[start:] if '/sessions/'+ROOT in c['path'] and '/bots/'+EXPERT+'/' in c['path']]
    (out/'all-calls.json').write_text(json.dumps(dict(calls=calls,errors=errors,bad=bad),ensure_ascii=False,indent=2))
    (out/'wrong-scope-traces.json').write_text(json.dumps(traces,ensure_ascii=False,indent=2))
    assert not bad,bad
    page.get_by_text('原专家历史正文保持',exact=True).wait_for(timeout=15000)
    page.reload(wait_until='domcontentloaded');page.get_by_text('原专家历史正文保持',exact=True).wait_for(timeout=15000)
    assert not errors,errors
    page.screenshot(path=str(out/'original-expert-narrow.png'),full_page=True)
    page.set_viewport_size(dict(width=1365,height=950));page.screenshot(path=str(out/'original-expert-desktop.png'),full_page=True)
    (out/'result.json').write_text(json.dumps(dict(calls=calls[start:],url=page.url,errors=errors),ensure_ascii=False,indent=2))
    print('PASS: relation click opens exact expert session without contact API or business writes; synthetic HTTP/WS')
    browser.close()
