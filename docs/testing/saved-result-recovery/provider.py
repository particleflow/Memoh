import json,threading,time
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer
state={'summary_calls':0,'unexpected_calls':0,'tools_offered':[]};lock=threading.Lock();gate=threading.Event()
class H(BaseHTTPRequestHandler):
 def log_message(self,*args):pass
 def do_GET(self):
  if self.path=='/release':gate.set()
  self.send_response(200);self.send_header('Content-Type','application/json');self.end_headers();self.wfile.write(json.dumps(state).encode())
 def do_POST(self):
  d=json.loads(self.rfile.read(int(self.headers.get('Content-Length','0'))))
  valid=d.get('model')=='local-coordinator' and not d.get('tools') and 'execution state:' in json.dumps(d.get('messages',[]))
  with lock:
   state['summary_calls' if valid else 'unexpected_calls']+=1
   state['tools_offered'].append(len(d.get('tools',[])))
  if not valid:self.send_error(500,'unexpected task execution');return
  gate.wait(90)
  self.send_response(200);self.send_header('Content-Type','text/event-stream');self.end_headers()
  for delta,finish in [({'role':'assistant','content':'RECOVERY_REPORTED: original saved specialist result.'},None),({},'stop')]:
   self.wfile.write(('data: '+json.dumps({'id':'recovery-local','object':'chat.completion.chunk','model':d['model'],'choices':[{'index':0,'delta':delta,'finish_reason':finish}]})+'\n\n').encode());self.wfile.flush()
  self.wfile.write(b'data: [DONE]\n\n')
ThreadingHTTPServer(('127.0.0.1',28120),H).serve_forever()
