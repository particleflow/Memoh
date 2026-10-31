import asyncio
import hashlib
import json
import re
import subprocess
import time
import urllib.parse
import urllib.request
from pathlib import Path

from playwright.async_api import async_playwright

ROOT = Path(__file__).resolve().parent
REPO = Path('/workspace/review-hardcoding-integration-3f')
BASE = 'http://127.0.0.1:28482'
FILES = ['apps/web/src/store/chat/bootstrap.ts', 'apps/web/src/store/chat-list.ts']
FIXTURE = json.loads((ROOT / 'fixture.json').read_text())
TOKEN = json.loads((ROOT / 'fixture-auth.json').read_text())['access_token']
HTTP = urllib.request.build_opener(urllib.request.ProxyHandler({}))
user = json.loads((ROOT / 'fixture-profile.json').read_text())
USER = {'id': user['id'], 'username': user['username'], 'role': user['role'],
        'displayName': user.get('display_name', ''), 'avatarUrl': user.get('avatar_url', ''),
        'timezone': user.get('timezone', 'UTC')}


def scrub(text):
    return re.sub(r'(token=)[^&\s\"\']+', r'\1<redacted>', str(text))


async def case(browser, phase, bot, session):
    context = await browser.new_context(viewport={'width': 1200, 'height': 850})
    # Reproduce an authenticated cold tab's persisted selection using an actual
    # JWT/user issued by this isolated Server. Do not change any server response.
    await context.add_init_script('''
      localStorage.setItem('token', %s);
      localStorage.setItem('user', JSON.stringify({userInfo: %s}));
      localStorage.setItem('chat-bot-id', %s);
    ''' % (json.dumps(TOKEN), json.dumps(USER), json.dumps(bot['id'])))
    page = await context.new_page()
    start = time.monotonic()
    record = {'phase': phase, 'bot': bot['id'], 'session': session['id'], 'http': [],
              'sockets': [], 'frames': [], 'samples': [], 'errors': [], 'writes': []}
    response_tasks = []

    async def response(response):
        url = urllib.parse.urlsplit(response.url)
        if '/messages' not in url.path and not url.path.endswith('/' + session['id']):
            return
        row = {'path': url.path, 'session_id': urllib.parse.parse_qs(url.query).get('session_id'),
               'status': response.status, 'at_ms': round((time.monotonic() - start) * 1000, 2)}
        if '/messages' in url.path:
            try:
                data = await response.json()
                row['items'] = len(data.get('items', []))
                row['ids'] = [x.get('id') for x in data.get('items', [])]
            except Exception as error:
                row['error'] = type(error).__name__
        record['http'].append(row)

    page.on('response', lambda r: response_tasks.append(asyncio.create_task(response(r))))
    page.on('pageerror', lambda e: record['errors'].append(scrub(e)))
    page.on('request', lambda r: record['writes'].append({'method': r.method, 'path': urllib.parse.urlsplit(r.url).path}) if r.method not in ('GET', 'HEAD', 'OPTIONS') else None)

    def websocket(ws):
        record['sockets'].append({'path': urllib.parse.urlsplit(ws.url).path,
                                  'at_ms': round((time.monotonic() - start) * 1000, 2)})
        def frame(payload, direction):
            try:
                value = json.loads(payload)
                record['frames'].append({'direction': direction, 'type': value.get('type'),
                    'session_id': value.get('session_id'),
                    'snapshot_bot': value.get('snapshot', {}).get('bot_id'),
                    'snapshot_session': value.get('snapshot', {}).get('session_id'),
                    'at_ms': round((time.monotonic() - start) * 1000, 2)})
            except (TypeError, ValueError):
                record['frames'].append({'direction': direction, 'type': 'non-json'})
        ws.on('framesent', lambda p: frame(p, 'sent'))
        ws.on('framereceived', lambda p: frame(p, 'received'))
    page.on('websocket', websocket)
    url = BASE + '/bot/' + bot['name'] + '?session=' + session['id']
    await page.goto(url, wait_until='domcontentloaded', timeout=60000)
    # This is an observation window, not a product timeout or a recovery retry.
    for _ in range(40):
        await page.wait_for_timeout(200)
        sample = await page.evaluate('''texts => ({
          visible: texts.map(t => document.body.innerText.includes(t)),
          contentRows: document.querySelectorAll('[data-chat-content]').length,
          skeletons: document.querySelectorAll('.animate-pulse').length,
          path: location.pathname + location.search,
        })''', [m['text'] for m in session['messages']])
        sample['at_ms'] = round((time.monotonic() - start) * 1000, 2)
        record['samples'].append(sample)
    if response_tasks:
        await asyncio.gather(*response_tasks)
    record['final'] = record['samples'][-1]
    record['page_title'] = await page.title()
    await page.screenshot(path=str(ROOT / (phase + '.png')), full_page=True)
    record['inference_frames'] = [f for f in record['frames'] if f['direction'] == 'sent' and f['type'] in ('message', 'retry_message', 'edit_message')]
    (ROOT / (phase + '.json')).write_text(json.dumps(record, indent=2) + '\n')
    print(json.dumps({'phase': phase, 'final': record['final'], 'sockets': len(record['sockets']),
                      'snapshot_count': len([f for f in record['frames'] if f['type'] == 'runtime_snapshot']),
                      'errors': record['errors'], 'writes': record['writes']}, ensure_ascii=False), flush=True)
    await context.close()
    return record


async def main():
    saved = {p: (REPO / p).read_bytes() for p in FILES}
    result = {}
    try:
        async with async_playwright() as play:
            browser = await play.chromium.launch(executable_path='/usr/bin/chromium', headless=True, args=['--no-sandbox'])
            for p in FILES:
                (REPO / p).write_bytes(subprocess.check_output(['git', 'show', 'cefdf0cbb3207d2a07db17ef365e1b8d2d43ca3f:' + p], cwd=REPO))
            result['before'] = await case(browser, 'before-cold', FIXTURE['bots'][0], FIXTURE['bots'][0]['sessions'][0])
            for p, data in saved.items():
                (REPO / p).write_bytes(data)
            result['after'] = await case(browser, 'after-cold', FIXTURE['bots'][0], FIXTURE['bots'][0]['sessions'][0])
            await browser.close()
    finally:
        for p, data in saved.items():
            (REPO / p).write_bytes(data)
            assert hashlib.sha256((REPO / p).read_bytes()).digest() == hashlib.sha256(data).digest()
    (ROOT / 'browser-outcome.json').write_text(json.dumps(result, indent=2) + '\n')


if __name__ == '__main__':
    asyncio.run(main())
