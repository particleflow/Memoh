"""Real client/Vite UI with isolated synthetic HTTP; not native/production acceptance."""
import base64
import os
from urllib.parse import urlsplit, parse_qs
import hashlib
import json
import re
import sys
import tempfile
from pathlib import Path
from playwright.sync_api import sync_playwright

ROOT = Path(__file__).resolve().parents[3]
OUT = Path(__file__).parent
sys.path.insert(0, str(ROOT / 'mobile/client/tests/stage2'))
import harmony_auth_persistence as f
f.server.shutdown()
f.base = os.environ.get('HARMONY_TEST_BASE', 'http://127.0.0.1:4181')
assert urlsplit(f.base).hostname in ('127.0.0.1', 'localhost')
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
operation = '99999999-9999-4999-8999-999999999999'
computer = {**task, 'task_type': 'computer_operation', 'task_id': operation, 'status': 'finishing'}
image_bytes = base64.b64decode('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a3ioAAAAASUVORK5CYII=')
image_hash = hashlib.sha256(image_bytes).hexdigest()
text_bytes = b'Synthetic committed task file.\n'
text_hash = hashlib.sha256(text_bytes).hexdigest()
media_reads = []
mode = 'ok'
observation_mode = 'online'
revision = '9007199254740993123'
pc_a = 'aaaaaaaa-1111-4111-8111-111111111111'
pc_b = 'bbbbbbbb-2222-4222-8222-222222222222'
mount_a = 'cccccccc-3333-4333-8333-333333333333'
mount_b = 'dddddddd-4444-4444-8444-444444444444'
runtimes = [{'id':pc_a,'name':'Synthetic Windows A','provider':'memoh','online':True,'os':'windows','capabilities':[]},
            {'id':pc_b,'name':'Synthetic Windows B','provider':'memoh','online':False,'os':'windows','capabilities':[]}]
def targets(bid):
    rows=[{'target_id':'native','kind':'native','name':'Server','primary':True,'online':True,'status':'online'}]
    if bid == f.bot_id: rows += [
        {'target_id':mount_a,'runtime_id':pc_a,'kind':'remote','provider':'memoh','name':'Synthetic Windows A','online':True,'status':'online'},
        {'target_id':mount_b,'runtime_id':pc_b,'kind':'remote','provider':'memoh','name':'Synthetic Windows B','online':False,'status':'offline'}]
    return rows

observed = []
native_mode = 'interactive'
followup_writes = []
receipt = None
provider_writes = []
provider_id = 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'
provider = {'id': provider_id, 'name': 'Synthetic Provider', 'client_type': 'openai-completions', 'enable': True,
 'config': {'base_url': 'https://provider.example.invalid/v1', 'api_key': 'synthetic********', 'images_api': {'protocol': 'previous-protocol', 'edits': True}}}


