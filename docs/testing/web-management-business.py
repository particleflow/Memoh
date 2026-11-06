import asyncio,threading,json,time,http.server,urllib.request,urllib.error,uuid,subprocess
from pathlib import Path
import websockets
root=Path('/tmp/web-management-checks');token=(root/'private-token').read_text();base='http://127.0.0.1:18100'
def request(path,data=None,method=None):
 req=urllib.request.Request(base+path,data=None if data is None else json.dumps(data).encode(),headers={'Content-Type':'application/json','Authorization':'Bearer '+token},method=method)
 try:
  with urllib.request.urlopen(req,timeout=20) as r:return json.load(r)
 except urllib.error.HTTPError as e:
  body=json.load(e);raise RuntimeError(json.dumps({'path':path,'status':e.code,'code':body.get('code')})) from None
calls=[]
class Model(http.server.BaseHTTPRequestHandler):
 def log_message(self,*_):pass
 def do_POST(self):
  data=json.loads(self.rfile.read(int(self.headers['Content-Length'])))
  messages=data.get('messages',[]);ids=[m.get('tool_call_id') for m in messages if m.get('role')=='tool']
  last=next((str(m.get('content','')) for m in reversed(messages) if m.get('role')=='user'),'')
  child='DOWNSTREAM_MANAGEMENT_PROBE' in last;role='child' if child else 'specialist' if data['model'].endswith('specialist') else 'followup' if 'execution state:' in last else 'supervisor'
  call=None
  if role=='child':text='DOWNSTREAM_VISIBLE_FINAL_42'
  elif role=='specialist':
   if 'spawn-management' not in ids:call=('spawn-management','spawn_agent',{'id':'management_probe','task':'DOWNSTREAM_MANAGEMENT_PROBE: report 42','run_in_background':False})
   else:text='SPECIALIST_MANAGEMENT_FINAL_42'
  elif role=='followup':text='SUPERVISOR_MANAGEMENT_RESULT_42'
  elif 'delegate-management' not in ids:call=('delegate-management','delegate_to_bot',{'target_bot_id':state['target'],'message':'Run one synthetic downstream Agent and return its result.'})
  else:text='SUPERVISOR_MANAGEMENT_DISPATCHED'
  tools=[t['function']['name'] for t in data.get('tools',[]) if 'function' in t]
  if call and call[1] not in tools:raise RuntimeError('Required tool absent: '+call[1])
  calls.append({'sequence':len(calls)+1,'role':role,'tool':call[1] if call else None,'result_ids':ids})
  (root/'model-calls.json').write_text(json.dumps(calls,indent=2)+'\n')
  self.send_response(200);self.send_header('Content-Type','text/event-stream');self.end_headers()
  def chunk(delta,finish=None):
   body={'id':'management-fixture','object':'chat.completion.chunk','created':int(time.time()),'model':data['model'],'choices':[{'index':0,'delta':delta,'finish_reason':finish}]}
   self.wfile.write(('data: '+json.dumps(body)+'\n\n').encode());self.wfile.flush()
  chunk({'role':'assistant'})
  if call:chunk({'tool_calls':[{'index':0,'id':call[0],'type':'function','function':{'name':call[1],'arguments':json.dumps(call[2])}}]});chunk({},'tool_calls')
  else:chunk({'content':text});chunk({},'stop')
  self.wfile.write(b'data: [DONE]\n\n')
server=http.server.ThreadingHTTPServer(('127.0.0.1',18289),Model);threading.Thread(target=server.serve_forever,daemon=True).start()
state=json.loads((root/'bots.json').read_text())
provider=request('/providers',{'name':'web-management-synthetic','client_type':'openai-completions','config':{'base_url':'http://127.0.0.1:18289/v1','api_key':'synthetic'}})
state['provider']=provider['id']
for role in ['source','target']:
 model=request('/models',{'model_id':'management-'+('supervisor' if role=='source' else 'specialist'),'name':'management '+role,'provider_id':provider['id'],'type':'chat','enable':True,'config':{'compatibilities':['tool-call'],'context_window':131072,'thinking_mode':'none'}})
 request('/bots/'+state[role]+'/settings',{'chat_model_id':model['id'],'chat_runtime':'model','compaction_enabled':False,'reasoning_enabled':False,'tool_approval_config':{'enabled':False,'read':{'mode':'allow'},'write':{'mode':'allow'},'exec':{'mode':'allow'}}},'PUT')
session=request('/bots/'+state['source']+'/sessions',{'type':'chat','title':'Web management actual delegation'})
state['session']=session['id'];(root/'business-state.json').write_text(json.dumps(state,indent=2)+'\n')
print('READY synthetic provider; real server/PG/session/task tools',flush=True)
async def run():
 async with websockets.connect('ws://127.0.0.1:18100/bots/'+state['source']+'/web/ws',additional_headers={'Authorization':'Bearer '+token}) as ws:
  invocation=str(uuid.uuid4());state['invocation']=invocation;(root/'business-state.json').write_text(json.dumps(state,indent=2)+'\n')
  await ws.send(json.dumps({'type':'message','session_id':state['session'],'invocation_id':invocation,'text':'Delegate the synthetic management check and report the result.','workspace_target_id':'native'}))
  deadline=time.monotonic()+110
  while time.monotonic()<deadline:
   try: event=json.loads(await asyncio.wait_for(ws.recv(),.5))
   except asyncio.TimeoutError:event={}
   if event.get('type')=='error':print('WS error code='+str(event.get('code')),flush=True)
   views=request('/bots/'+state['source']+'/sessions/'+state['session']+'/delegations')['items']
   if views and views[0]['state'] in ['completed','failed','unknown']:
    state['delegation']=views[0];(root/'business-state.json').write_text(json.dumps(state,indent=2)+'\n')
    if views[0]['delivery_state']=='accepted' and any(c['role']=='followup' for c in calls):
     print(json.dumps({'state':views[0]['state'],'delivery':views[0]['delivery_state'],'model_calls':len(calls),'child_calls':sum(c['role']=='child' for c in calls)}),flush=True);return
  raise RuntimeError('bounded delegation deadline; no resubmission')
try:asyncio.run(run())
except BaseException:
 (root/'business-state.json').write_text(json.dumps(state,indent=2)+'\n');raise
# Keep only the synthetic provider available for the separately verified retry.
(root/'business-complete').write_text('complete\n')
while not (root/'stop-provider').exists():time.sleep(.25)
server.shutdown()
