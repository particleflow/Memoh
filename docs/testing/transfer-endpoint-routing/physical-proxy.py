from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler
import http.client,json,threading,time
from pathlib import Path
out=Path('/workspace/scratch/fast-transfer-path')
lock=threading.Lock(); rows=[]
class Proxy(BaseHTTPRequestHandler):
 protocol_version='HTTP/1.1'
 def log_message(self,*args):pass
 def do_OPTIONS(self):
  self.send_response(204);self.send_header('Access-Control-Allow-Origin','http://127.0.0.1:18683');self.send_header('Access-Control-Allow-Methods','GET,HEAD,POST,PATCH,DELETE,OPTIONS');self.send_header('Access-Control-Allow-Headers',self.headers.get('Access-Control-Request-Headers',''));self.send_header('Content-Length','0');self.end_headers()
 def forward(self):
  started=time.perf_counter(); body=self.rfile.read(int(self.headers.get('Content-Length','0')))
  if not self.path.startswith('/api/bots/'):
   self.send_error(404);return
  headers={k:v for k,v in self.headers.items() if k.lower() not in ['host','connection','origin']}; headers['Host']='127.0.0.1:18680'
  c=http.client.HTTPConnection('127.0.0.1',18680,timeout=30); c.request(self.command,self.path[4:],body,headers);r=c.getresponse();ttfb=time.perf_counter()-started; data=r.read()
  self.send_response(r.status)
  for k,v in r.getheaders():
   if k.lower() not in ['connection','transfer-encoding','access-control-allow-origin']: self.send_header(k,v)
  self.send_header('Access-Control-Allow-Origin','http://127.0.0.1:18683');self.send_header('Access-Control-Expose-Headers','ETag,Content-Range,Content-Length,Content-Type,Upload-Offset');self.end_headers()
  if self.command!='HEAD':self.wfile.write(data)
  c.close()
  with lock:
   rows.append({'method':self.command,'path':self.path,'request_bytes':len(body),'response_bytes':len(data),'status':r.status,'ttfb_ms':ttfb*1000,'duration_ms':(time.perf_counter()-started)*1000,'range':self.headers.get('Range')})
   (out/'physical-requests.json').write_text(json.dumps(rows,indent=2))
 do_GET=do_HEAD=do_POST=do_PATCH=do_DELETE=forward
ThreadingHTTPServer(('127.0.0.1',18684),Proxy).serve_forever()
