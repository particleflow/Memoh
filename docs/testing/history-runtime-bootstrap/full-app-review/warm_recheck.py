import asyncio
import json
import urllib.parse

from playwright.async_api import async_playwright
from browser_recheck import ROOT, BASE, FIXTURE, TOKEN, USER, scrub
from outage_recheck import start_server, probe


async def main():
    proc, log = await start_server('warm-identity')
    record = {'sockets': [], 'http': [], 'errors': [], 'inference_frames': []}
    try:
        async with async_playwright() as play:
            browser = await play.chromium.launch(executable_path='/usr/bin/chromium', headless=True, args=['--no-sandbox'])
            context = await browser.new_context(viewport={'width': 1200, 'height': 850})
            a, b = FIXTURE['bots']; first, second = a['sessions']
            await context.add_init_script("localStorage.setItem('token',%s);localStorage.setItem('user',JSON.stringify({userInfo:%s}));localStorage.setItem('chat-bot-id',%s);" % (json.dumps(TOKEN), json.dumps(USER), json.dumps(a['id'])))
            page = await context.new_page()
            page.on('pageerror', lambda e: record['errors'].append(scrub(e)))
            page.on('response', lambda r: record['http'].append({'path': urllib.parse.urlsplit(r.url).path, 'status': r.status}) if urllib.parse.urlsplit(r.url).path.startswith('/api/') else None)
            def websocket(ws):
                path = urllib.parse.urlsplit(ws.url).path
                if not path.endswith('/web/ws'): return
                row = {'path': path, 'closed': False}; record['sockets'].append(row)
                ws.on('close', lambda: row.update(closed=True))
                def sent(raw):
                    try:
                        value = json.loads(raw)
                        if value.get('type') in ('message', 'retry_message', 'edit_message'):
                            record['inference_frames'].append(value['type'])
                    except (ValueError, TypeError): pass
                ws.on('framesent', sent)
            page.on('websocket', websocket)
            async def visible(session):
                for _ in range(100):
                    state = await probe(page, session)
                    if all(state['visible']): return state
                    await page.wait_for_timeout(100)
                raise AssertionError('actual application did not show the original pair')
            async def navigate(bot, session):
                await page.evaluate("url => document.querySelector('#app').__vue_app__.config.globalProperties.$router.push(url)", '/bot/' + bot['name'] + '?session=' + session['id'])
            await page.goto(BASE + '/bot/' + a['name'] + '?session=' + first['id'], wait_until='domcontentloaded')
            record['initial'] = await visible(first)
            record['router_identity'] = await page.evaluate("async () => { const app = document.querySelector('#app').__vue_app__.config.globalProperties.$router; const imported = (await import('/src/router.ts')).default; return {same:app===imported,app:app.currentRoute.value.fullPath,imported:imported.currentRoute.value.fullPath} }")
            count = len(record['sockets'])
            await navigate(a, second)
            record['same_bot'] = await visible(second)
            record['same_bot_socket_delta'] = len(record['sockets']) - count
            assert record['same_bot_socket_delta'] == 0
            count = len(record['sockets'])
            await navigate(b, b['sessions'][0])
            record['other_bot'] = await visible(b['sessions'][0])
            record['other_bot_socket_delta'] = len(record['sockets']) - count
            assert record['other_bot_socket_delta'] == 1
            assert not any(await page.evaluate('texts => texts.map(t=>document.body.innerText.includes(t))', [m['text'] for m in second['messages']]))
            await page.screenshot(path=str(ROOT / 'warm-cross-bot.png'), full_page=True)
            count = len(record['sockets'])
            await navigate(b, first)
            await page.wait_for_timeout(700)
            rejected = next((r for r in record['http'] if r['path'] == '/api/bots/' + b['id'] + '/sessions/' + first['id'] and r['status'] == 404), None)
            assert rejected is not None
            record['mismatched_session'] = {'status': 404, 'new_sockets': len(record['sockets']) - count}
            assert record['mismatched_session']['new_sockets'] == 0
            assert not record['inference_frames']
            record['passed'] = True
            await context.close(); await browser.close()
    except Exception as error:
        record['exception'] = {'type':type(error).__name__,'message':scrub(error)}
        if 'page' in locals():
            record['failed_view'] = await probe(page, second)
            record['mounted_route'] = await page.evaluate("() => document.querySelector('#app').__vue_app__.config.globalProperties.$router.currentRoute.value.fullPath")
        raise
    finally:
        proc.terminate(); code = await asyncio.wait_for(proc.wait(), 15); log.close()
        record['server_cleanup'] = {'direct_child_reaped': True, 'exit': code}
        (ROOT / 'warm-identity.json').write_text(json.dumps(record, indent=2) + '\n')
        print(json.dumps({k: record.get(k) for k in ['passed', 'same_bot_socket_delta', 'other_bot_socket_delta', 'mismatched_session', 'errors', 'inference_frames', 'server_cleanup']}), flush=True)


asyncio.run(main())
