from pathlib import Path
import sys,json,urllib.request,urllib.error,uuid,os,signal,subprocess
from playwright.sync_api import sync_playwright,expect
sys.path.insert(0,'/workspace/bot-dialog-handoff');import results_harness as h
p=Path('/workspace/bot-dialog-handoff/result-recovery-state-contract');f=json.loads((p/'fixture.json').read_text());h.cfg['database']=f['database'];out={'paid_calls':0,'cases':[]}
def api(path,body=None):
 req=urllib.request.Request('http://127.0.0.1:28090'+path,data=json.dumps(body).encode() if body is not None else None,headers={'Authorization':'Bearer '+h.auth['access_token'],'Content-Type':'application/json'})
 try:
  with urllib.request.urlopen(req,timeout=20) as r:return r.status,json.load(r)
 except urllib.error.HTTPError as e:return e.code,e.read().decode()
def counts():return json.load(urllib.request.urlopen('http://127.0.0.1:28120/status'))
def record(c):return json.loads(h.sql("SELECT (result->'result_delivery')::text FROM bot_delegations WHERE id='"+c['task_id']+"'"))[c['event_id']]
h.wait(lambda:api('/bots/'+h.bot)[0]==200);assert counts()=={'completed':0,'unknown':0,'unexpected':0,'tools':[]}
with sync_playwright() as pw:
 b=pw.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox']);context=b.new_context(viewport={'width':1440,'height':1000});context.add_init_script('localStorage.setItem("token",'+json.dumps(h.auth['access_token'])+');localStorage.setItem("language","en");');page=context.new_page();errors=[];urls=[];page.on('pageerror',lambda e:errors.append(str(e)));page.on('websocket',lambda w:urls.append(w.url) if '/web/ws' in w.url else None)
 page.goto('http://127.0.0.1:28111/bot/local-dialog',wait_until='domcontentloaded');page.locator('textarea:visible').wait_for(timeout=30000)
 for c in f['cases']:
  before=record(c);assert before['attempt']==1 and before['report_state']=='attention'
  endpoint='/bots/'+h.bot+'/sessions/'+f['source_session']+'/delegations/'+c['task_id']+'/delivery/recover';body={'event_id':c['event_id'],'expected_attempt':before['attempt']};status,v=api(endpoint,body);assert status==202,(status,v)
  text='STATE_RECOVERY_UNKNOWN: outcome remains unconfirmed.' if c['state']=='unknown' else 'STATE_RECOVERY_COMPLETED: saved result recovered after early interruption.'
  expect(page.get_by_text(text,exact=True)).to_be_visible(timeout=30000);h.wait(lambda:record(c)['report_state']=='reported');r=record(c)
  assert r['attempt']==2 and r['state']==c['state'] and len(r['recoveries'])==1 and r['automatic_attempts']==7
  assert api(endpoint,body)[0]==409
  executions=h.sql("SELECT count(*) FROM session_runs WHERE invocation_id='bot-delegation:"+c['task_id']+"'");assert executions==('0' if c['state']=='unknown' else '1')
  out['cases'].append({'state':c['state'],'event':c['event_id'],'report':r,'original_execution_count':executions,'published_duplicate_status':409})
 page.screenshot(path=str(p/'state-reports.png'));out['provider']=counts();assert out['provider']=={'completed':1,'unknown':1,'unexpected':0,'tools':[0,0]}
 # A saved remote mount remains selected even when the default becomes Native.
 mount=json.loads(Path('/workspace/bot-dialog-handoff/native-contact-continuation/fixture.json').read_text())['mount'];session=f['source_session'];metadata=json.loads(h.sql("SELECT metadata::text FROM bot_sessions WHERE id='"+session+"'"));primary=h.sql("SELECT is_primary FROM bot_remote_runtime_bindings WHERE id='"+mount+"'")
 def lit(s):return "'"+s.replace("'","''")+"'"
 updated={**metadata,'workspace_target_id':mount,'workspace_target':{'target_id':mount,'kind':'remote','name':'Saved remote fixture'}}
 try:
  h.sql("UPDATE bot_sessions SET metadata="+lit(json.dumps(updated))+" WHERE id='"+session+"';UPDATE bot_remote_runtime_bindings SET is_primary=false WHERE id='"+mount+"'")
  page.reload(wait_until='domcontentloaded');page.locator('textarea:visible').wait_for(timeout=30000);h.wait(lambda:urls)
  invocation=str(uuid.uuid4());msg={'type':'message','session_id':session,'text':'REMOTE_TARGET_RESTORE_PROBE','invocation_id':invocation}
  events=page.evaluate('''({url,msg})=>new Promise((resolve,reject)=>{const ws=new WebSocket(url),events=[];const timer=setTimeout(()=>{ws.close();resolve(events)},15000);ws.onopen=()=>ws.send(JSON.stringify(msg));ws.onmessage=e=>{const x=JSON.parse(e.data);if(x.invocation_id!==msg.invocation_id)return;events.push(x);if(['run_failed','error','run_rejected'].includes(x.type)){clearTimeout(timer);ws.close();resolve(events)}};ws.onerror=()=>reject(new Error('socket'))})''',{'url':urls[-1],'msg':msg})
  assert any(e.get('type') in ['run_failed','error','run_rejected'] for e in events),events
  assert counts()==out['provider'],'fell back to native model'
  assert h.sql("SELECT metadata->>'workspace_target_id' FROM bot_sessions WHERE id='"+session+"'")==mount
  out['remote']={'saved_target':mount,'default_target':'native','events':[{'type':e.get('type'),'code':e.get('code'),'run_id':e.get('run_id'),'message':e.get('message')} for e in events],'no_model_calls':True,'recorded_target_unchanged':True};page.screenshot(path=str(p/'remote-unavailable.png'))
 finally:
  h.sql("UPDATE bot_sessions SET metadata="+lit(json.dumps(metadata))+" WHERE id='"+session+"';UPDATE bot_remote_runtime_bindings SET is_primary="+('true' if primary=='t' else 'false')+" WHERE id='"+mount+"'")
 pid=int((p/'server.pid').read_text());proc=Path('/proc')/str(pid);assert str(p/'memoh-server').encode() in (proc/'cmdline').read_bytes();os.kill(pid,signal.SIGTERM);h.wait(lambda:not proc.exists() or (proc/'stat').read_text().split()[2]=='Z',15)
 server=subprocess.Popen([str(p/'memoh-server')],cwd='/workspace/Memoh-schedule-release',env={**os.environ,'CONFIG_PATH':str(p/'config.toml')},stdout=(p/'restart-server.log').open('w'),stderr=subprocess.STDOUT,start_new_session=True);(p/'server.pid').write_text(str(server.pid));h.wait(lambda:api('/bots/'+h.bot)[0]==200)
 page.reload(wait_until='domcontentloaded');expect(page.get_by_text('STATE_RECOVERY_UNKNOWN: outcome remains unconfirmed.',exact=True)).to_be_visible(timeout=30000)
 assert counts()==out['provider']
 for c,r in zip(f['cases'],out['cases']):assert record(c)==r['report']
 page.screenshot(path=str(p/'restart-reports.png'));out['restart_no_republication']=True;out['page_errors']=errors;assert not errors;b.close()
(p/'browser.json').write_text(json.dumps(out,indent=2));print(json.dumps(out,indent=2))
