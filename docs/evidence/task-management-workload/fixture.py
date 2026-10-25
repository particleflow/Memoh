"""Isolated acceptance peer: official MCP method shapes, synthetic Herdr.

Only two exact input strings execute the bundled bounded local workload. No
caller-provided shell, model, device, credentials, filesystem paths or URLs.
"""
import hashlib
import http.server
import json
import subprocess
import sys
import threading
import time
import uuid
from pathlib import Path
from urllib.parse import urlsplit

sys.path.insert(0, '/tmp/task-delivery-ui')
from db import sql

ROOT = Path('/tmp/task-management-workload')
read_only = sys.argv[1:] == ['--read-only']
assert not sys.argv[1:] or read_only
old = json.loads(Path('/tmp/task-native-observation/private-connection.json').read_text())
base = json.loads(Path('/tmp/task-delivery-ui/private-fixture.json').read_text())
assert old['auth_type'] == 'none'
assert urlsplit(old['config']['url']).hostname in ('127.0.0.1', 'localhost')
assert sql('select current_database()') == 'integration_web_types'

def q(v):
    return "'" + str(v).replace("'", "''") + "'"

def row(table, id):
    return json.loads(sql(f'SELECT row_to_json(x) FROM {table} x WHERE id={q(id)}'))

def digest_operations(exclude=()):
    clause = 'WHERE id NOT IN (' + ','.join(q(x) for x in exclude) + ')' if exclude else ''
    return hashlib.sha256(sql(f"SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]'::jsonb)::text FROM computer_operations x {clause}").encode()).hexdigest()

assert read_only or not (ROOT / 'private-fixture.json').exists(), 'Do not seed or execute twice'
original_runtime = row('user_runtimes', old['runtime_id'])
original_connection = row('mcp_connections', old['connection_id'])
assert original_runtime['provider_config'] == old['provider_config']
assert original_connection['config'] == old['config']
assert original_runtime['revoked_at'] is None
profile = json.loads(json.dumps(old['provider_config']))
profile['agent'].pop('execution_mode', None)
target = {k: profile[k] for k in ('connection_id', 'device_id', 'instance_id', 'session', 'work_dir', 'bash_path')}
target['agent'] = {k: profile['agent'][k] for k in ('execution_mode', 'provider', 'model', 'config_dir', 'path') if profile['agent'].get(k)}
fingerprint = hashlib.sha256(json.dumps(target, separators=(',', ':')).encode()).hexdigest()
config = {**old['config'], 'url': 'http://127.0.0.1:18489/mcp'}
op = row('computer_operations', base['operation'])
f = {k: str(uuid.uuid4()) for k in ('root', 'child', 'operation')}
f.update(bot=base['bot'], title='Synthetic executable task acceptance', target=op['target_id'], runtime=old['runtime_id'])
f['before'] = digest_operations()
if read_only:
    f = json.loads((ROOT / 'private-fixture.json').read_text())
    assert digest_operations([f['operation']]) == f['before']
refs = dict(name='synthetic-executable-agent', workspace_id='w-executable', pane_id='p-executable', terminal_id='t-executable', phase='prompt_submitted', prompt_submitted=True,
            agent_session=dict(source='herdr:pi', agent='pi', kind='id', value='synthetic-persistent-session'))
details = dict(kind='herdr_pi_task', phase='prompt_submitted', agent=refs)
statements = ['BEGIN', f"SELECT set_config('memoh.team_id',{q(op['team_id'])},true)"]
statements += [f"UPDATE mcp_connections SET config={q(json.dumps(config))}::jsonb WHERE id={q(old['connection_id'])}",
               f"UPDATE user_runtimes SET provider_config={q(json.dumps(profile))}::jsonb WHERE id={q(old['runtime_id'])}"]
for key, parent, kind, visibility, title in ([] if read_only else [('root', None, 'chat', 'user', f['title']), ('child', f['root'], 'subagent', 'internal', 'Synthetic executable child')]):
    patch = dict(id=f[key], parent_session_id=parent, type=kind, session_mode=kind, visibility=visibility, title=title)
    statements.append(f"INSERT INTO bot_sessions SELECT (jsonb_populate_record(NULL::bot_sessions,to_jsonb(s)||{q(json.dumps(patch))}::jsonb)).* FROM bot_sessions s WHERE id={q(base['session'])}")
if not read_only:
    statements.append(f"INSERT INTO computer_operations(id,team_id,bot_id,session_id,user_id,target_id,run_id,fingerprint,target_fingerprint,state,details) SELECT {q(f['operation'])},team_id,bot_id,{q(f['child'])},user_id,target_id,'synthetic-executable-run',{q(f['operation'])},{q(fingerprint)},'submitted',{q(json.dumps(details))}::jsonb FROM computer_operations WHERE id={q(base['operation'])}")
statements.append('COMMIT')
sql(';'.join(statements))
if not read_only:
    (ROOT / 'private-fixture.json').write_text(json.dumps(f))
    (ROOT / 'mode.txt').write_text('accepted')
    (ROOT / 'calls.json').write_text('[]')
calls, processes = json.loads((ROOT / 'calls.json').read_text()), []
lock = threading.Lock()

