import json,uuid,urllib.request,urllib.error,subprocess
from pathlib import Path
from playwright.sync_api import sync_playwright
API='http://127.0.0.1:18180';WEB='http://127.0.0.1:18182';OUT=Path('/tmp/contact-navigation-evidence')
f=json.loads(Path('/tmp/contact-navigation-fixture.json').read_text())
def api(path,method='GET',body=None,token=None):
 headers={'Content-Type':'application/json'}
 if token:headers['Authorization']='Bearer '+token
 req=urllib.request.Request(API+path,data=json.dumps(body).encode() if body is not None else (b'' if method=='POST' else None),headers=headers,method=method)
 try:
  with urllib.request.urlopen(req) as r:return r.status,json.load(r)
 except urllib.error.HTTPError as e:return e.code,json.load(e)
def sql(statement):
 return subprocess.run(['docker','exec','-i','memoh-dev-postgres','psql','-X','-qAt','-v','ON_ERROR_STOP=1','-U','memoh','-d','contact_lifecycle_app'],input="SET memoh.team_id='00000000-0000-0000-0000-000000000001';\n"+statement,text=True,capture_output=True,check=True).stdout
_,auth=api('/auth/login','POST',dict(username='admin',password='admin123'));token=auth['access_token']
u,d=str(uuid.uuid4()),str(uuid.uuid4());uslug='unique-contact-'+u[:8];dslug='choice-dark-'+d[:8]
sql(f"INSERT INTO bots(id,owner_user_id,name,display_name) VALUES('{u}','{f['user']}','{uslug}','Unique contact review'),('{d}','{f['user']}','{dslug}','Dark choice review');")
code,old=api('/bots/'+u+'/sessions','POST',dict(title='Only trusted old conversation',type='chat'),token);assert code==201
for title in ['旧对话一','旧对话二']:
 code,created=api('/bots/'+d+'/sessions','POST',dict(title=title,type='chat'),token);assert code==201
username='navigation-member-'+str(uuid.uuid4())[:8];password=str(uuid.uuid4())
code,member=api('/users','POST',dict(username=username,password=password,role='member'),token);assert code==201
sql("UPDATE users SET metadata=metadata || '{\"onboarding_completed\":true}'::jsonb WHERE id='"+member['id']+"';")
_,member_auth=api('/auth/login','POST',dict(username=username,password=password))
steps=[];errors=[];calls=[]
with sync_playwright() as p:
 browser=p.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox'])
 def client(url,tk=token,language='en',dark=False,width=1280):
  ctx=browser.new_context(viewport=dict(width=width,height=900 if width>500 else 844))
  ctx.add_init_script("localStorage.setItem('token',"+json.dumps(tk)+");localStorage.setItem('language',"+json.dumps(language)+");localStorage.setItem('theme',"+json.dumps('dark' if dark else 'light')+");")
  page=ctx.new_page();page.on('pageerror',lambda e:errors.append(str(e)))
  page.on('response',lambda r:calls.append(dict(method=r.request.method,path=r.url.split('/api',1)[1].split('?',1)[0],status=r.status)) if '/api/' in r.url else None)
  page.goto(url,wait_until='domcontentloaded');return ctx,page
 ctx,page=client(WEB+'/bot/'+uslug)
 page.wait_for_function("document.body.innerText.includes('Only trusted old conversation')",timeout=30000);page.wait_for_timeout(500)
 assert page.get_by_role('dialog').count()==0
 code,entry=api('/bots/'+u+'/sessions/contact',token=token);assert code==200 and entry['session']['id']==old['id']
 assert any(c['path']=='/bots/'+u+'/sessions/contact/bind' and c['status']==200 for c in calls)
 page.screenshot(path=str(OUT/'09-unique-history-no-prompt.png'),full_page=True);steps.append('one verified ordinary legacy chat continues automatically, same original ID; no dialog and no new session')
 ctx,denied=client(WEB+'/bot/'+f['slug']+'?session='+f['old'],member_auth['access_token'])
 denied.wait_for_timeout(1500)
 denied.wait_for_function("!document.body.innerText.includes('Loading...')",timeout=10000)
 assert denied.locator('textarea:visible').count()==0 and denied.get_by_text('Legacy conversation A',exact=True).count()==0
 denied.screenshot(path=str(OUT/'08-second-account-denied.png'),full_page=True);steps.append('second member account sees completed denied result, not a stuck loading state or another account conversation')
 ctx,dark=client(WEB+'/bot/'+dslug,language='zh',dark=True,width=390)
 dialog=dark.get_by_role('dialog');dialog.wait_for(timeout=30000)
 assert dialog.get_by_role('radio',checked=True).count()==0
 assert dialog.get_by_text('旧对话一',exact=False).count()==1 and dialog.get_by_text('旧对话二',exact=False).count()==1
 assert dark.locator('html').evaluate('(e)=>e.classList.contains("dark")')
 dark.screenshot(path=str(OUT/'10-choice-dark-narrow-zh.png'),full_page=True)
 dialog.get_by_role('button',name='取消',exact=True).click();dialog.wait_for(state='hidden')
 code,entry=api('/bots/'+d+'/sessions/contact',token=token);assert code==200 and not entry.get('session') and len(entry['candidates'])==2
 steps.append('390px Chinese dark dialog remains unselected; Cancel leaves both old chats and main binding unchanged')
 assert not errors,errors
 (OUT/'browser-supplement.json').write_text(json.dumps(dict(steps=steps,calls=calls,page_errors=errors,unique_bot=u,unique_original_session=old['id'],model_messages_sent=0),indent=2))
 browser.close();print('PASS final real browser: unique verified history without prompt; member denied result completes; narrow Chinese dark picker and cancellation.')
