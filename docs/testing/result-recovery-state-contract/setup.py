from pathlib import Path
import sys,json,uuid,hashlib,tomllib,subprocess,os
sys.path.insert(0,'/workspace/bot-dialog-handoff');import results_harness as h
p=Path('/workspace/bot-dialog-handoff/result-recovery-state-contract');h.cfg['database']='memoh_saved_result_recovery_20261030';old=Path('/workspace/bot-dialog-handoff/saved-result-recovery');original=json.loads((old/'fixture.json').read_text());source=original['source_session'];target='30000000-0000-4000-8000-000000000002'
assert h.sql("SELECT count(*) FROM session_runs WHERE state IN ('accepted','running','waiting_decision','finishing')")=='0'
user=h.sql("SELECT created_by_user_id FROM bot_sessions WHERE id='"+source+"'")
team=h.sql("SELECT team_id FROM bot_sessions WHERE id='"+source+"'")
raw_sql=h.sql
h.sql=lambda q:raw_sql("SET memoh.team_id='"+team+"';"+q).removeprefix('SET\n')
def lit(v):return "'"+v.replace("'","''")+"'"
cases=[]
for state in ['completed','unknown']:
 task,child,run,previous=[str(uuid.uuid4()) for _ in range(4)]
 h.sql(f"INSERT INTO bot_sessions(id,bot_id,created_by_user_id,metadata) VALUES('{child}','{target}','{user}','{{\"workspace_target_id\":\"native\"}}')")
 if state=='completed':h.sql(f"INSERT INTO session_runs(run_id,bot_id,session_id,invocation_id,turn_id,turn_position,state,input_json,input_fingerprint) VALUES('{run}','{target}','{child}','bot-delegation:{task}','{run}',1,'completed','{{}}','isolated-state-fixture')")
 text=f'Delegation {task} (Bot {target}) execution state: {state}.'
 if state=='completed':text+='\n\nSTATE_SAVED_RESULT'
 key=hashlib.sha256(json.dumps([state,run,text],separators=(',',':')).encode()).hexdigest()
 ref=f'bot_delegation:{task}:{key}:1';ns=uuid.NAMESPACE_URL;inv='follow-up:'+str(uuid.uuid5(ns,'unused'))
 # Go uuid.NewSHA1 uses RFC4122 version5 with SHA1, matching uuid5.
 inv='follow-up:'+str(uuid.uuid5(ns,'memoh/host-follow-up/v1\0'+h.bot+'\0'+source+'\0'+ref))
 h.sql(f"INSERT INTO session_runs(run_id,bot_id,session_id,invocation_id,turn_id,turn_position,state,input_json,input_fingerprint) VALUES('{previous}','{h.bot}','{source}',{lit(inv)},'{previous}',1,'aborted','{{}}','isolated-state-fixture')")
 delivery={key:{'report_state':'attention','state':state,'run_id':run,'text':text,'attempt':1,'automatic_attempts':7}}
 result={'result_delivery':delivery}
 if state=='completed':result['summary']={'text':'STATE_SAVED_RESULT'}
 h.sql(f"INSERT INTO bot_delegations(id,user_id,source_bot_id,source_session_id,source_root_id,target_bot_id,target_session_id,request_id,run_id,fingerprint,source_selection,state,payload,result) VALUES('{task}','{user}','{h.bot}','{source}','{source}','{target}','{child}','{task}','{run}','state-fixture','native','{state}','{{\"result_delivery_version\":1,\"target_selection\":\"native\"}}',{lit(json.dumps(result))})")
 h.sql("UPDATE bot_delegations SET payload=jsonb_set(payload,'{return_route}',(SELECT payload->'return_route' FROM bot_delegations WHERE id='"+original['task_id']+"')) WHERE id='"+task+"'")
 cases.append({'state':state,'task_id':task,'event_id':key,'previous_run':previous,'original_run':run,'target_session':child})
cfg=(old/'config.toml').read_text()
if '[bot_delegation]' in cfg:cfg=cfg.replace('[bot_delegation]','[bot_delegation]\nresult_summary_attempts=7',1)
else:cfg+='\n[bot_delegation]\nresult_summary_attempts=7\n'
(p/'config.toml').write_text(cfg);(p/'config.toml').chmod(0o600)
fixture={'database':h.cfg['database'],'source_session':source,'source_bot':h.bot,'cases':cases}
(p/'fixture.json').write_text(json.dumps(fixture,indent=2));print('Seeded isolated persisted outcomes: attempt1 attention, completed original and unknown without execution; policy7. No model calls.')
