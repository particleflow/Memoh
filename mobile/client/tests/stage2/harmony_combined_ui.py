"""Same compiled conversation: pending native upload and PR17 task summary coexist. Synthetic boundaries only."""
import ast,json,tempfile
from pathlib import Path
from playwright.sync_api import sync_playwright
import harmony_auth_persistence as fixture
# Reuse the existing reviewed synthetic native bridge fixture without executing
# that standalone test's top-level browser workflow.
bridge=next(ast.literal_eval(n.value) for n in ast.parse((Path(__file__).with_name('harmony_resumable_ui.py')).read_text()).body if isinstance(n,ast.Assign) and any(isinstance(t,ast.Name) and t.id=='bridge' for t in n.targets))
parent='22222222-2222-4222-8222-222222222222';child='33333333-3333-4333-8333-333333333333'
row={'task_type':'subagent','task_id':child,'session_id':child,'parent_session_id':parent,'created_at':'2026-10-21T10:00:00Z','updated_at':'2026-10-21T10:00:00Z','status':'unknown','summary':'Synthetic child summary','content_available':False,'attachments_available':False}
parent_row={'id':parent,'bot_id':fixture.bot_id,'type':'chat','title':'Synthetic parent'}
mode='normal';requests=[]
def api(path,request):
 if path==f'/bots/{fixture.bot_id}/sessions':return 200,{'items':[parent_row]}
 if path==f'/bots/{fixture.bot_id}/sessions/{parent}':return 200,parent_row
 if '/tasks' in path:
  requests.append({'path':path,'method':request.method})
  if mode=='revoked':return 403,{'code':'http.forbidden'}
  return 200,{'items':[row]} if path.endswith('/tasks') else row
 if path.endswith('/messages'):return 200,{'items':[]}
 return None
fixture.extra_api_handler=api
try:
 with tempfile.TemporaryDirectory(prefix='combined-ui-',dir=fixture.OUT) as profile,sync_playwright() as pw:
  fixture.current_dist=fixture.new_dist;context,page=fixture.start(pw,Path(profile));context.add_init_script(bridge);page.reload(wait_until='domcontentloaded');fixture.login(page)
  page.wait_for_function("document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.has('workspace-tabs')")
  page.evaluate("id=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('workspace-tabs').openSessionChat({sessionId:id,title:'Synthetic parent'})",parent)
  opener=page.get_by_role('button',name='任务列表',exact=True);opener.wait_for()
  # Runtime hydration is normally delivered over WS. This fixture deliberately
  # completes it without inventing model output, so the attachment control can run.
  page.evaluate("([bot,id])=>{const s=document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('chat');s.chatView({botId:bot,sessionId:id,viewId:'synthetic'}).transcript.loadingMessages.value=false}",[fixture.bot_id,parent])
  page.evaluate("window.__staged=[{handle:'a'.repeat(32),hash:'b'.repeat(64),size:209715200,name:'synthetic-200MiB.bin',type:'application/octet-stream',resumable:true}]")
  page.get_by_role('button',name='恢复本会话待传附件').click();page.get_by_text('synthetic-200MiB.bin',exact=False).wait_for()
  assert not page.evaluate("window.__uploadCalls.some(x=>x.command==='upload.run')")
  opener.click();dialog=page.get_by_role('dialog');dialog.get_by_text('Synthetic child summary',exact=True).click();dialog.get_by_role('heading',name='子 Agent',exact=True).wait_for()
  assert '最终摘要暂不可用' in dialog.inner_text()
  dialog.get_by_role('button',name='父对话',exact=True).click();dialog.wait_for(state='hidden');page.get_by_text('synthetic-200MiB.bin',exact=False).wait_for()
  assert not page.evaluate("window.__uploadCalls.some(x=>['upload.run','upload.discard'].includes(x.command))")
  mode='revoked';opener.click();dialog.get_by_role('alert').wait_for();assert 'Synthetic child summary' not in dialog.inner_text();dialog.get_by_role('button',name='返回父对话',exact=True).click()
  page.get_by_text('synthetic-200MiB.bin',exact=False).wait_for()
  page.get_by_role('button',name='移除',exact=True).click();page.get_by_text('synthetic-200MiB.bin',exact=False).wait_for(state='hidden')
  calls=page.evaluate('window.__uploadCalls');assert any(c['command']=='upload.discard' and c['scope']=='session:'+parent for c in calls)
  assert not any(c['command']=='upload.run' for c in calls)
  fixture.logout(page);assert page.evaluate("window.__uploadCalls.some(x=>x.command==='upload.close')")
  assert not fixture.page_errors
  report={'scope':__doc__,'candidate':fixture.candidate_label(),'sameBundleAndChat':True,'pendingOriginalSurvivesTaskNavigation':True,'task403ClearsOnlyTaskProjection':True,'restorationDoesNotSendModelOrUpload':True,'explicitRemoveUsesSameSessionScope':True,'logoutClosesBoth':True,'nativeRuntimeHydration':'fixture-only','viewport':'390x844','pageErrors':fixture.page_errors,'productionNetwork':False,'nativeDevice':False}
  (fixture.OUT/'combined-ui.json').write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report));context.close()
finally:fixture.server.shutdown()
