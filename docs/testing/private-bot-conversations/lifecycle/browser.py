import json, os, urllib.request, urllib.parse
from pathlib import Path
from playwright.sync_api import sync_playwright
API='http://127.0.0.1:18180'
WEB='http://127.0.0.1:18182'
BOT='a3fca982-3331-4c1e-b629-dd87017de421'
OUT=Path(os.environ.get('CONTACT_REVIEW_EVIDENCE','/tmp/contact-lifecycle-app'))
def api(path,method='GET',body=None,token=None):
    headers={'Content-Type':'application/json'}
    if token:headers['Authorization']='Bearer '+token
    req=urllib.request.Request(API+path,data=json.dumps(body).encode() if body is not None else (b'' if method=='POST' else None),headers=headers,method=method)
    with urllib.request.urlopen(req) as r:return json.load(r)
auth=api('/auth/login','POST',dict(username='admin',password='admin123'))
token=auth['access_token']; calls=[]; subscriptions=[]; errors=[]; contacts=[]
with sync_playwright() as p:
    browser=p.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox'])
    def client():
        context=browser.new_context(viewport=dict(width=1280,height=900))
        context.add_init_script("localStorage.setItem('token',"+json.dumps(token)+");localStorage.setItem('language','en');")
        page=context.new_page()
        page.on('pageerror',lambda e:errors.append(str(e)))
        def response(r):
            if not urllib.parse.urlparse(r.url).path.startswith('/api/'):return
            path=r.url.split('/api',1)[1]
            calls.append(dict(method=r.request.method,path=path.split('?',1)[0],status=r.status))
            if path.endswith('/sessions/contact') and r.status==200:
                d=r.json();contacts.append(dict(id=d.get('id'),bot_id=d.get('bot_id'),mode=d.get('session_mode'),runtime=d.get('runtime_type')))
        page.on('response',response)
        def socket(ws):
            def sent(raw):
                try:d=json.loads(raw)
                except:return
                if d.get('type')=='runtime_subscribe':subscriptions.append(dict(type=d['type'],session_id=d['session_id']))
            ws.on('framesent',sent)
        page.on('websocket',socket)
        page.goto(WEB+'/chat/contact-review',wait_until='domcontentloaded',timeout=60000)
        return context,page
    one,page=client()
    try:
        page.wait_for_function("document.body.innerText.includes('Contact lifecycle review')",timeout=30000)
        page.wait_for_timeout(1500)
        assert contacts, 'no successful contact response'
        canonical=contacts[-1]['id']
        assert any(s['session_id']==canonical for s in subscriptions),'no original session subscription'
        page.screenshot(path=str(OUT/'contact-real-server.png'),full_page=True)
        seen=len(contacts); seen_ws=len(subscriptions)
        two,second=client()
        second.wait_for_function("document.body.innerText.includes('Contact lifecycle review')",timeout=30000)
        second.wait_for_timeout(1000)
        assert len(contacts)>seen and contacts[-1]['id']==canonical,'second client changed contact'
        assert any(s['session_id']==canonical for s in subscriptions[seen_ws:]),'second client did not subscribe'
        changed=api('/bots/'+BOT+'/sessions/'+canonical,'PATCH',dict(session_mode='discuss'),token)
        assert changed['id']==canonical and changed['session_mode']=='discuss'
        seen=len(contacts); seen_ws=len(subscriptions)
        page.reload(wait_until='domcontentloaded')
        page.wait_for_timeout(2000)
        assert len(contacts)>seen and contacts[-1]['id']==canonical and contacts[-1]['mode']=='discuss','descriptor change broke contact'
        assert any(s['session_id']==canonical for s in subscriptions[seen_ws:])
        page.screenshot(path=str(OUT/'contact-after-mode-change.png'),full_page=True)
        second.set_viewport_size(dict(width=390,height=844));second.reload(wait_until='domcontentloaded');second.wait_for_timeout(1500)
        second.screenshot(path=str(OUT/'contact-real-server-narrow.png'),full_page=True)
        assert not errors,errors
        proof=dict(server='current source, real local PostgreSQL; no intercepted responses',api=API,web=WEB,bot_id=BOT,canonical_session_id=canonical,contacts=contacts,subscriptions=subscriptions,calls=calls,page_errors=errors,business_messages_sent=0)
        (OUT/'browser-proof.json').write_text(json.dumps(proof,indent=2))
        print('PASS real Server + PostgreSQL + Web: two independent clients, same contact/runtime subscription, real PATCH mode change and reload; no model messages sent')
    except Exception:
        page.screenshot(path=str(OUT/'browser-failure.png'),full_page=True)
        (OUT/'browser-failure.json').write_text(json.dumps(dict(contacts=contacts,subscriptions=subscriptions,calls=calls,errors=errors,url=page.url,body=page.locator('body').inner_text()),indent=2))
        raise
    finally:browser.close()
