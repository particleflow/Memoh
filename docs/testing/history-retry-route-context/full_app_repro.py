"""Owned full frontend with synthetic REST/WS ports; no Server/DB/model/field."""
import argparse, asyncio, json, os, signal, time, urllib.parse
from pathlib import Path
from playwright.async_api import async_playwright

ROOT = Path(__file__).resolve().parent
REPO = ROOT.parents[2]
BASE = 'http://127.0.0.1:49971'
BOT = '10000000-0000-4000-8000-000000000001'
HISTORY = '20000000-0000-4000-8000-000000000001'
CONTACT = '20000000-0000-4000-8000-000000000002'
ACTOR = '30000000-0000-4000-8000-000000000001'
STAMP = '2026-10-30T00:00:00Z'
B = dict(id=BOT, name='retry-history-fixture', type='personal', status='ready', agent_mode='chat', runtime_type='model', created_at=STAMP, updated_at=STAMP)
def session(sid):
    return dict(id=sid, bot_id=BOT, type='chat', session_mode='chat', runtime_type='model', title='Fixture history' if sid == HISTORY else 'Fixture contact', channel_type='local', visibility='user', created_by_user_id=ACTOR, route_id='40000000-0000-4000-8000-000000000001', created_at=STAMP, updated_at=STAMP)
MESSAGES = [dict(id='fixture-original-user', turn_id='fixture-original-turn', role='user', text='SYNTHETIC ORIGINAL INPUT', timestamp=STAMP, attachments=[]), dict(id='fixture-original-assistant', turn_id='fixture-original-turn', role='assistant', timestamp=STAMP, messages=[dict(id=1, type='text', content='SYNTHETIC ORIGINAL RESULT')])]

