import json,threading,time,sys
mode=sys.argv[1] if len(sys.argv)>1 else "red"
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer
state={'calls':[],'worker':0,'summary':0,'reply_b':0,'delegation':0};lock=threading.Lock();gates={x:threading.Event() for x in ['main','worker']}
class H(BaseHTTPRequestHandler):
 def log_message(self,*args):pass
 def do_GET(self):
  for key,gate in gates.items():
   if self.path=='/release/'+key:gate.set()
  self.send_response(200);self.send_header('Content-Type','application/json');self.end_headers();self.wfile.write(json.dumps(state).encode())
 def do_POST(self):
  data=json.loads(self.rfile.read(int(self.headers.get('Content-Length','0'))));model=data.get('model','');candidates=[str(m.get('content','')) for m in data.get('messages',[]) if m.get('role')=='user'];latest=next((x for x in reversed(candidates) if '<message ' in x or 'Report this saved' in x or 'execution state:' in x), candidates[-1] if candidates else '')
  call=None;text='LOCAL CONTINUATION fixture';kind='other'
  if model=='local-executor':
   kind='worker'
   with lock:state['worker']+=1
   gates['worker'].wait(90);text='CONTINUATION_WORKER_RESULT: original specialist completed once'
  elif 'execution state:' in latest:
   kind='summary'
   with lock:state['summary']+=1
   assert not data.get('tools'),'result-only turn offered tools'
   text='CONTINUATION_REPORTED: original specialist result received'
  elif 'CONTINUATION_B' in latest:
   kind='reply_b'
   with lock:state['reply_b']+=1
   text='CONTINUATION_REPLY_B: main chat answered while specialist held'
  elif 'CONTINUATION_A' in latest:
   with lock:
    first=state['delegation']==0
    if first:state['delegation']+=1
   if first:
    kind='delegate';call={'index':0,'id':'continuation-delegate-20261030-'+mode,'type':'function','function':{'name':'delegate_to_bot','arguments':json.dumps({'target_bot_id':'30000000-0000-4000-8000-000000000002','message':'CONTINUATION_WORKER: finish only after fixture barrier release'})}}
   else:
    kind='main_final_wait';gates['main'].wait(90);text='CONTINUATION_ACCEPTED: specialist running in background'
  if mode=='green-final' and text.startswith('CONTINUATION_'):text+=' FINAL'
  with lock:state['calls'].append({'kind':kind,'model':model,'at':time.time()})
  delta={'role':'assistant','tool_calls':[call]} if call else {'role':'assistant','content':text};finish='tool_calls' if call else 'stop'
  try:
   self.send_response(200);self.send_header('Content-Type','text/event-stream' if data.get('stream') else 'application/json');self.end_headers()
   if data.get('stream'):
    for d,f in [(delta,None),({},finish)]:self.wfile.write(('data: '+json.dumps({'id':'continuation-local','object':'chat.completion.chunk','model':model,'choices':[{'index':0,'delta':d,'finish_reason':f}]})+'\n\n').encode());self.wfile.flush()
    self.wfile.write(b'data: [DONE]\n\n')
   else:
    if call:call.pop('index')
    self.wfile.write(json.dumps({'id':'continuation-local','model':model,'choices':[{'index':0,'message':delta,'finish_reason':finish}],'usage':{'prompt_tokens':1,'completion_tokens':1,'total_tokens':2}}).encode())
  except (BrokenPipeError,ConnectionResetError):pass
ThreadingHTTPServer(('127.0.0.1',28120),H).serve_forever()