class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_): pass
    def do_GET(self):
        self.send_response(405); self.end_headers()
    def do_POST(self):
        req = json.loads(self.rfile.read(int(self.headers.get('Content-Length', '0'))))
        method = req.get('method')
        if 'id' not in req:
            self.send_response(202); self.end_headers(); return
        if method == 'initialize':
            result = dict(protocolVersion='2025-03-26', capabilities={'tools': {}}, serverInfo={'name': 'synthetic-executable-peer', 'version': '1'})
        elif method == 'tools/list':
            result = {'tools': [dict(name='execute', description='Isolated acceptance fixture', inputSchema={'type': 'object', 'properties': {'code': {'type': 'string'}}, 'required': ['code']})]}
        elif method == 'ping':
            result = {}
        elif method == 'tools/call':
            assert req['params']['name'] == 'execute'
            code = req['params']['arguments']['code']
            assert code.startswith('return await call(') and code.endswith(');')
            destination, native, args = json.loads('[' + code[len('return await call('):-2] + ']')
            assert destination == {'deviceId': profile['device_id'], 'instanceId': profile['instance_id']}
            assert native in ('workspace.list', 'pane.get', 'agent.get', 'agent.read', 'agent.prompt')
            assert args['session'] == profile['session']
            if native.startswith('agent.'):
                assert args['name'] in ([refs['name'], refs['pane_id']] if native == 'agent.get' else [refs['name']])
            with lock:
                calls.append(native)
                (ROOT / 'calls.json').write_text(json.dumps(calls))
                if native == 'agent.prompt':
                    assert not read_only, 'readback never accepts business input'
                    label = {'Synthetic accepted workload': 'accepted', 'Synthetic unknown workload': 'unknown'}[args['text']]
                    assert label == (ROOT / 'mode.txt').read_text().strip()
                    # A finite, fixed local process really runs. The input is a
                    # label from this allowlist, never executable source.
                    log = (ROOT / (label + '.stdout')).open('x')
                    proc = subprocess.Popen([sys.executable, str(ROOT / 'workload.py'), label], stdout=log, stderr=subprocess.STDOUT)
                    processes.append(proc)
                    (ROOT / (label + '.pid')).write_text(str(proc.pid))
                    time.sleep(.8)
            running = any(p.poll() is None for p in processes)
            agent = dict(name=refs['name'], agent='pi', workspace_id=refs['workspace_id'], pane_id=refs['pane_id'], terminal_id=refs['terminal_id'], agent_status='working' if running else 'done', interactive_ready=True, completion_seq=len([p for p in processes if p.poll() is not None]), agent_session=refs['agent_session'])
            if native == 'agent.read':
                assert args['lines'] == 200 and args['source'] == 'recent-unwrapped'
                output = '\n'.join(p.read_text() for p in sorted(ROOT.glob('*.stdout'))) or 'Synthetic Agent ready; no workload has been executed.'
                receipt = dict(execution='accepted', result={'session': args['session'], 'output': output})
            elif native == 'agent.prompt' and (ROOT / 'mode.txt').read_text().strip() == 'unknown':
                receipt = dict(execution='unknown', error={'code': 'response_lost'})
            else:
                if native == 'workspace.list': body = {'workspaces': [dict(workspace_id=refs['workspace_id'], label='Synthetic executable workspace', pane_count=1, agent_status=agent['agent_status'])]}
                elif native == 'pane.get': body = {'pane': {k: refs[k] for k in ('pane_id', 'workspace_id', 'terminal_id')}}
                else: body = {'agent': agent, 'type': 'agent_prompted' if native == 'agent.prompt' else 'agent'}
                receipt = dict(execution='accepted', result={'id': 'cli:agent:prompt' if native == 'agent.prompt' else 'cli:agent:get', 'session': args['session'], 'result': body})
            result = {'content': [{'type': 'text', 'text': json.dumps(receipt)}]}
        else:
            self.send_response(200); self.send_header('Content-Type', 'application/json'); self.end_headers()
            self.wfile.write(json.dumps({'jsonrpc': '2.0', 'id': req['id'], 'error': {'code': -32601, 'message': 'Method not found'}}).encode())
            return
        self.send_response(200); self.send_header('Content-Type', 'application/json'); self.end_headers()
        self.wfile.write(json.dumps({'jsonrpc': '2.0', 'id': req['id'], 'result': result}).encode())

server = http.server.ThreadingHTTPServer(('127.0.0.1', 18489), Handler)
threading.Thread(target=server.serve_forever, daemon=True).start()
print('READY real isolated PG/HTTP; synthetic official-shaped peer; exact bounded local workload only', flush=True)
try:
    while not (ROOT / ('stop-readonly' if read_only else 'stop-fixture')).exists(): time.sleep(.2)
finally:
    server.shutdown()
    for proc in processes: proc.wait(timeout=65)
    sql(f"BEGIN; SELECT set_config('memoh.team_id',{q(op['team_id'])},true); UPDATE mcp_connections SET config={q(json.dumps(old['config']))}::jsonb WHERE id={q(old['connection_id'])} AND config={q(json.dumps(config))}::jsonb; UPDATE user_runtimes SET provider_config={q(json.dumps(old['provider_config']))}::jsonb,revoked_at=NULL WHERE id={q(old['runtime_id'])} AND provider_config={q(json.dumps(profile))}::jsonb; COMMIT;")
    assert row('mcp_connections', old['connection_id']) == original_connection
    assert row('user_runtimes', old['runtime_id']) == original_runtime
    assert digest_operations([f['operation']]) == f['before']
    result = row('computer_operations', f['operation'])
    proof = dict(original_rows_unchanged=True, temporary_configuration_restored=True, workload_count=len(processes), workload_exits=[p.returncode for p in processes], final_state=result['state'], final_revision=result['revision'], receipts=[x['progress']['state'] for x in result['details'].get('followups', [])], peer_methods=calls, external_calls=0)
    (ROOT / ('readonly-cleanup.json' if read_only else 'pg-proof.json')).write_text(json.dumps(proof, indent=2))
    print('PASS configuration restored; all pre-existing operation rows unchanged; new synthetic receipts retained', flush=True)
