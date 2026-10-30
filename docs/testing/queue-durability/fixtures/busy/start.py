from util import *
import uuid,os
assert cfg['database']=='memoh_queue_durability_recovered_20261030'
assert sql('SELECT count(*) FROM schedule WHERE enabled')=='0'
assert sql("SELECT count(*) FROM session_runs WHERE state IN ('accepted','running','waiting_decision','finishing')")=='0'
sql("UPDATE providers SET config=jsonb_set(config,'{base_url}','\"http://127.0.0.1:28133\"')")
assert sql("SELECT count(*) FROM providers WHERE config->>'base_url' <> 'http://127.0.0.1:28133'")=='0'
key=uuid.uuid4().hex
contact=json.loads(Path('/workspace/bot-dialog-handoff/native-contact-continuation/final-green-browser.json').read_text())['contact']
run={'contact':contact,'source_bot':bot,'worker_bot':worker,'a_invocation':str(uuid.uuid4()),'b_invocation':str(uuid.uuid4()),'tool_id':'queue-seam-'+key}
for k in ['a_text','b_text','worker_text','a_reply','b_reply','worker_reply','summary_reply']:run[k]='QUEUE_SEAM_'+k.upper()+'_'+key
(P/'run.json').write_text(json.dumps(run,indent=2))
before={}
for table,keycol in [('bot_history_messages','id'),('session_runs','run_id'),('bot_delegations','id')]:
 before[table]=json.loads(sql("SELECT coalesce(json_agg("+keycol+"),'[]') FROM "+table))
(P/'original-ids.json').write_text(json.dumps(before))
for name,command in [('provider',['python',str(P/'provider.py')]),('server',['/tmp/queue-durability/memoh-server'])]:
 proc=subprocess.Popen(command,cwd='/workspace/Memoh-schedule-release',env={**os.environ,'CONFIG_PATH':str(P/'config.toml'),'HTTP_PROXY':meta['provider'],'HTTPS_PROXY':meta['provider'],'ALL_PROXY':meta['provider'],'NO_PROXY':'127.0.0.1,localhost,::1'},stdout=(P/(name+'.log')).open('w'),stderr=subprocess.STDOUT,start_new_session=True);(P/(name+'.pid')).write_text(str(proc.pid))
print('Started only isolated server and loopback model fixture; existing credentials reused')
