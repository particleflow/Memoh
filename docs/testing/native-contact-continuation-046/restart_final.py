from pathlib import Path
import sys,json,urllib.request,os,signal,subprocess
sys.path.insert(0,'/workspace/bot-dialog-handoff');import results_harness as h
from playwright.sync_api import sync_playwright,expect
p=Path('/workspace/bot-dialog-handoff/native-contact-continuation');h.cfg['database']='memoh_contact_runtime_20261030';out=json.loads((p/'final-green-browser.json').read_text());old=json.loads((p/'red-browser.json').read_text())
def provider():return json.load(urllib.request.urlopen('http://127.0.0.1:28120/status'))
def record(id):return json.loads(h.sql("SELECT (result->'result_delivery')::text FROM bot_delegations WHERE id='"+id+"'"))
counts=provider();before=record(out['task']['record_id']);failed=record(old['task']['record_id']);assert h.sql("SELECT count(*) FROM session_runs WHERE state IN ('running','accepted','waiting_decision','finishing')")=='0'
pid=int((p/'server.pid').read_text());proc=Path('/proc')/str(pid);assert str(p/'memoh-server-final').encode() in (proc/'cmdline').read_bytes();os.kill(pid,signal.SIGTERM);h.wait(lambda:not proc.exists() or (proc/'stat').read_text().split()[2]=='Z',10)
f=subprocess.Popen([str(p/'memoh-server-final')],cwd='/workspace/Memoh-schedule-release',env={**os.environ,'CONFIG_PATH':str(p/'config.toml')},stdout=(p/'final-restart-server.log').open('w'),stderr=subprocess.STDOUT,start_new_session=True);(p/'server.pid').write_text(str(f.pid))
with sync_playwright() as pw:
 b=pw.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox']);c=b.new_context(viewport={'width':1440,'height':1000});c.add_init_script('localStorage.setItem("token",'+json.dumps(h.auth['access_token'])+');localStorage.setItem("language","en");');page=c.new_page();errors=[];page.on('pageerror',lambda e:errors.append(str(e)))
 page.goto('http://127.0.0.1:28111/bot/local-dialog',wait_until='domcontentloaded');expect(page.get_by_text('CONTINUATION_REPORTED: original specialist result received FINAL',exact=True)).to_be_visible(timeout=30000);page.wait_for_timeout(1500)
 assert page.get_by_text('CONTINUATION_REPORTED: original specialist result received FINAL',exact=True).count()==1
 assert page.get_by_text('CONTINUATION_REPLY_B: main chat answered while specialist held FINAL',exact=True).count()==1
 assert page.get_by_text('Delegation '+out['task']['record_id'],exact=False).count()==0
 page.screenshot(path=str(p/'final-green-reported-restart.png'));b.close()
assert provider()==counts and record(out['task']['record_id'])==before and record(old['task']['record_id'])==failed
result={'same_saved_publication_after_restart':True,'no_model_calls_after_restart':True,'old_attention_unchanged':True,'no_machine_envelope_in_contact':True,'page_errors':errors,'publications':before}
(p/'final-restart.json').write_text(json.dumps(result,indent=2));print(json.dumps(result))