async def run(phase):
    output = ROOT / phase; output.mkdir(exist_ok=True)
    log = (output / 'vite.txt').open('w')
    env = dict(os.environ, MEMOH_WEB_PROXY_TARGET='http://127.0.0.1:9', MEMOH_CHANNEL_PROXY_TARGET='http://127.0.0.1:9')
    proc = await asyncio.create_subprocess_exec('node', str(REPO/'apps/web/node_modules/vite/bin/vite.js'), '--host', '127.0.0.1', '--port', '49971', '--strictPort', cwd=REPO/'apps/web', env=env, stdout=log, stderr=log)
    record = dict(phase=phase, fixture='synthetic HTTP/WS ports; complete unmodified frontend entry', http=[], frames=[], sockets=[], samples=[], errors=[], external_blocked=[], cleanup={})
    (output/'startup.json').write_text(json.dumps(dict(pid=proc.pid, argv=['node','vite','--port','49971'], direct_child_handle_retained=True, cwd=str(REPO/'apps/web'))))
    try:
        # Only the new owned Vite port is checked; never inspect existing PIDs.
        for _ in range(100):
            if proc.returncode is not None: raise RuntimeError('owned Vite exited; see log')
            try:
                reader, writer = await asyncio.open_connection('127.0.0.1',49971)
                writer.close(); await writer.wait_closed(); break
            except OSError: await asyncio.sleep(.1)
        else: raise RuntimeError('owned Vite readiness not obtained')
        async with async_playwright() as p:
            browser = await p.chromium.launch(executable_path='/usr/bin/chromium', headless=True)
            context = await browser.new_context(viewport=dict(width=1280,height=900))
            await context.add_init_script('localStorage.setItem("token","isolated-no-server-token");localStorage.setItem("chat-bot-id",'+json.dumps(BOT)+');')
            page = await context.new_page(); start = time.monotonic()
            history_seen = None; released = False; sse_done = asyncio.Event()
            def now(): return round((time.monotonic()-start)*1000,2)
            async def api(route):
                nonlocal history_seen, released
                req=route.request; u=urllib.parse.urlsplit(req.url); path=u.path
                if u.netloc != '127.0.0.1:49971':
                    record['external_blocked'].append(dict(path=path)); await route.abort(); return
                if not path.startswith('/api/'):
                    await route.continue_(); return
                data={}; status=200
                if path == '/api/bots':
                    if history_seen is None or time.monotonic()-history_seen < 1:
                        status=502; data=dict(message='synthetic catalog-only startup outage')
                    else: data=dict(items=[B]); released=True
                elif path == '/api/users/me': data=dict(id=ACTOR,username='fixture',display_name='Fixture',role='admin',metadata=dict(onboarding_completed=True))
                elif path.endswith('/sessions/events'):
                    record['http'].append(dict(path=path,method=req.method,status='held-synthetic-stream',at_ms=now()))
                    await sse_done.wait()
                    try: await route.fulfill(status=200,content_type='text/event-stream',body='')
                    except Exception: pass
                    return
                elif path == '/api/bots/'+BOT+'/sessions': data=dict(items=[session(HISTORY),session(CONTACT)], next_cursor='')
                elif path == '/api/bots/'+BOT+'/sessions/contact': data=session(CONTACT)
                elif path in ['/api/bots/'+BOT+'/sessions/'+HISTORY,'/api/bots/'+BOT+'/sessions/'+CONTACT]: data=session(path.rsplit('/',1)[1])
                elif path == '/api/bots/'+BOT+'/messages':
                    sid=urllib.parse.parse_qs(u.query).get('session_id',[''])[0]
                    data=dict(items=MESSAGES if sid==HISTORY else [])
                    if sid==HISTORY and history_seen is None: history_seen=time.monotonic()
                elif path in ['/api/bots/'+BOT,'/api/bots/'+B['name']]: data=B
                elif path in ['/api/models','/api/providers']: data=[]
                elif path.endswith('/settings'): data=dict(memory_enabled=False)
                elif path.endswith('/media/limits'): data=dict(max_file_bytes=1048576,max_files=5)
                else: data=dict(items=[])
                record['http'].append(dict(path=path,method=req.method,session=urllib.parse.parse_qs(u.query).get('session_id'),status=status,at_ms=now()))
                await route.fulfill(status=status,content_type='application/json',body=json.dumps(data))
            await page.route('**/*',api)
            async def socket(ws):
                u=urllib.parse.urlsplit(ws.url)
                if u.netloc == '127.0.0.1:49971' and u.path == '/':
                    ws.connect_to_server(); return
                if u.path != '/api/bots/'+BOT+'/web/ws':
                    record['external_blocked'].append(dict(path=u.path)); ws.close(); return
                num=len(record['sockets']); record['sockets'].append(dict(id=num,path=u.path,at_ms=now()))
                def message(raw):
                    value=json.loads(raw); sid=value.get('session_id')
                    record['frames'].append(dict(socket=num,direction='sent',type=value.get('type'),session_id=sid,at_ms=now()))
                    if value.get('type') == 'runtime_subscribe':
                        frame=dict(type='runtime_snapshot',session_id=sid,epoch='synthetic-epoch',seq=1,snapshot=dict(bot_id=BOT,session_id=sid,epoch='synthetic-epoch',seq=1,updated_at=STAMP))
                        ws.send(json.dumps(frame)); record['frames'].append(dict(socket=num,direction='received',type='runtime_snapshot',session_id=sid,at_ms=now()))
                    elif value.get('type') != 'runtime_unsubscribe': raise RuntimeError('Unexpected inference/mutation frame in isolated frontend')
                ws.on_message(message)
            await page.route_web_socket('**/*',socket)
            page.on('pageerror',lambda error:record['errors'].append(str(error)[:400]))
            await page.goto(BASE+'/bot/'+B['name']+'?session='+HISTORY,wait_until='domcontentloaded',timeout=60000)
            print('full app loaded; observing the later native initializer',flush=True)
            for _ in range(70):
                await asyncio.sleep(.2)
                sample=await page.evaluate('''async()=>{const {useChatStore}=await import('/src/store/chat-list.ts');const s=useChatStore();return {url:location.pathname+location.search,bot:s.currentBotId,session:s.sessionId,loading:s.loadingChats,rows:document.querySelectorAll('[data-message-id]').length,skeletons:document.querySelectorAll('[data-slot=skeleton]').length,originals:['fixture-original-user','fixture-original-assistant'].map(id=>!!document.querySelector('[data-message-id="'+id+'"]'))}}''')
                sample['at_ms']=now();record['samples'].append(sample)
                if released and now()>10000: break
            record['catalog_recovered']=released
            record['ownership_preserved']=bool(record['samples'] and record['samples'][-1]['session']==HISTORY)
            record['validation_boundary']='Complete real frontend and browser; synthetic HTTP/WS ports only, not backend/field validation'
            record['final']=record['samples'][-1] if record['samples'] else None
            record['inference_frames']=[f for f in record['frames'] if f['direction']=='sent' and f['type'] not in ['runtime_subscribe','runtime_unsubscribe']]
            await page.screenshot(path=str(output/'final.png'),full_page=True)
            sse_done.set(); await asyncio.sleep(.05)
            await context.close(); await browser.close();record['cleanup']['browser']='closed'
    finally:
        if proc.returncode is None:
            proc.send_signal(signal.SIGINT)
            try: await asyncio.wait_for(proc.wait(),8)
            except asyncio.TimeoutError: record['cleanup']['vite']='normal shutdown timeout; ownership receipt retained'
        record['cleanup']['vite_exit']=proc.returncode
        log.close(); (output/'OUTCOME.json').write_text(json.dumps(record,ensure_ascii=False,indent=2)+'\n')
    print(json.dumps(dict(phase=phase,final=record.get('final'),cleanup=record['cleanup']),ensure_ascii=False),flush=True)

def verify(record, phase):
    assert record['catalog_recovered'], 'late native initialize was not exercised'
    assert not record['errors'] and not record['inference_frames'], 'fixture had page errors or inference commands'
    assert all(r['path']=='/css2' for r in record['external_blocked']), 'new unexpected external attempt (all such traffic remains blocked)'
    assert record['final']['originals'] == [True, True], 'original frontend history did not render'
    expected = HISTORY if phase == 'green' else CONTACT
    assert record['final']['session'] == expected
    sent = [f for f in record['frames'] if f['direction']=='sent' and f['type']=='runtime_subscribe']
    assert sent[0]['session_id'] == HISTORY and sent[-1]['session_id'] == expected
    assert record['cleanup']['browser']=='closed' and record['cleanup']['vite_exit']==-2

if __name__=='__main__':
    args=argparse.ArgumentParser();args.add_argument('phase',choices=['red','green']);phase=args.parse_args().phase;asyncio.run(run(phase));verify(json.loads((ROOT/phase/'OUTCOME.json').read_text()),phase)
