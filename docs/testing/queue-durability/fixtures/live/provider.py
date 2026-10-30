import json,threading,time
from pathlib import Path
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer
f=json.loads((Path(__file__).parent/'run.json').read_text());gate=threading.Event();state={'a':0,'b':0,'other':0};lock=threading.Lock()
class H(BaseHTTPRequestHandler):
 def log_message(self,*a):pass
 def do_GET(self):
  if self.path=='/release':gate.set()
  self.send_response(200);self.send_header('Content-Type','application/json');self.end_headers();self.wfile.write(json.dumps(state).encode())
 def do_POST(self):
  # A conventional HTTP proxy request is always refused. The fixture has no
  # upstream transport, so it cannot forward a non-loopback model request.
  if self.path.startswith(('http://','https://')):self.send_error(403);return
  d=json.loads(self.rfile.read(int(self.headers['Content-Length'])));messages=[str(m.get('content','')) for m in d.get('messages',[]) if m.get('role')=='user'];last=messages[-1] if messages else ''
  kind='b' if f['b_text'] in last else ('a' if f['a_text'] in last else 'other')
  with lock:state[kind]+=1
  if kind=='other':self.send_error(400,'fixture only accepts its original A/B');return
  if kind=='a' and not gate.wait(180):self.send_error(500,'fixture barrier deadline');return
  text=f[kind+'_reply'];self.send_response(200);self.send_header('Content-Type','text/event-stream');self.end_headers()
  try:
   for delta,finish in [({'role':'assistant','content':text},None),({},'stop')]:self.wfile.write(('data: '+json.dumps({'id':'durability-local','object':'chat.completion.chunk','choices':[{'index':0,'delta':delta,'finish_reason':finish}]})+'\n\n').encode());self.wfile.flush()
   self.wfile.write(b'data: [DONE]\n\n')
  except (BrokenPipeError,ConnectionResetError):pass
 def do_CONNECT(self):self.send_error(403)
ThreadingHTTPServer(('127.0.0.1',28133),H).serve_forever()
