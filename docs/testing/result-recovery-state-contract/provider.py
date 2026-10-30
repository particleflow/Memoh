import json,threading
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer
state={'completed':0,'unknown':0,'unexpected':0,'tools':[]};lock=threading.Lock()
class H(BaseHTTPRequestHandler):
 def log_message(self,*a):pass
 def do_GET(self):
  self.send_response(200);self.send_header('Content-Type','application/json');self.end_headers();self.wfile.write(json.dumps(state).encode())
 def do_POST(self):
  d=json.loads(self.rfile.read(int(self.headers.get('Content-Length','0'))));texts=[str(m.get('content','')) for m in d.get('messages',[]) if m.get('role')=='user'];latest=next((s for s in reversed(texts) if 'execution state:' in s),'');kind='unknown' if 'execution state: unknown.' in latest else 'completed' if 'execution state: completed.' in latest else 'unexpected'
  if d.get('tools') or d.get('model')!='local-coordinator':kind='unexpected'
  with lock:state[kind]+=1;state['tools'].append(len(d.get('tools',[])))
  if kind=='unexpected':self.send_error(500,'unexpected model request');return
  text='STATE_RECOVERY_UNKNOWN: outcome remains unconfirmed.' if kind=='unknown' else 'STATE_RECOVERY_COMPLETED: saved result recovered after early interruption.'
  self.send_response(200);self.send_header('Content-Type','text/event-stream');self.end_headers()
  for delta,finish in [({'role':'assistant','content':text},None),({},'stop')]:
   self.wfile.write(('data: '+json.dumps({'id':'state-local','object':'chat.completion.chunk','model':d['model'],'choices':[{'index':0,'delta':delta,'finish_reason':finish}]})+'\n\n').encode());self.wfile.flush()
  self.wfile.write(b'data: [DONE]\n\n')
ThreadingHTTPServer(('127.0.0.1',28120),H).serve_forever()
