#!/usr/bin/env python3
"""Official Herdr CLI contract probe, isolated and model-free.

Uses an independently started official Herdr server and a synthetic stdin
actor in one test-owned pane. Never reads inherited credentials or sessions.
"""
import argparse, hashlib, json, os, pathlib, shutil, subprocess, sys, tempfile, time

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--binary', type=pathlib.Path, required=True, help='Official v0.9.5 Linux x86_64 binary; SHA256 is checked')
parser.add_argument('--output-dir', type=pathlib.Path, required=True)
options = parser.parse_args()
binary = options.binary.resolve()
expected_sha = '14d9e8bc8b4a9e0118d77c32266f76d461f09b40342eb23752960bd89f59929d'
assert hashlib.sha256(binary.read_bytes()).hexdigest() == expected_sha, 'Official binary checksum mismatch'
HERE = options.output_dir.resolve()
HERE.mkdir(parents=True, exist_ok=True)
root = pathlib.Path(tempfile.mkdtemp(prefix='herdr-contract-', dir='/tmp'))
config = root / 'herdr'
session = 'contract'
endpoint = config / 'sessions' / session / 'herdr.sock'
endpoint.parent.mkdir(parents=True)
home = root / 'home'
home.mkdir()
cwd = root / 'work'
cwd.mkdir()
(config / 'config.toml').write_text('[terminal]\ndefault_shell = "/bin/sh"\n')
env = {
    'PATH': '/usr/bin:/bin', 'HOME': str(home), 'SHELL': '/bin/sh',
    'LANG': 'C.UTF-8', 'TERM': 'xterm-256color',
    'XDG_CONFIG_HOME': str(root), 'XDG_DATA_HOME': str(root / 'data'),
    'XDG_CACHE_HOME': str(root / 'cache'),
    'HERDR_CONFIG_PATH': str(config / 'config.toml'),
    'HERDR_SOCKET_PATH': str(endpoint), 'HERDR_SESSION': session,
}
observations = []
def run(*args, timeout=8):
    p = subprocess.run([str(binary), *args], env=env, cwd=cwd,
                       capture_output=True, text=True, timeout=timeout)
    r = {'argv': list(args), 'exit_code': p.returncode, 'stdout': p.stdout, 'stderr': p.stderr}
    for stream in ('stdout','stderr'):
        try: r[stream + '_json'] = json.loads(r[stream])
        except (ValueError, TypeError): pass
    observations.append(r)
    return r
def result(r):
    assert r['exit_code'] == 0, r
    return r['stdout_json']['result']
def wait_for(fn, predicate, seconds=8):
    deadline = time.monotonic()+seconds
    while time.monotonic() < deadline:
        r=fn()
        if predicate(r): return r
        time.sleep(.1)
    raise AssertionError(r)

agent = root / 'fixture-agent.py'
agent.write_text('''import os, subprocess, sys, time
binary = sys.argv[1]
pane = os.environ['HERDR_PANE_ID']
def state(value):
    subprocess.run([binary, 'pane', 'report-agent', pane, '--source', 'custom:memoh-contract', '--agent', 'fixture', '--state', value], stdout=subprocess.DEVNULL, check=True)
print('SYNTHETIC_READY', flush=True)
state('idle')
for line in sys.stdin:
    state('working')
    time.sleep(.2)
    print('SYNTHETIC_REPLY:' + line.rstrip(), flush=True)
    state('idle')
''')
missing = run('workspace','list')
assert missing['exit_code'] == 1 and missing['stderr_json']['error']['code'] == 'server_not_running'
assert not endpoint.exists(), 'Read command must not start the server'
log = open(HERE / 'server.log', 'w')
server = subprocess.Popen([str(binary), 'server'], env=env, cwd=cwd,
                          stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
status='failed'
try:
    assert run('--version')['stdout'].strip() == 'herdr 0.9.5'
    wait_for(lambda: run('workspace','list'), lambda r: r['exit_code']==0)
    created=result(run('workspace','create','--cwd',str(cwd),'--label','memoh-contract','--no-focus'))
    workspace=created['workspace']['workspace_id']
    pane=created['root_pane']['pane_id']
    run('workspace','get',workspace)
    run('pane','get',pane)
    run('pane','process-info','--pane',pane)
    assert run('pane','run',pane, f"python3 '{agent}' '{binary}'")['exit_code'] == 0
    wait_for(lambda: run('agent','get',pane), lambda r: r['exit_code']==0)
    run('agent','list')
    run('agent','get',pane)
    run('agent','explain',pane,'--json')
    rejected=run('agent','prompt',pane,'synthetic-first')
    assert rejected['exit_code']==1 and rejected['stderr_json']['error']['code']=='agent_not_ready'
    # A custom lifecycle reporter is not a native named agent. Exercise ordinary pane input.
    assert run('pane','run',pane,'synthetic-first')['exit_code']==0
    wait_for(lambda: run('pane','read',pane,'--source','recent-unwrapped','--lines','60'), lambda r:'SYNTHETIC_REPLY:synthetic-first' in r['stdout'])
    rejected=run('agent','prompt',pane,'synthetic-second','--wait','--timeout','5000')
    assert rejected['exit_code']==1 and rejected['stderr_json']['error']['code']=='agent_not_ready'
    assert run('pane','run',pane,'synthetic-second')['exit_code']==0
    wait_for(lambda: run('agent','get',pane), lambda r: r.get('stdout_json',{}).get('result',{}).get('agent',{}).get('agent_status')=='idle')
    result(run('agent','wait',pane,'--timeout','2000'))
    run('agent','read',pane,'--source','recent-unwrapped','--lines','60')
    run('pane','read','w9999:p9999','--source','recent-unwrapped','--lines','60')
    # A fresh CLI process reads the SAME pane and result; it does not resubmit.
    result(run('workspace','get',workspace))
    same=run('pane','read',pane,'--source','recent-unwrapped','--lines','60')
    assert same['exit_code'] == 0 and 'SYNTHETIC_REPLY:synthetic-first' in same['stdout']
    assert run('pane','send-keys',pane,'ctrl+c')['exit_code'] == 0
    tab=result(run('tab','create','--workspace',workspace,'--cwd',str(cwd),'--label','synthetic-tab','--env','MEMOH_CONTRACT=nonsecret','--no-focus'))
    run('tab','get',tab['tab']['tab_id'])
    run('pane','list')
    result(run('tab','close',tab['tab']['tab_id']))
    result(run('workspace','close',workspace))
    status='passed'
finally:
    try: run('server','stop')
    except Exception: pass
    try: server.wait(timeout=8)
    except subprocess.TimeoutExpired:
        server.terminate(); server.wait(timeout=5)
    log.close()
    report={'status':status, 'platform':sys.platform, 'version':'0.9.5',
            'source_sha':'2ecf19a92fc3a7fff75b6ce3655ef04f4625ec01',
            'binary_sha256':'14d9e8bc8b4a9e0118d77c32266f76d461f09b40342eb23752960bd89f59929d',
            'scope':'official Linux CLI/server; synthetic actor; no model or production credentials; not Windows acceptance',
            'observations':observations}
    (HERE/'probe-result.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
    shutil.rmtree(root)
    print(json.dumps({'status':status,'observations':len(observations),'report':str(HERE/'probe-result.json')}))
