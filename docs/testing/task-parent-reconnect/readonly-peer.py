import json, threading, http.server, sys, hashlib, time
from pathlib import Path
from urllib.parse import urlsplit
sys.path.insert(0, '/tmp/task-delivery-ui')
from db import sql
root = Path('/tmp/task-management-reconnect')
f = json.loads(Path('/tmp/task-native-content/private-fixture.json').read_text())
old = json.loads(Path('/tmp/task-native-observation/private-connection.json').read_text())
assert old['auth_type'] == 'none' and urlsplit(old['config']['url']).hostname in ('localhost', '127.0.0.1')
def q(v): return "'" + str(v).replace("'", "''") + "'"
def snapshot(): return sql('select row_to_json(x) from (select * from computer_operations where id='+q(f['operation'])+') x')
before = snapshot(); op = json.loads(before)
profile = json.loads(json.dumps(old['provider_config'])); profile['agent'].pop('execution_mode')
target = {k:profile[k] for k in ['connection_id','device_id','instance_id','session','work_dir','bash_path']}
target['agent'] = {k:profile['agent'][k] for k in ['execution_mode','provider','model','config_dir','path'] if profile['agent'].get(k)}
assert hashlib.sha256(json.dumps(target,separators=(',',':'),ensure_ascii=False).encode()).hexdigest() == op['target_fingerprint']
assert json.loads(sql('select config from mcp_connections where id='+q(old['connection_id']))) == old['config']
assert json.loads(sql('select provider_config from user_runtimes where id='+q(old['runtime_id']))) == old['provider_config']
refs = op['details']['agent']; calls = []
config = {**old['config'], 'url':'http://127.0.0.1:18489/mcp'}
class Handler(http.server.BaseHTTPRequestHandler):
 def log_message(self, *_): pass
 def do_GET(self): self.send_response(405); self.end_headers()
 def do_POST(self):
  request = json.loads(self.rfile.read(int(self.headers.get('Content-Length','0')))); method = request.get('method')
  if 'id' not in request: self.send_response(202); self.end_headers(); return
  if method == 'initialize': result = {'protocolVersion':'2025-03-26','capabilities':{'tools':{}},'serverInfo':{'name':'synthetic-readonly-herdr','version':'1'}}
  elif method == 'tools/list': result = {'tools':[{'name':'execute','description':'Read-only synthetic native fixture','inputSchema':{'type':'object','properties':{'code':{'type':'string'}},'required':['code']}}]}
  elif method == 'tools/call':
   assert request['params']['name'] == 'execute'
   code = request['params']['arguments']['code']; assert code.startswith('return await call(') and code.endswith(');')
   _, native, args = json.loads('['+code[len('return await call('):-2]+']')
   calls.append(native); (root/'native-methods.json').write_text(json.dumps(calls))
   assert native in ['workspace.list','pane.get','agent.get','agent.read'], 'No execution allowed'
   agent = {'name':refs['name'],'agent':'pi','workspace_id':refs['workspace_id'],'pane_id':refs['pane_id'],'terminal_id':refs['terminal_id'],'agent_status':'working','interactive_ready':True,'completion_seq':5,'agent_session':refs['agent_session']}
   if native == 'agent.read':
    assert args['lines'] == 200 and args['source'] == 'recent-unwrapped'
    receipt = {'execution':'accepted','result':{'session':args['session'],'output':'SYNTHETIC_READONLY_RUNNING <script>not executable</script>\nNo business dispatch in this check.'}}
   else:
    if native == 'workspace.list': body = {'workspaces':[{'workspace_id':refs['workspace_id'],'label':'Synthetic observation workspace','pane_count':1,'agent_status':'working'}]}
    elif native == 'pane.get': body = {'pane':{'pane_id':refs['pane_id'],'workspace_id':refs['workspace_id'],'terminal_id':refs['terminal_id']}}
    else: body = {'agent':agent,'type':'agent'}
    receipt = {'execution':'accepted','result':{'id':'cli:agent:get','session':args['session'],'result':body}}
   result = {'content':[{'type':'text','text':json.dumps(receipt)}]}
  else:
   self.send_response(405); self.end_headers(); return
  self.send_response(200); self.send_header('Content-Type','application/json'); self.end_headers()
  self.wfile.write(json.dumps({'jsonrpc':'2.0','id':request['id'],'result':result}).encode())
server = http.server.ThreadingHTTPServer(('127.0.0.1',18489),Handler)
sql('BEGIN; UPDATE mcp_connections SET config='+q(json.dumps(config))+'::jsonb WHERE id='+q(old['connection_id'])+' AND config='+q(json.dumps(old['config']))+'::jsonb; UPDATE user_runtimes SET provider_config='+q(json.dumps(profile))+'::jsonb WHERE id='+q(old['runtime_id'])+' AND provider_config='+q(json.dumps(old['provider_config']))+'::jsonb; COMMIT;')
threading.Thread(target=server.serve_forever,daemon=True).start()
print('READY existing isolated PG operation with same identity; synthetic native GET/read only',flush=True)
try:
 deadline = time.monotonic()+420
 while not (root/'stop-peer').exists() and time.monotonic()<deadline: time.sleep(.2)
finally:
 server.shutdown(); server.server_close()
 sql('BEGIN; UPDATE mcp_connections SET config='+q(json.dumps(old['config']))+'::jsonb WHERE id='+q(old['connection_id'])+' AND config='+q(json.dumps(config))+'::jsonb; UPDATE user_runtimes SET provider_config='+q(json.dumps(old['provider_config']))+'::jsonb WHERE id='+q(old['runtime_id'])+' AND provider_config='+q(json.dumps(profile))+'::jsonb; COMMIT;')
 assert snapshot() == before
 assert json.loads(sql('select config from mcp_connections where id='+q(old['connection_id']))) == old['config']
 assert json.loads(sql('select provider_config from user_runtimes where id='+q(old['runtime_id']))) == old['provider_config']
 proof = {'operation_unchanged':True,'configuration_restored':True,'native_methods':calls,'business_dispatches':0,'real_pc_calls':0}
 (root/'pg-proof.json').write_text(json.dumps(proof,indent=2))
 print('PASS original operation byte-for-byte unchanged; synthetic configuration restored; no prompt/start/run/close',flush=True)
