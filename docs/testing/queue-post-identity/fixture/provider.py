import json,threading,time
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer
from pathlib import Path
f=json.loads((Path(__file__).parent/'run.json').read_text());gates={k:threading.Event() for k in ['main','worker','b']};lock=threading.Lock();state={'calls':[],'delegate':0,'worker':0,'b':0,'summary':0,'other':0}
class H(BaseHTTPRequestHandler):
 def log_message(self,*a):pass
 def do_GET(self):
  if self.path.startswith('/release/') and self.path[9:] in gates:gates[self.path[9:]].set()
  self.send_response(200);self.send_header('Content-Type','application/json');self.end_headers();self.wfile.write(json.dumps(state).encode())
 def do_POST(self):
  d=json.loads(self.rfile.read(int(self.headers['Content-Length'])));msgs=[str(m.get('content','')) for m in d.get('messages',[]) if m.get('role')=='user'];latest=next((x for x in reversed(msgs) if '<message ' in x or 'execution state:' in x),msgs[-1] if msgs else '')
  kind='other';call=None;text='FIXTURE_UNEXPECTED'
  if d.get('model')=='local-executor':kind='worker';text=f['worker_reply']
  elif 'execution state:' in latest:kind='summary';text=f['summary_reply'];assert not d.get('tools')
  elif f['b_text'] in latest:kind='b';text=f['b_reply']
  elif f['a_text'] in latest:
   with lock:
    if state['delegate']==0:state['delegate']=1;kind='delegate';call={'index':0,'id':f['tool_id'],'type':'function','function':{'name':'delegate_to_bot','arguments':json.dumps({'target_bot_id':f['worker_bot'],'message':f['worker_text']})}}
    else:kind='main';text=f['a_reply']
  with lock:
   if kind in ['worker','b','summary','other']:state[kind]+=1
   state['calls'].append({'kind':kind,'at':time.time()})
  if kind in gates and not gates[kind].wait(120):self.send_error(500,'fixture barrier timed out');return
  if kind=='other':self.send_error(500,'unexpected fixture model request');return
  delta={'role':'assistant','tool_calls':[call]} if call else {'role':'assistant','content':text};finish='tool_calls' if call else 'stop'
  try:
   self.send_response(200);self.send_header('Content-Type','text/event-stream' if d.get('stream') else 'application/json');self.end_headers()
   if d.get('stream'):
    for v,z in [(delta,None),({},finish)]:self.wfile.write(('data: '+json.dumps({'id':'queue-local','object':'chat.completion.chunk','model':d['model'],'choices':[{'index':0,'delta':v,'finish_reason':z}]})+'\n\n').encode());self.wfile.flush()
    self.wfile.write(b'data: [DONE]\n\n')
   else:
    if call:call.pop('index')
    self.wfile.write(json.dumps({'id':'queue-local','model':d['model'],'choices':[{'index':0,'message':delta,'finish_reason':finish}],'usage':{'prompt_tokens':1,'completion_tokens':1,'total_tokens':2}}).encode())
  except (BrokenPipeError,ConnectionResetError):pass
ThreadingHTTPServer(('127.0.0.1',28133),H).serve_forever()
