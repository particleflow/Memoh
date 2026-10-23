"""Compiled PR17 final-text UI using actual SDK/auth with synthetic fixed-contract HTTP only."""
import json,tempfile,copy
from pathlib import Path
from playwright.sync_api import sync_playwright
import harmony_auth_persistence as fixture
parent='22222222-2222-4222-8222-222222222222';other='22222222-2222-4222-8222-222222222223'
task_id='33333333-3333-4333-8333-333333333333';target='44444444-4444-4444-8444-444444444444';target_b='44444444-4444-4444-8444-444444444445'
bot_b='11111111-1111-4111-8111-111111111112'
text='<img src="https://example.invalid/no-fetch" onerror="window.__unsafe=true">\n[link](https://example.invalid/) <script>window.__unsafe=true</script>\n'+('长摘要😀 '*200)
while len(text.encode())>2048:text=text[:-1]
base={'task_type':'computer_operation','task_id':task_id,'session_id':parent,'parent_session_id':parent,'created_at':'2026-10-22T01:00:00Z','updated_at':'2026-10-22T01:01:00Z','status':'process_exited','summary':'Synthetic persisted result','content_available':True,'attachments_available':False,'target_id':target,'operation_kind':'herdr_pi_task','revision':'900719925474099312345','cancel_requested':False,'process_state':'exited','pi_result':'succeeded','business_state':'unknown','result_summary':{'source':'pi_final_assistant','text':text,'truncated':True,'thinking':'HIDDEN_THINKING_SENTINEL'},'output':'HIDDEN_OUTPUT_SENTINEL','metadata':{'secret':'HIDDEN_SECRET_SENTINEL'}}
row=copy.deepcopy(base);detail_status=200;requests=[];pending=[]
parent_row={'id':parent,'bot_id':fixture.bot_id,'type':'chat','title':'Synthetic parent','metadata':{'workspace_target_id':target}}
second={**parent_row,'id':other,'title':'Synthetic second'}
def api(path,request):
 if path=='/bots':return 200,{'items':[fixture.bot,{**fixture.bot,'id':bot_b,'name':'Synthetic Bot B'}]}
 if path==f'/bots/{bot_b}':return 200,{**fixture.bot,'id':bot_b,'name':'Synthetic Bot B'}
 if path==f'/bots/{fixture.bot_id}/sessions':return 200,{'items':[parent_row,second]}
 if path==f'/bots/{bot_b}/sessions':return 200,{'items':[]}
 if path==f'/bots/{fixture.bot_id}/sessions/{parent}':return 200,parent_row
 if path==f'/bots/{fixture.bot_id}/sessions/{other}':return 200,second
 if '/tasks' in path:
  requests.append({'path':path,'method':request.method})
  assert request.headers.get('authorization','').startswith('Bearer ')
  if path.endswith('/tasks'):return 200,{'items':[row] if '/'+parent+'/' in path else []}
  if detail_status!=200:return detail_status,{'code':'synthetic.unavailable'}
  return 200,row
 if path.endswith('/messages'):return 200,{'items':[]}
 return None
