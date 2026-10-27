import json,urllib.request,urllib.error,urllib.parse,subprocess
from pathlib import Path
from playwright.sync_api import sync_playwright
API='http://127.0.0.1:18180';WEB='http://127.0.0.1:18182'
OUT=Path('/tmp/contact-navigation-evidence');OUT.mkdir(exist_ok=True)
f=json.loads(Path('/tmp/contact-navigation-fixture.json').read_text())
def api(path,method='GET',body=None,token=None):
 h={'Content-Type':'application/json'}
 if token:h['Authorization']='Bearer '+token
 req=urllib.request.Request(API+path,data=json.dumps(body).encode() if body is not None else (b'' if method=='POST' else None),headers=h,method=method)
 try:
  with urllib.request.urlopen(req) as r:return r.status,json.load(r)
 except urllib.error.HTTPError as e:return e.code,json.load(e)
status,auth=api('/auth/login','POST',dict(username='admin',password='admin123'));assert status==200
token=auth['access_token'];calls=[];subscriptions=[];errors=[];steps=[]
status,entry=api('/bots/'+f['bot']+'/sessions/contact',token=token);assert status==200 and {c['id'] for c in entry['candidates']}=={f['old'],f['other']},entry
with sync_playwright() as p:
 browser=p.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox'])
 def client(url=WEB+'/chat/'+f['slug'],language='en',width=1280,session_token=token):
  ctx=browser.new_context(viewport=dict(width=width,height=900 if width>500 else 844))
  ctx.add_init_script("localStorage.setItem('token',"+json.dumps(session_token)+");localStorage.setItem('language',"+json.dumps(language)+");")
  page=ctx.new_page();page.on('pageerror',lambda e:errors.append(str(e)))
  def response(r):
   path=urllib.parse.urlparse(r.url).path
   if not path.startswith('/api/'):return
   calls.append(dict(method=r.request.method,path=path.split('/api',1)[1],status=r.status))
  page.on('response',response)
  def socket(ws):
   def sent(raw):
    try:d=json.loads(raw)
    except:return
    if d.get('type')=='runtime_subscribe':subscriptions.append(dict(client=len(browser.contexts),session_id=d['session_id']))
   ws.on('framesent',sent)
  page.on('websocket',socket)
  page.goto(url,wait_until='domcontentloaded',timeout=60000)
  return ctx,page
 one,page=client()
 try:
  dialog=page.get_by_role('dialog');dialog.wait_for(timeout=30000)
  assert dialog.get_by_text('Legacy conversation A',exact=False).count()==1
  assert dialog.get_by_text('Legacy conversation B',exact=False).count()==1
  assert dialog.get_by_role('radio',checked=True).count()==0,'automatically chose an old chat'
  assert 'Other account private' not in dialog.inner_text() and 'Delegated internal' not in dialog.inner_text() and 'External runtime history' not in dialog.inner_text()
  page.screenshot(path=str(OUT/'01-first-choice.png'),full_page=True);steps.append('first-use: exactly two trusted candidates; no auto-selection; no internal/foreign/external candidate')
  dialog.get_by_text('Legacy conversation A',exact=False).click()
  dialog.get_by_role('button',name='Confirm',exact=True).click();dialog.wait_for(state='hidden',timeout=15000)
  page.wait_for_timeout(1000)
  status,entry=api('/bots/'+f['bot']+'/sessions/contact',token=token);assert status==200 and entry['session']['id']==f['old'],entry
  assert any(x['session_id']==f['old'] for x in subscriptions)
  page.screenshot(path=str(OUT/'02-bound-contact.png'),full_page=True);steps.append('confirmed original legacy A; route bound to same session; native runtime subscription observed')
  two,second=client();second.wait_for_timeout(1800);assert second.get_by_role('dialog').count()==0
  status,again=api('/bots/'+f['bot']+'/sessions/contact',token=token);assert again['session']['id']==f['old']
  steps.append('independent second browser context opens same persisted contact without a picker')
  page.reload(wait_until='domcontentloaded');page.wait_for_timeout(1500);assert page.get_by_role('dialog').count()==0
  steps.append('reload retains persisted binding')
  before=sum(c['method']=='POST' and '/sessions/contact' in c['path'] for c in calls)
  page.goto(WEB+'/chat/'+f['slug']+'?session='+f['other'],wait_until='domcontentloaded');page.wait_for_timeout(1800)
  assert page.url.endswith('?session='+f['other'])
  assert any(x['session_id']==f['other'] for x in subscriptions)
  assert sum(c['method']=='POST' and '/sessions/contact' in c['path'] for c in calls)==before,'history link changed binding'
  page.screenshot(path=str(OUT/'03-explicit-history.png'),full_page=True);steps.append('explicit history B deep link opens original B; no contact POST; canonical still A')
  # Same-Bot click is meaningful while a history view is open.
  page.get_by_role('button',name=f['label'],exact=True).last.click();page.wait_for_timeout(1200)
  assert '?session=' not in page.url
  assert api('/bots/'+f['bot']+'/sessions/contact',token=token)[1]['session']['id']==f['old']
  steps.append('same Bot contact click exits history URL and returns to canonical A')
  page.get_by_role('button',name='History & debug',exact=True).click();page.wait_for_timeout(400)
  assert page.get_by_text('Legacy conversation B',exact=True).count()>0
  page.screenshot(path=str(OUT/'04-secondary-history.png'),full_page=True);steps.append('secondary history/debug retains old conversation B, external history and original new-session controls')
  page.get_by_role('button',name='New Session',exact=True).first.click();page.wait_for_timeout(400)
  composer=page.locator('textarea:visible');composer.fill('Unsent navigation draft')
  page.get_by_role('button',name='Contacts',exact=True).last.click()
  page.get_by_role('button',name=f['label'],exact=True).click();page.wait_for_timeout(1000)
  page.locator('.dv-tab').filter(has_text='New Session').first.click();page.wait_for_timeout(400)
  assert page.locator('textarea:visible').input_value()=='Unsent navigation draft','contact entry discarded an unsent draft'
  page.screenshot(path=str(OUT/'06-preserved-draft.png'),full_page=True);steps.append('unsent draft survives contact return and reopening original draft tab')
  # Real server rejects administrator adoption of another account's private chat.
  status,problem=api('/bots/'+f['empty_bot']+'/sessions/contact/bind','POST',dict(session_id=f['foreign']),token);assert status==409,(status,problem)
  status,problem=api('/bots/'+f['bot']+'/sessions/contact/bind','POST',dict(session_id=f['other'],user_id=f['user']),token);assert status==400
  steps.append('live HTTP: foreign/cross-Bot adoption rejected 409; injected account identity rejected 400')
  narrow,mobile=client(WEB+'/chat/'+f['slug']+'?session='+f['other'],'zh',390);mobile.wait_for_timeout(1500)
  mobile.screenshot(path=str(OUT/'05-history-narrow-zh.png'),full_page=True)
  # Two fresh clients land on the no-candidate Bot and converge on atomic POST.
  e1,p1=client(WEB+'/bot/'+f['empty_slug']);e2,p2=client(WEB+'/bot/'+f['empty_slug'])
  p1.wait_for_timeout(1600);p2.wait_for_timeout(1000)
  code,created=api('/bots/'+f['empty_bot']+'/sessions/contact',token=token)
  assert code==200 and created.get('session') and not created['candidates'],created
  assert p1.get_by_role('dialog').count()==0 and p2.get_by_role('dialog').count()==0
  assert any(x['session_id']==created['session']['id'] for x in subscriptions)
  p2.screenshot(path=str(OUT/'07-no-history-contact.png'),full_page=True);steps.append('no eligible history: two fresh browser contexts converge on native atomic contact creation')
  # Real second account receives a denied history view, never the admin's chat.
  import uuid
  username='navigation-member-'+str(uuid.uuid4())[:8];password=str(uuid.uuid4())
  code,member=api('/users','POST',dict(username=username,password=password,role='member'),token)
  assert code==201,(code,member)
  member_setup="UPDATE users SET metadata=metadata || '{\"onboarding_completed\":true}'::jsonb WHERE id='"+member['id']+"';"
  subprocess.run(['docker','exec','-i','memoh-dev-postgres','psql','-X','-q','-v','ON_ERROR_STOP=1','-U','memoh','-d','contact_lifecycle_app'],input=member_setup,text=True,capture_output=True,check=True)
  code,member_auth=api('/auth/login','POST',dict(username=username,password=password));assert code==200
  denied_ctx,denied=client(WEB+'/bot/'+f['slug']+'?session='+f['old'],session_token=member_auth['access_token'])
  denied.wait_for_timeout(1800)
  assert denied.locator('textarea:visible').count()==0,'unauthorized account saw writable chat'
  code,problem=api('/bots/'+f['bot']+'/sessions/contact',token=member_auth['access_token']);assert code in (403,404),(code,problem)
  denied.screenshot(path=str(OUT/'08-second-account-denied.png'),full_page=True);steps.append('independent member account cannot enter another account Bot/history or resolve its contact')
  # Snapshot original uncertain operation and unselected session after navigation.
  statement="SET memoh.team_id='00000000-0000-0000-0000-000000000001'; SELECT json_build_object('operation',row_to_json(o),'other_session_route',(SELECT route_id FROM bot_sessions WHERE id='"+f['other']+"'),'canonical',(SELECT active_session_id FROM bot_channel_routes WHERE bot_id='"+f['bot']+"')) FROM computer_operations o WHERE id='"+f['operation']+"';"
  sql=subprocess.run(['docker','exec','-i','memoh-dev-postgres','psql','-X','-qAt','-v','ON_ERROR_STOP=1','-U','memoh','-d','contact_lifecycle_app'],input=statement,text=True,capture_output=True,check=True)
  state=json.loads(sql.stdout);assert state['operation']['state']=='unknown' and state['operation']['revision']==0 and state['operation']['session_id']==f['other'] and state['other_session_route'] is None and state['canonical']==f['old'],state
  (OUT/'database-readback.json').write_text(json.dumps(state,indent=2));steps.append('PG readback: original unknown operation state/revision/session unchanged; historical B not rerouted; canonical A retained')
  assert not errors,errors
  (OUT/'browser-proof.json').write_text(json.dumps(dict(api=API,web=WEB,fixture=f,steps=steps,calls=calls,subscriptions=subscriptions,page_errors=errors,model_messages_sent=0,limitations=['No real device/Harmony test','Synthetic Bot without workspace/model']),indent=2))
  print('PASS real Server/PG/browser navigation choice, binding, two clients, refresh, explicit history, draft preservation, empty contact, permissions and unknown-operation readback.')
 except Exception:
  page.screenshot(path=str(OUT/'failure.png'),full_page=True)
  (OUT/'failure.json').write_text(json.dumps(dict(steps=steps,calls=calls,subscriptions=subscriptions,errors=errors,url=page.url,body=page.locator('body').inner_text()),indent=2));raise
 finally:browser.close()
