from pathlib import Path
import json,sys,urllib.request,urllib.error,concurrent.futures,os,signal,subprocess
from playwright.sync_api import sync_playwright,expect
sys.path.insert(0,'/workspace/bot-dialog-handoff');import results_harness as h
p=Path('/workspace/bot-dialog-handoff/saved-result-recovery');f=json.loads((p/'fixture.json').read_text());h.cfg['database']=f['database'];out={'paid_calls':0}
def api(path,body=None):
 req=urllib.request.Request('http://127.0.0.1:28090'+path,data=json.dumps(body).encode() if body is not None else None,headers={'Authorization':'Bearer '+h.auth['access_token'],'Content-Type':'application/json'})
 try:
  with urllib.request.urlopen(req,timeout=20) as r:return r.status,json.load(r)
 except urllib.error.HTTPError as e:return e.code,e.read().decode()
def provider(path='/status'):
 with urllib.request.urlopen('http://127.0.0.1:28120'+path,timeout=5) as r:return json.load(r)
def record():return json.loads(h.sql("SELECT (result->'result_delivery')::text FROM bot_delegations WHERE id='"+f['task_id']+"'"))
def snapshot():
 return {'delegations':h.sql('SELECT count(*) FROM bot_delegations'),'worker_runs':h.sql("SELECT count(*) FROM session_runs WHERE invocation_id='bot-delegation:"+f['task_id']+"'"),'original_execution':json.loads(h.sql("SELECT json_build_object('state',state,'run_id',run_id,'target_session_id',target_session_id,'result',result-'result_delivery')::text FROM bot_delegations WHERE id='"+f['task_id']+"'")),'failed_history':json.loads(h.sql("SELECT coalesce(json_agg(json_build_object('id',run_id,'state',state,'error',error_code) ORDER BY run_id),'[]')::text FROM session_runs WHERE session_id='"+f['source_session']+"' AND state='failed'"))}
h.wait(lambda:api('/bots/'+h.bot)[0]==200)
assert record()==f['before'] and provider()['summary_calls']==0
before=snapshot();endpoint='/bots/'+h.bot+'/sessions/'+f['source_session']+'/delegations/'+f['task_id']+'/delivery/recover';body={'event_id':f['event_id'],'expected_attempt':3}
with sync_playwright() as pw:
 b=pw.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox']);c=b.new_context(viewport={'width':1440,'height':1000});c.add_init_script('localStorage.setItem("token",'+json.dumps(h.auth['access_token'])+');localStorage.setItem("language","en");');page=c.new_page();errors=[];page.on('pageerror',lambda e:errors.append(str(e)))
 page.goto('http://127.0.0.1:28111/bot/local-dialog',wait_until='domcontentloaded');page.locator('textarea:visible').wait_for(timeout=30000)
 try:
  with concurrent.futures.ThreadPoolExecutor(max_workers=4) as ex:responses=list(ex.map(lambda _:api(endpoint,body),range(4)))
  out['concurrent_statuses']=[r[0] for r in responses];assert all(r[0]==202 for r in responses),responses
  h.wait(lambda:provider()['summary_calls']==1)
  held=record()[f['event_id']];assert held['attempt']==4 and len(held['recoveries'])==1 and held['report_state']=='pending',held
  out['inflight']=held
  wrong=api(endpoint,{**body,'expected_attempt':4});assert wrong[0]==409,wrong;out['inflight_new_attempt_status']=wrong[0]
  assert provider()['unexpected_calls']==0
  provider('/release')
  expect(page.get_by_text('RECOVERY_REPORTED: original saved specialist result.',exact=True)).to_be_visible(timeout=30000)
  h.wait(lambda:record()[f['event_id']]['report_state']=='reported')
  pub=record()[f['event_id']];assert pub['attempt']==4 and len(pub['recoveries'])==1
  assert page.get_by_text('Delegation '+f['task_id'],exact=False).count()==0
  page.screenshot(path=str(p/'green-reported.png'))
  out['published_duplicate_status']=api(endpoint,body)[0];assert out['published_duplicate_status']==409
  out['published']=pub;out['counts']=provider();assert out['counts']['summary_calls']==1 and out['counts']['unexpected_calls']==0 and out['counts']['tools_offered']==[0]
  assert snapshot()==before
  out['original_execution_and_failed_history_unchanged']=True
  out['assistant_receipts']=h.sql("SELECT count(*) FROM bot_history_messages WHERE role='assistant' AND metadata->>'task_result_receipt'='bot_delegation:"+f['task_id']+":"+f['event_id']+":4'");assert out['assistant_receipts']=='1'
  pid=int((p/'server.pid').read_text());proc=Path('/proc')/str(pid);assert str(p/'memoh-server').encode() in (proc/'cmdline').read_bytes();os.kill(pid,signal.SIGTERM);h.wait(lambda:not proc.exists() or (proc/'stat').read_text().split()[2]=='Z',15)
  server=subprocess.Popen([str(p/'memoh-server')],cwd='/workspace/Memoh-schedule-release',env={**os.environ,'CONFIG_PATH':str(p/'config.toml')},stdout=(p/'restart-server.log').open('w'),stderr=subprocess.STDOUT,start_new_session=True);(p/'server.pid').write_text(str(server.pid));h.wait(lambda:api('/bots/'+h.bot)[0]==200)
  page.reload(wait_until='domcontentloaded');expect(page.get_by_text('RECOVERY_REPORTED: original saved specialist result.',exact=True)).to_be_visible(timeout=30000);page.wait_for_timeout(1000)
  assert page.get_by_text('RECOVERY_REPORTED: original saved specialist result.',exact=True).count()==1
  assert record()[f['event_id']]==pub and provider()==out['counts'] and snapshot()==before
  assert api(endpoint,body)[0]==409
  page.screenshot(path=str(p/'green-restart.png'));out['restart_same_publication_no_calls']=True;out['page_errors']=errors;assert not errors
 finally:
  provider('/release');(p/'green.json').write_text(json.dumps(out,indent=2));b.close()
print(json.dumps(out,indent=2))