def api(path, request):
    global mode
    if path == '/users/me/runtimes': return 200, runtimes
    if path == '/users/me/computer-access': return 200, {'grants':[{'bot_id':f.bot_id,'target_id':mount_a,'runtime_id':pc_a}]}
    if path.endswith('/workspace-targets'):
        return 200, {'targets':targets(path.split('/')[2])}
    if path == '/providers': return 200, [provider]
    if path == '/provider-templates' or path == '/models': return 200, []
    if path == '/providers/' + provider_id:
        if request.method == 'PUT':
            payload = request.post_data_json
            assert 'api_key' not in payload.get('config', {})
            provider_writes.append(payload)
            provider.update({k:v for k,v in payload.items() if k != 'config'})
            provider['config'].update(payload.get('config', {}))
        return 200, provider
    if path == f'/bots/{f.bot_id}/sessions': return 200, {'items': [parent_row]}
    if path == f'/bots/{f.bot_id}/sessions/{parent}': return 200, parent_row
    if '/delegations' in path or '/tasks' in path:
        observed.append({'path': path, 'method': request.method, 'mode': mode})
        if path.endswith('/followup'):
            assert path == f'/bots/{target_bot}/sessions/{target_session}/tasks/computer_operation/{operation}/followup'
            assert request.method == 'POST'
            followup_writes.append(request.post_data_json)
            return 200, {'operation_id': operation, 'state': 'unknown'}
        assert request.method == 'GET', 'Observation must not dispatch or retry delivery'
        if path.endswith('/observation'):
            return 200, {'operation_id': operation, 'target_id': mount_a, 'workspace_id': 'synthetic-workspace', 'pane_id': 'synthetic-pane',
              'process_state':'exited','pi_result':'succeeded','result_reason':'verified_persisted_result','business_outcome':'unknown',
              'native': {'availability':observation_mode,'agent_status':'working','reference_match':'observed'},
              'reconciliation': {'revision':revision,'receipt_state':'validated','generation':'synthetic-generation','files':[
                {'meta':{'name':'partial-result','size':128},'state':'recorded','confirmed_offset':64,'current_availability':'not_rechecked','recorded_content_hash':'a'*64}]},
              'continuity':{'status':'unconfirmed','reason':'history_not_rechecked'}}
        if path.endswith('/content'):
            if native_mode == '404': return 404, {'status':404, 'code':'http.not_found'}
            if native_mode == 'batch': return 200, {'operation_id':operation, 'supported':False,'followup_available':False}
            return 200, {'operation_id':operation,'supported':True,'followup_available':True,
              'observation':{'output':'Synthetic terminal <script>not HTML</script>', 'output_fidelity':'terminal_snapshot_unverified','task_success_verified':False,'truncated':True,'completion_seq':'4'},
              'followups': [receipt] if receipt else []}

        if '/delegations' in path:
            if mode in ('503', '403'): return int(mode), {'status': int(mode), 'code': 'http.forbidden' if mode == '403' else 'internal.error'}
            if mode == 'offline': return 503, {'status': 503, 'code': 'internal.error'}
            return 200, {'items': [handoff]} if path.endswith('/delegations') else handoff
        if path == f'/bots/{target_bot}/sessions/{target_session}/tasks': return 200, {'items': [task, computer]}
        if path == f'/bots/{target_bot}/sessions/{target_session}/tasks/subagent/{child}': return 200, task
        if path == f'/bots/{target_bot}/sessions/{target_session}/tasks/computer_operation/{operation}': return 200, computer
        if path == f'/bots/{target_bot}/sessions/{target_session}/tasks/computer_operation/{operation}/deliveries':
            return 200, {'version': 1, 'operation_id': operation, 'revision': '3', 'has_more': False, 'files': [
                {'delivery_id': 'result.txt', 'state': 'committed', 'reference': {'version': 1, 'direction': 'upload', 'content_hash': text_hash, 'size_bytes': len(text_bytes), 'mime': 'text/plain'}},
                {'delivery_id': 'picture.png', 'state': 'committed', 'reference': {'version': 1, 'direction': 'upload', 'content_hash': image_hash, 'size_bytes': len(image_bytes), 'mime': 'image/png'}},
                {'delivery_id': 'pending-result', 'state': 'pending'},
            ]}
        if path.endswith('/tasks'): return 200, {'items': []}
        raise AssertionError('Unexpected task scope')
    if path.endswith('/messages'): return 200, {'items': []}
    return None

f.extra_api_handler = api

def intercept(route):
    request = route.request
    path = urlsplit(request.url).path
    if not path.startswith('/api/bots/') or '/media/' not in path: return f.intercept(route)
    if request.method == 'OPTIONS':
        route.fulfill(status=204, headers={'Access-Control-Allow-Origin': f.base, 'Access-Control-Allow-Headers': 'Authorization, Content-Type'})
        return
    assert f.valid_token(request.headers.get('authorization', '').removeprefix('Bearer ') or parse_qs(urlsplit(request.url).query).get('token', [''])[0])
    assert path.startswith(f'/api/bots/{target_bot}/media/')
    content_hash = path.rsplit('/', 1)[1]
    media_reads.append(content_hash)
    assert content_hash in (image_hash, text_hash)
    body = image_bytes if content_hash == image_hash else text_bytes
    route.fulfill(status=200, headers={'Access-Control-Allow-Origin': f.base}, content_type='image/png' if content_hash == image_hash else 'text/plain', body=body)


