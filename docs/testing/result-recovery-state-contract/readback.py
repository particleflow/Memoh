from pathlib import Path
import sys,json,urllib.request,os,signal,subprocess,hashlib
from playwright.sync_api import sync_playwright,expect
sys.path.insert(0,'/workspace/bot-dialog-handoff');import results_harness as h
p=Path('/workspace/bot-dialog-handoff/result-recovery-state-contract');f=json.loads((p/'fixture.json').read_text());h.cfg['database']=f['database'];out={'paid_calls':0,'cases':[]}
def counts():return json.load(urllib.request.urlopen('http://127.0.0.1:28120/status'))
def record(c):return json.loads(h.sql("SELECT (result->'result_delivery')::text FROM bot_delegations WHERE id='"+c['task_id']+"'"))[c['event_id']]
for c in f['cases']:
 r=record(c);assert r['attempt']==2 and r['state']==c['state'] and r['report_state']=='reported' and len(r['recoveries'])==1 and r['automatic_attempts']==7
 n=h.sql("SELECT count(*) FROM session_runs WHERE invocation_id='bot-delegation:"+c['task_id']+"'");assert n==('0' if c['state']=='unknown' else '1')
 receipts=h.sql("SELECT count(*) FROM bot_history_messages WHERE role='assistant' AND metadata->>'task_result_receipt'='bot_delegation:"+c['task_id']+":"+c['event_id']+":2'");assert receipts=='1'
 assert h.sql("SELECT state FROM session_runs WHERE run_id='"+c['previous_run']+"'")=='aborted'
 out['cases'].append({'state':c['state'],'report':r,'original_execution_count':n,'assistant_receipts':receipts,'previous_summary_state':'aborted'})
out['provider']=counts();assert out['provider']=={'completed':1,'unknown':1,'unexpected':0,'tools':[0,0]}
run='a7701780-f0ea-42c7-a65e-b5a058dd220c';r=json.loads(h.sql("SELECT json_build_object('run_id',run_id,'state',state,'error_code',error_code,'input_workspace_target',input_json->>'workspace_target_id')::text FROM session_runs WHERE run_id='"+run+"'"));assert r['state']=='failed' and r['error_code']=='runtime_run_failed' and r['input_workspace_target'] is None
log=(p/'server.log').read_text();assert any('run_id='+run in line and 'runtime_run_failed: remote runtime is offline' in line for line in log.splitlines())
mount=json.loads(Path('/workspace/bot-dialog-handoff/native-contact-continuation/fixture.json').read_text())['mount'];out['remote']={**r,'saved_target':mount,'default_during_probe':'native','diagnostic':'remote runtime is offline','no_model_calls':True,'terminal_observation':'read existing run and its matching server diagnostic; no resend'}
with sync_playwright() as pw:
 b=pw.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox']);c=b.new_context(viewport={'width':1440,'height':1000});c.add_init_script('localStorage.setItem("token",'+json.dumps(h.auth['access_token'])+');localStorage.setItem("language","en");');page=c.new_page();errors=[];page.on('pageerror',lambda e:errors.append(str(e)))
 page.goto('http://127.0.0.1:28111/bot/local-dialog',wait_until='domcontentloaded')
 for text in ['STATE_RECOVERY_UNKNOWN: outcome remains unconfirmed.','STATE_RECOVERY_COMPLETED: saved result recovered after early interruption.']:expect(page.get_by_text(text,exact=True)).to_be_visible(timeout=30000)
 page.screenshot(path=str(p/'readback-reports.png'))
 pid=int((p/'server.pid').read_text());proc=Path('/proc')/str(pid);assert str(p/'memoh-server').encode() in (proc/'cmdline').read_bytes();os.kill(pid,signal.SIGTERM);h.wait(lambda:not proc.exists() or (proc/'stat').read_text().split()[2]=='Z',15)
 child=subprocess.Popen([str(p/'memoh-server')],cwd='/workspace/Memoh-schedule-release',env={**os.environ,'CONFIG_PATH':str(p/'config.toml')},stdout=(p/'restart-server.log').open('w'),stderr=subprocess.STDOUT,start_new_session=True);(p/'server.pid').write_text(str(child.pid))
 def healthy():
  try:
   with urllib.request.urlopen(urllib.request.Request('http://127.0.0.1:28090/bots/'+h.bot,headers={'Authorization':'Bearer '+h.auth['access_token']}),timeout=3) as r:return r.status==200
  except OSError:return False
 h.wait(healthy);page.reload(wait_until='domcontentloaded');expect(page.get_by_text('STATE_RECOVERY_UNKNOWN: outcome remains unconfirmed.',exact=True)).to_be_visible(timeout=30000)
 for text in ['STATE_RECOVERY_UNKNOWN: outcome remains unconfirmed.','STATE_RECOVERY_COMPLETED: saved result recovered after early interruption.']:assert page.get_by_text(text,exact=True).count()==1
 page.screenshot(path=str(p/'restart-reports.png'));b.close();out['page_errors']=errors;assert not errors
assert counts()==out['provider']
for c,r in zip(f['cases'],out['cases']):assert record(c)==r['report']
out['restart_same_reports_no_calls']=True;out['binary_sha256']=hashlib.sha256((p/'memoh-server').read_bytes()).hexdigest();(p/'readback.json').write_text(json.dumps(out,indent=2));print(json.dumps(out,indent=2))
