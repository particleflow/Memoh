"""Free loopback Chromium + HTTP/WebSocket fixtures; never imports product runtime."""
import asyncio
import json
import tempfile
from pathlib import Path
from aiohttp import web
from playwright.async_api import async_playwright
from capture import Recorder

BOT, CONTACT = 'source-fixture', 'contact-fixture'
TOKEN = 'fixture-only-sensitive-token'
PREFIX = '/edge/bots/' + BOT + '/sessions/' + CONTACT

async def main():
    release, queue_seen, slow_seen, slow_release = [asyncio.Event() for _ in range(4)]
    state = {'worker': 'running', 'b_lookup': 0, 'posts': 0}
    async def index(_):
        return web.Response(content_type='text/html', text='''<script>
          window.ws=new WebSocket(location.origin.replace('http','ws')+'/edge/bots/source-fixture/web/ws?token=fixture-only-sensitive-token');
          window.get=()=>fetch('/edge/bots/source-fixture/sessions/contact-fixture',{headers:{Authorization:'Bearer fixture-only-sensitive-token'}});
          window.post=(id)=>fetch('/edge/bots/source-fixture/sessions/contact-fixture/follow-up-queue',{
            method:'POST',headers:{'Content-Type':'application/json',Authorization:'Bearer fixture-only-sensitive-token'},
            body:JSON.stringify({invocation_id:id,text:'same text cannot identify requests'})});
          get();</script>''')
    view = {'delegation_id': 'd-fixture', 'request_id': 'request-fixture', 'source_session_id': CONTACT,
            'target_bot_id': 'expert-fixture', 'target_session_id': 'expert-session', 'target_run_id': 'worker-exact'}
    async def socket(req):
        ws = web.WebSocketResponse(); await ws.prepare(req)
        async for msg in ws:
            x = json.loads(msg.data)
            await ws.send_json({'type':'run_accepted','invocation_id':x['invocation_id'],
                                'session_id':CONTACT,'run_id':'a-exact','turn_id':'a-turn'})
            # Tool frame deliberately lacks invocation_id, arrives before lookup finishes.
            await ws.send_json({'type':'session_runtime','run_id':'a-exact','session_id':CONTACT,
                'messages':[{'type':'tool','tool_call_id':'call-exact','name':'delegate_to_bot',
                             'output':{'structuredContent':view}}]})
        return ws
    async def queue(req):
        state['posts'] += 1
        body = await req.json()
        if body['invocation_id'] == 'lost-request':
            req.transport.close()
            return web.Response()
        if body['invocation_id'] != 'b-request':
            return web.json_response({'code':'http.conflict','request':body['invocation_id']},status=409)
        response = web.StreamResponse(status=202, headers={'Content-Type':'application/json'})
        await response.prepare(req)
        await response.write(b'{"item_id":"queue-exact",')
        queue_seen.set()
        await release.wait()
        await response.write(b'"status":"accepted","enqueued_during_run_id":"a-exact"}')
        await response.write_eof()
        return response
    async def invocation(req):
        inv = req.match_info['inv']
        if inv == 'a-request':
            return web.json_response({'found':True,'invocation_id':inv,'session_id':CONTACT,
                'run_id':'a-exact','turn_id':'a-turn','state':'completed'})
        if inv == 'follow-up:queue-exact':
            state['b_lookup'] += 1
            return web.json_response({'found':True,'invocation_id':inv,'session_id':CONTACT,
                'run_id':'b-exact','turn_id':'b-turn','state':'completed'})
        return web.json_response({'found':False,'invocation_id':inv,'session_id':CONTACT})
    async def messages(_):
        return web.json_response({'items':[
            {'turn_id':'old-turn','messages':[{'output':dict(view,delegation_id='old-not-A')}]},
            {'turn_id':'a-turn','role':'assistant','text':'Handed off'},
            {'turn_id':'b-turn','role':'assistant','text':'Substantive fixture answer',
             'metadata':{'token':TOKEN},'private_link':'https://private.invalid/secret'}]})
    async def delegation(_):
        return web.json_response(dict(view,target_run_state=state['worker']))
    async def slow(req):
        response=web.StreamResponse(headers={'Content-Type':'application/json'})
        await response.prepare(req); await response.write(b'{"saved":')
        slow_seen.set(); await slow_release.wait()
        await response.write(b'true}'); await response.write_eof()
        return response
    async def broken(req):
        req.transport.close()
        return web.Response()
    async def forbidden(_):
        return web.json_response({'code':'http.forbidden'},status=403)
    app=web.Application()
    app.router.add_get('/',index)
    app.router.add_get('/edge/bots/'+BOT+'/web/ws',socket)
    app.router.add_get(PREFIX,lambda _:web.json_response({'session_id':CONTACT}))
    app.router.add_post(PREFIX+'/follow-up-queue',queue)
    app.router.add_get(PREFIX+'/invocations/{inv}',invocation)
    app.router.add_get('/edge/bots/'+BOT+'/messages',messages)
    app.router.add_get(PREFIX+'/delegations/d-fixture',delegation)
    app.router.add_get(PREFIX+'/slow',slow)
    app.router.add_get(PREFIX+'/broken',broken)
    app.router.add_get(PREFIX+'/forbidden',forbidden)
    runner=web.AppRunner(app); await runner.setup()
    site=web.TCPSite(runner,'127.0.0.1',0); await site.start()
    port=site._server.sockets[0].getsockname()[1]
    base=f'http://127.0.0.1:{port}'
    async def wait_for(fn):
        async with asyncio.timeout(8):
            while not fn(): await asyncio.sleep(.01)
    with tempfile.TemporaryDirectory() as directory:
        path=Path(directory)/'capture.jsonl'
        async with async_playwright() as p:
            browser=await p.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox'])
            page=await browser.new_page()
            rec=Recorder(page,base+'/edge',BOT,CONTACT,path,timeout=2)
            try:
                await page.goto(base)
                await wait_for(lambda:rec.ws_seen and rec.auth_seen)
                rec.arm('A')
                await page.evaluate("ws.send(JSON.stringify({type:'message',session_id:'contact-fixture',invocation_id:'a-request',text:'A'}))")
                await wait_for(lambda:bool(rec.frames))
                await rec.sample()
                assert rec.roles['A']['run_id']=='a-exact'
                assert set(rec.delegations)=={'d-fixture'}
                rec.arm('B')
                await page.evaluate("void post('b-request')")
                await queue_seen.wait()
                # A later 409 completes before the original 202 full body. Must not steal B.
                await page.evaluate("post('other-request').then(r=>r.json())")
                assert rec.roles['B']['admitted_invocation'] is None
                release.set()
                await wait_for(lambda:rec.roles['B']['admitted_invocation'] is not None)
                assert rec.roles['B']['admitted_invocation']=='follow-up:queue-exact'
                assert rec.roles['B']['request_invocation']=='b-request'
                await rec.sample()
                rows=[json.loads(x) for x in path.read_text().splitlines()]
                candidate=[x for x in rows if x['kind']=='overlap_candidate'][-1]
                assert candidate['b_completed_seq']<candidate['persisted_history_seq']<candidate['subsequent_worker_seq']
                assert candidate['worker_run']=='worker-exact'
                view['target_run_id']='wrong-worker'
                await rec.sample()
                rows=[json.loads(x) for x in path.read_text().splitlines()]
                assert any(x['kind']=='worker_identity_mismatch' for x in rows)
                view['target_run_id']='worker-exact'
                state['worker']='completed'
                await rec.sample()
                rows=[json.loads(x) for x in path.read_text().splitlines()]
                assert len([x for x in rows if x['kind']=='overlap_candidate'])==1
                # Denial is retained and not automatically retried.
                await rec.get(rec.prefix+'/forbidden')
                before=rec.request_counter
                await rec.get(rec.prefix+'/forbidden')
                assert rec.request_counter==before
                await page.evaluate("fetch('"+PREFIX+"/broken').catch(()=>null)")
                # Stop while a response body is partial; drain must retain its final body.
                await page.evaluate("void fetch('"+PREFIX+"/slow')")
                await slow_seen.wait()
                closing=asyncio.create_task(rec.close())
                await asyncio.sleep(.05)
                assert not closing.done()
                slow_release.set(); await closing
                text=path.read_text(); rows=[json.loads(x) for x in text.splitlines()]
                assert any(x['kind']=='http_response' and x.get('body')=={'saved':True} for x in rows)
                assert any(x['kind']=='http_failed_unknown' for x in rows)
                assert TOKEN not in text and 'Authorization' not in text and '127.0.0.1' not in text and 'private.invalid' not in text
                assert any(x['kind']=='ws_received' and x['body'].get('run_id')=='a-exact' and 'invocation_id' not in x['body'] for x in rows)
                assert state['posts']==2  # Both explicitly fixture-driven; observer submitted none.
                # A real lost queue response preserves original intent but invents no item ID.
                lostpath=Path(directory)/'lost.jsonl'
                lost=Recorder(page,base+'/edge',BOT,CONTACT,lostpath,timeout=.3)
                await page.reload()
                await wait_for(lambda:lost.ws_seen and lost.auth_seen)
                lost.arm('B')
                await page.evaluate("post('lost-request').catch(()=>null)")
                await lost.close()
                lostrows=[json.loads(x) for x in lostpath.read_text().splitlines()]
                assert lost.roles['B']['request_invocation']=='lost-request'
                assert lost.roles['B']['admitted_invocation'] is None
                assert not any(x['kind']=='queue_mapping' for x in lostrows)
                assert any(x['kind']=='http_failed_unknown' for x in lostrows)
                assert state['posts'] >= 3  # Chromium may replay a disconnected transport; recorder never calls POST
                result={'status':'PASS','paid_calls':0,'production_requests':0,'observer_business_posts':0,
                  'fixture_browser':'Chromium','fixture_explicit_post_calls':3,'fixture_server_post_receipts':state['posts'],'cases':['pre-A WebSocket identity','tool frame without invocation retained',
                  'old unrelated turn excluded','delayed split 202 versus earlier unrelated 409','exact queue-to-run/turn mapping',
                  'persisted B then worker-running causal chain','changed worker identity rejected','worker completed produces no overlap',
                  '403 blocks affected GET','transport failure retained unknown','stop drains partial response',
                  'credential and address redaction','lost queue response remains unknown without resend'],'event_count':len(rows)}
                print(json.dumps(result,indent=2))
            finally:
                release.set();slow_release.set()
                if not rec.closed:await rec.close()
                await browser.close()
    await runner.cleanup()

if __name__=='__main__':asyncio.run(main())
