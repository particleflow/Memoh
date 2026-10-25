import os
import signal
import subprocess
import time
import tomllib
import urllib.request
from pathlib import Path

root = Path('/tmp/native-producer-unified')
cfg = Path('/tmp/task-native-observation/private-config.toml')
c = tomllib.loads(cfg.read_text())
assert c['postgres']['database'] == 'integration_web_types'
assert c['postgres']['host'] in ('127.0.0.1', 'localhost') and c['postgres']['port'] == 15432
assert c['server']['addr'] == '127.0.0.1:18400'
pid = int((root / 'server.pid').read_text())
assert Path(f'/proc/{pid}/cmdline').read_bytes().split(b'\0')[:2] == [str(root / 'server').encode(), b'serve']
assert Path(f'/proc/{pid}').stat().st_uid == os.getuid()
os.kill(pid, signal.SIGTERM)
for _ in range(100):
    status = Path(f'/proc/{pid}/status')
    if not status.exists() or 'State:\tZ' in status.read_text(): break
    time.sleep(.1)
else: raise RuntimeError('Owned synthetic Server failed to stop')
env = {**os.environ, 'CONFIG_PATH': str(cfg), 'MEMOH_CONFIG_PATH': str(cfg)}
log = (root / 'private-server.log').open('a')
proc = subprocess.Popen([str(root / 'server'), 'serve'], cwd='/tmp/memoh-candidate-7bcb-review', env=env, stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
(root / 'server.pid').write_text(str(proc.pid))
client = urllib.request.build_opener(urllib.request.ProxyHandler({}))
for _ in range(60):
    assert proc.poll() is None, 'Owned synthetic Server exited'
    try:
        with client.open(urllib.request.Request('http://127.0.0.1:18400/health', method='HEAD'), timeout=1) as response:
            if response.status == 200: break
    except OSError: pass
    time.sleep(.25)
else: raise RuntimeError('Owned synthetic Server not healthy')
print('PASS restarted only the owned loopback Server, same executable/configuration/PG schema; no migration or workload restart')
