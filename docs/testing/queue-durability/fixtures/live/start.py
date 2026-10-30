from util import *
import uuid,os
assert cfg['database']=='memoh_queue_durability_recovered_20261030'
assert sql("SELECT count(*) FROM session_runs WHERE state IN ('accepted','running','waiting_decision','finishing')")=='0'
assert sql('SELECT count(*) FROM schedule WHERE enabled')=='0'
assert sql("SELECT count(*) FROM providers WHERE config->>'base_url' <> 'http://127.0.0.1:28133'")=='0'
f={'contact':'558f2a9e-0d3a-4907-a836-cd7f13901f54','a_invocation':str(uuid.uuid4()),'b_invocation':str(uuid.uuid4())};key=uuid.uuid4().hex
for k in ['a_text','b_text','a_reply','b_reply']:f[k]='DURABLE_QUEUE_'+k.upper()+'_'+key
(P/'run.json').write_text(json.dumps(f,indent=2))
mode=os.environ['FIXTURE_PHASE'];(P/'phase').write_text(mode)
binary='/tmp/queue-post-identity/memoh-server-fixed' if mode=='red' else '/tmp/queue-durability/memoh-server'
env={**os.environ,'CONFIG_PATH':str(P/'config.toml'),'HTTP_PROXY':'http://127.0.0.1:28133','HTTPS_PROXY':'http://127.0.0.1:28133','ALL_PROXY':'http://127.0.0.1:28133','NO_PROXY':'127.0.0.1,localhost,::1'}
for name,cmd in [('provider',['python',str(P/'provider.py')]),('server',[binary])]:
 proc=subprocess.Popen(cmd,cwd='/workspace/Memoh-schedule-release',env=env,stdout=(P/(mode+'-'+name+'.log')).open('w'),stderr=subprocess.STDOUT,start_new_session=True);(P/(name+'.pid')).write_text(str(proc.pid))
print('Started isolated phase '+mode+'; all configured models loopback; outbound HTTP proxy refuses forwarding')
