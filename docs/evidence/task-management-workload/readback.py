import json
import sys
import urllib.error
import urllib.request
import uuid
from pathlib import Path
sys.path.insert(0, '/tmp/task-delivery-ui')
from db import sql
root = Path('/tmp/task-management-workload')
f = json.loads((root / 'private-fixture.json').read_text())
token = Path('/tmp/web-observation-reconnect/private-token').read_text().strip()
client = urllib.request.build_opener(urllib.request.ProxyHandler({}))
def get(path, auth=True):
    req = urllib.request.Request('http://127.0.0.1:18400' + path, headers={'Authorization': 'Bearer ' + token} if auth else {})
    try:
        with client.open(req, timeout=15) as r:
            assert r.headers.get('Cache-Control') == 'no-store'
            return r.status, json.load(r)
    except urllib.error.HTTPError as e:
        assert e.headers.get('Cache-Control') == 'no-store'
        return e.code, None
path = f"/bots/{f['bot']}/sessions/{f['root']}/tasks/computer_operation/{f['operation']}"
status, task = get(path)
assert status == 200 and task['status'] == 'submitted' and task['business_state'] == 'unknown'
assert task['session_id'] == f['child'] and task['parent_session_id'] == task['direct_parent_session_id'] == f['root'] and task['target_id'] == f['target']
status, content = get(path + '/content')
assert status == 200 and content['operation_id'] == f['operation']
assert [r['state'] for r in content['followups']] == ['accepted', 'unknown']
assert all(r['business_outcome'] == 'unknown' for r in content['followups'])
assert 'WORKLOAD unknown EXIT 0' in content['observation']['output']
status, observation = get(path + '/observation')
assert status == 200 and observation['target_id'] == f['target'] and observation['workspace_id'] == 'w-executable' and observation['pane_id'] == 'p-executable'
assert observation['process_state'] == observation['business_outcome'] == 'unknown'
assert observation['native']['reference_match'] == 'observed'
assert observation['native']['availability'] == 'online'
before = json.loads((root / 'calls.json').read_text())
assert get(path, False)[0] == 401
assert get(path.replace(f['root'], str(uuid.uuid4())) + '/content')[0] == 404
assert get(path.replace(f['bot'], str(uuid.uuid4())) + '/content')[0] == 404
assert json.loads((root / 'calls.json').read_text()) == before
assert before.count('agent.prompt') == 2
(root / 'private-readback.json').write_text(json.dumps({'request_id': content['followups'][1]['request_id'], 'operation': f['operation']}))
proof = dict(task_state=task['status'], business_state=task['business_state'], request_states=[x['state'] for x in content['followups']], direct_parent_preserved=True, selected_target_preserved=True, native_reference_match=observation['native']['reference_match'], native_availability=observation['native']['availability'], process_state=observation['process_state'], anonymous=401, wrong_parent=404, wrong_bot=404, unauthorized_peer_calls=0, actual_workload_starts=before.count('agent.prompt'))
(root / 'readback-proof.json').write_text(json.dumps(proof, indent=2))
print('PASS post-restart real HTTP/PG readback: original parent/target/workspace/pane + accepted/unknown receipts; sampled process exit does not complete task')
print('PASS anonymous401 + wrong parent/bot404/no-store; zero peer calls on all denied reads; workload count remains two')
