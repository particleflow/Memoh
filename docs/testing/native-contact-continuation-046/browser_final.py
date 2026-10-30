import sys,json,time,uuid,urllib.request
from pathlib import Path
from playwright.sync_api import sync_playwright,expect
sys.path.insert(0,'/workspace/bot-dialog-handoff');import results_harness as h
p=Path('/workspace/bot-dialog-handoff/native-contact-continuation');f=json.loads((p/'fixture.json').read_text());h.cfg['database']=f['database'];mode=sys.argv[1];out={'mode':mode,'paid_calls':0};op=urllib.request.build_opener(urllib.request.ProxyHandler({}))
def get(path,model=False):
 with op.open(urllib.request.Request(('http://127.0.0.1:28120' if model else 'http://127.0.0.1:28090')+path,headers={'Authorization':'Bearer '+h.auth['access_token']}),timeout=5) as r:return json.load(r)
def query(q):return h.sql(q)
def wait(fn,timeout=45):return h.wait(fn,timeout)
wait(lambda:get('/bots/'+h.bot))
with sync_playwright() as pw:
 b=pw.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox']);c=b.new_context(viewport={'width':1440,'height':1000});c.add_init_script('localStorage.setItem("token",'+json.dumps(h.auth['access_token'])+');localStorage.setItem("language","en");');page=c.new_page();urls=[];errors=[];page.on('websocket',lambda w:urls.append(w.url) if '/web/ws' in w.url else None);page.on('pageerror',lambda e:errors.append(str(e)))
 page.goto('http://127.0.0.1:28111/bot/local-dialog',wait_until='domcontentloaded');page.locator('textarea:visible').wait_for(timeout=30000);wait(lambda:urls)
 contact=page.evaluate("document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('chat').sessionId");out['contact']=contact
 assert query("SELECT metadata->>'workspace_target_id' FROM bot_sessions WHERE id='"+contact+"'")=='native'
 def send(text,selected=False):
  invocation=str(uuid.uuid4());msg={'type':'message','session_id':contact,'text':text,'invocation_id':invocation}
  if selected:msg['workspace_target_id']='native'
  events=page.evaluate('''({url,msg})=>new Promise((resolve,reject)=>{const ws=new WebSocket(url),events=[];const timer=setTimeout(()=>{ws.close();reject(new Error('timeout '+JSON.stringify(events)))},20000);ws.onopen=()=>ws.send(JSON.stringify(msg));ws.onmessage=e=>{const x=JSON.parse(e.data);if(x.invocation_id!==msg.invocation_id)return;events.push({type:x.type,run_id:x.run_id,code:x.code});if(['run_accepted','run_deferred','error','run_rejected'].includes(x.type)){clearTimeout(timer);ws.close();resolve(events)}};ws.onerror=()=>reject(new Error('socket error'))})''',{'url':urls[0],'msg':msg})
  return {'invocation':invocation,'events':events}
 try:
  out['a']=send('CONTINUATION_A_'+mode,True);assert any(e['type']=='run_accepted' for e in out['a']['events']),out
  wait(lambda:get('/status',True)['worker']==1)
  task=wait(lambda:next((t for t in get('/bots/'+h.bot+'/tasks?limit=100')['items'] if t['task_type']=='bot_delegation' and t['state']=='running' ),None))
  out['task']=task
  with page.expect_response(lambda r:r.request.method=='POST' and '/follow-up-queue' in r.url,timeout=15000) as queued:
   page.locator('textarea:visible').fill('CONTINUATION_B_'+mode);page.locator('textarea:visible').press('Enter')
  response=queued.value;assert response.ok,response.status;out['b']=response.json()
  get('/release/main',True)
  if mode=='red':
   wait(lambda:query("SELECT count(*) FROM session_runs WHERE session_id='"+contact+"' AND created_at > now()-interval '2 minutes' AND state='failed'")!='0')
   assert get('/status',True)['reply_b']==0
  else:
   wait(lambda:get('/status',True)['reply_b']==1)
   expect(page.get_by_text('CONTINUATION_REPLY_B: main chat answered while specialist held FINAL',exact=True).last).to_be_visible(timeout=15000)
   current=get('/bots/'+h.bot+'/tasks/bot_delegation/'+task['record_id']);assert current['state']=='running',current
   page.screenshot(path=str(p/'final-green-parallel.png'));out['reply_while_expert_held']=True
  get('/release/worker',True)
  wait(lambda:get('/bots/'+h.bot+'/tasks/bot_delegation/'+task['record_id'])['state']=='completed')
  if mode=='green':
   expect(page.get_by_text('CONTINUATION_REPORTED: original specialist result received FINAL',exact=True).last).to_be_visible(timeout=25000)
   out['receipt_count']=query("SELECT count(*) FROM bot_history_messages WHERE role='assistant' AND session_id='"+contact+"' AND metadata->>'task_result_receipt' LIKE '%"+task['record_id']+"%'")
   assert out['receipt_count']=='1',out
   page.screenshot(path=str(p/'final-green-reported.png'))
  else:wait(lambda:'attention' in query("SELECT (result->'result_delivery')::text FROM bot_delegations WHERE id='"+task['record_id']+"'"))
  out['delivery']=json.loads(query("SELECT (result->'result_delivery')::text FROM bot_delegations WHERE id='"+task['record_id']+"'"))
  out['provider']=get('/status',True);out['page_errors']=errors;assert not errors
 finally:
  get('/release/main',True);get('/release/worker',True);(p/('final-'+mode+'-browser.json')).write_text(json.dumps(out,indent=2));b.close()
print(json.dumps(out,indent=2))
