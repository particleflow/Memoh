import json, threading, time, http.server, urllib.request, urllib.error, uuid, os
from pathlib import Path
root=Path(os.environ['DELEGATION_TEST_ROOT'])
fixture=Path(os.environ['DELEGATION_EXISTING_FIXTURE'])
base='http://127.0.0.1:18100'
token=(fixture/'private-token').read_text().strip()
seed=json.loads((fixture/'fixture-state.json').read_text())
state={'source':seed['source'],'target':seed['delegation']['target_bot_id']}
state_file=root/'probe-private.json'
if state_file.exists(): raise SystemExit('probe already attempted; inspect saved state instead of redispatch')
def api(path,data=None,method=None):
 req=urllib.request.Request(base+path,data=None if data is None else json.dumps(data).encode(),headers={'Content-Type':'application/json','Authorization':'Bearer '+token},method=method)
 try:
  with urllib.request.urlopen(req,timeout=15) as r:return json.load(r)
 except urllib.error.HTTPError as e:
  v=json.load(e);raise RuntimeError('API status '+str(e.code)+' code '+str(v.get('code'))) from None
calls=[]
class Model(http.server.BaseHTTPRequestHandler):
 def log_message(self,*_): pass
 def do_POST(self):
  data=json.loads(self.rfile.read(int(self.headers['Content-Length'])))
  slow='TIMEOUT_SYNTHETIC' in json.dumps(data.get('messages',[])) and data['model'].endswith('specialist')
  calls.append({'role':data['model'].rsplit('-',1)[-1], 'slow':slow})
  if slow:time.sleep(8)
  try:
   self.send_response(200);self.send_header('Content-Type','text/event-stream');self.end_headers()
   for delta,finish in [({'role':'assistant'},None),({'content':'DELEGATION_SYNTHETIC_FINAL_42'},None),({},'stop')]:
    body={'id':'synthetic','object':'chat.completion.chunk','created':int(time.time()),'model':data['model'],'choices':[{'index':0,'delta':delta,'finish_reason':finish}]}
    self.wfile.write(('data: '+json.dumps(body)+'\n\n').encode());self.wfile.flush()
   self.wfile.write(b'data: [DONE]\n\n')
  except (BrokenPipeError,ConnectionResetError):pass
server=http.server.ThreadingHTTPServer(('127.0.0.1',18289),Model)
threading.Thread(target=server.serve_forever,daemon=True).start()
state_file.write_text(json.dumps(state));state_file.chmod(0o600)
provider=api('/providers',{'name':'delegation-route-config-synthetic','client_type':'openai-completions','config':{'base_url':'http://127.0.0.1:18289/v1','api_key':'synthetic-only'}})
state['provider']=provider['id'];state['cases']=[];state['saved_settings']={}
try:
 for role in ['source','target']:
  old=api('/bots/'+state[role]+'/settings');state['saved_settings'][role]=old
  model=api('/models',{'model_id':'route-'+('supervisor' if role=='source' else 'specialist'),'name':'route '+role,'provider_id':provider['id'],'type':'chat','enable':True,'config':{'compatibilities':['tool-call'],'context_window':131072,'thinking_mode':'none'}})
  api('/bots/'+state[role]+'/settings',{'chat_model_id':model['id'],'chat_runtime':'model','compaction_enabled':False,'reasoning_enabled':False},'PUT')
 state_file.write_text(json.dumps(state))
 for kind in ['normal','timeout']:
  session=api('/bots/'+state['source']+'/sessions',{'type':'chat','title':'Delegation route-config '+kind})
  case={'kind':kind,'session':session['id'],'request_id':str(uuid.uuid4())};state['cases'].append(case);state_file.write_text(json.dumps(state))
  path='/bots/'+state['source']+'/sessions/'+case['session']+'/delegations'
  payload={'request_id':case['request_id'],'target_bot_id':state['target'],'message':'TIMEOUT_SYNTHETIC' if kind=='timeout' else 'Return the synthetic final marker only.', 'return_route':{'channel':'telegram','reply_target':'forged-recipient'}}
  result=api(path,payload);case['id']=result['delegation_id'];state_file.write_text(json.dumps(state))
  deadline=time.monotonic()+30
  while time.monotonic()<deadline:
   result=api(path+'/'+case['id'])
   if result['state'] in ['completed','failed','unknown'] and result['state']!='running':break
   time.sleep(.2)
  case['view']=result;state_file.write_text(json.dumps(state))
  print(json.dumps({'case':kind,'state':result['state'],'delivery_state':result['delivery_state'],'summary_present':bool(result.get('summary')),'model_calls':len(calls)}),flush=True)
  if kind=='normal' and result['state']!='completed':raise RuntimeError('normal real runtime did not complete; no timeout task submitted')
  if kind=='timeout' and result['state']!='unknown':raise RuntimeError('timeout not unknown')
  time.sleep(1)
 (root/'probe-proof.json').write_text(json.dumps({'cases':[{'kind':c['kind'],'state':c['view']['state'],'delivery_state':c['view']['delivery_state'],'summary_present':bool(c['view'].get('summary'))} for c in state['cases']],'model_calls':calls,'business_submits':len(state['cases'])},indent=2))
finally:
 # Only restore the fixture's pre-existing chat settings, never credentials.
 for role, old in state.get('saved_settings',{}).items():
  values={k:old[k] for k in ['chat_model_id','chat_runtime','compaction_enabled','reasoning_enabled'] if k in old}
  try:api('/bots/'+state[role]+'/settings',values,'PUT')
  except Exception:print('fixture settings restore failed; inspect private state',flush=True)
 state_file.write_text(json.dumps(state));server.shutdown()