def start(pw, profile):
    context = pw.chromium.launch_persistent_context(str(profile), executable_path='/usr/bin/chromium', headless=True,
        viewport={'width': 390, 'height': 844}, locale='zh-CN', service_workers='block',
        args=['--proxy-server=http://127.0.0.1:9', '--proxy-bypass-list=127.0.0.1;localhost'])
    for page in context.pages: page.close()
    context.route('**/*', intercept)
    context.route_web_socket('**/*', runtime_socket)
    context.add_init_script("localStorage.setItem('locale','zh')")
    page = context.new_page()
    page.on('pageerror', lambda error: (OUT/'page-error-stack.txt').open('a').write(str(error.stack)+'\n'))
    page.on('pageerror', lambda error: f.page_errors.append({'message': re.sub(r'https?://\S+|eyJ[A-Za-z0-9._-]+', '[redacted]', str(error))[:300]}))
    page.goto(f.base + '/#/', wait_until='domcontentloaded')
    return context, page


root_run = 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb'
handoff.update(target_run_id=root_run, target_run_state='completed', target_run_created_at='2026-10-24T09:59:00Z', target_run_updated_at='2026-10-24T10:01:00Z')
target_row={'id':target_session,'bot_id':target_bot,'type':'chat','title':'Synthetic expert history','metadata':{}}
target_bot_row={**f.bot,'id':target_bot,'name':'Synthetic Expert'}
original_api=api
history_reads=[]
mutations=[]
unowned_history_reads=[]
def extended_api(path,request):
    if request.method not in ('GET','HEAD','OPTIONS'):
        mutations.append({'method':request.method,'path':path})
    if path == '/bots': return 200, {'items':[f.bot,target_bot_row]}
    if path == '/bots/' + target_bot: return 200,target_bot_row
    if path == f'/bots/{target_bot}/sessions': return 200,{'items':[target_row]}
    if path == f'/bots/{target_bot}/sessions/{target_session}':
        history_reads.append('session-authorized')
        return 200,target_row
    if path == f'/bots/{target_bot}/messages':
        selection=parse_qs(urlsplit(request.url).query).get('session_id')
        if not selection: return 200, {'items':[]}
        if selection != [target_session]:
            unowned_history_reads.append(selection)
            return 404, {'status':404,'code':'session.not_found'}
        history_reads.append('history-read')
        return 200,{'items':[{'id':'synthetic-history','turn_id':'synthetic-root-turn','timestamp':'2026-10-24T10:01:00Z','role':'assistant','messages':[{'id':1,'type':'text','content':'Synthetic expert root history; delivery remains pending.'}]}]}
    if re.fullmatch(r'/bots/[0-9a-f-]{36}/sessions/[0-9a-f-]{36}',path) and path != f'/bots/{f.bot_id}/sessions/{parent}':
        return 404, {'status':404,'code':'session.not_found'}
    return original_api(path,request)
f.extra_api_handler=extended_api

runtime_subscriptions=[]
def runtime_socket(socket):
    def receive(raw):
        data=json.loads(raw)
        assert data.get('type') not in ('message','followup'), 'No execution in observation test'
        if data.get('type')!='runtime_subscribe': return
        sid=data['session_id']
        bot_match=re.search(r'/bots/([0-9a-f-]{36})/',socket.url)
        assert bot_match
        bid=bot_match.group(1)
        if (bid,sid) not in [(f.bot_id,parent),(target_bot,target_session)]: return
        runtime_subscriptions.append({'bot':bid,'session':sid})
        socket.send(json.dumps({'type':'runtime_snapshot','session_id':sid,'epoch':'synthetic-epoch','seq':1,'snapshot':{'bot_id':bid,'session_id':sid,'epoch':'synthetic-epoch','seq':1,'updated_at':'2026-10-24T10:01:00Z'}}))
    socket.on_message(receive)

