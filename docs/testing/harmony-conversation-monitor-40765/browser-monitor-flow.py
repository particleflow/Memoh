from pathlib import Path
import sys,json,tempfile
from playwright.sync_api import sync_playwright
import os
ROOT=Path(__file__).resolve().parents[3]/'mobile/client'
sys.path.insert(0,str(ROOT/'tests/stage2'))
import harmony_auth_persistence as f
E=Path(os.environ['HARMONY_MONITOR_TEST_OUT']).resolve();E.mkdir(parents=True,exist_ok=True)
f.new_dist=f.current_dist=Path(os.environ['HARMONY_MONITOR_TEST_DIST']).resolve()
root='22222222-2222-4222-8222-222222222222';a='33333333-3333-4333-8333-333333333333';b='44444444-4444-4444-8444-444444444444';expert='55555555-5555-4555-8555-555555555555';expertSession='66666666-6666-4666-8666-666666666666';c='77777777-7777-4777-8777-777777777777';unrelated='88888888-8888-4888-8888-888888888888'
records={root:{'id':root,'bot_id':f.bot_id,'title':'当前合成会话','type':'chat'},a:{'id':a,'bot_id':f.bot_id,'title':'审阅 Agent','type':'chat','parent_session_id':root},b:{'id':b,'bot_id':f.bot_id,'title':'深层 Agent','type':'chat','parent_session_id':a},expertSession:{'id':expertSession,'bot_id':expert,'title':'专家原会话','type':'chat'},c:{'id':c,'bot_id':expert,'title':'专家子 Agent','type':'chat','parent_session_id':expertSession}}
rootbot={**f.bot,'display_name':'红玉'};expertbot={**f.bot,'id':expert,'name':'Synthetic expert','display_name':'图像专家'}
reads=[]
def task(sid,parent,status,owningRoot=root):
 return {'task_type':'subagent','task_id':'OPAQUE_TASK_'+sid,'parent_session_id':owningRoot,'direct_parent_session_id':parent,'session_id':sid,'status':status,'created_at':'2026-10-27T00:00:00Z','updated_at':'2026-10-27T00:00:00Z','summary':'BODY_NOT_REQUESTED','result_summary':{'text':'RESULT_NOT_REQUESTED'},'target_id':'DEVICE_NOT_REQUESTED','logs':['LOG_NOT_REQUESTED']}
def api(path,request):
 if path=='/bots':return 200,{'items':[rootbot,expertbot,{**f.bot,'id':unrelated,'display_name':'无关 Bot'}]}
 if path=='/bots/'+expert:return 200,expertbot
 if path=='/bots/'+f.bot_id:return 200,rootbot
 if path==f'/bots/{f.bot_id}/activity' or path==f'/bots/{expert}/activity':raise AssertionError('Conversation dialog must not read global activity')
 for bot in (f.bot_id,expert):
  if path==f'/bots/{bot}/sessions':return 200,{'items':[]}
 for sid,row in records.items():
  if path==f"/bots/{row['bot_id']}/sessions/{sid}":reads.append({'bot':row['bot_id'],'session':sid});return 200,row
  if path==f"/bots/{row['bot_id']}/sessions/{sid}/tasks":
   return 200,{'items':([task(a,root,'running'),task(b,a,'unknown'),task(unrelated,root,'running','different-conversation')] if sid==root else [task(c,expertSession,'aborting',expertSession)] if sid==expertSession else [])}
  if path==f"/bots/{row['bot_id']}/sessions/{sid}/delegations":
   return 200,{'items':([{'source_session_id':root,'target_bot_id':expert,'target_session_id':expertSession,'state':'running','target_run_state':'waiting_decision','delegation_id':'OPAQUE_DELEGATION','summary':{'text':'BODY_NOT_REQUESTED'}},{'source_session_id':'different-conversation','target_bot_id':unrelated,'target_session_id':unrelated,'state':'running'}] if sid==root else [])}
 if path.endswith('/messages'):return 200,{'items':[]}
 return None
