import json, subprocess, sys, threading, uuid, urllib.request, urllib.error
from pathlib import Path
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
sys.path.insert(0, '/tmp/task-delivery-ui')
from db import sql

root = Path('/tmp/media-owner-integration')
token = Path('/tmp/web-observation-reconnect/private-token').read_text().strip()
client = urllib.request.build_opener(urllib.request.ProxyHandler({}))
def call(path, method='GET', body=None, auth=True):
    headers = {'Content-Type': 'application/json'}
    if auth: headers['Authorization'] = 'Bearer ' + token
    req = urllib.request.Request('http://127.0.0.1:18400' + path, method=method, headers=headers,
                                 data=None if body is None else json.dumps(body).encode())
    try: response = client.open(req, timeout=15)
    except urllib.error.HTTPError as error: response = error
    data = response.read()
    return response.status, json.loads(data) if data else None
def literal(value): return "'" + value.replace("'", "''") + "'"
def snapshot(table):
    return sql("SELECT coalesce(json_agg(x ORDER BY id),'[]'::json) FROM (SELECT * FROM " + table + ") x")

calls = []
class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args): pass
    def do_GET(self):
        calls.append({'method': 'GET', 'path': self.path})
        assert self.path == '/v1/models'
        assert self.headers.get('X-Magpie-Videomakers') == '1'
        assert self.headers.get('Authorization') is None
        mode = (root / 'mode').read_text().strip()
        status = 403 if mode == 'forbidden' else 200
        result = {'private': 'synthetic-do-not-display'} if status == 403 else {'data': [] if mode == 'empty' else [
            {'id': 'synthetic/video-exact', 'kind': 'video', 'modalities': {'input': ['text', 'image'], 'output': ['video']}},
            {'id': 'synthetic/audio-exact', 'modalities': {'input': ['audio'], 'output': ['text']}}]}
        data = json.dumps(result).encode()
        self.send_response(status); self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(data))); self.end_headers(); self.wfile.write(data)
    def do_POST(self):
        calls.append({'method': 'POST', 'path': self.path})
        self.send_response(405); self.end_headers()

peer = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
threading.Thread(target=peer.serve_forever, daemon=True).start()
original_operations = snapshot('computer_operations')
registration = json.loads(Path('/tmp/computer-admission/private-registration.json').read_text())
owner_path = '/users/me/runtimes/' + registration['runtime_id'] + '/agenvo-endpoint'
owner_before = call(owner_path)
assert owner_before[0] == 200
provider_id = None
foreign = str(uuid.uuid4()); team = str(uuid.uuid4())
created_foreign = False
try:
    status, provider = call('/providers', 'POST', {'name': 'Synthetic combined owner media catalog',
        'client_type': 'openai-completions', 'config': {'base_url': f'http://127.0.0.1:{peer.server_port}/v1'},
        'metadata': {'fixture': 'owner-media-integration'}})
    assert status in (200, 201), 'Failed to create isolated synthetic provider'
    provider_id = provider['id']
    sql('BEGIN; INSERT INTO teams (id,slug) VALUES (' + literal(team) + ',' + literal('synthetic-media-' + team) + '); '
        'INSERT INTO providers(id,team_id,name,client_type,config) SELECT ' + literal(foreign) + ',' + literal(team) +
        ",'Synthetic foreign media catalog','openai-completions',config FROM providers WHERE id=" + literal(provider_id) + '; COMMIT;')
    created_foreign = True
    before = {table: snapshot(table) for table in ('providers', 'models')}
    assert call('/providers/' + provider_id + '/media-catalog?protocol=magpie', auth=False)[0] == 401
    assert call('/providers/' + foreign + '/media-catalog?protocol=magpie')[0] == 404
    assert calls == []
    (root / 'private-fixture.json').write_text(json.dumps({'provider': provider_id}))
    (root / 'mode').write_text('success')
    result = subprocess.run(['node', str(root / 'browser.mjs')], cwd='/tmp/memoh-candidate-7bcb-review')
    assert result.returncode == 0, 'Browser failed; evidence retained'
    assert len(calls) == 4 and all(c['method'] == 'GET' for c in calls), calls
    assert all(snapshot(table) == old for table, old in before.items())
    assert snapshot('computer_operations') == original_operations
    assert call(owner_path) == owner_before
    print('PASS combined actual Server/PG/Web: 4 catalog GET, 0 media POST; anonymous 401 and real foreign-team 404 made no upstream request; full provider/model/operation rows unchanged; owner locator response unchanged.')
finally:
    if created_foreign:
        sql('BEGIN; DELETE FROM providers WHERE id=' + literal(foreign) + '; DELETE FROM teams WHERE id=' + literal(team) + '; COMMIT;')
    if provider_id:
        assert call('/providers/' + provider_id, 'DELETE')[0] in (200, 204)
    peer.shutdown(); peer.server_close()
    print('Removed only the two fixtures created by this script and their synthetic team; stopped temporary catalog peer. No model call, PC dispatch, credentials or production change.')
