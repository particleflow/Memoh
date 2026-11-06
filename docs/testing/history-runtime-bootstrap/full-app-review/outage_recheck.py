import asyncio
import hashlib
import json
import os
import subprocess
import time
import urllib.parse
import urllib.request

from playwright.async_api import async_playwright
from browser_recheck import ROOT, REPO, BASE, FILES, FIXTURE, TOKEN, USER, scrub


async def start_server(phase):
    env = dict(os.environ, CONFIG_PATH=str(ROOT / 'config.toml'), MEMOH_CONFIG_PATH=str(ROOT / 'config.toml'))
    log = (ROOT / (phase + '-server.log')).open('ab')
    proc = await asyncio.create_subprocess_exec(str(ROOT / 'bin/memoh-server-cef'), 'serve', cwd=REPO, env=env, stdout=log, stderr=log)
    (ROOT / (phase + '-server-start.json')).write_text(json.dumps({'pid': proc.pid, 'argv': [str(ROOT / 'bin/memoh-server-cef'), 'serve'], 'direct_child_handle_retained': True}) + '\n')
    http = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    for _ in range(100):
        try:
            with http.open(urllib.request.Request('http://127.0.0.1:28480/health', method='HEAD'), timeout=.3) as response:
                if response.status == 200:
                    return proc, log
        except OSError:
            pass
        if proc.returncode is not None:
            raise RuntimeError('owned fixture Server exited at startup')
        await asyncio.sleep(.1)
    raise RuntimeError('owned fixture Server readiness not obtained')


async def probe(page, session):
    return await page.evaluate('''texts => ({visible:texts.map(t=>document.body.innerText.includes(t)),
      rows:document.querySelectorAll('[data-chat-content]').length,
      skeletons:document.querySelectorAll('.animate-pulse').length,path:location.pathname+location.search})''', [m['text'] for m in session['messages']])


