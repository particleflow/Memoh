import json,uuid,urllib.request,subprocess
from pathlib import Path
API='http://127.0.0.1:18180'
def call(path,method='GET',body=None,token=None):
 h={'Content-Type':'application/json'}
 if token:h['Authorization']='Bearer '+token
 req=urllib.request.Request(API+path,data=json.dumps(body).encode() if body is not None else (b'' if method=='POST' else None),headers=h,method=method)
 with urllib.request.urlopen(req) as r:return json.load(r)
a=call('/auth/login','POST',dict(username='admin',password='admin123'));user=a['user_id'];token=a['access_token']
bot,empty,foreign,internal,external,operation=[str(uuid.uuid4()) for _ in range(6)]
def sql(statement):
 p=subprocess.run(['docker','exec','-i','memoh-dev-postgres','psql','-X','-q','-v','ON_ERROR_STOP=1','-U','memoh','-d','contact_lifecycle_app'],input="SET memoh.team_id='00000000-0000-0000-0000-000000000001';\n"+statement,text=True,capture_output=True)
 if p.returncode:raise Exception(p.stderr)
 return p.stdout
sql(f"INSERT INTO bots(id,owner_user_id,name,display_name) VALUES('{bot}','{user}','navigation-review-{bot[:8]}','Navigation review {bot[:8]}'),('{empty}','{user}','navigation-empty-{empty[:8]}','New contact review {empty[:8]}');")
old=call('/bots/'+bot+'/sessions','POST',dict(title='Legacy conversation A',type='chat'),token)
other=call('/bots/'+bot+'/sessions','POST',dict(title='Legacy conversation B',type='chat'),token)
outsider=str(uuid.uuid4())
sql(f'''INSERT INTO users(id,username) VALUES('{outsider}','navigation-other-{outsider[:8]}'); INSERT INTO team_members(user_id) VALUES('{outsider}'); INSERT INTO bot_sessions(id,bot_id,created_by_user_id,channel_type,type,session_mode,runtime_type,visibility,title) VALUES('{foreign}','{bot}','{outsider}','local','chat','chat','model','user','Other account private'),('{internal}','{bot}','{user}','local','chat','chat','model','internal','Delegated internal'),('{external}','{bot}','{user}','local','chat','chat','codex','user','External runtime history'); INSERT INTO computer_operations(id,bot_id,session_id,user_id,target_id,run_id,fingerprint,target_fingerprint,state,details) VALUES('{operation}','{bot}','{other['id']}','{user}','{uuid.uuid4()}','navigation-proof','proof','proof','unknown','{{"marker":"unchanged by navigation"}}');''')
entry=call('/bots/'+bot+'/sessions/contact',token=token)
assert {x['id'] for x in entry['candidates']}=={old['id'],other['id']},entry
metadata=dict(slug='navigation-review-'+bot[:8],label='Navigation review '+bot[:8],empty_slug='navigation-empty-'+empty[:8],empty_label='New contact review '+empty[:8],bot=bot,empty_bot=empty,old=old['id'],other=other['id'],foreign=foreign,internal=internal,external=external,operation=operation,user=user)
Path('/tmp/contact-navigation-fixture.json').write_text(json.dumps(metadata,indent=2))
print('Synthetic app fixtures created; two trusted candidates, excluded foreign/internal/external, preserved unknown operation. No business messages sent.')
