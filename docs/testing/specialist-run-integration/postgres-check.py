import os,subprocess,tomllib
from pathlib import Path
from urllib.parse import quote
p=tomllib.loads(Path('/tmp/task-native-observation/private-config.toml').read_text())['postgres']
assert p['host'] in ('127.0.0.1','localhost') and p['database']=='integration_web_types'
dsn='postgresql://'+quote(p['user'],safe='')+':'+quote(p['password'],safe='')+'@'+p['host']+':'+str(p['port'])+'/'+p['database']+'?sslmode='+p.get('ssl_mode','disable')
env={**os.environ,'TEST_POSTGRES_DSN':dsn}
command=['go','test','-p=2','-tags=integration','-count=1','-v','./internal/db','-run','^TestBotDelegationPostgresClaimIsolationAndDescendantAssets$','-timeout=180s']
print('Verified existing isolated loopback database; test creates/cleans only its own unique temporary database.',flush=True)
print('COMMAND: '+' '.join(command),flush=True)
raise SystemExit(subprocess.run(command,env=env).returncode)