fixture.extra_api_handler=api
try:
 with tempfile.TemporaryDirectory(prefix='task-lifecycle-',dir=fixture.OUT) as profile,sync_playwright() as pw:
  fixture.current_dist=fixture.new_dist;context,page=fixture.start(pw,Path(profile));fixture.login(page)
  page.wait_for_function("document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.has('workspace-tabs')")
  page.evaluate("id=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('workspace-tabs').openSessionChat({sessionId:id,title:'Synthetic parent'})",parent)
  opener=page.get_by_role('button',name='任务列表',exact=True);opener.wait_for();assert opener.count()==1
  dialog=page.get_by_role('dialog');held=[]
  def hold(route):held.append(route)
  context.route('**/sessions/*/tasks?*',hold)
  opener.evaluate("el=>{el.click();el.click()}")
  page.wait_for_timeout(150);assert len(held)==1
  dialog.get_by_role('alert').get_by_text('读取任务超时',exact=False).wait_for(timeout=18000)
  assert not dialog.get_by_text('正在读取…',exact=True).count()
  assert 'Synthetic persisted result' not in dialog.inner_text()
  assert not dialog.get_by_role('button',name='刷新',exact=True).is_disabled()
  page.screenshot(path=str(fixture.OUT/'task-timeout-390.png'))
  # Deliver after deadline: it must not replace the timeout or repopulate rows.
  for route in held:
   try:route.fulfill(status=200,content_type='application/json',body=json.dumps({'items':[base]}))
   except Exception:pass
  held.clear();page.wait_for_timeout(100)
  assert 'Synthetic persisted result' not in dialog.inner_text()
  context.unroute('**/sessions/*/tasks?*',hold)
  dialog.get_by_role('button',name='刷新',exact=True).click()
  dialog.get_by_text('Synthetic persisted result',exact=True).wait_for()
  assert opener.get_attribute('data-button') is not None
  card=dialog.locator('button[data-slot="item"]');assert card.count()==1
  assert not dialog.locator('button button').count()
  card.focus();page.keyboard.press('Enter')
  dialog.get_by_role('region',name='最终摘要').wait_for()
  page.keyboard.press('Escape');assert not dialog.get_by_role('region',name='最终摘要').count()
  assert dialog.is_visible()
  page.keyboard.press('Escape');assert not dialog.is_visible()
  assert opener.evaluate('el=>el===document.activeElement')
  opener.click();dialog.get_by_text('Synthetic persisted result',exact=True).wait_for()
  dialog.get_by_text('Synthetic persisted result',exact=True).click()
  dialog.get_by_role('region',name='最终摘要').wait_for()
  # Exercise browser pagehide lifecycle; this is not a Harmony OS background test.
  page.evaluate("window.dispatchEvent(new PageTransitionEvent('pagehide'))")
  assert not dialog.is_visible();assert text not in page.locator('body').inner_text()
  count=len(requests);page.evaluate("window.dispatchEvent(new PageTransitionEvent('pageshow'))")
  page.wait_for_timeout(100);assert len(requests)==count
  opener.click();dialog.get_by_text('Synthetic persisted result',exact=True).wait_for();assert len(requests)>count
  fixture.network_offline=True
  dialog.get_by_role('button',name='刷新',exact=True).click()
  dialog.get_by_role('alert').wait_for();assert 'Synthetic persisted result' not in dialog.inner_text()
  fixture.network_offline=False
  dialog.get_by_role('button',name='刷新',exact=True).click();dialog.get_by_text('Synthetic persisted result',exact=True).wait_for()
  context.route('**/tasks/computer_operation/*',hold)
  dialog.get_by_text('Synthetic persisted result',exact=True).click();page.wait_for_timeout(100);assert held
  dialog.get_by_role('button',name='返回父对话',exact=True).click();assert not dialog.is_visible()
  for route in held:
   try:route.fulfill(status=200,content_type='application/json',body=json.dumps(base))
   except Exception:pass
  held.clear();page.wait_for_timeout(100);assert text not in page.locator('body').inner_text()
  context.unroute('**/tasks/computer_operation/*',hold)
  opener.click();dialog.get_by_text('Synthetic persisted result',exact=True).wait_for()
  page.evaluate("document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('settings').theme='dark'")
  page.wait_for_function("document.documentElement.classList.contains('dark')")
  page.wait_for_timeout(250) # Let the existing color transitions reach their target.
  assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
  page.screenshot(path=str(fixture.OUT/'tasks-recovered-dark-390.png'))
  assert not fixture.page_errors
  report={'deployedContract':'6ddf61dcd8199758e4110398645c556a3d6d7375','viewport':'390x844','singleTaskEntry':True,'duplicateOpenOneRequest':True,'actual15SecondTimeout':True,'lateTimeoutResponseDiscarded':True,'refreshAfterTimeout':True,'pagehideClearsContent':True,'pageshowDoesNotSilentlyReuseOrQuery':True,'reopenFreshAuthorizedRead':True,'networkFailureClearsAndRecovers':True,'backDuringRequestDiscardsLateDetail':True,'darkNarrow':True,'pageErrors':fixture.page_errors,'nativeLifecycle':False,'liveAPI':False}
  report.update(sharedButtonAndItem=True,noNestedButtons=True,keyboardEnterOpensDetail=True,escapeDetailToListToChat=True,focusReturnsToOpener=True)
  (fixture.OUT/'task-lifecycle-ui.json').write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report));context.close()
finally:fixture.server.shutdown()
