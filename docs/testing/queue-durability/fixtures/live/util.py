from pathlib import Path
import json,tomllib,subprocess,urllib.request,urllib.error,time
P=Path('/tmp/queue-durability/live');cfg=tomllib.loads((P/'config.toml').read_text())['postgres'];auth=json.loads(Path('/workspace/bot-dialog-handoff/auth.json').read_text());meta=json.loads((P/'fixture.json').read_text());bot='30000000-0000-4000-8000-000000000001';worker='30000000-0000-4000-8000-000000000002'
def sql(q):
 r=subprocess.run(['docker','exec','-e','PGPASSWORD='+cfg['password'],'memoh-dev-postgres','psql','-h','127.0.0.1','-U',cfg['user'],'-d',cfg['database'],'-At','-c',q],capture_output=True,text=True);assert r.returncode==0,r.stderr;return r.stdout.strip()
def http(path,body=None,method=None,provider=False):
 req=urllib.request.Request((meta['provider'] if provider else meta['base'])+path,data=json.dumps(body).encode() if body is not None else None,headers={'Authorization':'Bearer '+auth['access_token'],'Content-Type':'application/json'},method=method)
 op=urllib.request.build_opener(urllib.request.ProxyHandler({}))
 try:
  with op.open(req,timeout=5) as r:return r.status,json.load(r)
 except urllib.error.HTTPError as e:return e.code,json.loads(e.read())
def wait(fn,timeout=30):
 until=time.monotonic()+timeout
 while time.monotonic()<until:
  v=fn()
  if v:return v
  time.sleep(.05)
 raise AssertionError('local fixture condition deadline')
