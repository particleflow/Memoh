from pathlib import Path
import json,sys,urllib.request,urllib.error
from playwright.sync_api import sync_playwright
sys.path.insert(0,'/workspace/bot-dialog-handoff');import results_harness as h
p=Path('/workspace/bot-dialog-handoff/saved-result-recovery');f=json.loads((p/'fixture.json').read_text());h.cfg['database']=f['database'];op=urllib.request.build_opener(urllib.request.ProxyHandler({}))
def call(path,body=None):
 req=urllib.request.Request('http://127.0.0.1:28090'+path,data=json.dumps(body).encode() if body is not None else None,headers={'Authorization':'Bearer '+h.auth['access_token'],'Content-Type':'application/json'})
 try:
  with op.open(req,timeout=20) as r:return r.status,json.load(r)
 except urllib.error.HTTPError as e:return e.code,e.read().decode()
h.wait(lambda:call('/bots/'+h.bot)[0]==200)
base='/bots/'+h.bot+'/sessions/'+f['source_session']+'/delegations/'+f['task_id']+'/delivery'
code,_=call(base+'/recover',{'event_id':f['event_id'],'expected_attempt':3});assert code==404,code
legacy,_=call(base,{});assert legacy==200,legacy
now=json.loads(h.sql("SELECT (result->'result_delivery')::text FROM bot_delegations WHERE id='"+f['task_id']+"'"));assert now==f['before']
with sync_playwright() as pw:
 b=pw.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox']);c=b.new_context(viewport={'width':1440,'height':1000});c.add_init_script('localStorage.setItem("token",'+json.dumps(h.auth['access_token'])+');localStorage.setItem("language","en");');page=c.new_page();errors=[];page.on('pageerror',lambda e:errors.append(str(e)))
 page.goto('http://127.0.0.1:28111/bot/local-dialog',wait_until='domcontentloaded');page.locator('textarea:visible').wait_for(timeout=30000)
 assert page.get_by_text('RECOVERY_REPORTED: original saved specialist result.',exact=True).count()==0
 page.screenshot(path=str(p/'red-attention.png'));b.close()
result={'baseline':'766a99b1c5a331f595e864554be82d90e2d270ea','recovery_endpoint_status':code,'existing_delivery_status':legacy,'delivery_unchanged':now,'page_errors':errors,'paid_calls':0};(p/'red.json').write_text(json.dumps(result,indent=2));print(json.dumps(result))