with tempfile.TemporaryDirectory(prefix='specialist-run-',dir=OUT) as directory, sync_playwright() as pw:
    context,page=start(pw,Path(directory)/'profile')
    page.locator('#service-api').fill('https://alpha.example/api')
    page.get_by_role('button',name='保存并继续',exact=True).click()
    f.login(page)
    fingerprint=f.fingerprint(page)
    page.wait_for_function("document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.has('workspace-tabs')")
    page.evaluate("id=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('workspace-tabs').openSessionChat({sessionId:id,title:'Synthetic parent'})",parent)
    page.get_by_role('button',name='电脑',exact=True).last.click()
    menu=page.get_by_role('menu')
    menu.get_by_role('menuitem',name=re.compile('Synthetic Windows A')).wait_for()
    assert menu.get_by_role('menuitem',name=re.compile('Synthetic Windows B')).get_attribute('data-disabled') is not None
    menu.get_by_role('menuitem',name=re.compile('Synthetic Windows A')).click()
    page.get_by_role('button',name='电脑',exact=True).last.click()
    menu.get_by_role('menuitem',name=re.compile('Synthetic Windows A')).wait_for()
    assert menu.get_by_role('menuitem',name=re.compile('Synthetic Windows A')).locator('svg.lucide-check').count()==1
    page.screenshot(path=str(OUT/'native-runtime-selection-390.png'))
    page.keyboard.press('Escape')
    page.get_by_role('button',name='任务',exact=True).click()
    dialog=page.get_by_role('dialog')
    dialog.get_by_role('tab',name='专业 Bot 委派',exact=True).click()
    dialog.get_by_role('button',name=re.compile('目标 Bot')).click()
    page.wait_for_timeout(600)
    (OUT/'root-dialog-debug.txt').write_text(dialog.inner_text())
    page.screenshot(path=str(OUT/'root-debug.png'))
    dialog.get_by_text(root_run,exact=False).wait_for()
    assert '待投递' in dialog.inner_text() and '持久化状态' in dialog.inner_text()
    assert dialog.get_by_text(run,exact=False).count()==0
    page.screenshot(path=str(OUT/'expert-root-run-390.png'))
    dialog.get_by_role('button',name='查看下游任务',exact=True).click()
    dialog.get_by_text('子 Agent 与电脑任务',exact=True).wait_for()
    dialog.get_by_text(root_run,exact=False).wait_for()
    dialog.get_by_role('button',name=re.compile('子 Agent')).click()
    dialog.get_by_text('合成子任务可见回复：正在处理',exact=True).wait_for()
    assert '子 Agent · 77777777' in dialog.inner_text()
    assert dialog.get_by_text(root_run,exact=False).count()==0
    page.screenshot(path=str(OUT/'child-run-distinct-390.png'))
    dialog.get_by_role('button',name='返回',exact=True).click()
    dialog.get_by_role('button',name=re.compile('电脑任务')).click()
    dialog.get_by_text('pending-result',exact=True).wait_for()
    assert '等待交付' in dialog.inner_text()
    assert not [x for x in observed if x['path'].endswith('/observation')]
    observation=dialog.get_by_role('region',name='当前电脑观察',exact=True)
    observation.get_by_role('button',name='刷新电脑观察',exact=True).click()
    observation.get_by_text(revision,exact=True).wait_for()
    observation.get_by_text('64 / 128 字节',exact=True).wait_for()
    assert observation.get_by_role('region',name='收集检查点',exact=True).locator('a,button,img').count()==0
    observation.get_by_text('64 / 128 字节',exact=True).scroll_into_view_if_needed()
    page.screenshot(path=str(OUT/'collection-checkpoints-390.png'))
    observation_mode='authentication_unavailable'
    observation.get_by_role('button',name='刷新电脑观察',exact=True).click()
    observation.get_by_text('自动检查已暂停。',exact=False).wait_for()
    paused_reads=len([x for x in observed if x['path'].endswith('/observation')])
    page.wait_for_timeout(5300)
    assert len([x for x in observed if x['path'].endswith('/observation')])==paused_reads
    observation_mode='online'
    observation.get_by_role('button',name='刷新电脑观察',exact=True).click()
    observation.get_by_text('电脑连接可用',exact=True).wait_for()

    page.evaluate("""() => {
      window.nativeTaskFixture=[];
      window.begoniaNative={request:async raw=>{
        const args=JSON.parse(raw);
        if(args.command==='download.fetch'){
          if(!args.authorization?.startsWith('Bearer ')||!args.url.includes('/bots/55555555-5555-4555-8555-555555555555/media/'))throw Error('wrong scope');
          window.nativeTaskFixture.push({command:args.command,botId:args.botId,hash:args.hash,size:args.size});
          return JSON.stringify({ok:true,data:{operation:args.operation,hash:args.hash,size:args.size}});
        }
        if(args.command!=='download.save'&&args.command!=='download.abort')throw Error('unexpected native command');
        window.nativeTaskFixture.push({command:args.command});
        return JSON.stringify({ok:true,data:true});
      }};
    }""")
    dialog.get_by_role('button',name='下载',exact=True).first.click()
    page.wait_for_function("window.nativeTaskFixture.some(x=>x.command==='download.save')")
    native_calls=page.evaluate('window.nativeTaskFixture')
    assert native_calls[0]=={'command':'download.fetch','botId':target_bot,'hash':text_hash,'size':len(text_bytes)}
    picture=dialog.locator('img').first
    picture.wait_for(); picture.click()
    preview=dialog.locator('[data-keyboard-scope="mediaLightbox"] img')
    preview.wait_for()
    assert preview.get_attribute('src').startswith('blob:')
    page.screenshot(path=str(OUT/'attachment-preview-390.png'))
    page.keyboard.press('Escape')
    dialog.get_by_text('pending-result',exact=True).wait_for()
    dialog.get_by_role('button',name='返回',exact=True).click()
    dialog.get_by_text(root_run,exact=False).wait_for()
    dialog.get_by_role('button',name='打开专家会话记录',exact=True).click()
    page.wait_for_timeout(2500)
    (OUT/'history-debug.json').write_text(json.dumps({'url':page.url,'body':page.locator('body').inner_text(),'historyReads':history_reads,'unowned':unowned_history_reads,'api':f.api_events,'errors':f.page_errors,'mutations':mutations},indent=2))
    page.screenshot(path=str(OUT/'history-debug.png'))
    page.get_by_text('Synthetic expert root history; delivery remains pending.',exact=True).wait_for()
    assert page.evaluate("document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('chat').currentBotId") == target_bot
    assert history_reads.index('session-authorized') < history_reads.index('history-read')
    assert dialog.count()==0
    assert f.fingerprint(page)==fingerprint and f.counts['login']==1
    page.get_by_role('button',name='电脑',exact=True).last.click()
    menu=page.get_by_role('menu')
    menu.get_by_role('menuitem',name=re.compile('云端电脑')).wait_for()
    assert menu.get_by_text('Synthetic Windows A',exact=True).count()==0
    assert menu.get_by_text('Synthetic Windows B',exact=True).count()==0
    page.screenshot(path=str(OUT/'other-bot-isolated-390.png'))
    page.keyboard.press('Escape')
    page.evaluate("document.documentElement.classList.add('dark')")
    assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
    page.screenshot(path=str(OUT/'expert-history-390.png'))
    page.reload(wait_until='domcontentloaded')
    page.get_by_text('Synthetic expert root history; delivery remains pending.',exact=True).wait_for()
    assert f.fingerprint(page)==fingerprint and f.counts['login']==1
    assert not mutations and not followup_writes and not f.page_errors
    result={'sharedCommit':'fa77a073fe6380d297474a6053a1445201bcab83','viewport':[390,844],'nativeMemohRuntimeListedAndSelectable':True,'offlineNativeRuntimeDisabled':True,'otherBotCannotReuseUnboundPC':True,'noImplicitDeviceObservation':True,'exactCheckpointRevisionAndPartialOffset':True,'authenticationUnavailableStopsPolling':True,'explicitObservationRestoresSameTask':True,'rootRunDistinctFromChild':True,'persistedStateNotDeliveryCompletion':True,'historyAuthorizedBeforeNavigation':True,'actualTargetBotSessionHistoryVisible':True,'loginRetainedAfterNavigationAndReload':True,'nativeBridgeCalls':native_calls,'attachmentPreviewAndReturn':True,'observedMediaReads':media_reads,'runtimeSubscriptions':runtime_subscriptions,'unownedHistoryReadsDenied':unowned_history_reads,'dispatchOrContinuationWrites':len(mutations),'pageErrors':f.page_errors,'boundary':'Actual phone-adapted Vue UI with synthetic HTTP and native OS bridge; not device/NAS/signing acceptance.'}
    (OUT/'specialist-browser-result.json').write_text(json.dumps(result,indent=2)+'\n')
    print(json.dumps(result))
    context.close()
