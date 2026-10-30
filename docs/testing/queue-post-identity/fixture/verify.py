from util import *
from websockets.sync.client import connect
from urllib.parse import quote
from concurrent.futures import ThreadPoolExecutor
import time
f=json.loads((P/'run.json').read_text());C=f['contact'];prefix='/bots/'+bot+'/sessions/'+C;out={'source_commit':meta['source_commit'],'paid_calls':0,'environment':'isolated cloned original PostgreSQL and local model fixture','queue_backend':'memory live queue; admitted runs/history persist in PostgreSQL','checks':[]};sockets=[]
def checkpoint(): (P/'result.json').write_text(json.dumps(out,indent=2))
def status():return http('/status',provider=True)[1]
def inv(i):
 code,v=http(prefix+'/invocations/'+quote(i,safe=''));assert code==200;return v
def connect_ws():
 ws=connect(meta['base'].replace('http:','ws:')+'/bots/'+bot+'/web/ws?token='+quote(auth['access_token'],safe=''),open_timeout=5);sockets.append(ws);return ws
def until_ws(ws,pred):
 deadline=time.monotonic()+10
 while time.monotonic()<deadline:
  v=json.loads(ws.recv(timeout=deadline-time.monotonic()))
  if pred(v):return v
 raise AssertionError('WS observation deadline')
def release(k):assert http('/release/'+k,provider=True)[0]==200
def delegate():
 rows=json.loads(sql("SELECT coalesce(json_agg(json_build_object('id',id,'target_session_id',target_session_id,'run_id',run_id,'state',state)),'[]') FROM bot_delegations WHERE fingerprint<>'' AND request_id IN (SELECT request_id FROM bot_delegations) AND id NOT IN ("+','.join("'"+x+"'" for x in json.loads((P/'original-ids.json').read_text())['bot_delegations'])+")"))
 assert len(rows)<=1;return rows[0] if rows and rows[0]['target_session_id'] else None
try:
 ws=connect_ws();ws.send(json.dumps({'type':'message','session_id':C,'invocation_id':f['a_invocation'],'text':f['a_text'],'workspace_target_id':'native'}))
 ack=until_ws(ws,lambda x:x.get('invocation_id')==f['a_invocation'] and x.get('type') in ['run_accepted','error','run_rejected']);assert ack['type']=='run_accepted',{'type':ack['type'],'code':ack.get('code')};out['a']=inv(f['a_invocation']);checkpoint()
 wait(lambda:status()['worker']==1);d=wait(delegate);out['delegation']=d
 body={'invocation_id':f['b_invocation'],'text':f['b_text']}
 with ThreadPoolExecutor(max_workers=2) as pool:responses=list(pool.map(lambda _:http(prefix+'/follow-up-queue',body),range(2)))
 assert all(code==202 for code,v in responses),[x[0] for x in responses]
 items=[v for code,v in responses];assert items[0]['item_id']==items[1]['item_id'];item=items[0];assert item['enqueued_during_run_id']==out['a']['run_id'];out['post_request_invocation']=f['b_invocation'];out['concurrent_post_responses']=items;checkpoint()
 code,conflict=http(prefix+'/follow-up-queue',{**body,'text':body['text']+' changed'});assert code==409;out['changed_payload_status']=code
 code,q=http(prefix+'/follow-up-queue');assert code==200 and len(q['items'])==1
 bInv='follow-up:'+item['item_id'];assert inv(bInv)['found'] is False;out['queued_before_admission']=True
 ws.close();new=connect_ws();new.send(json.dumps({'type':'runtime_subscribe','session_id':C}));snap=until_ws(new,lambda x:x.get('type')=='runtime_snapshot');assert snap['snapshot']['current_run_view']['run_id']==out['a']['run_id'];out['reconnect_before_claim_run']=out['a']['run_id']
 code,retry=http(prefix+'/follow-up-queue',body);assert code==202 and retry['item_id']==item['item_id'];out['replay_after_reconnect']=retry;assert status()['b']==0
 release('main');wait(lambda:status()['b']==1);b=wait(lambda:(v if (v:=inv(bInv)).get('state')=='running' else None));out['b_running']=b
 new.close();new=connect_ws();new.send(json.dumps({'type':'runtime_subscribe','session_id':C}));snap=until_ws(new,lambda x:x.get('type')=='runtime_snapshot');assert snap['snapshot']['current_run_view']['run_id']==b['run_id'];out['reconnect_during_b_run']=b['run_id']
 code,retry=http(prefix+'/follow-up-queue',body);assert code==202 and retry['item_id']==item['item_id'];out['replay_during_b']=retry
 release('b');done=wait(lambda:(v if (v:=inv(bInv)).get('state')=='completed' else None));assert done['run_id']==b['run_id'] and done['turn_id']==b['turn_id'];out['b_completed']=done
 code,hist=http('/bots/'+bot+'/messages?session_id='+C+'&limit=100');assert code==200
 turns=[x for x in hist['items'] if x.get('turn_id')==done['turn_id']];assert turns and f['b_reply'] in json.dumps(turns);out['b_reply_persisted_in_original_C']=True
 code,v=http(prefix+'/delegations/'+d['id']);assert code==200 and v['target_run_state']=='running' and v['target_run_id']==d['run_id'];out['same_expert_running_after_b_persisted']=True
 code,replay=http(prefix+'/follow-up-queue',body);assert code==202 and replay['item_id']==item['item_id'];out['terminal_queue_replay']=replay
 assert sql("SELECT count(*) FROM session_runs WHERE session_id='"+C+"' AND invocation_id='"+bInv+"'")=='1';assert status()['b']==1
 release('worker');wait(lambda:http(prefix+'/delegations/'+d['id'])[1].get('state')=='completed');wait(lambda:status()['summary']==1)
 wait(lambda:f['summary_reply'] in json.dumps(http('/bots/'+bot+'/messages?session_id='+C+'&limit=100')[1]));out['natural_result_back_to_C']=True
 out['receipt_count']=sql("SELECT count(*) FROM bot_history_messages WHERE role='assistant' AND session_id='"+C+"' AND metadata->>'task_result_receipt' LIKE '%"+d['id']+"%'");assert out['receipt_count']=='1'
 out['provider']=status();assert out['provider']['b']==out['provider']['worker']==out['provider']['summary']==1 and out['provider']['other']==0
 originals=json.loads((P/'original-ids.json').read_text());out['original_rows_preserved']={}
 for table,key in [('bot_history_messages','id'),('session_runs','run_id'),('bot_delegations','id')]:
  ids=originals[table];n=int(sql('SELECT count(*) FROM '+table+' WHERE '+key+' IN ('+','.join("'"+x+"'" for x in ids)+')'));assert n==len(ids);out['original_rows_preserved'][table]=n
 out['checks']=['concurrent duplicate POST same202/item','different payload409','pending replay after disconnect/reconnect same item','exact follow-up:item invocation maps single durable run/turn','running and terminal replay no second execution','B persisted while same expert running','original expert result returned once to original C','all prior rows preserved']
 out['status']='PASS';checkpoint();print(json.dumps({k:out[k] for k in ['status','checks','receipt_count','original_rows_preserved']},indent=2))
except BaseException as e:
 out['status']='FAILED';out['failure_type']=type(e).__name__;out['failure']=str(e).replace(auth['access_token'],'[REDACTED]');checkpoint();print(json.dumps({'status':'FAILED','failure_type':type(e).__name__,'failure':out['failure']}));raise
finally:
 for k in ['main','worker','b']:
  try:release(k)
  except Exception:pass
 for ws in sockets:
  try:ws.close()
  except Exception:pass
