"""Compiled deployed final-text UI using actual SDK/auth with synthetic fixed-contract HTTP only."""
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
 with tempfile.TemporaryDirectory(prefix='results-ui-',dir=fixture.OUT) as profile,sync_playwright() as pw:
  fixture.current_dist=fixture.new_dist;context,page=fixture.start(pw,Path(profile));fixture.login(page)
  def open_parent():
   page.wait_for_function("document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.has('workspace-tabs')")
   page.evaluate("id=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('workspace-tabs').openSessionChat({sessionId:id,title:'Synthetic parent'})",parent)
   page.get_by_role('button',name='任务列表',exact=True).wait_for()
  open_parent();opener=page.get_by_role('button',name='任务列表',exact=True);dialog=page.get_by_role('dialog')
  def show_detail():
   opener.click();dialog.get_by_text('Synthetic persisted result',exact=True).wait_for()
   dialog.get_by_text('Synthetic persisted result',exact=True).click();dialog.get_by_role('heading',name='电脑 Agent 任务',exact=True).wait_for()
  def refresh():
   with page.expect_response(lambda res:'/tasks/computer_operation/' in res.url):dialog.get_by_role('button',name='刷新概要',exact=True).click()
   page.wait_for_timeout(50)
  show_detail();region=dialog.get_by_role('region',name='最终摘要')
  assert region.locator('p').first.text_content()==text
  assert not dialog.locator('img,iframe,a,script').count()
  assert not page.evaluate('Boolean(window.__unsafe)')
  assert 'HIDDEN_' not in dialog.inner_text()
  assert 'Pi 调用结果：成功' in dialog.inner_text() and '业务结果未知' in dialog.inner_text()
  assert '摘要已截断，仅显示部分最终文本。' in dialog.inner_text()
  assert not dialog.get_by_role('button',name='取消',exact=True).count() and not dialog.get_by_role('button',name='重试',exact=True).count()
  assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
  page.screenshot(path=str(fixture.OUT/'task-results-internal.png'))
  for outcome,label in [('failed','失败'),('aborted','已中止'),('incomplete','未完整结束'),('not_started','未启动'),('invalid_protocol','协议结果无效'),('output_limit','输出超出限制'),('future_enum','未知')]:
   row={**base,'pi_result':outcome};refresh();assert f'Pi 调用结果：{label}' in dialog.inner_text();assert '业务结果未知' in dialog.inner_text()
   if outcome in ['invalid_protocol','output_limit','future_enum']:assert not region.count();assert '最终摘要暂不可用' in dialog.inner_text()
  row={**base,'result_summary':{'source':'pi_final_assistant','text':'Short final answer','truncated':False}};refresh();assert region.locator('p').first.text_content()=='Short final answer';assert '摘要已截断' not in dialog.inner_text()
  for legacy in [{**base,'result_summary':None,'content_available':False},{k:v for k,v in base.items() if k not in ['result_summary','content_available','pi_result']}]:
   row=legacy;refresh();assert not region.count();assert '最终摘要暂不可用' in dialog.inner_text()
  row=copy.deepcopy(base);refresh()
  for code in [403,404,500]:
   detail_status=code;refresh();dialog.get_by_role('alert').wait_for();assert text not in dialog.inner_text();assert 'Synthetic persisted result' not in dialog.inner_text();assert not region.count()
   dialog.get_by_role('button',name='返回父对话',exact=True).click();detail_status=200;show_detail()
  detail_status=401;refresh();page.wait_for_selector('#username');assert not dialog.is_visible();assert text not in page.locator('body').inner_text();detail_status=200;fixture.login(page);open_parent()
  # Late detail bodies after session, Bot, computer or login-generation changes.
  def hold(route):pending.append(route)
  context.route('**/tasks/computer_operation/*',hold)
  def start_held():
   opener.click();dialog.get_by_text('Synthetic persisted result',exact=True).click();page.wait_for_timeout(120);assert pending
  def deliver_late():
   for route in pending:
    try:route.fulfill(status=200,content_type='application/json',body=json.dumps(row))
    except Exception:pass
   pending.clear();page.wait_for_timeout(100);assert not page.get_by_role('dialog').is_visible();assert text not in page.locator('body').inner_text()
  start_held();page.evaluate("id=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('workspace-tabs').openSessionChat({sessionId:id,title:'Synthetic second'})",other);deliver_late();open_parent()
  start_held();page.evaluate("id=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('chat').selectBot(id)",bot_b);deliver_late()
  page.evaluate("id=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('chat').selectBot(id)",fixture.bot_id);open_parent()
  selection={'botId':fixture.bot_id,'sessionId':parent,'viewId':'synthetic'}
  start_held();page.evaluate("([view,id])=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('chat').setWorkspaceTargetSelection(view,id)",[selection,target_b]);deliver_late()
  count=len(requests);opener.click();dialog.get_by_text('电脑选择尚未保存到服务端。',exact=False).wait_for();page.wait_for_timeout(80);assert len(requests)==count;dialog.get_by_role('button',name='返回父对话',exact=True).click()
  page.evaluate("([view,id])=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('chat').setWorkspaceTargetSelection(view,id)",[selection,target])
  start_held();fixture.logout(page);fixture.login(page);open_parent();deliver_late()
  assert not fixture.page_errors
  assert not any(x.get('path')=='/no-fetch' for x in fixture.blocked)
  assert all(x['method']=='GET' for x in requests)
  report={'scope':__doc__,'candidate':fixture.candidate_label(),'contract':json.loads((fixture.ROOT/'config/memoh_web.lock.json').read_text())['commit'],'viewport':'390x844','utf8TextBytes':len(text.encode()),'pureTextNoHtmlMediaOrLinkFetch':True,'truncatedAndFullLabels':True,'oldFieldsAndUnknownCompatible':True,'piResultNotBusinessCompletion':True,'failedRefreshClearsResultAndRows':[401,403,404,500],'lateScopeRepliesDiscarded':['session','Bot','computer','new-login-generation'],'unsavedComputerBlocksQuery':True,'noFakeManagementOrAttachmentActions':True,'onlyReadRequests':True,'pageErrors':fixture.page_errors,'productionNetwork':False,'nativeDevice':False}
  (fixture.OUT/'task-results-ui.json').write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report));context.close()
finally:fixture.server.shutdown()
