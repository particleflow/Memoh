from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler
import http.client,json,threading,time,urllib.parse
from pathlib import Path
out=Path('/workspace/scratch/fast-transfer-path/entry-coverage');out.mkdir(exist_ok=True)
lock=threading.Lock();rows=[];foreign=[]
class Proxy(BaseHTTPRequestHandler):
 protocol_version='HTTP/1.1'
 def log_message(self,*args):pass
 def cors(self):
  self.send_header('Access-Control-Allow-Origin','http://127.0.0.1:18683');self.send_header('Access-Control-Expose-Headers','ETag,Content-Range,Content-Length,Content-Type,Content-Disposition');self.send_header('Timing-Allow-Origin','http://127.0.0.1:18683')
 def do_OPTIONS(self):
  self.send_response(204);self.cors();self.send_header('Access-Control-Allow-Methods','GET,HEAD,POST,PATCH,DELETE,OPTIONS');self.send_header('Access-Control-Allow-Headers',self.headers.get('Access-Control-Request-Headers',''));self.send_header('Content-Length','0');self.end_headers()
 def forward(self):
  started=time.perf_counter();body=self.rfile.read(int(self.headers.get('Content-Length','0')));parsed=urllib.parse.urlsplit(self.path);query=urllib.parse.parse_qs(parsed.query);safe_query={k:v for k,v in query.items() if k!='token'};safe_path=parsed.path+('?' + urllib.parse.urlencode(safe_query,doseq=True) if safe_query else '')
  if not self.path.startswith('/api/bots/'):
   self.send_error(404);return
  redirect=self.headers.get('X-Connection-Fixture-Redirect')=='1' or 'fixture_redirect' in query
  if redirect:
   status=307;data=b'';headers=[('Location','http://127.0.0.1:18685/foreign.png'),('Content-Length','0')];ttfb=time.perf_counter()-started
  else:
   headers={k:v for k,v in self.headers.items() if k.lower() not in ['host','connection','origin']};headers['Host']='127.0.0.1:18680';c=http.client.HTTPConnection('127.0.0.1',18680,timeout=30);c.request(self.command,self.path[4:],body,headers);r=c.getresponse();ttfb=time.perf_counter()-started;status=r.status;headers=r.getheaders();data=r.read();c.close()
  self.send_response(status)
  for k,v in headers:
   if k.lower() not in ['connection','transfer-encoding','access-control-allow-origin']:self.send_header(k,v)
  if not any(k.lower()=='content-length' for k,v in headers):self.send_header('Content-Length',str(len(data)))
  self.cors();self.end_headers()
  if self.command!='HEAD':self.wfile.write(data)
  with lock:
   rows.append({'method':self.command,'path':safe_path,'has_query_credential':'token' in query,'has_bearer':self.headers.get('Authorization','').startswith('Bearer '),'request_bytes':len(body),'response_bytes':len(data),'status':status,'ttfb_ms':ttfb*1000,'duration_ms':(time.perf_counter()-started)*1000})
   (out/'physical-requests.json').write_text(json.dumps(rows,indent=2))
 do_GET=do_HEAD=do_POST=do_PATCH=do_DELETE=forward
class Foreign(BaseHTTPRequestHandler):
 def log_message(self,*args):pass
 def do_GET(self):
  parsed=urllib.parse.urlsplit(self.path);row={'method':self.command,'path':parsed.path,'query_credential':'token' in urllib.parse.parse_qs(parsed.query),'authorization':self.headers.get('Authorization') is not None};foreign.append(row);(out/'foreign-requests.json').write_text(json.dumps(foreign,indent=2));data=(out/'preview.png').read_bytes();self.send_response(200);self.send_header('Content-Type','image/png');self.send_header('Content-Length',str(len(data)));self.send_header('Access-Control-Allow-Origin','*');self.end_headers();self.wfile.write(data)
threading.Thread(target=ThreadingHTTPServer(('127.0.0.1',18685),Foreign).serve_forever,daemon=True).start()
ThreadingHTTPServer(('127.0.0.1',18684),Proxy).serve_forever()