f.extra_api_handler=api
f.wait_chat=lambda page: page.wait_for_function("document.body.innerText.includes('红玉') && !location.hash.includes('/login')")
try:
 with tempfile.TemporaryDirectory(dir=E) as profile,sync_playwright() as pw:
  context,page=f.start(pw,Path(profile));f.login(page);identity=f.fingerprint(page)
  def openRoot():
   page.evaluate('id=>document.querySelector("#app").__vue_app__.config.globalProperties.$pinia._s.get("workspace-tabs").openSessionChat({sessionId:id,title:"当前合成会话"})',root)
   page.wait_for_function('sid=>document.querySelector(\"#app\").__vue_app__.config.globalProperties.$pinia._s.get(\"chat\").sessionId===sid',arg=root)
   page.get_by_role('button',name='任务',exact=True).wait_for()
  openRoot()
  # Actual footer click must open the shared relation dialog, not the drawer.
  page.get_by_role('button',name='任务',exact=True).click();dialog=page.get_by_role('dialog')
  dialog.get_by_text('专家子 Agent',exact=True).wait_for()
  assert page.locator('[data-slot=sheet-content][data-state=open]').count()==0
  rows=dialog.locator('li > button').evaluate_all('(nodes)=>nodes.map(n=>({name:n.querySelector("span.min-w-0")?.textContent?.trim(),state:n.querySelector("span.text-caption")?.textContent?.trim()}))')
  assert {r['name'] for r in rows}=={'红玉','审阅 Agent','深层 Agent','图像专家','专家子 Agent'}
  assert dialog.locator('ul ul ul').count()>=1
  for forbidden in ['无关 Bot','OPAQUE','BODY_NOT_REQUESTED','RESULT_NOT_REQUESTED','DEVICE_NOT_REQUESTED','LOG_NOT_REQUESTED','2026-10-27',root,a,b,expertSession]:assert forbidden not in dialog.inner_text()
  assert dialog.locator('pre,code,textarea,input,[role=tablist]').count()==0
  assert not dialog.get_by_role('button',name='重试',exact=True).count()
  assert page.evaluate('document.documentElement.scrollWidth<=innerWidth')
  page.screenshot(path=str(E/'conversation-tasks-390.png'));dialog.screenshot(path=str(E/'conversation-task-dialog.png'))
  dialog.get_by_role('button').filter(has_text='深层 Agent').click()
  page.wait_for_function('sid=>document.querySelector("#app").__vue_app__.config.globalProperties.$pinia._s.get("chat").sessionId===sid',arg=b)
  assert {'bot':f.bot_id,'session':b} in reads
  openRoot();page.get_by_role('button',name='Bots',exact=True).click();dialog=page.get_by_role('dialog')
  dialog.get_by_text('图像专家',exact=True).wait_for()
  bots=dialog.locator('li > button').evaluate_all('(nodes)=>nodes.map(n=>n.querySelector("span.min-w-0")?.textContent?.trim())')
  assert set(bots)=={'红玉','图像专家'}
  for forbidden in ['审阅 Agent','深层 Agent','无关 Bot']:assert forbidden not in dialog.inner_text()
  page.screenshot(path=str(E/'conversation-bots-390.png'))
  dialog.get_by_role('button').filter(has_text='图像专家').click()
  page.wait_for_function('p=>{const c=document.querySelector("#app").__vue_app__.config.globalProperties.$pinia._s.get("chat");return c.currentBotId===p.bot&&c.sessionId===p.session}',arg={'bot':expert,'session':expertSession})
  assert {'bot':expert,'session':expertSession} in reads
  # Back dismisses the next relation dialog using the existing phone back contract.
  page.get_by_role('button',name='任务',exact=True).click();dialog=page.get_by_role('dialog');dialog.get_by_text('专家子 Agent',exact=True).wait_for()
  assert page.evaluate('window.__memohBack()') is True;dialog.wait_for(state='hidden')
  assert f.fingerprint(page)==identity and f.counts['login']==1 and not f.page_errors,f.page_errors
  writes=[e for e in f.api_events if e['method'] not in ['GET','HEAD','OPTIONS'] and e['path']!='/auth/login'];assert not writes,writes
  assert not page.get_by_text('恢复本会话待传附件',exact=True).count()
  result={'sharedCommit':'40765b9e0979d561c49e58d2f384799e6c589e27','viewport':[390,844],'rows':rows,'relatedBots':bots,'globalSidebarOpened':False,'globalActivityRequests':0,'threeLevelRelations':True,'unrelatedBotExcluded':True,'childOriginalSessionNavigation':True,'expertOriginalBotSessionNavigation':True,'existingBackClosesDialog':True,'visibleRowFields':['name','state'],'detailsTimeLogsRawIDsRendered':False,'recoveryButtonAdded':False,'wholeTurnRetryCalled':False,'acceptedAnswerResubmitted':False,'loginRetained':True,'businessWrites':0,'pageErrors':f.page_errors,'API':'synthetic exact scoped read-only contract; not production','actualArkWebOrPhone':False,'HAPBuilt':False}
  (E/'browser-monitor-flow.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n');print(json.dumps(result,ensure_ascii=False));context.close()
finally:f.server.shutdown()
