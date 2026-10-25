import json
import sys
from pathlib import Path
sys.path.insert(0, '/tmp/task-delivery-ui')
from db import sql
f = json.loads(Path('/tmp/task-management-workload/private-fixture.json').read_text())
assert sql('select current_database()') == 'integration_web_types'
assert sys.argv[1] in ('revoke', 'restore')
value = 'now()' if sys.argv[1] == 'revoke' else 'NULL'
sql("UPDATE user_runtimes SET revoked_at=" + value + " WHERE id='" + f['runtime'] + "' AND provider='agenvo' AND provider_config->'agent'->>'execution_mode' IS NULL")
print('Changed only the owned synthetic Runtime revocation fixture: ' + sys.argv[1])