async def phase(browser, name, warm=False):
    a, b = FIXTURE['bots']; first, target = a['sessions']
    context = await browser.new_context(viewport={'width': 1200, 'height': 850})
    await context.add_init_script("localStorage.setItem('token', %s);localStorage.setItem('user',JSON.stringify({userInfo:%s}));localStorage.setItem('chat-bot-id',%s);" % (json.dumps(TOKEN), json.dumps(USER), json.dumps(a['id'])))
    page = await context.new_page(); start = time.monotonic()
    record = {'phase': name, 'failed_bot_reads': [], 'http': [], 'sockets': [], 'frames': [], 'errors': [], 'writes': []}
    target_rest = asyncio.Event(); catalog = asyncio.Event(); tasks = []
    now = lambda: round((time.monotonic()-start)*1000,2)

    async def response(r):
        url = urllib.parse.urlsplit(r.url)
        if not url.path.startswith('/api/'): return
        item = {'path':url.path,'query':urllib.parse.parse_qs(url.query),'status':r.status,'at_ms':now()}
        record['http'].append(item)
        if url.path == '/api/bots' and r.status >= 500:
            record['failed_bot_reads'].append(item['at_ms'])
        if url.path.endswith('/messages') and r.status == 200:
            body=await r.json(); item['items']=len(body.get('items',[]))
            if item['query'].get('session_id') == [target['id']]:
                assert item['items']==2
                target_rest.set()
        if url.path == '/api/bots/'+a['id']+'/sessions' and r.status == 200:
            catalog.set()

    page.on('response',lambda r:tasks.append(asyncio.create_task(response(r))))
    page.on('pageerror',lambda e:record['errors'].append(scrub(e)))
    page.on('request',lambda r:record['writes'].append({'method':r.method,'path':urllib.parse.urlsplit(r.url).path}) if r.method not in ('GET','HEAD','OPTIONS') else None)

    def websocket(ws):
        path=urllib.parse.urlsplit(ws.url).path
        if not path.endswith('/web/ws'): return
        item={'path':path,'opened_ms':now(),'closed_ms':None};record['sockets'].append(item)
        ws.on('close',lambda: item.update(closed_ms=now()))
        def frame(raw,direction):
            try:
                value=json.loads(raw)
                record['frames'].append({'direction':direction,'type':value.get('type'),'session_id':value.get('session_id'),'bot_id':value.get('snapshot',{}).get('bot_id'),'at_ms':now()})
            except (ValueError,TypeError): pass
        ws.on('framesent',lambda raw:frame(raw,'sent'));ws.on('framereceived',lambda raw:frame(raw,'received'))
    page.on('websocket',websocket)
    proc=None;log=None
    try:
        await page.goto(BASE+'/bot/'+a['name']+'?session='+first['id'],wait_until='domcontentloaded')
        # Real backend absence exercises the existing initializer's backoff.
        # No successful REST response, WS frame or application flag is mocked.
        for _ in range(180):
            failed=record['failed_bot_reads']
            if len(failed)>1 and failed[-1]-failed[-2]>=3000: break
            await page.wait_for_timeout(200)
        else: raise RuntimeError('did not observe a real initialization backoff gap')
        record['before_restore']=await probe(page,first)
        proc,log=await start_server(name);record['server_ready_ms']=now()
        target_rest.clear();catalog.clear()
        await page.evaluate("url => document.querySelector('#app').__vue_app__.config.globalProperties.$router.push(url)",'/bot/'+a['name']+'?session='+target['id'])
        await asyncio.wait_for(target_rest.wait(),10)
        await page.wait_for_timeout(1500)
        record['probe']=await probe(page,target)
        record['probe_ms']=now();await page.screenshot(path=str(ROOT/(name+'-probe.png')),full_page=True)
        for _ in range(80):
            if all((await probe(page,target))['visible']) and catalog.is_set(): break
            await page.wait_for_timeout(200)
        record['eventual']=await probe(page,target)
        if warm:
            await page.wait_for_timeout(500)
            count=len(record['sockets'])
            await page.evaluate("url => document.querySelector('#app').__vue_app__.config.globalProperties.$router.push(url)",'/bot/'+a['name']+'?session='+first['id'])
            for _ in range(50):
                if all((await probe(page,first))['visible']):break
                await page.wait_for_timeout(100)
            record['same_bot_warm']={'socket_count_before':count,'socket_count_after':len(record['sockets']),'view':await probe(page,first)}
            await page.evaluate("url => document.querySelector('#app').__vue_app__.config.globalProperties.$router.push(url)",'/bot/'+b['name']+'?session='+b['sessions'][0]['id'])
            for _ in range(60):
                if all((await probe(page,b['sessions'][0]))['visible']):break
                await page.wait_for_timeout(100)
            record['other_bot']={'socket_count':len(record['sockets']),'view':await probe(page,b['sessions'][0])}
        if tasks: await asyncio.gather(*tasks)
        record['inference_frames']=[f for f in record['frames'] if f['direction']=='sent' and f['type'] in ['message','retry_message','edit_message']]
        assert not record['inference_frames']
        (ROOT/(name+'.json')).write_text(json.dumps(record,indent=2)+'\n')
        print(json.dumps({'phase':name,'failed_initializations_observed':len(record['failed_bot_reads']),'probe':record['probe'],'eventual':record['eventual'],'same_bot_warm':record.get('same_bot_warm'),'other_bot':record.get('other_bot'),'errors':record['errors']},ensure_ascii=False),flush=True)
        return record
    except Exception as error:
        record['exception'] = {'type': type(error).__name__, 'message': scrub(error)}
        record['failed_view'] = await probe(page, target)
        if tasks:
            await asyncio.gather(*tasks, return_exceptions=True)
        (ROOT / (name + '-incomplete.json')).write_text(json.dumps(record, indent=2) + '\n')
        await page.screenshot(path=str(ROOT / (name + '-incomplete.png')), full_page=True)
        raise
    finally:
        await context.close()
        if proc is not None:
            proc.terminate();code=await asyncio.wait_for(proc.wait(),15)
            (ROOT/(name+'-server-stop.json')).write_text(json.dumps({'pid':proc.pid,'actual_wait_exit':code,'direct_child_reaped':True})+'\n')
            log.close()


async def main():
    saved={p:(REPO/p).read_bytes() for p in FILES};results={}
    try:
        async with async_playwright() as play:
            browser=await play.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox'])
            for p in FILES:(REPO/p).write_bytes(subprocess.check_output(['git','show','cefdf0cbb3207d2a07db17ef365e1b8d2d43ca3f:'+p],cwd=REPO))
            results['before']=await phase(browser,'before-recovery-gap-mounted')
            for p,data in saved.items():(REPO/p).write_bytes(data)
            results['after']=await phase(browser,'after-recovery-gap-mounted',warm=True)
            await browser.close()
    finally:
        for p,data in saved.items():
            (REPO/p).write_bytes(data)
            assert hashlib.sha256((REPO/p).read_bytes()).digest()==hashlib.sha256(data).digest()
        (ROOT/'outage-outcome.json').write_text(json.dumps(results,indent=2)+'\n')


if __name__ == '__main__':
    asyncio.run(main())
