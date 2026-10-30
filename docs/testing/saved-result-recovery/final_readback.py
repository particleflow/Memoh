from pathlib import Path
import sys,json,subprocess,os,urllib.request,hashlib
from playwright.sync_api import sync_playwright,expect
sys.path.insert(0,'/workspace/bot-dialog-handoff');import results_harness as h
p=Path('/workspace/bot-dialog-handoff/saved-result-recovery');f=json.loads((p/'fixture.json').read_text());h.cfg['database']=f['database'];green=json.loads((p/'green.json').read_text())
def get(path,provider=False):
 with urllib.request.urlopen(urllib.request.Request(('http://127.0.0.1:28120' if provider else 'http://127.0.0.1:28090')+path,headers={'Authorization':'Bearer '+h.auth['access_token']}),timeout=5) as r:return json.load(r)
proc=subprocess.Popen([str(p/'memoh-server')],cwd='/workspace/Memoh-schedule-release',env={**os.environ,'CONFIG_PATH':str(p/'config.toml')},stdout=(p/'final-server.log').open('w'),stderr=subprocess.STDOUT,start_new_session=True);(p/'server.pid').write_text(str(proc.pid));h.wait(lambda:get('/bots/'+h.bot))
spec=get('/api/swagger.json');path='/bots/{bot_id}/sessions/{session_id}/delegations/{delegation_id}/delivery/recover';assert path in spec['paths']
with sync_playwright() as pw:
 b=pw.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox']);c=b.new_context(viewport={'width':1440,'height':1000});c.add_init_script('localStorage.setItem("token",'+json.dumps(h.auth['access_token'])+');localStorage.setItem("language","en");');page=c.new_page();errors=[];page.on('pageerror',lambda e:errors.append(str(e)))
 page.goto('http://127.0.0.1:28111/bot/local-dialog',wait_until='domcontentloaded');expect(page.get_by_text('RECOVERY_REPORTED: original saved specialist result.',exact=True)).to_be_visible(timeout=30000)
 assert page.get_by_text('RECOVERY_REPORTED: original saved specialist result.',exact=True).count()==1
 assert page.get_by_text('Delegation '+f['task_id'],exact=False).count()==0
 page.screenshot(path=str(p/'final-reported.png'));b.close()
now=json.loads(h.sql("SELECT (result->'result_delivery')::text FROM bot_delegations WHERE id='"+f['task_id']+"'"))[f['event_id']];assert now==green['published'] and get('/status',True)==green['counts']
r={'final_product':'e7f91d6b32c20f22e5edc3331a0a433d589ed9b5','binary_sha256':hashlib.sha256((p/'memoh-server').read_bytes()).hexdigest(),'served_recovery_contract':True,'same_publication':True,'no_additional_model_calls':True,'page_errors':errors};assert not errors;(p/'final-readback.json').write_text(json.dumps(r,indent=2));print(json.dumps(r))
