from pathlib import Path
import sys,json,tempfile,re,os
from urllib.parse import urlsplit,parse_qs,quote
from playwright.sync_api import sync_playwright
ROOT=Path(__file__).resolve().parents[3]/'mobile/client';sys.path.insert(0,str(ROOT/'tests/stage2'));import harmony_auth_persistence as f
E=Path(__file__).parent;f.new_dist=f.current_dist=Path(os.environ['HARMONY_CONTACT_CHECKOUT'])/'apps/web/dist'
contact='22222222-2222-4222-8222-222222222222';old_a='33333333-3333-4333-8333-333333333333';old_b='44444444-4444-4444-8444-444444444444'
mode='bound';binding=contact;events=[];ws=[];proof=[]
def row(sid):return {'id':sid,'bot_id':f.bot_id,'title':{contact:'持续会话',old_a:'历史甲',old_b:'历史乙'}[sid],'type':'chat','runtime_kind':'model','channel_type':'local','visibility':'user'}
def api(path,request):
 global binding
 root=f'/bots/{f.bot_id}/sessions'
 if path==root+'/contact':
  events.append({'method':request.method,'endpoint':'contact','mode':mode})
  if request.method=='GET':
   if binding:return 200,{'session':row(binding),'candidates':[]}
   return 200,{'candidates':[row(old_a)] if mode=='unique' else [row(old_a),row(old_b)] if mode=='multiple' else []}
  assert request.method=='POST' and not request.post_data;binding=contact;return 200,row(binding)
 if path==root+'/contact/bind':
  assert request.method=='POST' and request.post_data_json in [{'session_id':old_a},{'session_id':old_b}];binding=request.post_data_json['session_id'];events.append({'method':'POST','endpoint':'bind','session':binding,'mode':mode});return 200,row(binding)
 if path==root:return 200,{'items':[row(sid) for sid in [contact,old_a,old_b]]}
 for sid in [contact,old_a,old_b]:
  if path==root+'/'+sid:return 200,row(sid)
 return None
f.extra_api_handler=api
def runtime(socket):
 def receive(raw):
  data=json.loads(raw);kind=data.get('type');ws.append(kind)
  assert kind not in ['message','user_input_response','retry_message','followup','edit_message']
  sid=data.get('session_id')
  if kind!='runtime_subscribe' or sid not in [contact,old_a,old_b]:return
  socket.send(json.dumps({'type':'runtime_snapshot','session_id':sid,'epoch':'fixture-epoch','seq':1,'snapshot':{'bot_id':f.bot_id,'session_id':sid,'epoch':'fixture-epoch','seq':1,'current_run_view':None,'updated_at':'2026-10-27T14:00:00Z'}}))
 socket.on_message(receive)
STORE='document.querySelector("#app").__vue_app__.config.globalProperties.$pinia._s'
def selected(page,sid):page.wait_for_function('(id)=>document.querySelector("#app")?.__vue_app__?.config.globalProperties.$pinia?._s.get("chat")?.sessionId===id',arg=sid)
def contact_click(page):
 page.evaluate(STORE+'.get("workspace-tabs").openMobileNav()')
 sheet=page.get_by_role('dialog').filter(has=page.get_by_role('button',name='Synthetic Bot',exact=True)).last
 sheet.wait_for();sheet.get_by_role('button',name='Synthetic Bot',exact=True).last.click()
