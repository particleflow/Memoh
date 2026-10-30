from util import *
from urllib.parse import quote
from websockets.sync.client import connect
import signal,os,time
from urllib.parse import quote as escape
lock=None
phase=(P/'phase').read_text();f=json.loads((P/'run.json').read_text());C=f['contact'];base='/bots/'+bot+'/sessions/'+C;out={'phase':phase,'paid_calls':0,'source_commit':'29685958ec896489371a894b6854ee8b2e9df9c5','product_patch':phase!='red','product_commit':'efac3b837b6c5291c09a714dddb4b73251367361','database':cfg['database']}
def save(): (P/(phase+'-result.json')).write_text(json.dumps(out,indent=2))
def inv(i):return http(base+'/invocations/'+quote(i,safe=''))[1]
def initial_ready():
 try:return http(base+'/follow-up-queue')[0]==200
 except Exception:return False
wait(initial_ready)
ws=connect(meta['base'].replace('http:','ws:')+'/bots/'+bot+'/web/ws?token='+quote(auth['access_token'],safe=''),open_timeout=5)
try:
 ws.send(json.dumps({'type':'message','session_id':C,'invocation_id':f['a_invocation'],'text':f['a_text'],'workspace_target_id':'native'}))
 wait(lambda:http('/status',provider=True)[1]['a']==1)
 out['a']=inv(f['a_invocation']);assert out['a']['found'] and out['a']['state']=='running'
 body={'invocation_id':f['b_invocation'],'text':f['b_text']};responses=[http(base+'/follow-up-queue',body) for _ in range(2)];assert all(code==202 for code,v in responses)
 item=responses[0][1];assert responses[1][1]['item_id']==item['item_id'];out['responses']=responses;out['item']=item;out['before']=http(base+'/follow-up-queue')[1];assert len(out['before']['items'])==1
 bInv='follow-up:'+item['item_id'];assert not inv(bInv)['found'];save()
 # In the terminal variant, a real PG row lock prevents the terminal
 # observer from claiming B, while A is allowed to complete normally.
 if phase=='terminal':
  dsn='postgresql://'+escape(cfg['user'],safe='')+':'+escape(cfg['password'],safe='')+'@127.0.0.1:15432/'+cfg['database']+'?sslmode=disable'
  lock=subprocess.Popen(['/tmp/queue-durability/rowlock'],env={**os.environ,'FIXTURE_PG_DSN':dsn,'FIXTURE_BOT':bot,'FIXTURE_SESSION':C},stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
  assert lock.stdout.readline().strip()=='LOCKED'
  http('/release',provider=True)
  wait(lambda:inv(f['a_invocation']).get('state')=='completed')
  assert inv(bInv)['found'] is False
  out['source_completed_before_process_death']=True;save()
 # A real process death before B admission; original item is already stored.
 pid=int((P/'server.pid').read_text());assert 'memoh-server' in Path('/proc/'+str(pid)+'/cmdline').read_text();os.kill(pid,signal.SIGKILL);ws.close();http('/release',provider=True)
 if lock is not None:lock.stdin.write('release\n');lock.stdin.flush();assert lock.wait(timeout=10)==0;lock=None
 binary='/tmp/queue-post-identity/memoh-server-fixed' if phase=='red' else '/tmp/queue-durability/memoh-server'
 env={**os.environ,'CONFIG_PATH':str(P/'config.toml'),'HTTP_PROXY':meta['provider'],'HTTPS_PROXY':meta['provider'],'ALL_PROXY':meta['provider'],'NO_PROXY':'127.0.0.1,localhost,::1'}
 proc=subprocess.Popen([binary],cwd='/workspace/Memoh-schedule-release',env=env,stdout=(P/(phase+'-restart.log')).open('w'),stderr=subprocess.STDOUT,start_new_session=True);(P/'server.pid').write_text(str(proc.pid))
 def ready():
  try:return http(base+'/follow-up-queue')[0]==200
  except Exception:return False
 wait(ready)
 # A is allowed to become lost under the existing generation/reaper rule;
 # the original A model action must never be repeated as a recovery shortcut.
 wait(lambda:inv(f['a_invocation']).get('state') in (['completed'] if phase=='terminal' else ['lost','failed','aborted']),timeout=90)
 out['a_after']=inv(f['a_invocation']);out['after']=http(base+'/follow-up-queue')[1];out['b_after']=inv(bInv);out['model_calls']=http('/status',provider=True)[1]
 if phase=='red':
  assert out['after']['items']==[] and out['b_after']['found'] is False and out['model_calls']['b']==0
  out['status']='CONFIRMED_PENDING_ITEM_LOST';save();print(json.dumps(out,indent=2))
 else:
  done=wait(lambda:(v if (v:=inv(bInv)).get('state')=='completed' else None),timeout=90);out['b_completed']=done
  code,replay=http(base+'/follow-up-queue',body);assert code==202 and replay['item_id']==item['item_id'];out['replay_after_restart']=replay
  history=http('/bots/'+bot+'/messages?session_id='+C+'&limit=100')[1]['items'];matches=[x for x in history if x.get('turn_id')==done['turn_id']];assert f['b_reply'] in json.dumps(matches);out['original_contact_answer_persisted']=True
  out['b_run_count']=sql("SELECT count(*) FROM session_runs WHERE session_id='"+C+"' AND invocation_id='"+bInv+"'");assert out['b_run_count']=='1'
  out['model_calls']=http('/status',provider=True)[1];assert out['model_calls']=={'a':1,'b':1,'other':0};out['status']='PASS';save();print(json.dumps(out,indent=2))
except BaseException as e:
 out['status']='FAILED';out['error']=str(e).replace(auth['access_token'],'[REDACTED]');save();print(json.dumps({'phase':phase,'error':out['error']}));raise
finally:
 ws.close()
 if lock is not None:
  lock.stdin.close();lock.wait(timeout=10)