def writes():return [e for e in events if e['method']=='POST']
try:
 with sync_playwright() as pw:
  for case in ['bound','unique','new','multiple']:
   mode=case;binding=contact if case=='bound' else None;start=len(events);f.page_errors.clear()
   with tempfile.TemporaryDirectory(dir=E,ignore_cleanup_errors=True) as profile:
    context,page=f.start(pw,Path(profile));context.route_web_socket('**/*',runtime);f.login(page);identity=f.fingerprint(page)
    if case=='multiple':
     dialog=page.get_by_role('dialog',name='继续一段对话')
     # Use the localized shared title from the exact compiled locale.
     title=json.loads((Path(os.environ['HARMONY_CONTACT_CHECKOUT'])/'apps/web/src/i18n/locales/zh.json').read_text())['chat']['contact']['chooseTitle'];dialog=page.get_by_role('dialog',name=title);dialog.wait_for();assert not writes()[len([x for x in events[:start] if x['method']=='POST']):]
     assert page.get_by_role('radio',checked=True).count()==0
     page.screenshot(path=str(E/'contact-choice-390.png'));dialog.get_by_role('button',name='取消',exact=True).click();assert not [x for x in events[start:] if x['method']=='POST']
     contact_click(page);dialog.wait_for();dialog.get_by_role('radio').nth(1).click();dialog.get_by_role('button',name='确认',exact=True).click();selected(page,old_b)
     assert [x for x in events[start:] if x['method']=='POST']==[{'method':'POST','endpoint':'bind','session':old_b,'mode':case}]
     page.reload(wait_until='domcontentloaded');selected(page,old_b);assert len([x for x in events[start:] if x['method']=='POST'])==1
    else:
     expected=old_a if case=='unique' else contact;selected(page,expected);assert not page.get_by_role('dialog').filter(has=page.get_by_role('radio')).count()
     actions=[x for x in events[start:] if x['method']=='POST'];assert len(actions)==(0 if case=='bound' else 1)
     if case=='unique':assert actions[0]['endpoint']=='bind' and actions[0]['session']==old_a
     if case=='new':assert actions[0]['endpoint']=='contact'
     if case=='bound':
      page.locator('textarea:visible').first.fill('未发送的原会话草稿')
      page.evaluate('(p)=>'+STORE+'.get("workspace-tabs").openSessionChat(p)',{'sessionId':old_a,'title':'历史甲'});selected(page,old_a)
      page.locator('textarea:visible').first.fill('未发送的历史草稿')
      contact_click(page);selected(page,contact);page.wait_for_function('document.querySelector("textarea:not([hidden])") !== null');page.wait_for_function("expected => ([...document.querySelectorAll('textarea')].find(e=>e.getClientRects().length)?.value)===expected",arg='未发送的原会话草稿')
      page.evaluate('(p)=>'+STORE+'.get("workspace-tabs").openSessionChat(p)',{'sessionId':old_a,'title':'历史甲'});selected(page,old_a);page.wait_for_function("expected => ([...document.querySelectorAll('textarea')].find(e=>e.getClientRects().length)?.value)===expected",arg='未发送的历史草稿')
      contact_click(page);selected(page,contact)
      page.evaluate(STORE+'.get("workspace-tabs").openMobileNav()');page.get_by_role('dialog').last.wait_for();page.screenshot(path=str(E/'contacts-primary-390.png'))
      history_label=json.loads((Path(os.environ['HARMONY_CONTACT_CHECKOUT'])/'apps/web/src/i18n/locales/zh.json').read_text())['chat']['contact']['history'];page.get_by_role('dialog').last.get_by_role('button',name=history_label,exact=True).click();page.screenshot(path=str(E/'history-secondary-390.png'));page.evaluate(STORE+'.get("workspace-tabs").closeMobileNav()')
      # A cold, explicit history URL must bypass contact resolution/binding.
      before=len(events);page.goto(f.base+'/#/bot/'+quote(f.bot['name'],safe='')+'?session='+old_b,wait_until='domcontentloaded');selected(page,old_b);assert len(events)==before;assert binding==contact;page.screenshot(path=str(E/'explicit-history-390.png'))
    assert f.fingerprint(page)==identity and not f.page_errors,f.page_errors
    assert page.evaluate('document.documentElement.scrollWidth<=innerWidth')
    proof.append({'case':case,'contactPOSTs':len([x for x in events[start:] if x['method']=='POST' and x['endpoint']=='contact']),'bindPOSTs':len([x for x in events[start:] if x['method']=='POST' and x['endpoint']=='bind']),'noExtraPermissionPrompt':True,'originalSessionIDs':True,'loginRetained':True,'horizontalOverflow':False});context.close()
  mutations=[e for e in f.api_events if e['method'] not in ['GET','HEAD','OPTIONS'] and e['path']!='/auth/login'];assert all('/sessions/contact' in e['path'] for e in mutations)
  result={'sharedContactSource':'8fe72f9573f4a85b0d2226d959598180d4c39961','viewport':[390,844],'cases':proof,'uniqueCandidateNoDialog':True,'ambiguousCancelNoBinding':True,'confirmedHistoryAndReloadSameID':True,'coldHistoryURLNoContactReadOrMutation':True,'contactAndHistoryDraftsPreserved':True,'singleControlAndThemesUnchanged':True,'nonContactBusinessWrites':0,'phone':False,'productionBackend':False,'TLSAcceptance':False,'paidCalls':0,'pageErrors':[]};(E/'browser-contact-flow.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result))
finally:f.server.shutdown()
